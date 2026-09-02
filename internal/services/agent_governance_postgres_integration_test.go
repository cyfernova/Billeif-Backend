package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/repositories/interfaces"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	migrationbundle "invoice-backend/migrations"

	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAgentGovernancePostgresConcurrentReplayAndBudgetSettlement(t *testing.T) {
	database := newAgentGovernancePostgresIntegrationDB(t)
	repository := postgresrepo.NewAgentGovernanceRepository(database)
	fixture := seedAgentGovernanceFixture(t, database, repository)
	service := NewAgentGovernanceService(AgentGovernanceServiceConfig{
		ExecutionEnabled: true, Repository: repository, Permissions: allowGovernancePermission{}, Now: func() time.Time { return fixture.now },
	})
	request := fixture.serviceRequest(t, "concurrent-replay", 100)
	started, release := make(chan struct{}), make(chan struct{})
	var invocations atomic.Int32
	invoke := func(context.Context, string, json.RawMessage) (GovernedToolInvocationResult, error) {
		if invocations.Add(1) == 1 {
			close(started)
			<-release
		}
		return GovernedToolInvocationResult{Output: json.RawMessage(`{"items":[]}`)}, nil
	}
	type result struct {
		output json.RawMessage
		err    error
	}
	results := make(chan result, 2)
	go func() {
		output, err := service.ExecuteTool(context.Background(), request, invoke)
		results <- result{output: output, err: err}
	}()
	<-started
	go func() {
		output, err := service.ExecuteTool(context.Background(), request, invoke)
		results <- result{output: output, err: err}
	}()
	second := <-results
	close(release)
	first := <-results
	if invocations.Load() != 1 {
		t.Fatalf("transport invocations = %d, want exactly one", invocations.Load())
	}
	if first.err == nil && second.err == nil {
		t.Fatalf("duplicate executions both succeeded: %#v %#v", first, second)
	}
	if !errors.Is(first.err, ErrAgentToolReplayUnavailable) && !errors.Is(second.err, ErrAgentToolReplayUnavailable) {
		t.Fatalf("duplicate replay errors = %v, %v", first.err, second.err)
	}
	var executionCount int64
	if err := database.Table("ai_tool_executions").Where("run_id = ?", request.RunID).Count(&executionCount).Error; err != nil || executionCount != 1 {
		t.Fatalf("tool execution count = %d, error = %v", executionCount, err)
	}

	secondRequest := fixture.serviceRequest(t, "budget-second", 30)
	secondRequest.BusinessSpendCeilingMicros = 30
	secondRequest.AgentDailySpendLimitMicros = 30
	_, err := service.ExecuteTool(context.Background(), secondRequest, func(context.Context, string, json.RawMessage) (GovernedToolInvocationResult, error) {
		invocations.Add(1)
		return GovernedToolInvocationResult{Output: json.RawMessage(`{"items":[]}`)}, nil
	})
	if !errors.Is(err, ErrAgentToolDenied) {
		t.Fatalf("second charged run error = %v, want budget denial", err)
	}
	if invocations.Load() != 1 {
		t.Fatalf("budget-denied transport invocations = %d, want one total", invocations.Load())
	}
	var settled int64
	if err := database.Table("ai_budget_periods").Where("scope_kind = 'business' AND business_id = ?", fixture.businessID).Select("settled_micros").Scan(&settled).Error; err != nil || settled != 20 {
		t.Fatalf("business settled micros = %d, error = %v, want 20", settled, err)
	}
}

