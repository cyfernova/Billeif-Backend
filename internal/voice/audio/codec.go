package audio

import "errors"

var (
	ErrLibopusUnavailable     = errors.New("audio: libopus support is unavailable in this build")
	ErrLibopusFailure         = errors.New("audio: libopus operation failed")
	ErrCodecClosed            = errors.New("audio: codec is closed")
	ErrMalformedOpus          = errors.New("audio: malformed or unsupported Opus packet")
	ErrUnexpectedOpusDuration = errors.New("audio: Opus packet is not 20 milliseconds")
)
