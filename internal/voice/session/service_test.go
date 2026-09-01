package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryStore struct {
	sessions        map[string]*Session
	idempotency     map[string]CreateRecord
	createCalls     int
	resumeCalls     int
	releaseCalls    int
	markClosedCalls int
	beforeResume    func(*Session)
	markClosedErr   error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{sessions: map[string]*Session{}, idempotency: map[string]CreateRecord{}}
}

func (s *memoryStore) Create(_ context.Context, record CreateRecord) (*Session, bool, error) {
	s.createCalls++
	key := record.Session.UserID + "|" + record.IdempotencyKey
	if existing, ok := s.idempotency[key]; ok {
		if existing.RequestHash != record.RequestHash {
			return nil, false, ErrIdempotencyConflict
		}
		return cloneSession(s.sessions[existing.Session.ID]), false, nil
	}
	s.idempotency[key] = record
	s.sessions[record.Session.ID] = cloneSession(record.Session)
	return cloneSession(record.Session), true, nil
}

func (s *memoryStore) Get(_ context.Context, scope Scope, sessionID string) (*Session, error) {
	stored, ok := s.sessions[sessionID]
	if !ok || stored.UserID != scope.UserID || stored.BusinessID != scope.BusinessID || !scopeAllowsBranch(scope, stored.BranchID) {
		return nil, ErrNotFound
	}
	return cloneSession(stored), nil
}

func (s *memoryStore) Resume(_ context.Context, input ResumeRecord) (*Session, error) {
	s.resumeCalls++
	stored, ok := s.sessions[input.SessionID]
	if !ok || stored.UserID != input.Scope.UserID || stored.BusinessID != input.Scope.BusinessID || !scopeAllowsBranch(input.Scope, stored.BranchID) {
		return nil, ErrNotFound
	}
	if s.beforeResume != nil {
		s.beforeResume(stored)
	}
	if stored.BranchID != input.ExpectedBranchID || stored.Status != StatusActive || !stored.ExpiresAt.After(input.UpdatedAt) || !stored.LeaseExpiresAt.After(input.UpdatedAt) || !stored.LeaseExpiresAt.Equal(input.ExpectedLeaseExpiresAt) {
		return nil, ErrNotResumable
	}
	if stored.RuntimeState != input.ExpectedRuntimeState {
		return nil, ErrNotResumable
	}
	stored.LeaseExpiresAt = input.LeaseExpiresAt
	stored.UpdatedAt = input.UpdatedAt
	if input.NewRuntimeSessionID != "" {
		stored.RuntimeSessionID = input.NewRuntimeSessionID
		stored.RuntimeState = RuntimeStateRunning
	}
	return cloneSession(stored), nil
}

func (s *memoryStore) Release(_ context.Context, scope Scope, sessionID string, now time.Time) (*Session, ReleaseState, error) {
	s.releaseCalls++
	stored, ok := s.sessions[sessionID]
	if !ok || stored.UserID != scope.UserID || stored.BusinessID != scope.BusinessID || !scopeAllowsBranch(scope, stored.BranchID) {
		return nil, ReleaseState{}, ErrNotFound
	}
	if stored.Status == StatusClosed {
		return cloneSession(stored), ReleaseState{AlreadyClosed: true}, nil
	}
	if stored.Status == StatusActive {
		stored.Status = StatusClosing
		stored.CapacityReleased = true
		stored.UpdatedAt = now
		return cloneSession(stored), ReleaseState{Released: true, ShouldStop: true}, nil
	}
	if stored.Status == StatusClosing {
		return cloneSession(stored), ReleaseState{ShouldStop: true}, nil
	}
	return nil, ReleaseState{}, ErrNotResumable
}

func (s *memoryStore) MarkClosed(_ context.Context, scope Scope, sessionID, expectedBranchID string, now time.Time) error {
	s.markClosedCalls++
	if s.markClosedErr != nil {
		return s.markClosedErr
	}
	stored, ok := s.sessions[sessionID]
	if !ok || stored.UserID != scope.UserID || stored.BusinessID != scope.BusinessID || !scopeAllowsBranch(scope, stored.BranchID) || stored.BranchID != expectedBranchID {
		return ErrNotFound
	}
	stored.Status = StatusClosed
	stored.ClosedAt = &now
	stored.UpdatedAt = now
	return nil
}

func (s *memoryStore) ExpiredLeases(context.Context, time.Time, int32) ([]*Session, error) {
	return nil, nil
}

