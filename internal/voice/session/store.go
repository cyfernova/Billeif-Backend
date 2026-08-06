package session

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidRequest      = errors.New("invalid voice session request")
	ErrNotFound            = errors.New("voice session not found")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for another request")
	ErrUserCapacity        = errors.New("user already has an active voice session")
	ErrGlobalCapacity      = errors.New("voice session capacity is full")
	ErrNotResumable        = errors.New("voice session cannot be resumed")
	ErrBranchForbidden     = errors.New("voice session branch access denied")
	ErrUnavailable         = errors.New("voice session service is unavailable")
)

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

type ReleaseState struct {
	Released      bool
	ShouldStop    bool
	AlreadyClosed bool
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
