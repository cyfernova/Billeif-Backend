//go:build !voice_live_load

package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestUntaggedBinaryHasNoLiveRunner(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-mode=live"}, &stdout, &stderr, func(string) string { return "" })
	if !errors.Is(err, ErrLiveBuildDisabled) {
		t.Fatalf("run(live) error = %v, want ErrLiveBuildDisabled", err)
	}
}
