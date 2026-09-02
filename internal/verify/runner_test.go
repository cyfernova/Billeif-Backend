package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCheck struct {
	meta    Metadata
	outcome func(ctx context.Context, attempt int) Outcome
	mu      sync.Mutex
	attempt int
}

func (f *fakeCheck) Metadata() Metadata { return f.meta }

func (f *fakeCheck) Probe(ctx context.Context) Outcome {
	f.mu.Lock()
	f.attempt++
	attempt := f.attempt
	f.mu.Unlock()
	return f.outcome(ctx, attempt)
}

func (f *fakeCheck) attempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempt
}

func newCheck(id string, impact Impact, deps ...string) *fakeCheck {
	return &fakeCheck{
		meta: Metadata{ID: id, Provider: "fake", Description: "fake check " + id, Impact: impact, Dependencies: deps},
	}
}

func passingOutcome(evidence ...Evidence) Outcome {
	return Outcome{Status: StatusPassed, Evidence: evidence}
}

func optionsForRun() RunOptions {
	return RunOptions{
		Environment:  "staging",
		Mode:         ModeRead,
		Concurrency:  4,
		CheckTimeout: 2 * time.Second,
		MaxAttempts:  3,
		BackoffBase:  time.Millisecond,
		BackoffMax:   2 * time.Millisecond,
	}
}

func executeForTest(t *testing.T, opts RunOptions, checks []Check) []CheckResult {
	t.Helper()
	results, err := executeChecks(context.Background(), opts, checks)
	if err != nil {
		t.Fatalf("executeChecks: %v", err)
	}
	return results
}

func resultFor(t *testing.T, results []CheckResult, id string) CheckResult {
	t.Helper()
	for _, result := range results {
		if result.ID == id {
			return result
		}
	}
	t.Fatalf("no result for check %q", id)
	return CheckResult{}
}

func TestExecuteProducesEveryStatus(t *testing.T) {
	checks := []Check{
		&fakeCheck{meta: Metadata{ID: "a.passed", Impact: ImpactReadOnly}, outcome: func(context.Context, int) Outcome {
			return Outcome{Status: StatusPassed}
		}},
		&fakeCheck{meta: Metadata{ID: "b.failed", Impact: ImpactReadOnly}, outcome: func(context.Context, int) Outcome {
			return Outcome{Status: StatusFailed, Err: errors.New("boom"), ErrCode: "probe_failed"}
		}},
		&fakeCheck{meta: Metadata{ID: "c.not_configured", Impact: ImpactReadOnly}, outcome: func(context.Context, int) Outcome {
			return Outcome{Status: StatusNotConfigured}
		}},
	}
	opts := optionsForRun()
	opts.Mode = ModeWrite
	opts.AllowWrites = true
	results := executeForTest(t, opts, checks)
	byID := map[string]CheckResult{}
	for _, result := range results {
		byID[result.ID] = result
	}
	if byID["a.passed"].Status != StatusPassed {
		t.Fatalf("a.passed status = %q", byID["a.passed"].Status)
	}
	if byID["b.failed"].Status != StatusFailed {
		t.Fatalf("b.failed status = %q", byID["b.failed"].Status)
	}
	if byID["b.failed"].Error == nil || byID["b.failed"].Error.Code != "probe_failed" {
		t.Fatalf("b.failed error = %+v", byID["b.failed"].Error)
	}
	if byID["c.not_configured"].Status != StatusNotConfigured {
		t.Fatalf("c.not_configured status = %q", byID["c.not_configured"].Status)
	}
}

func TestWriteChecksAreSkippedInReadMode(t *testing.T) {
	check := newCheck("journey.x", ImpactExternalWrite)
	results := executeForTest(t, optionsForRun(), []Check{check})
	result := resultFor(t, results, "journey.x")
	if result.Status != StatusSkipped {
		t.Fatalf("write check in read mode status = %q, want skipped", result.Status)
	}
	if check.attempts() != 0 {
		t.Fatalf("write check must not execute in read mode, attempts=%d", check.attempts())
	}
	var reason string
	for _, item := range result.Evidence {
		if item.Key == "reason" {
			reason, _ = item.Value.(string)
		}
	}
	if reason != ReasonWriteModeRequired {
		t.Fatalf("skip reason = %q, want %q", reason, ReasonWriteModeRequired)
	}
}