type stopRecorder struct {
	calls []RuntimeTarget
	err   error
}

func (s *stopRecorder) StopRuntimeSession(_ context.Context, target RuntimeTarget) error {
	s.calls = append(s.calls, target)
	return s.err
}

func TestServiceCreateIsIdempotentAndValidatesContract(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 6, 7, 0, 0, 0, time.UTC)
	svc := NewService(store, &stopRecorder{}, testConfig(), ServiceOptions{
		Now:     func() time.Time { return now },
		NewULID: func() string { return "01K1ABCDE2FGHIJK3LMNOPQRST" },
	})
	input := validCreateInput()
	identity := Scope{UserID: "user-1", BusinessID: "business-1", AllBranches: true}

	created, err := svc.Create(context.Background(), identity, input)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if created.ID != "voice_01K1ABCDE2FGHIJK3LMNOPQRST" {
		t.Fatalf("unexpected logical id %q", created.ID)
	}
	if got := len(created.RuntimeSessionID); got < 33 || got > 256 {
		t.Fatalf("runtime id length %d outside AgentCore bounds", got)
	}
	if created.KVSChannelIndex < 0 || created.KVSChannelIndex >= 12 {
		t.Fatalf("invalid KVS channel index %d", created.KVSChannelIndex)
	}
	if created.ExpiresAt != now.Add(55*time.Minute) || created.RotateAt != now.Add(52*time.Minute) {
		t.Fatalf("unexpected expiry/rotation: %s %s", created.ExpiresAt, created.RotateAt)
	}

	retried, err := svc.Create(context.Background(), identity, input)
	if err != nil {
		t.Fatalf("retry create: %v", err)
	}
	if retried.ID != created.ID || retried.RuntimeSessionID != created.RuntimeSessionID {
		t.Fatalf("idempotent retry returned different session: %#v", retried)
	}

	conflict := input
	conflict.PreferredLanguage = "hi-IN"
	if _, err := svc.Create(context.Background(), identity, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestServiceCreateRejectsCapabilityGuardBeforeAdmission(t *testing.T) {
	store := newMemoryStore()
	guardErr := errors.New("voice capability unavailable")
	svc := NewService(store, &stopRecorder{}, testConfig(), ServiceOptions{
		CreateGuard: func(_ context.Context, scope Scope, input CreateInput) error {
			if scope.BusinessID != "business-1" || scope.UserID != "user-1" || input.Client.Platform != "ios" {
				t.Fatalf("unexpected guarded create: scope=%#v input=%#v", scope, input)
			}
			return guardErr
		},
	})

	_, err := svc.Create(context.Background(), Scope{UserID: "user-1", BusinessID: "business-1", AllBranches: true}, validCreateInput())

	if !errors.Is(err, guardErr) {
		t.Fatalf("create error = %v, want guard error", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("store create calls = %d, want zero", store.createCalls)
	}
}

func TestServiceCreateRejectsInvalidConsentAndLanguage(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CreateInput)
	}{
		{name: "audio recording true", mutate: func(in *CreateInput) { value := true; in.Consent.AudioRecording = &value }},
		{name: "missing transcript consent", mutate: func(in *CreateInput) { in.Consent.TranscriptStorage = nil }},
		{name: "unsupported language", mutate: func(in *CreateInput) { in.PreferredLanguage = "fr-FR" }},
		{name: "bad protocol", mutate: func(in *CreateInput) { in.Client.ProtocolVersion = 2 }},
		{name: "bad platform", mutate: func(in *CreateInput) { in.Client.Platform = "web" }},
		{name: "bad idempotency", mutate: func(in *CreateInput) { in.IdempotencyKey = "not-a-uuid" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := validCreateInput()
			tc.mutate(&input)
			svc := NewService(newMemoryStore(), &stopRecorder{}, testConfig(), ServiceOptions{})
			if _, err := svc.Create(context.Background(), Scope{UserID: "u", BusinessID: "b", AllBranches: true}, input); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected invalid request, got %v", err)
			}
		})
	}
}

