package reconciler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/voice/session"
)

func TestHandlerReleasesExpiredActiveLeaseStopsRuntimeAndMarksClosed(t *testing.T) {
	now := time.Date(2026, 8, 7, 5, 0, 0, 0, time.UTC)
	candidate := reconcilerSession("voice_expired", session.StatusActive, false, now.Add(-time.Minute))
	released := cloneReconcilerSession(candidate)
	released.Status = session.StatusClosing
	released.CapacityReleased = true
	store := &fakeLeaseStore{
		candidates: []*session.Session{candidate},
		releases: map[string]releaseResult{
			candidate.ID: {value: released, state: session.ReleaseState{Released: true, ShouldStop: true}},
		},
	}
	stopper := &fakeRuntimeStopper{}
	handler := newTestHandler(t, store, stopper, now)

	result, err := handler.Handle(context.Background())
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if result != (Result{Examined: 1, Released: 1, Stopped: 1, Closed: 1}) {
		t.Fatalf("wrong result: %#v", result)
	}
	if len(stopper.targets) != 1 || stopper.targets[0].RuntimeSessionID != candidate.RuntimeSessionID ||
		stopper.targets[0].AgentRuntimeARN != "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/voice" ||
		stopper.targets[0].Qualifier != "PROD" {
		t.Fatalf("wrong stop target: %#v", stopper.targets)
	}
	if len(store.closed) != 1 || store.closed[0].sessionID != candidate.ID || store.closed[0].branchID != candidate.BranchID {
		t.Fatalf("session not terminally closed: %#v", store.closed)
	}
}

func TestHandlerRetriesAlreadyReleasedCapacityWithoutDecrementingAgain(t *testing.T) {
	now := time.Date(2026, 8, 7, 5, 0, 0, 0, time.UTC)
	closing := reconcilerSession("voice_closing", session.StatusClosing, true, now.Add(-time.Minute))
	store := &fakeLeaseStore{
		candidates: []*session.Session{closing},
		releases: map[string]releaseResult{
			closing.ID: {value: closing, state: session.ReleaseState{ShouldStop: true}},
		},
	}
	stopper := &fakeRuntimeStopper{}
	handler := newTestHandler(t, store, stopper, now)

	result, err := handler.Handle(context.Background())
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if result != (Result{Examined: 1, Stopped: 1, Closed: 1}) {
		t.Fatalf("already released capacity counted twice: %#v", result)
	}
	if len(stopper.targets) != 1 || len(store.closed) != 1 {
		t.Fatalf("stop retry did not converge: targets=%#v closed=%#v", stopper.targets, store.closed)
	}
}

func TestHandlerSkipsRenewedLeaseAndClosedSession(t *testing.T) {
	now := time.Date(2026, 8, 7, 5, 0, 0, 0, time.UTC)
	renewed := reconcilerSession("voice_renewed", session.StatusActive, false, now.Add(time.Minute))
	closed := reconcilerSession("voice_closed", session.StatusClosed, true, now.Add(-time.Minute))
	store := &fakeLeaseStore{
		candidates: []*session.Session{renewed, closed},
		releases: map[string]releaseResult{
			renewed.ID: {value: renewed, state: session.ReleaseState{LeaseRenewed: true}},
			closed.ID:  {value: closed, state: session.ReleaseState{AlreadyClosed: true}},
		},
	}
	stopper := &fakeRuntimeStopper{}
	handler := newTestHandler(t, store, stopper, now)

	result, err := handler.Handle(context.Background())
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if result != (Result{Examined: 2, Skipped: 2}) {
		t.Fatalf("wrong skip result: %#v", result)
	}
	if len(stopper.targets) != 0 || len(store.closed) != 0 {
		t.Fatal("renewed or closed session reached stop path")
	}
}

