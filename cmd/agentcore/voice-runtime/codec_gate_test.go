//go:build !cgo || !voice_libopus

package main

import (
	"errors"
	"testing"

	"invoice-backend/internal/voice/audio"
)

func TestRuntimeFailsClosedWhenLibopusWasNotCompiledIn(t *testing.T) {
	err := run(t.Context())
	if !errors.Is(err, audio.ErrLibopusUnavailable) {
		t.Fatalf("run() error = %v, want ErrLibopusUnavailable", err)
	}
}