func TestWriteChecksExecuteOnlyWithWriteModeAndAllowWrites(t *testing.T) {
	check := newCheck("journey.x", ImpactExternalWrite)
	check.outcome = func(context.Context, int) Outcome { return Outcome{Status: StatusPassed} }
	opts := optionsForRun()
	opts.Mode = ModeWrite
	opts.AllowWrites = false
	results, err := executeChecks(context.Background(), opts, []Check{check})
	if err == nil {
		t.Fatal("write mode without allow-writes must be refused")
	}
	var refusal *UsageRefusalError
	if !errors.As(err, &refusal) || refusal.Reason != ReasonWriteRequiresExplicitAllow {
		t.Fatalf("refusal = %v, want UsageRefusalError(%s)", err, ReasonWriteRequiresExplicitAllow)
	}
	if check.attempts() != 0 {
		t.Fatal("no check may execute on refusal")
	}

	opts.AllowWrites = true
	results = executeForTest(t, opts, []Check{check})
	if resultFor(t, results, "journey.x").Status != StatusPassed {
		t.Fatal("write check must execute with mode=write and allow-writes")
	}
}

func TestRunnerBoundsGlobalConcurrency(t *testing.T) {
	var current, max atomic.Int64
	checks := make([]Check, 0, 12)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("c.%02d", i)
		checks = append(checks, &fakeCheck{
			meta: Metadata{ID: id, Impact: ImpactReadOnly},
			outcome: func(ctx context.Context, _ int) Outcome {
				now := current.Add(1)
				for {
					old := max.Load()
					if now <= old || max.CompareAndSwap(old, now) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				current.Add(-1)
				return Outcome{Status: StatusPassed}
			},
		})
	}
	opts := optionsForRun()
	opts.Concurrency = 3
	results := executeForTest(t, opts, checks)
	if len(results) != 12 {
		t.Fatalf("results = %d, want 12", len(results))
	}
	if got := max.Load(); got > 3 {
		t.Fatalf("max concurrent checks = %d, want <= 3", got)
	}
	if got := max.Load(); got < 2 {
		t.Fatalf("checks did not run concurrently (max=%d)", got)
	}
}

func TestRunnerRetriesAreBoundedWithBackoff(t *testing.T) {
	var sleeps []time.Duration
	failures := 0
	check := &fakeCheck{
		meta: Metadata{ID: "flaky", Impact: ImpactReadOnly},
		outcome: func(_ context.Context, attempt int) Outcome {
			if attempt < 3 {
				return Outcome{Status: StatusFailed, Err: errors.New("transient"), ErrCode: "transient", Retryable: true}
			}
			return Outcome{Status: StatusPassed}
		},
	}
	opts := optionsForRun()
	opts.MaxAttempts = 3
	opts.BackoffBase = 10 * time.Millisecond
	opts.BackoffMax = 25 * time.Millisecond
	opts.Sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	results := executeForTest(t, opts, []Check{check})
	result := resultFor(t, results, "flaky")
	if result.Status != StatusPassed || result.Attempts != 3 {
		t.Fatalf("status=%s attempts=%d, want passed/3", result.Status, result.Attempts)
	}
	_ = failures
	if len(sleeps) != 2 {
		t.Fatalf("sleeps = %v, want 2 backoffs", sleeps)
	}
	if sleeps[0] != 10*time.Millisecond || sleeps[1] != 20*time.Millisecond {
		t.Fatalf("backoff = %v, want exponential 10ms,20ms", sleeps)
	}

	capped := &fakeCheck{
		meta: Metadata{ID: "capped", Impact: ImpactReadOnly},
		outcome: func(context.Context, int) Outcome {
			return Outcome{Status: StatusFailed, Err: errors.New("still down"), ErrCode: "down", Retryable: true}
		},
	}
	opts.Exclude = nil
	opts.Sleep = func(_ context.Context, d time.Duration) error {
		if d > 25*time.Millisecond {
			t.Fatalf("backoff %v exceeds BackoffMax", d)
		}
		return nil
	}
	results = executeForTest(t, opts, []Check{capped})
	result = resultFor(t, results, "capped")
	if result.Status != StatusFailed || result.Attempts != 3 {
		t.Fatalf("capped status=%s attempts=%d, want failed/3", result.Status, result.Attempts)
	}
}

