package a2a

import "errors"

// A2A v0.3 specific errors
var (
	ErrInvalidTaskState  = errors.New("invalid task state")
	ErrTaskStateTerminal = errors.New("cannot transition from terminal state")
	ErrTaskNotFound      = errors.New("task not found")
	ErrSessionNotFound   = errors.New("session not found")
	ErrAgentNotFound     = errors.New("agent not found")
	ErrAgentUnavailable  = errors.New("agent unavailable")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrInvalidMessage    = errors.New("invalid message")
	ErrPushConfigInvalid = errors.New("invalid push notification configuration")
	ErrStreamClosed      = errors.New("stream closed")
)
