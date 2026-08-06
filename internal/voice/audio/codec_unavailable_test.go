//go:build !cgo || !voice_libopus

package audio

import (
	"errors"
	"testing"
)

func TestLibopusConstructorsFailClosedWithoutTheProductionBuildTag(t *testing.T) {
	t.Parallel()

	if err := VerifyLibopus(); !errors.Is(err, ErrLibopusUnavailable) {
		t.Fatalf("VerifyLibopus() error = %v, want ErrLibopusUnavailable", err)
	}
	if decoder, err := NewLibopusDecoder(); decoder != nil || !errors.Is(err, ErrLibopusUnavailable) {
		t.Fatalf("NewLibopusDecoder() = (%v, %v), want (nil, ErrLibopusUnavailable)", decoder, err)
	}
	if encoder, err := NewLibopusEncoder(); encoder != nil || !errors.Is(err, ErrLibopusUnavailable) {
		t.Fatalf("NewLibopusEncoder() = (%v, %v), want (nil, ErrLibopusUnavailable)", encoder, err)
	}
}
