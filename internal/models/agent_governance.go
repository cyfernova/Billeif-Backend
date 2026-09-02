package models

import "time"

type AIGovernanceControl struct {
	ID               string    `gorm:"primaryKey;type:uuid"`
	ScopeKind        string    `gorm:"not null"`
	BusinessID       *string   `gorm:"type:uuid"`
	AgentID          *string   `gorm:"type:uuid"`
	ExecutionEnabled bool      `gorm:"not null;default:false"`
	ReasonCode       string    `gorm:"not null"`
	Version          int64     `gorm:"not null"`
	UpdatedByUserID  *string   `gorm:"type:uuid"`
	UpdatedAt        time.Time `gorm:"not null"`
}

func (AIGovernanceControl) TableName() string { return "ai_governance_controls" }

type AIToolApproval struct {
	ID, BusinessID, AgentID, UserID      string
	ToolKey, RiskClass, ArgumentsHash    string
	SanitizedArguments                   string
	ResourceType, ResourceID             string
	MonetaryAmountMinor, TaxAmountMinor  *int64
	MonetaryCurrency, TaxCurrency        *string
	ExecutionIdempotencyKey, RequestHash string
	IssuedAt, ExpiresAt, CreatedAt       time.Time
	ConsumedAt                           *time.Time
}

func (AIToolApproval) TableName() string { return "ai_tool_approvals" }

type AIAgentRun struct {
	ID, BusinessID, AgentID, UserID                  string
	IdempotencyKey, RequestHash                      string
	ProviderKey, ModelKey, ModelConfig               string
	PromptTemplateVersion                            string
	TokenBudget, TokenReserved, TokenUsed            int64
	MaxSteps, StepsUsed, MaxToolCalls, ToolCallsUsed int
	MaxRetries, RetriesUsed                          int
	DeadlineAt                                       time.Time
	CancelRequestedAt                                *time.Time
	Status                                           string
	InputTokens, OutputTokens, CostMicros            int64
	CostCurrency, FailureCode, FinalDisposition      string
	CreatedAt, UpdatedAt                             time.Time
	StartedAt, CompletedAt                           *time.Time
}

func (AIAgentRun) TableName() string { return "ai_agent_runs" }

type AIBudgetPeriod struct {
	ID, ScopeKind, BusinessID, SpendCurrency     string
	AgentID                                      *string
	PeriodStart, PeriodEnd, CreatedAt, UpdatedAt time.Time
	LimitMicros, ReservedMicros, SettledMicros   int64
	Version                                      int64
}

func (AIBudgetPeriod) TableName() string { return "ai_budget_periods" }

type AIToolExecution struct {
	ID, RunID, BusinessID, AgentID, UserID                string
	Sequence                                              int
	ToolKey, RiskClass                                    string
	ApprovalRequired                                      bool
	ApprovalID                                            *string
	ArgumentsHash, SanitizedArguments                     string
	ResourceType, ResourceID, IdempotencyKey, RequestHash string
	Status, EffectDisposition                             string
	AttemptCount                                          int
	ResultType, ResultID, FailureCode, FinalDisposition   string
	InputTokens, OutputTokens, CostMicros                 int64
	RequestedAt, UpdatedAt                                time.Time
	StartedAt, CompletedAt                                *time.Time
}

func (AIToolExecution) TableName() string { return "ai_tool_executions" }

type AISpendReservation struct {
	ID, RunID                                   string
	ToolExecutionID                             *string
	BusinessBudgetPeriodID, AgentBudgetPeriodID string
	ProviderKey, ModelKey, SpendCurrency        string
	ReservedMicros, SettledMicros               int64
	Status, IdempotencyKey, RequestHash         string
	ReservedAt, ExpiresAt, UpdatedAt            time.Time
	SettledAt, ReleasedAt                       *time.Time
}

func (AISpendReservation) TableName() string { return "ai_spend_reservations" }

type AIProviderCircuitBreaker struct {
	ProviderKey            string `gorm:"primaryKey"`
	State, LastFailureCode string
	ConsecutiveFailures    int
	OpenedAt, RetryAt      *time.Time
	ProbeToken             *string
	ProbeExpiresAt         *time.Time
	Version                int64
	UpdatedAt              time.Time
}

func (AIProviderCircuitBreaker) TableName() string { return "ai_provider_circuit_breakers" }