func TestAgentGovernancePostgresDeniedBudgetDoesNotConsumeCounters(t *testing.T) {
	database := newAgentGovernancePostgresIntegrationDB(t)
	repository := postgresrepo.NewAgentGovernanceRepository(database)
	fixture := seedAgentGovernanceFixture(t, database, repository)
	runID, requestHash := uuid.NewString(), strings.Repeat("a", 64)
	_, err := repository.StartRun(context.Background(), interfaces.StartAgentRunCommand{
		ID: runID, BusinessID: fixture.businessID, AgentID: fixture.agentID, UserID: fixture.userID,
		IdempotencyKey: "denied-counter-run", RequestHash: requestHash, ProviderKey: "sarvam", ModelKey: "model",
		ModelConfig: `{}`, PromptTemplateVersion: "v1", SpendCurrency: "INR", TokenBudget: 100,
		MaxSteps: 2, MaxToolCalls: 2, MaxRetries: 1, DeadlineAt: fixture.now.Add(time.Minute), Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.ReserveRunCapacity(context.Background(), interfaces.ReserveAgentRunCapacityCommand{
		RunID: runID, BusinessID: fixture.businessID, AgentID: fixture.agentID, UserID: fixture.userID,
		IdempotencyKey: "denied-capacity", RequestHash: requestHash, ProviderKey: "sarvam", ModelKey: "model",
		SpendCurrency: "INR", TokenAmount: 10, SpendMicros: 20, BusinessLimitMicros: 10, AgentDailyLimitMicros: 10,
		PeriodStart: fixture.now.Truncate(24 * time.Hour), PeriodEnd: fixture.now.Truncate(24 * time.Hour).Add(24 * time.Hour),
		ExpiresAt: fixture.now.Add(time.Minute), Now: fixture.now, Step: true, Retry: true,
	})
	if !errors.Is(err, interfaces.ErrAgentGovernanceBusinessSpendLimit) {
		t.Fatalf("ReserveRunCapacity() error = %v", err)
	}
	var counters struct {
		TokenReserved          int64
		StepsUsed, RetriesUsed int
	}
	if err := database.Table("ai_agent_runs").Select("token_reserved, steps_used, retries_used").Where("id = ?", runID).Scan(&counters).Error; err != nil {
		t.Fatal(err)
	}
	if counters.TokenReserved != 0 || counters.StepsUsed != 0 || counters.RetriesUsed != 0 {
		t.Fatalf("denied counters = %#v, want zero", counters)
	}
}

func TestAgentGovernancePostgresExpiredHalfOpenProbeHasOneRecoveryWinner(t *testing.T) {
	database := newAgentGovernancePostgresIntegrationDB(t)
	repository := postgresrepo.NewAgentGovernanceRepository(database)
	fixture := seedAgentGovernanceFixture(t, database, repository)
	expired := fixture.now.Add(-time.Second)
	if err := database.Exec(`UPDATE ai_provider_circuit_breakers SET state = 'half_open', opened_at = ?, retry_at = ?, probe_token = 'abandoned', probe_expires_at = ? WHERE provider_key = 'sarvam'`, fixture.now.Add(-time.Minute), fixture.now.Add(-30*time.Second), expired).Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var winners atomic.Int32
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			admission, err := repository.AdmitProvider(context.Background(), interfaces.ProviderAdmissionCommand{
				ProviderKey: "sarvam", FailureThreshold: 2, Cooldown: time.Minute, ProbeLease: 30 * time.Second, Now: fixture.now,
			})
			if err == nil && admission != nil && admission.HalfOpen {
				winners.Add(1)
			}
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	busy := 0
	for err := range errs {
		if errors.Is(err, interfaces.ErrAgentGovernanceProbeBusy) {
			busy++
		} else if err != nil {
			t.Fatalf("unexpected admission error: %v", err)
		}
	}
	if winners.Load() != 1 || busy != 1 {
		t.Fatalf("probe results winners=%d busy=%d, want 1/1", winners.Load(), busy)
	}
}

func TestAgentGovernancePostgresApprovalScopeReplayAndKillSwitch(t *testing.T) {
	database := newAgentGovernancePostgresIntegrationDB(t)
	repository := postgresrepo.NewAgentGovernanceRepository(database)
	fixture := seedAgentGovernanceFixture(t, database, repository)
	runID, requestHash := uuid.NewString(), strings.Repeat("b", 64)
	_, err := repository.StartRun(context.Background(), interfaces.StartAgentRunCommand{
		ID: runID, BusinessID: fixture.businessID, AgentID: fixture.agentID, UserID: fixture.userID,
		IdempotencyKey: "approval-run", RequestHash: requestHash, ProviderKey: "sarvam", ModelKey: "model",
		ModelConfig: `{}`, PromptTemplateVersion: "v1", SpendCurrency: "INR", TokenBudget: 100,
		MaxSteps: 2, MaxToolCalls: 2, MaxRetries: 1, DeadlineAt: fixture.now.Add(time.Minute), Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	approvalID, argumentsHash := uuid.NewString(), strings.Repeat("a", 64)
	_, err = repository.IssueApproval(context.Background(), interfaces.AgentApprovalCommand{
		ID: approvalID, BusinessID: fixture.businessID, AgentID: fixture.agentID, UserID: fixture.userID,
		ToolKey: "issue_invoice", RiskClass: string(RiskFinancialCommitment), ArgumentsHash: argumentsHash,
		SanitizedArguments: `{}`, ResourceType: "invoice", ResourceID: uuid.NewString(),
		ExecutionIdempotencyKey: "approved-effect", RequestHash: requestHash,
		IssuedAt: fixture.now.Add(-time.Second), ExpiresAt: fixture.now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	capacity := &interfaces.ReserveAgentRunCapacityCommand{
		RunID: runID, BusinessID: fixture.businessID, AgentID: fixture.agentID, UserID: fixture.userID,
		IdempotencyKey: "approved-effect:capacity", RequestHash: requestHash, ProviderKey: "sarvam", ModelKey: "model",
		SpendCurrency: "INR", TokenAmount: 10, SpendMicros: 10, BusinessLimitMicros: 100, AgentDailyLimitMicros: 100,
		PeriodStart: fixture.now.Truncate(24 * time.Hour), PeriodEnd: fixture.now.Truncate(24 * time.Hour).Add(24 * time.Hour),
		ExpiresAt: fixture.now.Add(time.Minute), Now: fixture.now, Step: true,
	}
	command := interfaces.AuthorizeAgentToolCommand{
		ID: uuid.NewString(), RunID: runID, BusinessID: fixture.businessID, AgentID: fixture.agentID, UserID: fixture.userID,
		Sequence: 1, ToolKey: "issue_invoice", RiskClass: string(RiskFinancialCommitment), ApprovalRequired: true,
		ApprovalID: approvalID, ArgumentsHash: strings.Repeat("c", 64), SanitizedArguments: `{}`,
		ResourceType: "invoice", ResourceID: "changed-resource", IdempotencyKey: "approved-effect", RequestHash: requestHash,
		Now: fixture.now, Capacity: capacity,
	}
	if _, err := repository.AuthorizeToolExecution(context.Background(), command); !errors.Is(err, interfaces.ErrAgentGovernanceApprovalInvalid) {
		t.Fatalf("changed approval arguments error = %v", err)
	}
	var consumedAt *time.Time
	if err := database.Table("ai_tool_approvals").Select("consumed_at").Where("id = ?", approvalID).Scan(&consumedAt).Error; err != nil || consumedAt != nil {
		t.Fatalf("changed approval consumed_at = %v, error = %v", consumedAt, err)
	}
	var resourceID string
	if err := database.Table("ai_tool_approvals").Select("resource_id").Where("id = ?", approvalID).Scan(&resourceID).Error; err != nil {
		t.Fatal(err)
	}
	command.ArgumentsHash, command.ResourceID = argumentsHash, resourceID
	command.ID = uuid.NewString()
	execution, err := repository.AuthorizeToolExecution(context.Background(), command)
	if err != nil || execution.Replayed {
		t.Fatalf("exact approval authorization = %#v, %v", execution, err)
	}
	replay := command
	replay.ID = uuid.NewString()
	replayed, err := repository.AuthorizeToolExecution(context.Background(), replay)
	if err != nil || !replayed.Replayed || replayed.ID != execution.ID {
		t.Fatalf("approval replay = %#v, %v", replayed, err)
	}

	wrongBusiness := uuid.NewString()
	if err := database.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, wrongBusiness).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.CheckExecution(context.Background(), interfaces.CheckAgentExecutionCommand{
		RunID: runID, BusinessID: wrongBusiness, AgentID: fixture.agentID, UserID: fixture.userID, Now: fixture.now,
	}); !errors.Is(err, interfaces.ErrAgentGovernanceNotFound) {
		t.Fatalf("cross-tenant execution check error = %v", err)
	}
	if err := repository.SetExecutionGate(context.Background(), interfaces.AgentExecutionGateCommand{
		ScopeKind: "global", ExecutionEnabled: false, ReasonCode: "kill_switch_test", Now: fixture.now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.CheckExecution(context.Background(), interfaces.CheckAgentExecutionCommand{
		RunID: runID, BusinessID: fixture.businessID, AgentID: fixture.agentID, UserID: fixture.userID, Now: fixture.now.Add(time.Second),
	}); !errors.Is(err, interfaces.ErrAgentGovernanceGateDisabled) {
		t.Fatalf("kill-switch execution check error = %v", err)
	}
}

type agentGovernancePostgresFixture struct {
	businessID, agentID, userID string
	now                         time.Time
}

func (f agentGovernancePostgresFixture) serviceRequest(t *testing.T, key string, businessLimit int64) GovernedToolRequest {
	t.Helper()
	canonical, hash, err := CanonicalAgentToolArguments(json.RawMessage(`{"limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	return GovernedToolRequest{
		RunID: uuid.NewString(), BusinessID: f.businessID, AgentID: f.agentID, UserID: f.userID,
		ToolKey: "list_invoices", CanonicalArguments: canonical, ArgumentsHash: hash, Risk: RiskReadOnly,
		IdempotencyKey: key, ProviderKey: "sarvam", ModelKey: "model", ModelConfig: `{}`, PromptTemplateVersion: "v1",
		TokenBudget: 100, ExpectedCostMicros: 20, BusinessSpendCeilingMicros: businessLimit,
		AgentDailySpendLimitMicros: businessLimit, SpendCurrency: "INR", MaxSteps: 2, MaxToolCalls: 2, MaxRetries: 1,
		DeadlineAt: f.now.Add(time.Minute), ProviderFailureThreshold: 2, ProviderCooldown: time.Minute, ProviderProbeLease: 30 * time.Second,
	}
}

func seedAgentGovernanceFixture(t *testing.T, database *gorm.DB, repository interfaces.AgentGovernanceRepository) agentGovernancePostgresFixture {
	t.Helper()
	fixture := agentGovernancePostgresFixture{businessID: uuid.NewString(), agentID: uuid.NewString(), userID: uuid.NewString(), now: time.Now().UTC()}
	if err := database.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, fixture.businessID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO users (id) VALUES (?)`, fixture.userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO agents (id, business_id) VALUES (?, ?)`, fixture.agentID, fixture.businessID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.SetExecutionGate(context.Background(), interfaces.AgentExecutionGateCommand{ScopeKind: "global", ExecutionEnabled: true, ReasonCode: "integration_test", Now: fixture.now}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func newAgentGovernancePostgresIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping AI governance PostgreSQL concurrency integration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	admin, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable PostgreSQL database: %v", err)
	}
	schema := "agent_governance_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := gorm.Open(gormpostgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := database.Exec(`
CREATE TABLE business_profiles (id UUID PRIMARY KEY);
CREATE TABLE users (id UUID PRIMARY KEY);
CREATE TABLE agents (id UUID PRIMARY KEY, business_id UUID NOT NULL REFERENCES business_profiles(id));
`).Error; err != nil {
		t.Fatalf("create governance prerequisites: %v", err)
	}
	migration, err := migrationbundle.Embedded.ReadFile("000061_ai_agent_governance.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(string(migration)).Error; err != nil {
		t.Fatalf("apply AI governance migration: %v", err)
	}
	return database
}