func TestRunnerDoesNotRetryNonRetryableFailures(t *testing.T) {
	check := &fakeCheck{
		meta: Metadata{ID: "hard", Impact: ImpactReadOnly},
		outcome: func(context.Context, int) Outcome {
			return Outcome{Status: StatusFailed, Err: errors.New("permanent"), ErrCode: "permanent"}
		},
	}
	results := executeForTest(t, optionsForRun(), []Check{check})
	if result := resultFor(t, results, "hard"); result.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (non-retryable)", result.Attempts)
	}
}

func TestRunnerEnforcesPerCheckDeadline(t *testing.T) {
	check := &fakeCheck{
		meta: Metadata{ID: "slow", Impact: ImpactReadOnly},
		outcome: func(ctx context.Context, _ int) Outcome {
			<-ctx.Done()
			return Outcome{Status: StatusFailed, Err: ctx.Err(), ErrCode: "unexpected"}
		},
	}
	opts := optionsForRun()
	opts.CheckTimeout = 30 * time.Millisecond
	opts.MaxAttempts = 1
	results := executeForTest(t, opts, []Check{check})
	result := resultFor(t, results, "slow")
	if result.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "deadline_exceeded" {
		t.Fatalf("error = %+v, want deadline_exceeded", result.Error)
	}
	if result.DurationMS < 25 {
		t.Fatalf("duration %dms should reflect the deadline", result.DurationMS)
	}
}

func TestRunnerDeadlineRetryStaysBounded(t *testing.T) {
	check := &fakeCheck{
		meta: Metadata{ID: "slow", Impact: ImpactReadOnly},
		outcome: func(ctx context.Context, _ int) Outcome {
			<-ctx.Done()
			return Outcome{Status: StatusFailed, Err: ctx.Err(), ErrCode: "timeout", Retryable: true}
		},
	}
	opts := optionsForRun()
	opts.CheckTimeout = 10 * time.Millisecond
	opts.MaxAttempts = 2
	opts.Sleep = func(context.Context, time.Duration) error { return nil }
	results := executeForTest(t, opts, []Check{check})
	result := resultFor(t, results, "slow")
	if result.Status != StatusFailed || result.Attempts != 2 {
		t.Fatalf("status=%s attempts=%d, want failed/2", result.Status, result.Attempts)
	}
}

func TestFailedDependencyBlocksDependents(t *testing.T) {
	root := &fakeCheck{
		meta: Metadata{ID: "root", Impact: ImpactReadOnly},
		outcome: func(context.Context, int) Outcome {
			return Outcome{Status: StatusFailed, ErrCode: "down"}
		},
	}
	mid := newCheck("mid", ImpactReadOnly, "root")
	leaf := newCheck("leaf", ImpactReadOnly, "mid")
	results := executeForTest(t, optionsForRun(), []Check{root, mid, leaf})
	midResult := resultFor(t, results, "mid")
	if midResult.Status != StatusBlocked {
		t.Fatalf("mid status = %q, want blocked", midResult.Status)
	}
	if midResult.Attempts != 0 {
		t.Fatalf("blocked check must not execute, attempts=%d", midResult.Attempts)
	}
	var blockedBy string
	for _, item := range midResult.Evidence {
		if item.Key == "blocked_by" {
			blockedBy, _ = item.Value.(string)
		}
	}
	if blockedBy != "root:failed" {
		t.Fatalf("blocked_by = %q, want root:failed", blockedBy)
	}
	if leafResult := resultFor(t, results, "leaf"); leafResult.Status != StatusBlocked {
		t.Fatalf("leaf status = %q, want blocked (transitive)", leafResult.Status)
	}
}

func TestNotConfiguredAndSkippedDependenciesPropagate(t *testing.T) {
	nc := &fakeCheck{
		meta: Metadata{ID: "nc", Impact: ImpactReadOnly},
		outcome: func(context.Context, int) Outcome {
			return Outcome{Status: StatusNotConfigured}
		},
	}
	dependent := newCheck("dependent", ImpactReadOnly, "nc")
	skipped := &fakeCheck{
		meta: Metadata{ID: "skipped", Impact: ImpactExternalWrite},
		outcome: func(context.Context, int) Outcome {
			return Outcome{Status: StatusPassed}
		},
	}
	dependentOnSkipped := newCheck("after_skip", ImpactExternalWrite, "skipped")
	results := executeForTest(t, optionsForRun(), []Check{nc, dependent, skipped, dependentOnSkipped})
	if got := resultFor(t, results, "dependent").Status; got != StatusBlocked {
		t.Fatalf("dependent of not_configured = %q, want blocked", got)
	}
	if got := resultFor(t, results, "skipped").Status; got != StatusSkipped {
		t.Fatalf("write check in read mode = %q, want skipped", got)
	}
	if got := resultFor(t, results, "after_skip").Status; got != StatusSkipped {
		t.Fatalf("dependent of skipped = %q, want skipped", got)
	}
}

