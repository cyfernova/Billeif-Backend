package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLLMBargainingOperationsRejectUnavailableNegotiationsBeforeCallingLLM(t *testing.T) {
	svc, negotiationID, llmCalls := newBargainingLLMScopeService(t)
	ctx := context.Background()
	foreignScope := BargainingActorScope{UserID: "foreign-user", BusinessID: "foreign-business"}

	_, foreignDecisionErr := svc.GetLLMBargainingDecision(ctx, foreignScope, "buyer-1", "buyer", negotiationID)
	if !errors.Is(foreignDecisionErr, ErrNegotiationNotFound) {
		t.Fatalf("foreign decision error = %v, want negotiation not found", foreignDecisionErr)
	}
	_, absentDecisionErr := svc.GetLLMBargainingDecision(ctx, foreignScope, "buyer-1", "buyer", "absent-negotiation")
	if !errors.Is(absentDecisionErr, ErrNegotiationNotFound) {
		t.Fatalf("absent decision error = %v, want negotiation not found", absentDecisionErr)
	}
	if foreignDecisionErr.Error() != absentDecisionErr.Error() {
		t.Fatalf("foreign and absent decision errors differ: %q != %q", foreignDecisionErr, absentDecisionErr)
	}

	_, foreignSummaryErr := svc.GetLLMNegotiationSummary(ctx, foreignScope, negotiationID)
	if !errors.Is(foreignSummaryErr, ErrNegotiationNotFound) {
		t.Fatalf("foreign summary error = %v, want negotiation not found", foreignSummaryErr)
	}
	_, absentSummaryErr := svc.GetLLMNegotiationSummary(ctx, foreignScope, "absent-negotiation")
	if !errors.Is(absentSummaryErr, ErrNegotiationNotFound) {
		t.Fatalf("absent summary error = %v, want negotiation not found", absentSummaryErr)
	}
	if foreignSummaryErr.Error() != absentSummaryErr.Error() {
		t.Fatalf("foreign and absent summary errors differ: %q != %q", foreignSummaryErr, absentSummaryErr)
	}

	if got := llmCalls.Load(); got != 0 {
		t.Fatalf("LLM calls after unauthorized or absent requests = %d, want 0", got)
	}
}

func TestLLMBargainingOperationsAllowNegotiationUserAndActiveAgentBusiness(t *testing.T) {
	svc, negotiationID, llmCalls := newBargainingLLMScopeService(t)
	ctx := context.Background()

	if _, err := svc.GetLLMBargainingDecision(ctx, BargainingActorScope{UserID: "negotiation-user", BusinessID: "foreign-business"}, "buyer-1", "buyer", negotiationID); err != nil {
		t.Fatalf("decision for negotiation user: %v", err)
	}
	if _, err := svc.GetLLMNegotiationSummary(ctx, BargainingActorScope{UserID: "foreign-user", BusinessID: "buyer-business"}, negotiationID); err != nil {
		t.Fatalf("summary for active agent business: %v", err)
	}

	if got := llmCalls.Load(); got != 2 {
		t.Fatalf("LLM calls after authorized requests = %d, want 2", got)
	}
}

func TestLLMBargainingDecisionRejectsBusinessActorRequestingOppositeParticipant(t *testing.T) {
	svc, negotiationID, llmCalls := newBargainingLLMScopeService(t)

	_, err := svc.GetLLMBargainingDecision(
		context.Background(),
		BargainingActorScope{UserID: "foreign-user", BusinessID: "buyer-business"},
		"seller-1",
		"seller",
		negotiationID,
	)
	if !errors.Is(err, ErrNegotiationNotFound) {
		t.Fatalf("opposite participant decision error = %v, want negotiation not found", err)
	}
	if got := llmCalls.Load(); got != 0 {
		t.Fatalf("LLM calls after opposite participant request = %d, want 0", got)
	}
}

func TestLLMBargainingDecisionRejectsMismatchedAgentRoleBeforeCallingLLM(t *testing.T) {
	svc, negotiationID, llmCalls := newBargainingLLMScopeService(t)

	_, err := svc.GetLLMBargainingDecision(
		context.Background(),
		BargainingActorScope{UserID: "foreign-user", BusinessID: "buyer-business"},
		"buyer-1",
		"seller",
		negotiationID,
	)
	if !errors.Is(err, ErrInvalidBargainingAgentRole) {
		t.Fatalf("mismatched agent role error = %v, want invalid bargaining agent role", err)
	}
	if got := llmCalls.Load(); got != 0 {
		t.Fatalf("LLM calls after mismatched agent role = %d, want 0", got)
	}
}

func newBargainingLLMScopeService(t *testing.T) (*BargainingService, string, *atomic.Int32) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE agents (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			deleted_at DATETIME
		);
		CREATE TABLE bargaining_negotiations (
			id TEXT PRIMARY KEY,
			buyer_agent_id TEXT NOT NULL,
			seller_agent_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			initial_amount REAL NOT NULL,
			current_amount REAL NOT NULL,
			buyer_volatility REAL NOT NULL DEFAULT 0,
			seller_volatility REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			rounds INTEGER NOT NULL DEFAULT 0,
			max_rounds INTEGER NOT NULL,
			expires_at DATETIME NOT NULL,
			metadata TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create bargaining tables: %v", err)
	}
	if err := db.Exec("INSERT INTO agents (id, owner_id, business_id, name, type) VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)",
		"buyer-1", "buyer-owner", "buyer-business", "Buyer", "shopping",
		"seller-1", "seller-owner", "seller-business", "Seller", "merchant",
	).Error; err != nil {
		t.Fatalf("seed agents: %v", err)
	}
	const negotiationID = "negotiation-1"
	if err := db.Exec("INSERT INTO bargaining_negotiations (id, buyer_agent_id, seller_agent_id, user_id, initial_amount, current_amount, status, max_rounds, expires_at, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		negotiationID, "buyer-1", "seller-1", "negotiation-user", 100, 100, "initiated", 5, time.Now().Add(time.Hour), "{}",
	).Error; err != nil {
		t.Fatalf("seed negotiation: %v", err)
	}

	var llmCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"accept\",\"proposed_amount\":100,\"reason\":\"done\",\"confidence\":1}"}}],"model":"test-llm-model"}`))
	}))
	t.Cleanup(server.Close)

	return &BargainingService{
		ap2Repo: postgres.NewAP2Repository(db),
		llm: NewLLMService(config.LLMConfig{
			APIKey:  "test-key",
			APIURL:  server.URL,
			Model:   "test-llm-model",
			Timeout: 5,
		}, logger.NewWithEnv("test")),
		log: logger.NewWithEnv("test"),
	}, negotiationID, &llmCalls
}
