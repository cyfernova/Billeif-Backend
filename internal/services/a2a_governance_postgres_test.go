package services

import (
	"context"
	"encoding/json"
	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	migrationbundle "invoice-backend/migrations"
	"invoice-backend/pkg/logger"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGovernedBargainingProposalPersistsWithTenantScopeAndReplay(t *testing.T) {
	db := newAgentGovernancePostgresIntegrationDB(t)
	repo := postgresrepo.NewAgentGovernanceRepository(db)
	fixture := seedAgentGovernanceFixture(t, db, repo)
	require.NoError(t, db.Exec(`CREATE TABLE bargaining_negotiations (id UUID PRIMARY KEY, business_id UUID NOT NULL, buyer_agent_id UUID NOT NULL, seller_agent_id UUID NOT NULL)`).Error)
	up, err := migrationbundle.Embedded.ReadFile("000062_governed_bargaining.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	id := uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO bargaining_negotiations VALUES (?, ?, ?, ?)`, id, fixture.businessID, fixture.agentID, fixture.agentID).Error)
	raw := json.RawMessage(`{"action":"counteroffer","proposed_amount":1100,"reason":"Test proposal"}`)
	require.NoError(t, repo.SaveBargainingProposal(context.Background(), fixture.businessID, id, fixture.agentID, 1, raw))
	stored, err := repo.ReadBargainingProposal(context.Background(), fixture.businessID, id, fixture.agentID, 1)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(stored))
	var modelCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		var body OpenAIChatRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, 512, body.MaxTokens)
		require.Equal(t, "disabled", body.Thinking["type"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"counteroffer\",\"proposed_amount\":1050,\"reason\":\"A better offer\"}"}}]}`))
	}))
	defer server.Close()
	cfg := &config.Config{LLM: config.LLMConfig{APIURL: server.URL, APIKey: "local-test-key", Model: "deepseek-flash"}, AIGovernance: config.AIGovernanceConfig{ExecutionEnabled: true, SpendCurrency: "USD", RunTokenBudget: 10000, BusinessDailyLimitMicros: 1000000, AgentDailyLimitMicros: 250000, MaxDuration: 5 * time.Minute, ProviderFailureThreshold: 3, ProviderCooldown: time.Minute}}
	executor := NewAgentGovernanceService(AgentGovernanceServiceConfig{ExecutionEnabled: true, Repository: repo, Permissions: allowGovernancePermission{}})
	users := invoiceActorRepositoryStub{bySubject: func(string) (*models.User, error) { return &models.User{ID: fixture.userID}, nil }}
	adapter := NewA2AGovernanceAdapter(executor, users, NewLLMService(cfg.LLM, logger.New()), cfg)
	negotiation := &models.BargainingNegotiation{ID: id, BusinessID: fixture.businessID, UserID: fixture.userID, BuyerAgentID: fixture.agentID, SellerAgentID: fixture.agentID, InitialAmount: 1200, CurrentAmount: 1100, MaxRounds: 5, CreatedAt: fixture.now}
	first, err := adapter.Decide(context.Background(), negotiation, fixture.agentID, "buyer", 2)
	require.NoError(t, err)
	require.Equal(t, 1050.0, first.ProposedAmount)
	replayed, err := adapter.Decide(context.Background(), negotiation, fixture.agentID, "buyer", 2)
	require.NoError(t, err)
	require.Equal(t, first, replayed)
	require.Equal(t, int32(1), modelCalls.Load())
	require.NoError(t, repo.SetExecutionGate(context.Background(), interfaces.AgentExecutionGateCommand{ScopeKind: "business", BusinessID: fixture.businessID, ExecutionEnabled: false, ReasonCode: "test_pause", Now: time.Now()}))
	_, err = adapter.Decide(context.Background(), negotiation, fixture.agentID, "buyer", 3)
	require.ErrorIs(t, err, ErrA2AGovernanceRequired)
	require.Equal(t, int32(1), modelCalls.Load())
	require.Error(t, repo.SaveBargainingProposal(context.Background(), fixture.businessID, id, fixture.agentID, 1, raw))
	_, err = repo.ReadBargainingProposal(context.Background(), uuid.NewString(), id, fixture.agentID, 1)
	require.Error(t, err)
	require.ErrorIs(t, repo.SaveBargainingProposal(context.Background(), uuid.NewString(), id, fixture.agentID, 2, raw), interfaces.ErrAgentGovernanceNotFound)
	down, err := migrationbundle.Embedded.ReadFile("000062_governed_bargaining.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(down)).Error)
}