func TestCleanupSuccessIsRecorded(t *testing.T) {
	cleaned := false
	check := &fakeCheck{
		meta: Metadata{ID: "journey.resource", Impact: ImpactExternalWrite},
		outcome: func(context.Context, int) Outcome {
			return Outcome{
				Status:    StatusPassed,
				Resources: []Resource{{Kind: "synthetic-object", Reference: "verify/synthetic-1"}},
				Cleanup: func(context.Context) ([]Evidence, error) {
					cleaned = true
					return []Evidence{{Key: "deleted", Value: true}}, nil
				},
			}
		},
	}
	opts := optionsForRun()
	opts.Mode = ModeWrite
	opts.AllowWrites = true
	results := executeForTest(t, opts, []Check{check})
	result := resultFor(t, results, "journey.resource")
	if result.Cleanup.Status != CleanupSucceeded {
		t.Fatalf("cleanup status = %q, want succeeded", result.Cleanup.Status)
	}
	if !cleaned {
		t.Fatal("cleanup function must run")
	}
	if len(result.Cleanup.Resources) != 1 || result.Cleanup.Resources[0].Preserved {
		t.Fatalf("resources = %+v, want preserved=false", result.Cleanup.Resources)
	}
}

func TestCleanupFailurePreservesResourcesWithDiagnosticReference(t *testing.T) {
	check := &fakeCheck{
		meta: Metadata{ID: "journey.cleanup_fail", Impact: ImpactExternalWrite},
		outcome: func(context.Context, int) Outcome {
			return Outcome{
				Status:    StatusPassed,
				Resources: []Resource{{Kind: "synthetic-invoice", Reference: "inv_123"}},
				Cleanup: func(context.Context) ([]Evidence, error) {
					return nil, errors.New("delete denied: secret-token=abcdef123456")
				},
			}
		},
	}
	opts := optionsForRun()
	opts.Mode = ModeWrite
	opts.AllowWrites = true
	opts.RunID = "run-abc"
	results := executeForTest(t, opts, []Check{check})
	result := resultFor(t, results, "journey.cleanup_fail")
	if result.Cleanup.Status != CleanupFailed {
		t.Fatalf("cleanup status = %q, want failed", result.Cleanup.Status)
	}
	if len(result.Cleanup.Resources) != 1 || !result.Cleanup.Resources[0].Preserved {
		t.Fatalf("resources = %+v, want preserved", result.Cleanup.Resources)
	}
	wantRef := "verify://cleanup/run-abc/journey.cleanup_fail"
	if result.Cleanup.DiagnosticRef != wantRef {
		t.Fatalf("diagnostic ref = %q, want %q", result.Cleanup.DiagnosticRef, wantRef)
	}
	if strings.Contains(result.Cleanup.Error, "abcdef123456") {
		t.Fatalf("cleanup error leaked secret: %q", result.Cleanup.Error)
	}
	if !strings.Contains(result.Cleanup.Error, "delete denied") {
		t.Fatalf("cleanup error lost sanitized context: %q", result.Cleanup.Error)
	}
}

func TestPanickingProbeBecomesFailedResult(t *testing.T) {
	check := &fakeCheck{
		meta: Metadata{ID: "panic", Impact: ImpactReadOnly},
		outcome: func(context.Context, int) Outcome {
			panic("boom: token=supersecretvalue123456")
		},
	}
	results := executeForTest(t, optionsForRun(), []Check{check})
	result := resultFor(t, results, "panic")
	if result.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "check_panicked" {
		t.Fatalf("error = %+v, want check_panicked", result.Error)
	}
	if strings.Contains(result.Error.Message, "supersecretvalue123456") {
		t.Fatalf("panic message leaked secret: %q", result.Error.Message)
	}
}