func TestHandlerContinuesAfterPartialBatchErrorsAndReturnsAggregateFailure(t *testing.T) {
	now := time.Date(2026, 8, 7, 5, 0, 0, 0, time.UTC)
	first := reconcilerSession("voice_first", session.StatusActive, false, now.Add(-time.Minute))
	broken := reconcilerSession("voice_broken", session.StatusActive, false, now.Add(-time.Minute))
	last := reconcilerSession("voice_last", session.StatusClosing, true, now.Add(-time.Minute))
	firstReleased := cloneReconcilerSession(first)
	firstReleased.Status = session.StatusClosing
	firstReleased.CapacityReleased = true
	store := &fakeLeaseStore{
		candidates: []*session.Session{first, broken, last},
		releases: map[string]releaseResult{
			first.ID:  {value: firstReleased, state: session.ReleaseState{Released: true, ShouldStop: true}},
			broken.ID: {err: errors.New("conditional transition failed")},
			last.ID:   {value: last, state: session.ReleaseState{ShouldStop: true}},
		},
	}
	stopper := &fakeRuntimeStopper{}
	handler := newTestHandler(t, store, stopper, now)

	result, err := handler.Handle(context.Background())
	if err == nil {
		t.Fatal("partial batch failure was hidden")
	}
	if result != (Result{Examined: 3, Released: 1, Stopped: 2, Closed: 2, Failed: 1}) {
		t.Fatalf("wrong partial result: %#v", result)
	}
	if len(stopper.targets) != 2 || len(store.closed) != 2 {
		t.Fatalf("later candidates were not processed: targets=%d closed=%d", len(stopper.targets), len(store.closed))
	}
}

func TestHandlerLeavesReleasedLeaseDiscoverableWhenStopOrCloseFails(t *testing.T) {
	now := time.Date(2026, 8, 7, 5, 0, 0, 0, time.UTC)
	closing := reconcilerSession("voice_retry", session.StatusClosing, true, now.Add(-time.Minute))

	t.Run("stop failure", func(t *testing.T) {
		store := &fakeLeaseStore{
			candidates: []*session.Session{closing},
			releases:   map[string]releaseResult{closing.ID: {value: closing, state: session.ReleaseState{ShouldStop: true}}},
		}
		stopper := &fakeRuntimeStopper{err: errors.New("runtime unavailable")}
		handler := newTestHandler(t, store, stopper, now)
		result, err := handler.Handle(context.Background())
		if err == nil || result.Failed != 1 || result.Closed != 0 || len(store.closed) != 0 {
			t.Fatalf("stop failure incorrectly closed session result=%#v err=%v", result, err)
		}
	})

	t.Run("mark closed failure", func(t *testing.T) {
		store := &fakeLeaseStore{
			candidates: []*session.Session{closing},
			releases:   map[string]releaseResult{closing.ID: {value: closing, state: session.ReleaseState{ShouldStop: true}}},
			markErr:    errors.New("conditional close failed"),
		}
		stopper := &fakeRuntimeStopper{}
		handler := newTestHandler(t, store, stopper, now)
		result, err := handler.Handle(context.Background())
		if err == nil || result.Failed != 1 || result.Stopped != 1 || result.Closed != 0 {
			t.Fatalf("mark failure incorrectly converged result=%#v err=%v", result, err)
		}
	})
}

