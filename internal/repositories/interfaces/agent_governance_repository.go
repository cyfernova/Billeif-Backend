package interfaces

import (
	"context"
	"errors"
	"time"
)

var (
	ErrAgentGovernanceInvalidScope       = errors.New("agent governance scope is invalid")
	ErrAgentGovernanceNotFound           = errors.New("agent governance record not found")
	ErrAgentGovernanceConflict           = errors.New("agent governance idempotency conflict")
	ErrAgentGovernanceApprovalRequired   = errors.New("agent governance approval required")
	ErrAgentGovernanceApprovalInvalid    = errors.New("agent governance approval invalid")
	ErrAgentGovernanceGateDisabled       = errors.New("agent governance execution gate disabled")
	ErrAgentGovernanceRunCancelled       = errors.New("agent governance run cancelled")
	ErrAgentGovernanceRunExpired         = errors.New("agent governance run expired")
	ErrAgentGovernanceStepLimit          = errors.New("agent governance step limit exceeded")
	ErrAgentGovernanceToolCallLimit      = errors.New("agent governance tool call limit exceeded")
	ErrAgentGovernanceTokenLimit         = errors.New("agent governance token limit exceeded")
	ErrAgentGovernanceRetryLimit         = errors.New("agent governance retry limit exceeded")
	ErrAgentGovernanceAgentSpendLimit    = errors.New("agent daily spend limit exceeded")
	ErrAgentGovernanceBusinessSpendLimit = errors.New("business spend ceiling exceeded")
	ErrAgentGovernanceUnsafeTransition   = errors.New("agent governance unsafe transition")
	ErrAgentGovernanceCircuitOpen        = errors.New("agent provider circuit open")
	ErrAgentGovernanceProbeBusy          = errors.New("agent provider circuit probe busy")
)

type AgentApprovalCommand struct {
	ID, BusinessID, AgentID, UserID, ToolKey, RiskClass string
	ArgumentsHash, SanitizedArguments                   string
	ResourceType, ResourceID                            string
	MonetaryAmountMinor, TaxAmountMinor                 *int64
	MonetaryCurrency, TaxCurrency                       string
	ExecutionIdempotencyKey, RequestHash                string
	IssuedAt, ExpiresAt                                 time.Time
}

type AgentApprovalResult struct {
	ID       string
	Replayed bool
}

type StartAgentRunCommand struct {
	ID, BusinessID, AgentID, UserID, IdempotencyKey, RequestHash string
	ProviderKey, ModelKey, ModelConfig, PromptTemplateVersion    string
	SpendCurrency                                                string
	TokenBudget                                                  int64
	MaxSteps, MaxToolCalls, MaxRetries                           int
	DeadlineAt, Now                                              time.Time
}

type AgentRunResult struct {
	ID, Status, FinalDisposition string
	Replayed                     bool
}

type ReserveAgentRunCapacityCommand struct {
	RunID, BusinessID, AgentID, UserID, IdempotencyKey, RequestHash string
	ProviderKey, ModelKey, SpendCurrency                            string
	TokenAmount, SpendMicros                                        int64
	BusinessLimitMicros, AgentDailyLimitMicros                      int64
	PeriodStart, PeriodEnd, ExpiresAt, Now                          time.Time
	Step, Retry                                                     bool
}

type AgentSpendReservationResult struct {
	ID, Status string
	Replayed   bool
}

type AuthorizeAgentToolCommand struct {
	ID, RunID, BusinessID, AgentID, UserID string
	Sequence                               int
	ToolKey, RiskClass                     string
	ApprovalRequired                       bool
	ApprovalID                             string
	ArgumentsHash, SanitizedArguments      string
	ResourceType, ResourceID               string
	MonetaryAmountMinor, TaxAmountMinor    *int64
	MonetaryCurrency, TaxCurrency          string
	IdempotencyKey, RequestHash            string
	Now                                    time.Time
	Capacity                               *ReserveAgentRunCapacityCommand
}

type AgentToolExecutionResult struct {
	ID, Status, FinalDisposition string
	SpendReservationID           string
	Replayed                     bool
}

type CompleteAgentToolCommand struct {
	ExecutionID, RunID, BusinessID, AgentID, UserID string
	RequestHash, Status, EffectDisposition          string
	ResultType, ResultID, FailureCode               string
	InputTokens, OutputTokens, CostMicros           int64
	SpendReservationID                              string
	Now                                             time.Time
}

type CompleteAgentRunCommand struct {
	RunID, BusinessID, AgentID, UserID, RequestHash string
	Status, FailureCode, FinalDisposition           string
	InputTokens, OutputTokens, CostMicros           int64
	Now                                             time.Time
}

type CancelAgentRunCommand struct {
	RunID, BusinessID, AgentID, UserID, ReasonCode string
	Now                                            time.Time
}

type AgentExecutionGateCommand struct {
	ScopeKind, BusinessID, AgentID, UpdatedByUserID, ReasonCode string
	ExecutionEnabled                                            bool
	Now                                                         time.Time
}

type ProviderAdmissionCommand struct {
	ProviderKey, ProbeToken string
	FailureThreshold        int
	Cooldown, ProbeLease    time.Duration
	Now                     time.Time
}

type ProviderAdmission struct {
	ProbeToken string
	HalfOpen   bool
}

type ProviderOutcomeCommand struct {
	ProviderKey, ProbeToken, FailureCode string
	Success                              bool
	FailureThreshold                     int
	Cooldown                             time.Duration
	Now                                  time.Time
}

// AgentGovernanceRepository keeps admission, one-use approval, counters,
// reservations, audit disposition, gates, and breaker transitions atomic.
type AgentGovernanceRepository interface {
	ExecutionReady(context.Context, string) (bool, error)
	IssueApproval(context.Context, AgentApprovalCommand) (*AgentApprovalResult, error)
	StartRun(context.Context, StartAgentRunCommand) (*AgentRunResult, error)
	ReserveRunCapacity(context.Context, ReserveAgentRunCapacityCommand) (*AgentSpendReservationResult, error)
	AuthorizeToolExecution(context.Context, AuthorizeAgentToolCommand) (*AgentToolExecutionResult, error)
	CompleteToolExecution(context.Context, CompleteAgentToolCommand) (*AgentToolExecutionResult, error)
	CompleteRun(context.Context, CompleteAgentRunCommand) (*AgentRunResult, error)
	RequestCancellation(context.Context, CancelAgentRunCommand) error
	SetExecutionGate(context.Context, AgentExecutionGateCommand) error
	AdmitProvider(context.Context, ProviderAdmissionCommand) (*ProviderAdmission, error)
	RecordProviderOutcome(context.Context, ProviderOutcomeCommand) error
}
