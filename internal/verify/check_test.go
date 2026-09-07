package verify

import (
	"context"
	"testing"
	"time"
)

func TestProbeHelpersCarryStableReasons(t *testing.T) {
	nc := NotConfigured("COGNITO_USER_POOL_ID", "COGNITO_CLIENT_ID")
	if nc.Status != StatusNotConfigured {
		t.Fatalf("status = %q", nc.Status)
	}
	if len(nc.Evidence) != 2 || nc.Evidence[1].Value != "COGNITO_USER_POOL_ID,COGNITO_CLIENT_ID" {
		t.Fatalf("not_configured evidence = %#v", nc.Evidence)
	}

	blocked := Blocked("sandbox_required", EvidenceItem("mode", "live"))
	if blocked.Status != StatusBlocked || blocked.Evidence[0].Value != "sandbox_required" {
		t.Fatalf("blocked outcome = %#v", blocked)
	}

	failed := Failed(errString("boom"), "probe_failed", true)
	if !failed.Retryable || failed.ErrCode != "probe_failed" {
		t.Fatalf("failed outcome = %#v", failed)
	}
}

func TestCheckInterfaceHonorsContextDeadline(t *testing.T) {
	check := &fakeCheck{
		meta: Metadata{ID: "deadline", Impact: ImpactReadOnly},
		outcome: func(ctx context.Context, _ int) Outcome {
			select {
			case <-ctx.Done():
				return NotConfigured()
			case <-time.After(50 * time.Millisecond):
				t.Fatal("probe must observe its deadline")
			}
			return Outcome{}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	outcome := check.Probe(ctx)
	if outcome.Status != StatusNotConfigured {
		t.Fatalf("status = %q", outcome.Status)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