func TestCanceledRunMarksPendingChecksBlocked(t *testing.T) {
	release := make(chan struct{})
	blocker := &fakeCheck{
		meta: Metadata{ID: "blocker", Impact: ImpactReadOnly},
		outcome: func(ctx context.Context, _ int) Outcome {
			<-release
			return Outcome{Status: StatusPassed}
		},
	}
	dependent := newCheck("dependent", ImpactReadOnly, "blocker")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []CheckResult, 1)
	errCh := make(chan error, 1)
	go func() {
		results, err := executeChecks(ctx, optionsForRun(), []Check{blocker, dependent})
		done <- results
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case results := <-done:
		if got := resultFor(t, results, "dependent").Status; got != StatusBlocked {
			t.Fatalf("pending dependent status = %q, want blocked", got)
		}
		if got := resultFor(t, results, "blocker").Status; got != StatusBlocked {
			t.Fatalf("interrupted check status = %q, want blocked", got)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("executeChecks did not finish after cancel")
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("canceled run must not error, got %v", err)
	}
}

func TestRunnerRejectsCyclesAndMissingDependencies(t *testing.T) {
	a := newCheck("a", ImpactReadOnly, "b")
	b := newCheck("b", ImpactReadOnly, "a")
	if _, err := executeChecks(context.Background(), optionsForRun(), []Check{a, b}); err == nil {
		t.Fatal("cycle must be rejected")
	}
	orphan := newCheck("orphan", ImpactReadOnly, "missing")
	if _, err := executeChecks(context.Background(), optionsForRun(), []Check{orphan}); err == nil {
		t.Fatal("missing dependency must be rejected")
	}
}

func TestRunHarnessRefusesProductionBeforeAnyProbe(t *testing.T) {
	probeRan := false
	checks := []Check{&fakeCheck{
		meta: Metadata{ID: "x", Impact: ImpactReadOnly},
		outcome: func(context.Context, int) Outcome {
			probeRan = true
			return Outcome{Status: StatusPassed}
		},
	}}
	opts := optionsForRun()
	opts.Environment = "prod"
	_, err := RunHarness(context.Background(), opts, checks)
	var refusal *ProductionRefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("RunHarness error = %v, want ProductionRefusalError", err)
	}
	if probeRan {
		t.Fatal("production refusal must prevent every probe")
	}
}

func TestRunHarnessOverrideEnablesSelectionWithoutProductionExecution(t *testing.T) {
	probeRan := false
	checks := []Check{&fakeCheck{
		meta: Metadata{ID: "x", Impact: ImpactReadOnly},
		outcome: func(context.Context, int) Outcome {
			probeRan = true
			return Outcome{Status: StatusPassed}
		},
	}}
	opts := optionsForRun()
	opts.Environment = "prod"
	opts.AllowProductionOverride = true
	report, err := RunHarness(context.Background(), opts, checks)
	if err != nil {
		t.Fatalf("RunHarness with override: %v", err)
	}
	if !probeRan {
		t.Fatal("override must allow selection; injected probe should run")
	}
	if report.Environment != "production" || !report.AllowProductionOverride {
		t.Fatalf("report must record override: %+v", report)
	}
}

func TestRunHarnessReportIsRedactedAndDeterministic(t *testing.T) {
	build := func() *Report {
		checks := []Check{&fakeCheck{
			meta: Metadata{ID: "leaky", Impact: ImpactReadOnly},
			outcome: func(context.Context, int) Outcome {
				return Outcome{
					Status:  StatusFailed,
					Err:     errors.New("upstream said password=hunter2secret"),
					ErrCode: "leak_probe",
					Evidence: []Evidence{
						{Key: "provider_payload", Value: map[string]any{"access_token": "tok_123", "count": 2}},
					},
				}
			},
		}}
		opts := optionsForRun()
		opts.RunID = "deterministic-run"
		opts.Now = func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }
		opts.Sleep = func(context.Context, time.Duration) error { return nil }
		report, err := RunHarness(context.Background(), opts, checks)
		if err != nil {
			t.Fatalf("RunHarness: %v", err)
		}
		return report
	}
	first, err := json.Marshal(build())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := json.Marshal(build())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("report not deterministic:\n%s\n%s", first, second)
	}
	output := string(first)
	for _, leaked := range []string{"hunter2secret", "tok_123"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("report leaked %q: %s", leaked, output)
		}
	}
	if !strings.Contains(output, `"access_token":"[REDACTED]"`) {
		t.Fatalf("provider payload key must be redacted: %s", output)
	}
}
