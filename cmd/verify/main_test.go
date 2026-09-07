package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestRunRefusesProductionBeforeBuildingChecks(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--environment", "production"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	assertCLIErrorCode(t, stderr.Bytes(), "environment_refused")
}

func TestRunRefusesWriteModeWithoutExplicitAllow(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--environment", "staging", "--mode", "write"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	assertCLIErrorCode(t, stderr.Bytes(), "write_mode_requires_explicit_allow_writes")
}

func TestRunRefusesNonDefaultAWSProfile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--environment", "staging", "--aws-profile", "admin"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	assertCLIErrorCode(t, stderr.Bytes(), "aws_profile_refused")
}

func TestRunRequiresMatchingTargetEnvironmentBinding(t *testing.T) {
	t.Setenv("VERIFY_TARGET_ENVIRONMENT", "qa")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--environment", "staging"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	assertCLIErrorCode(t, stderr.Bytes(), "environment_binding_refused")
}

func TestRunRefusesAmbientAWSCredentialOverrides(t *testing.T) {
	t.Setenv("VERIFY_TARGET_ENVIRONMENT", "staging")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--environment", "staging"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	assertCLIErrorCode(t, stderr.Bytes(), "ambient_aws_credentials_refused")
}

func TestRunRefusesTargetsOutsideEnvironmentManifest(t *testing.T) {
	t.Setenv("VERIFY_TARGET_ENVIRONMENT", "staging")
	t.Setenv("LLM_API_URL", "https://untrusted.example/v1/models")
	t.Setenv("VERIFY_ALLOWED_HOSTS", "staging.example")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--environment", "staging"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	assertCLIErrorCode(t, stderr.Bytes(), "target_binding_refused")
}

func TestBuildChecksCoversTaskFourInventory(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	checks := buildChecks(context.Background(), "verify-unit")
	got := make(map[string]bool, len(checks))
	for _, check := range checks {
		id := check.Metadata().ID
		if got[id] {
			t.Fatalf("duplicate check ID %q", id)
		}
		got[id] = true
	}
	for _, id := range []string{
		"aws.identity", "cognito.userpool", "cognito.phone_pool", "cognito.google_auth",
		"postgres.connect", "redis.ping", "s3.buckets", "sqs.queues", "eventbridge.scheduler",
		"websocket.endpoint", "ses.identity", "razorpay.test", "whatsapp.test", "gst.sandbox",
		"llm.claude", "llm.gemini", "sarvam.controlled", "agentcore.runtime",
		"journey.invoice", "journey.report", "journey.upload", "journey.recurring",
		"journey.websocket_ticket", "journey.checkout",
	} {
		if !got[id] {
			t.Errorf("missing Task 4 check %q", id)
		}
	}
}

func assertCLIErrorCode(t *testing.T, raw []byte, want string) {
	t.Helper()
	var payload struct {
		Status string `json:"status"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode error JSON %q: %v", raw, err)
	}
	if payload.Status != "refused" || payload.Error.Code != want {
		t.Fatalf("payload=%+v want code=%q", payload, want)
	}
}
