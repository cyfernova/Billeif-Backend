package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"

	"github.com/google/uuid"
)

type chatAgentReader interface {
	GetAgentsByBusiness(context.Context, string, int, int) ([]*models.Agent, int64, error)
}

type GovernedChatService struct {
	executor AgentGovernanceExecutor
	agents   chatAgentReader
	users    ReportUserRepository
	config   *config.Config
}

func (s *LLMService) WithGovernedChat(executor AgentGovernanceExecutor, agents chatAgentReader, users ReportUserRepository, cfg *config.Config) *LLMService {
	s.governedChat = &GovernedChatService{executor: executor, agents: agents, users: users, config: cfg}
	return s
}

func (g *GovernedChatService) execute(ctx context.Context, businessID, subject string, messages []ChatMessage, invoke func(context.Context) (*LLMChatResult, error)) (*LLMChatResult, error) {
	if g == nil || g.executor == nil || g.agents == nil || g.users == nil || g.config == nil {
		return nil, ErrAgentGovernanceUnavailable
	}
	actorID, err := resolveDatabaseUserID(ctx, g.users, subject)
	if err != nil {
		return nil, err
	}
	var agentID string
	for page := 1; ; page++ {
		agents, total, readErr := g.agents.GetAgentsByBusiness(ctx, businessID, page, 100)
		if readErr != nil {
			return nil, readErr
		}
		for _, agent := range agents {
			if agent != nil && agent.BusinessID == businessID && agent.OwnerID == actorID && agent.IsActive {
				agentID = agent.ID
				break
			}
		}
		if agentID != "" || len(agents) == 0 || int64(page*100) >= total {
			break
		}
	}
	if agentID == "" {
		return nil, fmt.Errorf("%w: active owned agent required", ErrAgentToolDenied)
	}
	payload, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	policy := g.config.AIGovernance
	// A byte is a conservative input-token upper bound for text requests.
	tokens := int64(len(payload)) + 2048
	if tokens > policy.RunTokenBudget || policy.SpendCurrency != "USD" || g.config.LLM.Model != "deepseek-flash" {
		return nil, fmt.Errorf("%w: chat exceeds configured model budget", ErrAgentToolDenied)
	}
	digest := sha256.Sum256(payload)
	arguments, _ := json.Marshal(map[string]string{"messages_hash": hex.EncodeToString(digest[:])})
	canonical, hash, err := CanonicalAgentToolArguments(arguments)
	if err != nil {
		return nil, err
	}
	runID := uuid.NewString()
	request := GovernedToolRequest{
		RunID: runID, BusinessID: businessID, AgentID: agentID, UserID: actorID, AuthorizationUserID: subject,
		ToolKey: "chat_response", Risk: RiskReadOnly, CanonicalArguments: canonical, ArgumentsHash: hash,
		ResourceType: "chat", ResourceID: runID, IdempotencyKey: runID,
		ProviderKey: "deepseek", ModelKey: g.config.LLM.Model, ModelConfig: `{"max_tokens":2048,"thinking":"disabled"}`, PromptTemplateVersion: "chat-v1",
		TokenBudget: policy.RunTokenBudget, ExpectedCostMicros: 20000, BusinessSpendCeilingMicros: policy.BusinessDailyLimitMicros,
		AgentDailySpendLimitMicros: policy.AgentDailyLimitMicros, SpendCurrency: "USD",
		MaxSteps: 1, MaxToolCalls: 1, DeadlineAt: time.Now().Add(policy.MaxDuration),
		ProviderFailureThreshold: policy.ProviderFailureThreshold, ProviderCooldown: policy.ProviderCooldown, ProviderProbeLease: time.Minute,
	}
	var response *LLMChatResult
	_, err = g.executor.ExecuteTool(ctx, request, func(callCtx context.Context, _ string, _ json.RawMessage) (GovernedToolInvocationResult, error) {
		result := GovernedToolInvocationResult{Chargeable: true, CostMicros: 20000, InputTokens: policy.RunTokenBudget - 2048, OutputTokens: 2048, ResultType: "chat", ResultID: runID}
		var callErr error
		response, callErr = invoke(callCtx)
		// Chat text is persisted by the scoped history service, not the audit log.
		result.Output = json.RawMessage(`{"generated":true}`)
		return result, callErr
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, ErrAgentToolReplayUnavailable
	}
	return response, nil
}
