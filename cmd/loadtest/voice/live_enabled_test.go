//go:build voice_live_load

package main

import (
	"bytes"
	"errors"
	"testing"

	"invoice-backend/internal/voice/loadtest"
)

func TestTaggedLiveRunnerStillStopsAfterValidatedPreflight(t *testing.T) {
	args := []string{
		"-mode=live",
		"-stage=aws-fake-120",
		"-sessions=120",
		"-duration=30m",
		"-dry-run=false",
		"-environment=staging",
		"-revision=0123456789abcdef0123456789abcdef01234567",
		"-image-digest=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"-api-endpoint=https://api.example.com/staging",
		"-agentcore-endpoint=https://agentcore.example.com/invocations",
		"-stt-endpoint=wss://stt.example.com/stream",
		"-chat-endpoint=https://chat.example.com/v1/chat",
		"-tts-endpoint=wss://tts.example.com/stream",
		"-confirm-spend",
		"-confirm-quotas",
		"-confirm-staging",
		"-confirm-dns-revalidation",
		"-confirm-provider-approval",
	}
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr, func(name string) string {
		if name == loadtest.LiveAcknowledgementEnvironment {
			return loadtest.LiveAcknowledgement
		}
		return ""
	})
	if !errors.Is(err, loadtest.ErrLiveExecutorUnavailable) {
		t.Fatalf("run(tagged live) error = %v, want ErrLiveExecutorUnavailable", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("preflight-only runner wrote live evidence: %q", stdout.String())
	}
}

func TestTaggedLiveRunnerRequiresExactEnvironmentAcknowledgement(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-mode=live", "-dry-run=false"}, &stdout, &stderr, func(string) string { return "wrong" })
	if !errors.Is(err, loadtest.ErrLiveGateClosed) {
		t.Fatalf("run(incomplete gates) error = %v, want ErrLiveGateClosed", err)
	}
}
