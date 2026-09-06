package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"

	"github.com/google/uuid"
)

// Each round is a bounded, audited internal proposal. No purchase or external
// callback is dispatched by this adapter.
type A2AGovernanceAdapter struct {
	proposals interface {
		SaveBargainingProposal(context.Context, string, string, string, int, json.RawMessage) error
		ReadBargainingProposal(context.Context, string, string, string, int) (json.RawMessage, error)
	}
	executor *AgentGovernanceService
	users    ReportUserRepository
	llm      *LLMService
	config   *config.Config
}

func NewA2AGovernanceAdapter(executor *AgentGovernanceService, users ReportUserRepository, llm *LLMService, cfg *config.Config) *A2AGovernanceAdapter {
	adapter := &A2AGovernanceAdapter{executor: executor, users: users, llm: llm, config: cfg}
	if executor != nil {
		adapter.proposals, _ = executor.repository.(interface {
			SaveBargainingProposal(context.Context, string, string, string, int, json.RawMessage) error
			ReadBargainingProposal(context.Context, string, string, string, int) (json.RawMessage, error)
		})
	}
	return adapter
}

func (a *A2AGovernanceAdapter) Ready(ctx context.Context, businessID string) bool {
	return a != nil && a.proposals != nil && a.config != nil && a.users != nil && a.llm != nil && a.executor.ReadyForBusiness(ctx, businessID) && a.config.LLM.Model == "deepseek-v4-flash" && a.config.AIGovernance.SpendCurrency == "USD" && a.config.AIGovernance.RunTokenBudget >= 10000
}

func (a *A2AGovernanceAdapter) Decide(ctx context.Context, negotiation *models.BargainingNegotiation, agentID, role string, round int) (*LLMBargainingResponse, error) {
	if negotiation == nil || !a.Ready(ctx, negotiation.BusinessID) {
		return nil, ErrA2AGovernanceRequired
	}
	actorID, err := resolveDatabaseUserID(ctx, a.users, negotiation.UserID)
	if err != nil {
		return nil, err
	}
	arguments, err := json.Marshal(map[string]any{"negotiation_id": negotiation.ID, "agent_id": agentID, "round": round})
	if err != nil {
		return nil, err
	}
	canonical, hash, err := CanonicalAgentToolArguments(arguments)
	if err != nil {
		return nil, err
	}
	policy := a.config.AIGovernance
	deadline := negotiation.CreatedAt.Add(policy.MaxDuration)
	runID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(negotiation.ID+fmt.Sprintf(":round:%d", round))).String()
	request := GovernedToolRequest{
		RunID: runID, BusinessID: negotiation.BusinessID, AgentID: agentID, UserID: actorID, AuthorizationUserID: negotiation.UserID,
		ToolKey: "bargaining_proposal", Risk: RiskInternalDraft, CanonicalArguments: canonical, ArgumentsHash: hash,
		ResourceType: "negotiation", ResourceID: negotiation.ID, IdempotencyKey: runID,
		ProviderKey: "deepseek", ModelKey: a.config.LLM.Model, ModelConfig: `{"max_tokens":512}`, PromptTemplateVersion: "bargaining-v1",
		TokenBudget: 10000, ExpectedCostMicros: 20000, BusinessSpendCeilingMicros: policy.BusinessDailyLimitMicros, AgentDailySpendLimitMicros: policy.AgentDailyLimitMicros, SpendCurrency: "USD",
		MaxSteps: 1, MaxToolCalls: 1, MaxRetries: 0, DeadlineAt: deadline,
		ProviderFailureThreshold: policy.ProviderFailureThreshold, ProviderCooldown: policy.ProviderCooldown, ProviderProbeLease: time.Minute,
	}
	var decision *LLMBargainingResponse
	_, err = a.executor.ExecuteTool(ctx, request, func(callCtx context.Context, _ string, _ json.RawMessage) (GovernedToolInvocationResult, error) {
		prompt := fmt.Sprintf("You are the %s agent negotiating an internal non-binding price proposal in INR. Starting price %.2f; current offer %.2f; round %d of %d. Buyer aims to lower price; seller aims to retain value. Return only JSON with action (counteroffer, accept, reject), proposed_amount (positive number no higher than starting price), reason (short plain sentence). Accept must use the current offer. Never purchase, pay, call tools, or contact anyone.", role, negotiation.InitialAmount, negotiation.CurrentAmount, round, negotiation.MaxRounds)
		response, callErr := a.llm.ChatWithOptions(callCtx, []ChatMessage{{Role: "user", Content: prompt}}, LLMChatOptions{MaxTokens: 512, DisableThinking: true})
		// Reserve a conservative maximum of $0.02 per attempt, including uncertain failures.
		result := GovernedToolInvocationResult{Chargeable: true, CostMicros: 20000, InputTokens: int64(len(prompt)), OutputTokens: 512, ResultType: "negotiation", ResultID: negotiation.ID}
		if callErr != nil {
			return result, callErr
		}
		decision, callErr = parseGovernedBargainingDecision(response, negotiation.InitialAmount, negotiation.CurrentAmount)
		if callErr != nil {
			return result, callErr
		}
		result.Output, callErr = json.Marshal(decision)
		if callErr == nil {
			callErr = a.proposals.SaveBargainingProposal(callCtx, negotiation.BusinessID, negotiation.ID, agentID, round, result.Output)
		}
		return result, callErr
	})
	if err != nil {
		return nil, err
	}
	// Durable replay returns references rather than a fresh decision; never invent
	// another proposal or repeat a provider call after a completed attempt.
	if decision == nil {
		raw, readErr := a.proposals.ReadBargainingProposal(ctx, negotiation.BusinessID, negotiation.ID, agentID, round)
		if readErr != nil {
			return nil, readErr
		}
		return parseGovernedBargainingDecision(string(raw), negotiation.InitialAmount, negotiation.CurrentAmount)
	}
	return decision, nil
}

func parseGovernedBargainingDecision(raw string, initial, current float64) (*LLMBargainingResponse, error) {
	var decision LLMBargainingResponse
	if len(raw) > 8192 || json.Unmarshal([]byte(raw), &decision) != nil {
		return nil, fmt.Errorf("agent returned an invalid proposal")
	}
	if decision.Action != "counteroffer" && decision.Action != "accept" && decision.Action != "reject" {
		return nil, fmt.Errorf("agent returned an unsupported action")
	}
	if math.IsNaN(decision.ProposedAmount) || math.IsInf(decision.ProposedAmount, 0) || decision.ProposedAmount <= 0 || decision.ProposedAmount > initial || len(decision.Reason) > 1000 {
		return nil, fmt.Errorf("agent proposal exceeds negotiation limits")
	}
	if decision.Action == "accept" {
		decision.ProposedAmount = current
	}
	return &decision, nil
}
