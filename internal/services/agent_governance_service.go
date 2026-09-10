package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
)

type AgentToolRiskClass string

const (
	RiskReadOnly                         AgentToolRiskClass = "read-only"
	RiskInternalDraft                    AgentToolRiskClass = "internal draft"
	RiskReversibleWrite                  AgentToolRiskClass = "reversible write"
	RiskExternalCommunication            AgentToolRiskClass = "external communication"
	RiskFinancialCommitment              AgentToolRiskClass = "financial commitment"
	RiskTaxOrCompliance                  AgentToolRiskClass = "tax or compliance"
	RiskCredentialOrSecurity             AgentToolRiskClass = "credential or security"
	RiskIrreversibleOrLegallySignificant AgentToolRiskClass = "irreversible or legally significant"
)

var (
	ErrAgentGovernanceUnavailable            = errors.New("agent governance unavailable")
	ErrAgentGovernanceInvalidArguments       = errors.New("agent tool arguments invalid")
	ErrAgentToolDenied                       = errors.New("agent tool denied")
	ErrAgentToolReplayUnavailable            = errors.New("agent tool replay output unavailable")
	ErrAgentToolExecutionInProgress          = errors.New("agent tool execution in progress")
	ErrAgentToolReplayCancelled              = errors.New("agent tool replay cancelled")
	ErrAgentToolReplayTimedOut               = errors.New("agent tool replay timed out")
	ErrAgentToolReplayReconciliationRequired = errors.New("agent tool replay requires reconciliation")
	ErrAgentToolExecutionFailed              = errors.New("agent tool execution failed")
)

func AgentToolRiskClasses() []AgentToolRiskClass {
	return []AgentToolRiskClass{
		RiskReadOnly, RiskInternalDraft, RiskReversibleWrite, RiskExternalCommunication,
		RiskFinancialCommitment, RiskTaxOrCompliance, RiskCredentialOrSecurity,
		RiskIrreversibleOrLegallySignificant,
	}
}

func (value AgentToolRiskClass) Valid() bool {
	for _, candidate := range AgentToolRiskClasses() {
		if value == candidate {
			return true
		}
	}
	return false
}

func (value AgentToolRiskClass) ApprovalRequired() bool {
	switch value {
	case RiskExternalCommunication, RiskFinancialCommitment, RiskTaxOrCompliance,
		RiskCredentialOrSecurity, RiskIrreversibleOrLegallySignificant:
		return true
	default:
		return false
	}
}

type AgentToolPolicy struct {
	Key, Permission string
	Risk            AgentToolRiskClass
	Enabled         bool
	HighRiskAdapter bool
}

var codeOwnedAgentToolCatalog = map[string]AgentToolPolicy{
	"chat_response":       {Key: "chat_response", Permission: PermissionAgentsManage, Risk: RiskReadOnly, Enabled: true},
	"list_invoices":       {Key: "list_invoices", Permission: PermissionDocumentsExport, Risk: RiskReadOnly, Enabled: true},
	"get_invoice":         {Key: "get_invoice", Permission: PermissionDocumentsExport, Risk: RiskReadOnly, Enabled: true},
	"list_customers":      {Key: "list_customers", Permission: "customers.view", Risk: RiskReadOnly, Enabled: false},
	"get_customer":        {Key: "get_customer", Permission: "customers.view", Risk: RiskReadOnly, Enabled: false},
	"bargaining_proposal": {Key: "bargaining_proposal", Permission: PermissionAgentsManage, Risk: RiskInternalDraft, Enabled: true},
}

func AgentToolCatalog() []AgentToolPolicy {
	keys := make([]string, 0, len(codeOwnedAgentToolCatalog))
	for key := range codeOwnedAgentToolCatalog {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]AgentToolPolicy, 0, len(keys))
	for _, key := range keys {
		result = append(result, codeOwnedAgentToolCatalog[key])
	}
	return result
}

func AgentToolPolicyFor(key string) (AgentToolPolicy, bool) {
	policy, ok := codeOwnedAgentToolCatalog[strings.TrimSpace(key)]
	return policy, ok && policy.Key != "" && policy.Permission != "" && policy.Risk.Valid()
}

