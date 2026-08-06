package session

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidRequest       = errors.New("invalid voice session request")
	ErrNotFound             = errors.New("voice session not found")
	ErrIdempotencyConflict  = errors.New("idempotency key was already used for another request")
	ErrUserCapacity         = errors.New("user already has an active voice session")
	ErrGlobalCapacity       = errors.New("voice session capacity is full")
	ErrNotResumable         = errors.New("voice session cannot be resumed")
	ErrBranchForbidden      = errors.New("voice session branch access denied")
	ErrUnavailable          = errors.New("voice session service is unavailable")
	ErrInvalidFinalTurn     = errors.New("invalid final voice turn")
	ErrFinalTurnSequence    = errors.New("final voice turn sequence was rejected")
	ErrFinalTurnConflict    = errors.New("final voice turn conflicts with retained state")
	ErrLeaseNotRenewable    = errors.New("voice session lease cannot be renewed")
	ErrLeaseHeartbeatFailed = errors.New("voice session lease heartbeat failed")
	ErrRolloutDenied        = errors.New("realtime voice rollout is not available")
)

const (
	MaxPersistedTranscriptBytes = 16 << 10
	MaxPersistedResponseBytes   = 32 << 10
	MaxPersistedToolOutcomes    = 4
	MaxFinalTurnRetention       = 55 * time.Minute
)

type FinalTurnToolOutcome struct {
	Name      string `dynamodbav:"name" json:"name"`
	Success   bool   `dynamodbav:"success" json:"success"`
	ErrorCode string `dynamodbav:"error_code,omitempty" json:"error_code,omitempty"`
}

type FinalTurnTimings struct {
	SpeechEndedAt      time.Time
	STTFinalAt         time.Time
	LLMFirstTokenAt    time.Time
	TTSFirstAudioAt    time.Time
	ClientFirstAudioAt time.Time
}

type FinalTurnUsage struct {
	InputTokens   *int64
	OutputTokens  *int64
	TotalTokens   *int64
	TTSCharacters *int64
}

// FinalTurn is a completed, authoritative transcript/response pair. Partial
// transcripts and in-progress generations deliberately have no persistence API.
type FinalTurn struct {
	SessionID        string
	Sequence         int64
	GenerationID     int64
	Transcript       string
	ProviderLanguage string
	SelectedLanguage string
	Response         string
	Tools            []FinalTurnToolOutcome
	Timings          FinalTurnTimings
	Usage            FinalTurnUsage
	Cancelled        bool
	CompletedAt      time.Time
	ExpiresAt        time.Time
}

type FinalTurnWriter interface {
	PersistFinalTurn(context.Context, FinalTurn) error
}

type CreateRecord struct {
	Session              *Session
	IdempotencyKey       string
	RequestHash          string
	IdempotencyExpiresAt time.Time
}

type ResumeRecord struct {
	Scope                  Scope
	SessionID              string
	ExpectedBranchID       string
	OldRuntimeSessionID    string
	ExpectedRuntimeState   RuntimeState
	ExpectedLeaseExpiresAt time.Time
	NewRuntimeSessionID    string
	LeaseExpiresAt         time.Time
	UpdatedAt              time.Time
}

// LeaseRenewal is a server-authored compare-and-swap request for one exact
// runtime lease. RenewedAt must come from the runtime clock; client heartbeat
// timestamps are deliberately absent from this durable boundary.
type LeaseRenewal struct {
	Scope                  Scope
	SessionID              string
	ExpectedBranchID       string
	RuntimeSessionID       string
	ExpectedLeaseExpiresAt time.Time
	ExpectedExpiresAt      time.Time
	RenewedAt              time.Time
	LeaseDuration          time.Duration
}

// LeaseRenewer is intentionally narrower than Store so a connected runtime
// peer receives only the durable capability needed by heartbeat handling.
type LeaseRenewer interface {
	RenewLease(context.Context, LeaseRenewal) (*Session, error)
}

type ReleaseState struct {
	Released      bool
	ShouldStop    bool
	AlreadyClosed bool
	LeaseRenewed  bool
}

type Store interface {
	Create(context.Context, CreateRecord) (*Session, bool, error)
	Get(context.Context, Scope, string) (*Session, error)
	Resume(context.Context, ResumeRecord) (*Session, error)
	Release(context.Context, Scope, string, time.Time) (*Session, ReleaseState, error)
	MarkClosed(context.Context, Scope, string, string, time.Time) error
	ExpiredLeases(context.Context, time.Time, int32) ([]*Session, error)
}

type RuntimeStopper interface {
	StopRuntimeSession(context.Context, RuntimeTarget) error
}