func TestHandlerEmitsRecoveredSessionLeaksWithoutChangingReconciliation(t *testing.T) {
	now := time.Date(2026, 8, 7, 5, 0, 0, 0, time.UTC)
	candidate := reconcilerSession("voice_metric", session.StatusActive, false, now.Add(-time.Minute))
	released := cloneReconcilerSession(candidate)
	released.Status = session.StatusClosing
	released.CapacityReleased = true
	store := &fakeLeaseStore{
		candidates: []*session.Session{candidate},
		releases: map[string]releaseResult{
			candidate.ID: {value: released, state: session.ReleaseState{Released: true, ShouldStop: true}},
		},
	}
	metrics := &fakeLeaseMetrics{}
	handler, err := New(Options{
		Store: store, Stopper: &fakeRuntimeStopper{}, Metrics: metrics,
		AgentRuntimeARN:       "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/voice",
		AgentRuntimeQualifier: "PROD", BatchSize: 10, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, err := handler.Handle(t.Context())
	if err != nil || result.Released != 1 || metrics.count() != 1 {
		t.Fatalf("reconcile with metrics = result %#v err %v leaks %d", result, err, metrics.count())
	}

	panicHandler, err := New(Options{
		Store: store, Stopper: &fakeRuntimeStopper{}, Metrics: panicLeaseMetrics{},
		AgentRuntimeARN:       "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/voice",
		AgentRuntimeQualifier: "PROD", BatchSize: 10, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New(panic metrics) error = %v", err)
	}
	if result, err := panicHandler.Handle(t.Context()); err != nil || result.Released != 1 {
		t.Fatalf("metrics panic changed reconciliation: result %#v err %v", result, err)
	}
}

func newTestHandler(t *testing.T, store LeaseStore, stopper session.RuntimeStopper, now time.Time) *Handler {
	t.Helper()
	handler, err := New(Options{
		Store: store, Stopper: stopper,
		AgentRuntimeARN:       "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/voice",
		AgentRuntimeQualifier: "PROD", BatchSize: 10,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return handler
}

func reconcilerSession(id string, status session.Status, released bool, lease time.Time) *session.Session {
	return &session.Session{
		ID: id, RuntimeSessionID: "voice-session-" + id,
		UserID: "user-1", BusinessID: "business-1", BranchID: "cbd6e793-62e6-4c32-a106-065709caf460",
		Status: status, RuntimeState: session.RuntimeStateRunning,
		LeaseExpiresAt: lease, ExpiresAt: lease.Add(time.Hour), CapacityReleased: released,
	}
}

func cloneReconcilerSession(value *session.Session) *session.Session {
	cloned := *value
	return &cloned
}

type releaseResult struct {
	value *session.Session
	state session.ReleaseState
	err   error
}

type closedCall struct {
	sessionID string
	branchID  string
}

type fakeLeaseStore struct {
	candidates []*session.Session
	queryErr   error
	releases   map[string]releaseResult
	markErr    error

	mu      sync.Mutex
	closed  []closedCall
	cutoffs []time.Time
	limits  []int32
}

func (store *fakeLeaseStore) ExpiredLeases(_ context.Context, cutoff time.Time, limit int32) ([]*session.Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.cutoffs = append(store.cutoffs, cutoff)
	store.limits = append(store.limits, limit)
	return append([]*session.Session(nil), store.candidates...), store.queryErr
}

func (store *fakeLeaseStore) ReleaseExpired(_ context.Context, candidate *session.Session, _ time.Time) (*session.Session, session.ReleaseState, error) {
	result, ok := store.releases[candidate.ID]
	if !ok {
		return nil, session.ReleaseState{}, errors.New("unexpected release candidate")
	}
	return result.value, result.state, result.err
}

func (store *fakeLeaseStore) MarkClosed(_ context.Context, _ session.Scope, sessionID, branchID string, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.closed = append(store.closed, closedCall{sessionID: sessionID, branchID: branchID})
	return store.markErr
}

type fakeRuntimeStopper struct {
	mu      sync.Mutex
	targets []session.RuntimeTarget
	err     error
}

type fakeLeaseMetrics struct {
	mu    sync.Mutex
	leaks int
}

func (metrics *fakeLeaseMetrics) SessionLeaksRecovered(value int) {
	metrics.mu.Lock()
	metrics.leaks += value
	metrics.mu.Unlock()
}

func (metrics *fakeLeaseMetrics) count() int {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	return metrics.leaks
}

type panicLeaseMetrics struct{}

func (panicLeaseMetrics) SessionLeaksRecovered(int) { panic("metrics must fail open") }

func (stopper *fakeRuntimeStopper) StopRuntimeSession(_ context.Context, target session.RuntimeTarget) error {
	stopper.mu.Lock()
	defer stopper.mu.Unlock()
	stopper.targets = append(stopper.targets, target)
	return stopper.err
}
