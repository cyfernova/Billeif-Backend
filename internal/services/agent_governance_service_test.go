package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/repositories/interfaces"
)

func TestAgentToolRiskClassesAreExact(t *testing.T) {
	want := []AgentToolRiskClass{
		RiskReadOnly,
		RiskInternalDraft,
		RiskReversibleWrite,
		RiskExternalCommunication,
		RiskFinancialCommitment,
		RiskTaxOrCompliance,
		RiskCredentialOrSecurity,
		RiskIrreversibleOrLegallySignificant,
	}
	got := AgentToolRiskClasses()
	if len(got) != len(want) {
		t.Fatalf("risk class count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("risk class %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestInvoiceAgentToolsUseExistingExportPermission(t *testing.T) {
	for _, key := range []string{"list_invoices", "get_invoice"} {
		policy, ok := AgentToolPolicyFor(key)
		if !ok || policy.Permission != PermissionDocumentsExport {
			t.Fatalf("policy %q = %#v, want %q", key, policy, PermissionDocumentsExport)
		}
	}
}

func TestAgentGovernanceCannotReturnSuccessWhenDurableAuditFails(t *testing.T) {
	repository := &governanceRepositoryFake{completeToolErr: errors.New("audit unavailable")}
	service := NewAgentGovernanceService(AgentGovernanceServiceConfig{
		ExecutionEnabled: true, Repository: repository, Permissions: allowGovernancePermission{},
		Now: func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
	})
	canonical, hash, err := CanonicalAgentToolArguments(json.RawMessage(`{"limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	request := GovernedToolRequest{
		RunID: "11111111-1111-4111-8111-111111111111", BusinessID: "22222222-2222-4222-8222-222222222222",
		AgentID: "33333333-3333-4333-8333-333333333333", UserID: "44444444-4444-4444-8444-444444444444",
		ToolKey: "list_invoices", CanonicalArguments: canonical, ArgumentsHash: hash, Risk: RiskReadOnly,
		IdempotencyKey: "read-1", ProviderKey: "sarvam", ModelKey: "sarvam-m",
		ModelConfig: `{}`, PromptTemplateVersion: "voice-v1", TokenBudget: 180, ExpectedCostMicros: 20,
		BusinessSpendCeilingMicros: 1_000, AgentDailySpendLimitMicros: 500, SpendCurrency: "INR",
		MaxSteps: 2, MaxToolCalls: 4, MaxRetries: 1, DeadlineAt: time.Date(2099, 9, 2, 12, 0, 20, 0, time.UTC),
		ProviderFailureThreshold: 3, ProviderCooldown: time.Minute, ProviderProbeLease: 10 * time.Second,
	}
	output, err := service.ExecuteTool(context.Background(), request, func(context.Context, string, json.RawMessage) (GovernedToolInvocationResult, error) {
		return GovernedToolInvocationResult{Output: json.RawMessage(`{"items":[]}`)}, nil
	})
	if output != nil || !errors.Is(err, ErrAgentToolExecutionFailed) {
		t.Fatalf("ExecuteTool() = (%s, %v), want durable failure", output, err)
	}
}

func TestCanonicalAgentToolArgumentsNormalizeAndRejectDuplicateKeys(t *testing.T) {
	canonical, hash, err := CanonicalAgentToolArguments(json.RawMessage(`{ "z":2, "a":{"b":true,"a":1} }`))
	if err != nil {
		t.Fatalf("CanonicalAgentToolArguments() error = %v", err)
	}
	if string(canonical) != `{"a":{"a":1,"b":true},"z":2}` {
		t.Fatalf("canonical = %s", canonical)
	}
	if hash != "3d1bd265044a0e4df2cb03a2b25553066cf16f37e373b5ba6c425150af49787c" {
		t.Fatalf("hash = %q", hash)
	}
	if _, _, err := CanonicalAgentToolArguments(json.RawMessage(`{"id":"a","id":"b"}`)); !errors.Is(err, ErrAgentGovernanceInvalidArguments) {
		t.Fatalf("duplicate-key error = %v", err)
	}
	if _, _, err := CanonicalAgentToolArguments(json.RawMessage(`{"authorization":"Bearer sensitive"}`)); !errors.Is(err, ErrAgentGovernanceInvalidArguments) {
		t.Fatalf("sensitive-key error = %v", err)
	}
}

func TestAgentGovernanceRejectsArgumentsHashMismatchBeforeAdmission(t *testing.T) {
	repository := &governanceRepositoryFake{}
	service := NewAgentGovernanceService(AgentGovernanceServiceConfig{
		ExecutionEnabled: true, Repository: repository, Permissions: allowGovernancePermission{},
		Now: func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
	})
	request := validGovernedToolRequest(t)
	request.ArgumentsHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	invoked := false
	_, err := service.ExecuteTool(context.Background(), request, func(context.Context, string, json.RawMessage) (GovernedToolInvocationResult, error) {
		invoked = true
		return GovernedToolInvocationResult{Output: json.RawMessage(`{"items":[]}`)}, nil
	})
	if !errors.Is(err, ErrAgentGovernanceInvalidArguments) || invoked {
		t.Fatalf("ExecuteTool() = (%v, invoked=%v), want invalid arguments before invocation", err, invoked)
	}
}

func TestAgentGovernanceReplayNeverInvokesTransportTwice(t *testing.T) {
	repository := &governanceRepositoryFake{authorizeReplayed: true}
	service := NewAgentGovernanceService(AgentGovernanceServiceConfig{
		ExecutionEnabled: true, Repository: repository, Permissions: allowGovernancePermission{},
		Now: func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
	})
	invocations := 0
	_, err := service.ExecuteTool(context.Background(), validGovernedToolRequest(t), func(context.Context, string, json.RawMessage) (GovernedToolInvocationResult, error) {
		invocations++
		return GovernedToolInvocationResult{Output: json.RawMessage(`{"items":[]}`)}, nil
	})
	if !errors.Is(err, ErrAgentToolReplayUnavailable) || invocations != 0 {
		t.Fatalf("ExecuteTool() = (%v, invocations=%d), want replay failure without invocation", err, invocations)
	}
}

func TestAgentGovernancePersistsAuthoritativeUsageAndCost(t *testing.T) {
	repository := &governanceRepositoryFake{}
	service := NewAgentGovernanceService(AgentGovernanceServiceConfig{
		ExecutionEnabled: true, Repository: repository, Permissions: allowGovernancePermission{},
		Now: func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
	})
	output, err := service.ExecuteTool(context.Background(), validGovernedToolRequest(t), func(context.Context, string, json.RawMessage) (GovernedToolInvocationResult, error) {
		return GovernedToolInvocationResult{Output: json.RawMessage(`{"items":[]}`), InputTokens: 11, OutputTokens: 7, CostMicros: 18}, nil
	})
	if err != nil || string(output) != `{"items":[]}` {
		t.Fatalf("ExecuteTool() = (%s, %v)", output, err)
	}
	if got := repository.completeTool; got.InputTokens != 11 || got.OutputTokens != 7 || got.CostMicros != 18 || !got.Chargeable {
		t.Fatalf("tool usage = %#v, want authoritative metering", got)
	}
	if got := repository.completeRun; got.InputTokens != 11 || got.OutputTokens != 7 || got.CostMicros != 18 || got.Status != "completed" {
		t.Fatalf("run usage = %#v, want authoritative metering", got)
	}
}

func TestAgentGovernanceDeadlineCannotReturnFalseSuccess(t *testing.T) {
	repository := &governanceRepositoryFake{}
	now := time.Now().UTC()
	service := NewAgentGovernanceService(AgentGovernanceServiceConfig{
		ExecutionEnabled: true, Repository: repository, Permissions: allowGovernancePermission{}, Now: time.Now,
	})
	request := validGovernedToolRequest(t)
	request.DeadlineAt = now.Add(20 * time.Millisecond)
	output, err := service.ExecuteTool(context.Background(), request, func(callCtx context.Context, _ string, _ json.RawMessage) (GovernedToolInvocationResult, error) {
		deadline, bounded := callCtx.Deadline()
		if !bounded || !deadline.Equal(request.DeadlineAt) {
			return GovernedToolInvocationResult{Output: json.RawMessage(`{"unsafe":true}`)}, nil
		}
		<-callCtx.Done()
		return GovernedToolInvocationResult{Output: json.RawMessage(`{"unsafe":true}`), Chargeable: true}, nil
	})
	if output != nil || !errors.Is(err, ErrAgentToolExecutionFailed) {
		t.Fatalf("ExecuteTool() = (%s, %v), want no false success", output, err)
	}
	if repository.completeTool.Status != "timed_out" || repository.completeTool.CostMicros != request.ExpectedCostMicros {
		t.Fatalf("completion = %#v, want timed-out chargeable audit", repository.completeTool)
	}
}

func TestAgentGovernanceRechecksDurableGateAfterInvocation(t *testing.T) {
	repository := &governanceRepositoryFake{executionCheckErrors: []error{nil, interfaces.ErrAgentGovernanceGateDisabled}}
	service := NewAgentGovernanceService(AgentGovernanceServiceConfig{
		ExecutionEnabled: true, Repository: repository, Permissions: allowGovernancePermission{},
		Now: func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
	})
	invocations := 0
	output, err := service.ExecuteTool(context.Background(), validGovernedToolRequest(t), func(context.Context, string, json.RawMessage) (GovernedToolInvocationResult, error) {
		invocations++
		return GovernedToolInvocationResult{Output: json.RawMessage(`{"unsafe":true}`)}, nil
	})
	if output != nil || !errors.Is(err, ErrAgentToolExecutionFailed) || invocations != 1 {
		t.Fatalf("ExecuteTool() = (%s, %v, invocations=%d), want durable gate failure", output, err, invocations)
	}
	if repository.executionChecks != 2 || repository.completeTool.Status == "succeeded" {
		t.Fatalf("checks=%d completion=%#v, want post-invocation denial", repository.executionChecks, repository.completeTool)
	}
}

func TestGovernedVoiceToolsFailClosedAndExposeOnlyInvoiceReads(t *testing.T) {
	transport := &governanceToolTransportFake{definitions: []sarvam.ChatToolDefinition{
		{Name: "list_invoices"}, {Name: "get_invoice"}, {Name: "list_customers"}, {Name: "invented"},
	}}

	disabled := NewGovernedToolExecutor(GovernedToolExecutorConfig{}, transport)
	if definitions := disabled.Definitions(); len(definitions) != 0 {
		t.Fatalf("disabled definitions = %#v, want none", definitions)
	}
	if _, err := disabled.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`)); !errors.Is(err, ErrAgentGovernanceUnavailable) {
		t.Fatalf("disabled Execute() error = %v", err)
	}

	enabled := NewGovernedToolExecutor(GovernedToolExecutorConfig{
		ExecutionEnabled: true,
		Governance:       &governanceExecutorFake{},
		BusinessID:       "11111111-1111-4111-8111-111111111111",
		UserID:           "22222222-2222-4222-8222-222222222222",
		AgentID:          "33333333-3333-4333-8333-333333333333",
	}, transport)
	definitions := enabled.Definitions()
	if len(definitions) != 2 || definitions[0].Name != "list_invoices" || definitions[1].Name != "get_invoice" {
		t.Fatalf("enabled definitions = %#v", definitions)
	}
	if _, err := enabled.Execute(context.Background(), "invented", json.RawMessage(`{}`)); !errors.Is(err, ErrAgentToolDenied) {
		t.Fatalf("unknown tool error = %v", err)
	}
}

type governanceToolTransportFake struct {
	definitions []sarvam.ChatToolDefinition
}

func (f *governanceToolTransportFake) Definitions() []sarvam.ChatToolDefinition { return f.definitions }
func (f *governanceToolTransportFake) Execute(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"items":[]}`), nil
}

type governanceExecutorFake struct{}

func (*governanceExecutorFake) ExecuteTool(_ context.Context, request GovernedToolRequest, invoke GovernedToolInvoker) (json.RawMessage, error) {
	result, err := invoke(context.Background(), request.ToolKey, request.CanonicalArguments)
	return result.Output, err
}

type allowGovernancePermission struct{}

func (allowGovernancePermission) UserHasPermission(context.Context, string, string, string) bool {
	return true
}

type governanceRepositoryFake struct {
	interfaces.AgentGovernanceRepository
	completeToolErr      error
	authorizeReplayed    bool
	completeTool         interfaces.CompleteAgentToolCommand
	completeRun          interfaces.CompleteAgentRunCommand
	executionCheckErrors []error
	executionChecks      int
}

func validGovernedToolRequest(t *testing.T) GovernedToolRequest {
	t.Helper()
	canonical, hash, err := CanonicalAgentToolArguments(json.RawMessage(`{"limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	return GovernedToolRequest{
		RunID: "11111111-1111-4111-8111-111111111111", BusinessID: "22222222-2222-4222-8222-222222222222",
		AgentID: "33333333-3333-4333-8333-333333333333", UserID: "44444444-4444-4444-8444-444444444444",
		ToolKey: "list_invoices", CanonicalArguments: canonical, ArgumentsHash: hash, Risk: RiskReadOnly,
		IdempotencyKey: "read-1", ProviderKey: "sarvam", ModelKey: "sarvam-m",
		ModelConfig: `{}`, PromptTemplateVersion: "voice-v1", TokenBudget: 180, ExpectedCostMicros: 20,
		BusinessSpendCeilingMicros: 1_000, AgentDailySpendLimitMicros: 500, SpendCurrency: "INR",
		MaxSteps: 2, MaxToolCalls: 4, MaxRetries: 1, DeadlineAt: time.Date(2099, 9, 2, 12, 0, 20, 0, time.UTC),
		ProviderFailureThreshold: 3, ProviderCooldown: time.Minute, ProviderProbeLease: 10 * time.Second,
	}
}

func (*governanceRepositoryFake) StartRun(context.Context, interfaces.StartAgentRunCommand) (*interfaces.AgentRunResult, error) {
	return &interfaces.AgentRunResult{ID: "11111111-1111-4111-8111-111111111111", Status: "running"}, nil
}

func (f *governanceRepositoryFake) AuthorizeToolExecution(context.Context, interfaces.AuthorizeAgentToolCommand) (*interfaces.AgentToolExecutionResult, error) {
	return &interfaces.AgentToolExecutionResult{ID: "55555555-5555-4555-8555-555555555555", Status: "authorized", SpendReservationID: "66666666-6666-4666-8666-666666666666", Replayed: f.authorizeReplayed}, nil
}

func (*governanceRepositoryFake) AdmitProvider(context.Context, interfaces.ProviderAdmissionCommand) (*interfaces.ProviderAdmission, error) {
	return &interfaces.ProviderAdmission{}, nil
}

func (*governanceRepositoryFake) RecordProviderOutcome(context.Context, interfaces.ProviderOutcomeCommand) error {
	return nil
}

func (f *governanceRepositoryFake) CompleteToolExecution(_ context.Context, command interfaces.CompleteAgentToolCommand) (*interfaces.AgentToolExecutionResult, error) {
	f.completeTool = command
	return &interfaces.AgentToolExecutionResult{ID: "55555555-5555-4555-8555-555555555555", Status: command.Status}, f.completeToolErr
}

func (f *governanceRepositoryFake) CompleteRun(_ context.Context, command interfaces.CompleteAgentRunCommand) (*interfaces.AgentRunResult, error) {
	f.completeRun = command
	return &interfaces.AgentRunResult{ID: "11111111-1111-4111-8111-111111111111", Status: command.Status, FinalDisposition: command.FinalDisposition}, nil
}

func (f *governanceRepositoryFake) CheckExecution(context.Context, interfaces.CheckAgentExecutionCommand) error {
	index := f.executionChecks
	f.executionChecks++
	if index < len(f.executionCheckErrors) {
		return f.executionCheckErrors[index]
	}
	return nil
}