func TestServiceResumeRotatesOnlyStoppedRuntimeWithoutAdmission(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	active := &Session{
		ID: "voice_existing", RuntimeSessionID: "voice-session-existing-1234567890123456",
		UserID: "u", BusinessID: "b", Status: StatusActive, RuntimeState: RuntimeStateRunning,
		ExpiresAt: now.Add(time.Hour), LeaseExpiresAt: now.Add(time.Minute),
	}
	store.sessions[active.ID] = active
	svc := NewService(store, &stopRecorder{}, testConfig(), ServiceOptions{Now: func() time.Time { return now }, NewULID: func() string { return "01K1ABCDE2FGHIJK3LMNOPQRST" }})
	originalRuntimeID := active.RuntimeSessionID

	resumed, err := svc.Resume(context.Background(), Scope{UserID: "u", BusinessID: "b", AllBranches: true}, active.ID)
	if err != nil {
		t.Fatalf("resume active: %v", err)
	}
	if resumed.RuntimeSessionID != originalRuntimeID {
		t.Fatalf("running runtime must be reused, got %q", resumed.RuntimeSessionID)
	}
	if store.createCalls != 0 || store.resumeCalls != 1 {
		t.Fatalf("resume must not touch admission: create=%d resume=%d", store.createCalls, store.resumeCalls)
	}

	store.sessions[active.ID].RuntimeState = RuntimeStateStopped
	rotated, err := svc.Resume(context.Background(), Scope{UserID: "u", BusinessID: "b", AllBranches: true}, active.ID)
	if err != nil {
		t.Fatalf("resume stopped: %v", err)
	}
	if rotated.RuntimeSessionID == originalRuntimeID || len(rotated.RuntimeSessionID) < 33 {
		t.Fatalf("stopped runtime must rotate to a valid id, got %q", rotated.RuntimeSessionID)
	}
	if store.createCalls != 0 {
		t.Fatal("rotation must not increment admission")
	}
}

func TestServiceResumeRejectsLeaseChangedAfterRead(t *testing.T) {
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	stored := &Session{
		ID: "voice_race", RuntimeSessionID: "voice-session-race-123456789012345678",
		UserID: "u", BusinessID: "b", Status: StatusActive, RuntimeState: RuntimeStateRunning,
		ExpiresAt: now.Add(time.Hour), LeaseExpiresAt: now.Add(time.Minute),
	}
	store.sessions[stored.ID] = stored
	store.beforeResume = func(current *Session) {
		current.LeaseExpiresAt = now.Add(-time.Nanosecond)
	}
	svc := NewService(store, &stopRecorder{}, testConfig(), ServiceOptions{Now: func() time.Time { return now }})

	if _, err := svc.Resume(context.Background(), Scope{UserID: "u", BusinessID: "b", AllBranches: true}, stored.ID); !errors.Is(err, ErrNotResumable) {
		t.Fatalf("renewed/reconciled lease race must reject resume, got %v", err)
	}
}

func TestServiceCloseReleasesOnceStopsExactRuntimeAndIsIdempotent(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	store.sessions["voice_one"] = &Session{
		ID: "voice_one", RuntimeSessionID: "voice-session-one-12345678901234567890",
		UserID: "u", BusinessID: "b", Status: StatusActive, RuntimeState: RuntimeStateRunning,
		ExpiresAt: now.Add(time.Hour),
	}
	stopper := &stopRecorder{}
	svc := NewService(store, stopper, testConfig(), ServiceOptions{Now: func() time.Time { return now }})
	scope := Scope{UserID: "u", BusinessID: "b", AllBranches: true}

	if err := svc.Close(context.Background(), scope, "voice_one"); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(stopper.calls) != 1 {
		t.Fatalf("expected one stop, got %d", len(stopper.calls))
	}
	want := RuntimeTarget{AgentRuntimeARN: testConfig().AgentRuntimeARN, RuntimeSessionID: "voice-session-one-12345678901234567890", Qualifier: "PROD"}
	if stopper.calls[0] != want {
		t.Fatalf("unexpected stop target: %#v", stopper.calls[0])
	}
	if !store.sessions["voice_one"].CapacityReleased || store.sessions["voice_one"].Status != StatusClosed {
		t.Fatalf("session not closed/released: %#v", store.sessions["voice_one"])
	}

	if err := svc.Close(context.Background(), scope, "voice_one"); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if len(stopper.calls) != 1 {
		t.Fatalf("already closed session was stopped again: %d", len(stopper.calls))
	}
}

