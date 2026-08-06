package protocol

import "errors"

var (
	ErrInvalidControlMessage      = errors.New("invalid voice control message")
	ErrControlMessageTooLarge     = errors.New("voice control message exceeds 16 KiB")
	ErrUnsupportedProtocolVersion = errors.New("unsupported voice protocol version")
	ErrSessionIDRequired          = errors.New("voice session_id is required")
	ErrSequenceRequired           = errors.New("voice sequence must be positive")
	ErrDuplicateSequence          = errors.New("duplicate voice sequence")
	ErrNonMonotonicSequence       = errors.New("voice sequence is not monotonic")
	ErrUnsupportedEvent           = errors.New("unsupported voice control event")
	ErrGenerationIDRequired       = errors.New("voice generation_id is required")
)