const maxGovernedArgumentsBytes = 4 << 10

func CanonicalAgentToolArguments(raw json.RawMessage) (json.RawMessage, string, error) {
	if len(raw) == 0 || len(raw) > maxGovernedArgumentsBytes || !utf8.Valid(raw) {
		return nil, "", ErrAgentGovernanceInvalidArguments
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeUniqueJSONValue(decoder)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrAgentGovernanceInvalidArguments, err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, "", ErrAgentGovernanceInvalidArguments
	}
	if containsSensitiveAgentArgument(value) {
		return nil, "", ErrAgentGovernanceInvalidArguments
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return nil, "", ErrAgentGovernanceInvalidArguments
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) > maxGovernedArgumentsBytes {
		return nil, "", ErrAgentGovernanceInvalidArguments
	}
	digest := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(digest[:]), nil
}

func containsSensitiveAgentArgument(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
			for _, forbidden := range []string{"authorization", "access_token", "api_key", "secret", "password", "credential", "bearer"} {
				if strings.Contains(normalized, forbidden) {
					return true
				}
			}
			if containsSensitiveAgentArgument(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveAgentArgument(child) {
				return true
			}
		}
	}
	return false
}

func decodeUniqueJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, delimited := token.(json.Delim)
	if !delimited {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("object key is not a string")
			}
			if _, duplicate := object[key]; duplicate {
				return nil, errors.New("duplicate object key")
			}
			value, err := decodeUniqueJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return nil, errors.New("unterminated object")
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeUniqueJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return nil, errors.New("unterminated array")
		}
		return array, nil
	default:
		return nil, errors.New("invalid delimiter")
	}
}

type GovernedToolRequest struct {
	AuthorizationUserID                         string
	RunID, BusinessID, AgentID, UserID, ToolKey string
	CanonicalArguments                          json.RawMessage
	ArgumentsHash                               string
	ResourceType, ResourceID                    string
	ApprovalID, IdempotencyKey                  string
	Risk                                        AgentToolRiskClass
	ProviderKey, ModelKey, ModelConfig          string
	PromptTemplateVersion                       string
	TokenBudget, ExpectedCostMicros             int64
	BusinessSpendCeilingMicros                  int64
	AgentDailySpendLimitMicros                  int64
	SpendCurrency                               string
	MaxSteps, MaxToolCalls, MaxRetries          int
	DeadlineAt                                  time.Time
	ProviderFailureThreshold                    int
	ProviderCooldown, ProviderProbeLease        time.Duration
}

type GovernedToolInvocationResult struct {
	Output                    json.RawMessage
	InputTokens, OutputTokens int64
	CostMicros                int64
	Chargeable                bool
	EffectDisposition         string
	ResultType, ResultID      string
}

type GovernedToolInvoker func(context.Context, string, json.RawMessage) (GovernedToolInvocationResult, error)

type AgentGovernanceExecutor interface {
	ExecuteTool(context.Context, GovernedToolRequest, GovernedToolInvoker) (json.RawMessage, error)
}

type AgentGovernanceServiceConfig struct {
	ExecutionEnabled       bool
	Repository             interfaces.AgentGovernanceRepository
	Permissions            PermissionChecker
	Now                    func() time.Time
	ExecutionCheckInterval time.Duration
}

type AgentGovernanceService struct {
	executionEnabled       bool
	repository             interfaces.AgentGovernanceRepository
	permissions            PermissionChecker
	now                    func() time.Time
	executionCheckInterval time.Duration
}

func NewAgentGovernanceService(config AgentGovernanceServiceConfig) *AgentGovernanceService {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ExecutionCheckInterval <= 0 {
		config.ExecutionCheckInterval = 100 * time.Millisecond
	}
	return &AgentGovernanceService{
		executionEnabled: config.ExecutionEnabled, repository: config.Repository, permissions: config.Permissions,
		now: config.Now, executionCheckInterval: config.ExecutionCheckInterval,
	}
}

func (service *AgentGovernanceService) Ready() bool {
	return service != nil && service.executionEnabled && !nilGovernanceInterface(service.repository) && !nilGovernanceInterface(service.permissions)
}

