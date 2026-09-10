package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"

	"github.com/stretchr/testify/require"
)

type chatTestUsers struct{ ReportUserRepository }

const chatActorID = "00000000-0000-4000-8000-000000000001"

func (chatTestUsers) GetByCognitoID(context.Context, string) (*models.User, error) {
	return &models.User{ID: chatActorID}, nil
}

type chatTestAgents struct{ agents []*models.Agent }

func (a chatTestAgents) GetAgentsByBusiness(context.Context, string, int, int) ([]*models.Agent, int64, error) {
	return a.agents, int64(len(a.agents)), nil
}

type chatTestExecutor struct {
	request GovernedToolRequest
	denied  bool
}

func (e *chatTestExecutor) ExecuteTool(ctx context.Context, req GovernedToolRequest, call GovernedToolInvoker) (json.RawMessage, error) {
	e.request = req
	if e.denied {
		return nil, ErrAgentToolDenied
	}
	result, err := call(ctx, req.ToolKey, req.CanonicalArguments)
	return result.Output, err
}

func TestGovernedChatScopeBudgetAndDenial(t *testing.T) {
	for _, test := range []struct {
		name                       string
		denied, foreign, oversized bool
	}{
		{name: "success"}, {name: "paused execution", denied: true}, {name: "foreign owner", foreign: true}, {name: "budget exceeded", oversized: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &chatTestExecutor{denied: test.denied}
			owner := chatActorID
			if test.foreign {
				owner = "someone-else"
			}
			cfg := &config.Config{LLM: config.LLMConfig{Model: "deepseek-v4-flash"}, AIGovernance: config.AIGovernanceConfig{RunTokenBudget: 10000, SpendCurrency: "USD", MaxDuration: time.Minute}}
			if test.oversized {
				cfg.AIGovernance.RunTokenBudget = 2048
			}
			g := &GovernedChatService{executor: executor, users: chatTestUsers{}, agents: chatTestAgents{agents: []*models.Agent{{ID: "agent", OwnerID: owner, BusinessID: "business", IsActive: true}}}, config: cfg}
			calls := 0
			result, err := g.execute(context.Background(), "business", "subject", []ChatMessage{{Role: "user", Content: "private greeting"}}, func(context.Context) (*LLMChatResult, error) { calls++; return &LLMChatResult{Response: "Hello"}, nil })
			if test.denied || test.foreign || test.oversized {
				require.Error(t, err)
				require.Zero(t, calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "Hello", result.Response)
			require.Equal(t, 1, calls)
			require.Equal(t, "subject", executor.request.AuthorizationUserID)
			require.Equal(t, chatActorID, executor.request.UserID)
			require.Equal(t, "chat_response", executor.request.ToolKey)
			require.NotContains(t, string(executor.request.CanonicalArguments), "private greeting")
			require.Equal(t, int64(10000), executor.request.TokenBudget)
		})
	}
}

func TestGovernedChatMissingExecutorFailsClosed(t *testing.T) {
	var g *GovernedChatService
	_, err := g.execute(context.Background(), "business", "user", nil, nil)
	require.True(t, errors.Is(err, ErrAgentGovernanceUnavailable))
}
