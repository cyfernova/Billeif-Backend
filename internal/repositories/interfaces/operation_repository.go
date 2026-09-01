package interfaces

import (
	"context"
	"errors"
	"time"
)

var (
	ErrOperationNotFound     = errors.New("operation not found")
	ErrUnsupportedRecovery   = errors.New("unsupported recovery")
	ErrOperationRace         = errors.New("operation changed during recovery")
	ErrUnsafeOperationReplay = errors.New("unsafe operation replay")
)

// OperationRecord is the intentionally sanitized common shape returned by a
// domain adapter. Provider payloads, provider identifiers, queue identifiers,
// account identifiers, and lease topology do not cross this boundary.
type OperationRecord struct {
	ID                     string
	Type                   string
	ResourceType           string
	ResourceID             string
	InternalStatus         string
	Attempts               int
	LastAttemptAt          *time.Time
	NextAttemptAt          *time.Time
	Retryable              bool
	ReconciliationRequired bool
	DeadLetter             bool
	ErrorCode              string
	CorrelationID          string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	CompletedAt            *time.Time
}

type OperationRecordQuery struct {
	Types          []string
	Statuses       []string
	ExactID        string
	Limit          int
	SnapshotAt     time.Time
	AfterUpdatedAt *time.Time
	AfterType      string
	AfterID        string
}

type OperationRecordPage struct {
	Records          []OperationRecord
	UnavailableTypes []string
}

type OperationTimelineRecord struct {
	Status     string
	Code       string
	OccurredAt time.Time
}

type RenderRecoveryCommand struct {
	BusinessID       string
	OperationID      string
	ActorSubject     string
	PrincipalKind    string
	Action           string
	Reason           string
	IdempotencyKey   string
	CorrelationID    string
	RequestHash      string
	OperationVersion string
	OccurredAt       time.Time
}

type OperationRecoveryResult struct {
	CommandID     string
	ResultCode    string
	CorrelationID string
	Replayed      bool
	AcceptedAt    time.Time
}

type OperationRecoveryDecision struct {
	BusinessID       string
	OperationType    string
	OperationID      string
	ActorSubject     string
	PrincipalKind    string
	Action           string
	Reason           string
	IdempotencyKey   string
	CorrelationID    string
	RequestHash      string
	OperationVersion string
	ResultCode       string
	OccurredAt       time.Time
}

type OperationRepository interface {
	ListOperations(context.Context, string, OperationRecordQuery) (OperationRecordPage, error)
	GetOperation(context.Context, string, string, string) (*OperationRecord, error)
	ListOperationTimeline(context.Context, string, string, string, int) ([]OperationTimelineRecord, error)
	RetryRender(context.Context, RenderRecoveryCommand) (*OperationRecoveryResult, error)
	RecordRecoveryDecision(context.Context, OperationRecoveryDecision) (*OperationRecoveryResult, error)
}