func (service *AgentGovernanceService) ReadyForBusiness(ctx context.Context, businessID string) bool {
	if !service.Ready() || ctx == nil || businessID == "" {
		return false
	}
	ready, err := service.repository.ExecutionReady(ctx, businessID)
	return err == nil && ready
}

func (service *AgentGovernanceService) ExecuteTool(ctx context.Context, request GovernedToolRequest, invoke GovernedToolInvoker) (json.RawMessage, error) {
	if !service.Ready() || ctx == nil || invoke == nil {
		return nil, ErrAgentGovernanceUnavailable
	}
	policy, ok := AgentToolPolicyFor(request.ToolKey)
	if !ok || !policy.Enabled || policy.Risk != request.Risk || (policy.Risk.ApprovalRequired() && !policy.HighRiskAdapter) {
		return nil, ErrAgentToolDenied
	}
	canonicalArguments, argumentsHash, err := CanonicalAgentToolArguments(request.CanonicalArguments)
	if err != nil || request.ArgumentsHash != argumentsHash {
		return nil, ErrAgentGovernanceInvalidArguments
	}
	request.CanonicalArguments, request.ArgumentsHash = canonicalArguments, argumentsHash
	permissionActor := request.UserID
	if request.AuthorizationUserID != "" {
		permissionActor = request.AuthorizationUserID
	}
	if request.BusinessID == "" || request.UserID == "" || request.AgentID == "" || request.RunID == "" ||
		request.ArgumentsHash == "" || request.IdempotencyKey == "" || request.DeadlineAt.IsZero() ||
		request.ProviderKey == "" || request.ModelKey == "" || request.ExpectedCostMicros <= 0 ||
		request.BusinessSpendCeilingMicros <= 0 || request.AgentDailySpendLimitMicros <= 0 ||
		request.ProviderFailureThreshold <= 0 || request.ProviderCooldown <= 0 || request.ProviderProbeLease <= 0 ||
		!service.permissions.UserHasPermission(ctx, permissionActor, request.BusinessID, policy.Permission) {
		return nil, ErrAgentToolDenied
	}
	now := service.now().UTC()
	modelConfig := safeJSONObject(request.ModelConfig)
	templateVersion := safeCode(request.PromptTemplateVersion)
	requestHash := hashGovernanceFields(request.BusinessID, request.AgentID, request.UserID, request.ToolKey,
		request.ArgumentsHash, request.ResourceType, request.ResourceID, request.IdempotencyKey,
		request.ProviderKey, request.ModelKey, modelConfig, templateVersion)
	replayed, found, err := service.repository.FindToolExecutionReplay(ctx, interfaces.FindAgentToolExecutionReplayQuery{
		RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID, UserID: request.UserID,
		RunIdempotencyKey: request.IdempotencyKey + ":run", ToolKey: request.ToolKey,
		ToolIdempotencyKey: request.IdempotencyKey, RequestHash: requestHash, ArgumentsHash: request.ArgumentsHash,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: lookup replay", ErrAgentToolDenied)
	}
	if found {
		if replayed == nil {
			return nil, ErrAgentToolReplayUnavailable
		}
		return replayedAgentToolResult(replayed)
	}
	if !request.DeadlineAt.After(now) {
		return nil, interfaces.ErrAgentGovernanceRunExpired
	}
	_, err = service.repository.StartRun(ctx, interfaces.StartAgentRunCommand{
		ID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID, UserID: request.UserID,
		IdempotencyKey: request.IdempotencyKey + ":run", RequestHash: requestHash,
		ProviderKey: request.ProviderKey, ModelKey: request.ModelKey, ModelConfig: modelConfig,
		SpendCurrency:         request.SpendCurrency,
		PromptTemplateVersion: templateVersion, TokenBudget: request.TokenBudget,
		MaxSteps: request.MaxSteps, MaxToolCalls: request.MaxToolCalls, MaxRetries: request.MaxRetries,
		DeadlineAt: request.DeadlineAt, Now: now,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: start run", ErrAgentToolDenied)
	}
	periodStart := now.Truncate(24 * time.Hour)
	capacity := interfaces.ReserveAgentRunCapacityCommand{
		RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID, UserID: request.UserID,
		IdempotencyKey: request.IdempotencyKey + ":capacity", RequestHash: requestHash,
		ProviderKey: request.ProviderKey, ModelKey: request.ModelKey, SpendCurrency: request.SpendCurrency,
		TokenAmount: request.TokenBudget, SpendMicros: request.ExpectedCostMicros,
		BusinessLimitMicros: request.BusinessSpendCeilingMicros, AgentDailyLimitMicros: request.AgentDailySpendLimitMicros,
		PeriodStart: periodStart, PeriodEnd: periodStart.Add(24 * time.Hour), ExpiresAt: request.DeadlineAt, Now: now, Step: true,
	}
	executionID := uuid.NewString()
	execution, err := service.repository.AuthorizeToolExecution(ctx, interfaces.AuthorizeAgentToolCommand{
		ID: executionID, RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID, UserID: request.UserID,
		Sequence: 1, ToolKey: request.ToolKey, RiskClass: string(policy.Risk), ApprovalRequired: policy.Risk.ApprovalRequired(),
		ApprovalID: request.ApprovalID, ArgumentsHash: request.ArgumentsHash, SanitizedArguments: string(request.CanonicalArguments),
		ResourceType: safeCode(request.ResourceType), ResourceID: safeCode(request.ResourceID),
		IdempotencyKey: request.IdempotencyKey, RequestHash: requestHash, Now: now, Capacity: &capacity,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: authorize tool: %w", ErrAgentToolDenied, err)
	}
	if execution.Replayed {
		durable, readErr := service.repository.ReadToolExecution(ctx, interfaces.ReadAgentToolExecutionQuery{
			ExecutionID: execution.ID, RunID: request.RunID, BusinessID: request.BusinessID,
			AgentID: request.AgentID, UserID: request.UserID,
		})
		if readErr != nil || durable == nil {
			return nil, ErrAgentToolReplayUnavailable
		}
		return replayedAgentToolResult(durable)
	}
	check := interfaces.CheckAgentExecutionCommand{
		RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID,
		UserID: request.UserID, Now: service.now().UTC(),
	}
	if err := service.repository.CheckExecution(ctx, check); err != nil {
		persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancelPersist()
		status := governanceControlFailureStatus(err)
		completedTool, completeErr := service.repository.CompleteToolExecution(persistCtx, interfaces.CompleteAgentToolCommand{
			ExecutionID: execution.ID, RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID,
			UserID: request.UserID, RequestHash: requestHash, Status: status, EffectDisposition: "none",
			FailureCode: "governance_pre_dispatch_denied", SpendReservationID: execution.SpendReservationID, Now: service.now().UTC(),
		})
		if completeErr != nil || completedTool == nil || completedTool.Status != status {
			return nil, fmt.Errorf("%w: persist pre-dispatch tool denial", ErrAgentToolExecutionFailed)
		}
		runStatus := terminalRunStatus(status)
		completedRun, completeErr := service.repository.CompleteRun(persistCtx, interfaces.CompleteAgentRunCommand{
			RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID, UserID: request.UserID,
			RequestHash: requestHash, Status: runStatus, FailureCode: "governance_pre_dispatch_denied",
			FinalDisposition: runStatus, Now: service.now().UTC(),
		})
		if completeErr != nil || completedRun == nil || completedRun.Status != runStatus {
			return nil, fmt.Errorf("%w: persist pre-dispatch run denial", ErrAgentToolExecutionFailed)
		}
		return nil, fmt.Errorf("%w: pre-dispatch check", ErrAgentToolExecutionFailed)
	}
	admission, err := service.repository.AdmitProvider(ctx, interfaces.ProviderAdmissionCommand{
		ProviderKey: request.ProviderKey, FailureThreshold: request.ProviderFailureThreshold,
		Cooldown: request.ProviderCooldown, ProbeLease: request.ProviderProbeLease, Now: now,
	})
	if err != nil {
		_, _ = service.repository.CompleteToolExecution(ctx, interfaces.CompleteAgentToolCommand{
			ExecutionID: execution.ID, RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID,
			UserID: request.UserID, RequestHash: requestHash, Status: "failed", EffectDisposition: "none",
			FailureCode: "provider_circuit_open", SpendReservationID: execution.SpendReservationID, Now: service.now().UTC(),
		})
		_, _ = service.repository.CompleteRun(ctx, interfaces.CompleteAgentRunCommand{
			RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID, UserID: request.UserID,
			RequestHash: requestHash, Status: "blocked", FailureCode: "provider_circuit_open",
			FinalDisposition: "blocked", Now: service.now().UTC(),
		})
		return nil, fmt.Errorf("%w: provider admission", ErrAgentToolExecutionFailed)
	}
	executionCtx, cancelExecution := context.WithDeadline(ctx, request.DeadlineAt)
	stopWatcher := service.watchExecution(executionCtx, check, cancelExecution)
	invocation, invokeErr := invoke(executionCtx, request.ToolKey, request.CanonicalArguments)
	watcherErr := stopWatcher()
	executionCtxErr := executionCtx.Err()
	cancelExecution()
	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancelPersist()
	check.Now = service.now().UTC()
	controlErr := service.repository.CheckExecution(persistCtx, check)
	if controlErr == nil {
		controlErr = watcherErr
	}
	outcomeErr := service.repository.RecordProviderOutcome(persistCtx, interfaces.ProviderOutcomeCommand{
		ProviderKey: request.ProviderKey, ProbeToken: admission.ProbeToken, Success: invokeErr == nil && executionCtxErr == nil,
		FailureCode: "tool_transport_failed", FailureThreshold: request.ProviderFailureThreshold,
		Cooldown: request.ProviderCooldown, Now: service.now().UTC(),
	})
	completion := interfaces.CompleteAgentToolCommand{
		ExecutionID: execution.ID, RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID, UserID: request.UserID,
		RequestHash: requestHash, Status: "succeeded", EffectDisposition: "none", SpendReservationID: execution.SpendReservationID, Now: service.now().UTC(),
		InputTokens: invocation.InputTokens, OutputTokens: invocation.OutputTokens, CostMicros: invocation.CostMicros,
		Chargeable: invokeErr == nil || invocation.Chargeable,
	}
	if completion.Chargeable && completion.CostMicros == 0 {
		completion.CostMicros = request.ExpectedCostMicros
	}
	if invocation.EffectDisposition != "" {
		completion.EffectDisposition = safeCode(invocation.EffectDisposition)
	}
	completion.ResultType, completion.ResultID = safeReference(invocation.ResultType), safeReference(invocation.ResultID)
	if invokeErr != nil {
		completion.Status = "failed"
		completion.FailureCode = "tool_transport_failed"
	}
	if executionCtxErr != nil {
		completion.Status = "timed_out"
		if errors.Is(executionCtxErr, context.Canceled) {
			completion.Status = "cancelled"
		}
		if policy.Risk != RiskReadOnly {
			completion.Status = "reconciliation_required"
			completion.EffectDisposition = "unknown"
		}
	}
	if controlErr != nil {
		completion.Status = governanceControlFailureStatus(controlErr)
		completion.FailureCode = "governance_state_changed"
		if policy.Risk != RiskReadOnly {
			completion.Status = "reconciliation_required"
			completion.EffectDisposition = "unknown"
		}
	}
	if outcomeErr != nil && completion.EffectDisposition != "unknown" {
		completion.Status = "failed"
		completion.FailureCode = "provider_outcome_audit_failed"
	}
	completedTool, err := service.repository.CompleteToolExecution(persistCtx, completion)
	if err != nil || completedTool == nil || completedTool.Status != completion.Status {
		return nil, fmt.Errorf("%w: persist tool completion", ErrAgentToolExecutionFailed)
	}
	runStatus := terminalRunStatus(completion.Status)
	completedRun, err := service.repository.CompleteRun(persistCtx, interfaces.CompleteAgentRunCommand{
		RunID: request.RunID, BusinessID: request.BusinessID, AgentID: request.AgentID, UserID: request.UserID,
		RequestHash: requestHash, Status: runStatus, FailureCode: completion.FailureCode,
		FinalDisposition: runStatus, InputTokens: completion.InputTokens, OutputTokens: completion.OutputTokens,
		CostMicros: completion.CostMicros, Now: service.now().UTC(),
	})
	if err != nil || completedRun == nil || completedRun.Status != runStatus {
		return nil, fmt.Errorf("%w: persist run completion", ErrAgentToolExecutionFailed)
	}
	if invokeErr != nil || outcomeErr != nil || completion.Status != "succeeded" {
		return nil, fmt.Errorf("%w: %s (%s)", ErrAgentToolExecutionFailed, completion.FailureCode, completion.Status)
	}
	return invocation.Output, nil
}

func (service *AgentGovernanceService) watchExecution(executionCtx context.Context, check interfaces.CheckAgentExecutionCommand, cancelExecution context.CancelFunc) func() error {
	watchCtx, stopWatch := context.WithCancel(context.WithoutCancel(executionCtx))
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(service.executionCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				done <- nil
				return
			case <-ticker.C:
				check.Now = service.now().UTC()
				checkCtx, cancelCheck := context.WithTimeout(watchCtx, 250*time.Millisecond)
				err := service.repository.CheckExecution(checkCtx, check)
				cancelCheck()
				// Stopping the watcher cancels an in-flight check, not the completed tool.
				// Drivers may report a rolled-back transaction instead of context.Canceled.
				// The caller still performs a final durable governance check.
				if watchCtx.Err() != nil {
					done <- nil
					return
				}
				if err != nil {
					cancelExecution()
					done <- err
					return
				}
			}
		}
	}()
	return func() error {
		stopWatch()
		return <-done
	}
}

func replayedAgentToolResult(result *interfaces.AgentToolExecutionResult) (json.RawMessage, error) {
	switch result.Status {
	case "authorized", "running":
		return nil, ErrAgentToolExecutionInProgress
	case "succeeded":
		if safeReference(result.ResultType) == "" || safeReference(result.ResultID) == "" {
			return nil, ErrAgentToolReplayUnavailable
		}
		encoded, err := json.Marshal(struct {
			ResultID   string `json:"result_id"`
			ResultType string `json:"result_type"`
		}{ResultID: result.ResultID, ResultType: result.ResultType})
		if err != nil {
			return nil, ErrAgentToolReplayUnavailable
		}
		return encoded, nil
	case "cancelled":
		return nil, ErrAgentToolReplayCancelled
	case "timed_out":
		return nil, ErrAgentToolReplayTimedOut
	case "reconciliation_required":
		return nil, ErrAgentToolReplayReconciliationRequired
	case "failed", "denied":
		return nil, ErrAgentToolExecutionFailed
	default:
		return nil, ErrAgentToolReplayUnavailable
	}
}

func governanceControlFailureStatus(err error) string {
	if errors.Is(err, interfaces.ErrAgentGovernanceRunExpired) {
		return "timed_out"
	}
	if errors.Is(err, interfaces.ErrAgentGovernanceRunCancelled) || errors.Is(err, interfaces.ErrAgentGovernanceGateDisabled) {
		return "cancelled"
	}
	return "failed"
}

func terminalRunStatus(toolStatus string) string {
	switch toolStatus {
	case "succeeded":
		return "completed"
	case "cancelled", "timed_out", "reconciliation_required":
		return toolStatus
	default:
		return "failed"
	}
}

type GovernedToolExecutorConfig struct {
	ExecutionEnabled                                                           bool
	Governance                                                                 AgentGovernanceExecutor
	BusinessID                                                                 string
	UserID                                                                     string
	AgentID                                                                    string
	RunID                                                                      string
	ProviderKey                                                                string
	ModelKey                                                                   string
	ModelConfig                                                                string
	TemplateVersion                                                            string
	SpendCurrency                                                              string
	TokenBudget                                                                int64
	ExpectedCostMicros, BusinessSpendCeilingMicros, AgentDailySpendLimitMicros int64
	MaxSteps, MaxToolCalls, MaxRetries                                         int
	DeadlineAt                                                                 time.Time
	ProviderFailureThreshold                                                   int
	ProviderCooldown, ProviderProbeLease                                       time.Duration
}

type toolTransport interface {
	Definitions() []sarvam.ChatToolDefinition
	Execute(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

type GovernedToolExecutor struct {
	config    GovernedToolExecutorConfig
	transport toolTransport
}

func NewGovernedToolExecutor(config GovernedToolExecutorConfig, transport toolTransport) *GovernedToolExecutor {
	return &GovernedToolExecutor{config: config, transport: transport}
}

func (executor *GovernedToolExecutor) ready() bool {
	return executor != nil && executor.config.ExecutionEnabled && !nilGovernanceInterface(executor.config.Governance) && !nilGovernanceInterface(executor.transport) &&
		executor.config.BusinessID != "" && executor.config.UserID != "" && executor.config.AgentID != ""
}

func (executor *GovernedToolExecutor) Definitions() []sarvam.ChatToolDefinition {
	if !executor.ready() {
		return nil
	}
	definitions := executor.transport.Definitions()
	result := make([]sarvam.ChatToolDefinition, 0, 2)
	for _, definition := range definitions {
		policy, ok := AgentToolPolicyFor(definition.Name)
		if ok && policy.Enabled && policy.Risk == RiskReadOnly {
			result = append(result, definition)
		}
	}
	return result
}

func (executor *GovernedToolExecutor) Execute(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	if !executor.ready() {
		return nil, ErrAgentGovernanceUnavailable
	}
	policy, ok := AgentToolPolicyFor(name)
	if !ok || !policy.Enabled {
		return nil, ErrAgentToolDenied
	}
	canonical, argumentsHash, err := CanonicalAgentToolArguments(arguments)
	if err != nil {
		return nil, err
	}
	runID := executor.config.RunID
	if runID == "" {
		runID = uuid.NewString()
	}
	deadline := executor.config.DeadlineAt
	if deadline.IsZero() {
		deadline = time.Now().UTC().Add(30 * time.Second)
	}
	idempotency := hashGovernanceFields(runID, name, argumentsHash)
	request := GovernedToolRequest{
		RunID: runID, BusinessID: executor.config.BusinessID, AgentID: executor.config.AgentID, UserID: executor.config.UserID,
		ToolKey: name, CanonicalArguments: canonical, ArgumentsHash: argumentsHash, Risk: policy.Risk,
		IdempotencyKey: idempotency, ProviderKey: executor.config.ProviderKey, ModelKey: executor.config.ModelKey,
		ModelConfig: executor.config.ModelConfig, PromptTemplateVersion: executor.config.TemplateVersion,
		TokenBudget: executor.config.TokenBudget, ExpectedCostMicros: executor.config.ExpectedCostMicros,
		BusinessSpendCeilingMicros: executor.config.BusinessSpendCeilingMicros,
		AgentDailySpendLimitMicros: executor.config.AgentDailySpendLimitMicros,
		SpendCurrency:              executor.config.SpendCurrency, MaxSteps: executor.config.MaxSteps,
		MaxToolCalls: executor.config.MaxToolCalls, MaxRetries: executor.config.MaxRetries, DeadlineAt: deadline,
		ProviderFailureThreshold: executor.config.ProviderFailureThreshold,
		ProviderCooldown:         executor.config.ProviderCooldown, ProviderProbeLease: executor.config.ProviderProbeLease,
	}
	return executor.config.Governance.ExecuteTool(ctx, request, func(callCtx context.Context, toolKey string, arguments json.RawMessage) (GovernedToolInvocationResult, error) {
		output, err := executor.transport.Execute(callCtx, toolKey, arguments)
		return GovernedToolInvocationResult{Output: output}, err
	})
}

func hashGovernanceFields(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(fmt.Sprintf("%d:", len(value))))
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func safeCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 255 || !utf8.ValidString(value) {
		return ""
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return ""
		}
	}
	return value
}

func safeReference(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || !utf8.ValidString(value) {
		return ""
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '-' ||
			character == '.' || character == ':' || character == '/') {
			return ""
		}
	}
	return value
}

func safeJSONObject(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	canonical, _, err := CanonicalAgentToolArguments(json.RawMessage(value))
	if err != nil {
		return "{}"
	}
	return string(canonical)
}

func nilGovernanceInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