func TestServiceCloseFailuresLeaveClosingSessionRetryable(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	newSession := func(id string) *Session {
		return &Session{
			ID: id, RuntimeSessionID: "voice-session-retry-12345678901234567",
			UserID: "u", BusinessID: "b", Status: StatusActive, RuntimeState: RuntimeStateRunning,
			LeaseExpiresAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour),
		}
	}
	scope := Scope{UserID: "u", BusinessID: "b", AllBranches: true}

	t.Run("runtime stop failure", func(t *testing.T) {
		store := newMemoryStore()
		store.sessions["voice_stop_retry"] = newSession("voice_stop_retry")
		stopper := &stopRecorder{err: errors.New("transient stop failure")}
		svc := NewService(store, stopper, testConfig(), ServiceOptions{Now: func() time.Time { return now }})

		if err := svc.Close(context.Background(), scope, "voice_stop_retry"); err == nil {
			t.Fatal("expected stop failure")
		}
		if got := store.sessions["voice_stop_retry"]; got.Status != StatusClosing || !got.CapacityReleased {
			t.Fatalf("stop failure lost retryable closing state: %#v", got)
		}
		stopper.err = nil
		if err := svc.Close(context.Background(), scope, "voice_stop_retry"); err != nil {
			t.Fatalf("retry close: %v", err)
		}
		if store.sessions["voice_stop_retry"].Status != StatusClosed {
			t.Fatalf("retry did not terminally close: %#v", store.sessions["voice_stop_retry"])
		}
	})

	t.Run("terminal write failure", func(t *testing.T) {
		store := newMemoryStore()
		store.sessions["voice_mark_retry"] = newSession("voice_mark_retry")
		store.markClosedErr = errors.New("transient mark failure")
		stopper := &stopRecorder{}
		svc := NewService(store, stopper, testConfig(), ServiceOptions{Now: func() time.Time { return now }})

		if err := svc.Close(context.Background(), scope, "voice_mark_retry"); err == nil {
			t.Fatal("expected mark-closed failure")
		}
		if got := store.sessions["voice_mark_retry"]; got.Status != StatusClosing || !got.CapacityReleased {
			t.Fatalf("terminal write failure lost retryable closing state: %#v", got)
		}
		store.markClosedErr = nil
		if err := svc.Close(context.Background(), scope, "voice_mark_retry"); err != nil {
			t.Fatalf("retry close: %v", err)
		}
		if len(stopper.calls) != 2 || store.sessions["voice_mark_retry"].Status != StatusClosed {
			t.Fatalf("retry did not stop and close: calls=%d session=%#v", len(stopper.calls), store.sessions["voice_mark_retry"])
		}
	})
}

func TestServiceDoesNotLeakForeignSession(t *testing.T) {
	store := newMemoryStore()
	store.sessions["voice_private"] = &Session{ID: "voice_private", UserID: "owner", BusinessID: "biz", Status: StatusActive, ExpiresAt: time.Now().Add(time.Hour)}
	svc := NewService(store, &stopRecorder{}, testConfig(), ServiceOptions{})
	if _, err := svc.Get(context.Background(), Scope{UserID: "stranger", BusinessID: "biz", AllBranches: true}, "voice_private"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign session should look absent, got %v", err)
	}
}

func TestBranchlessScopeIsExactAndAllBranchScopeStillAdmitsIt(t *testing.T) {
	exact := Scope{UserID: "user", BusinessID: "business", AllowBranchless: true}
	if !scopeAllowsBranch(exact, "") {
		t.Fatal("exact branchless scope did not admit a branchless session")
	}
	if scopeAllowsBranch(exact, "cbd6e793-62e6-4c32-a106-065709caf460") {
		t.Fatal("exact branchless scope broadened into branch access")
	}
	if !scopeAllowsBranch(Scope{UserID: "user", BusinessID: "business", AllBranches: true}, "") {
		t.Fatal("current all-branch HTTP scope must continue to admit branchless sessions")
	}
}

func testConfig() Config {
	return Config{
		TableName: "voice-sessions", AgentRuntimeARN: "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/test",
		AgentRuntimeQualifier: "PROD", AdmissionEnabled: true, RolloutStage: "100", ProtocolVersion: 1, KVSChannelCount: 12,
		MaxDuration: 55 * time.Minute, RotateAfter: 52 * time.Minute, LeaseDuration: 2 * time.Minute,
	}
}

func validCreateInput() CreateInput {
	transcript := true
	audio := false
	return CreateInput{
		BranchID: "", IdempotencyKey: "35e046c7-23f4-4c8d-b79d-581229de44ad",
		PreferredLanguage: "en-IN", FallbackLanguage: "en-IN",
		Consent: ConsentInput{TranscriptStorage: &transcript, AudioRecording: &audio, PolicyVersion: "2026-08-01"},
		Client:  ClientInput{Platform: "ios", AppVersion: "1.0.0", ProtocolVersion: 1},
	}
}
