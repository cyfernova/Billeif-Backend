package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceCreateFailsClosedWhenVoiceAdmissionIsDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.AdmissionEnabled = false
	cfg.RolloutStage = "100"
	store := newMemoryStore()
	svc := NewService(store, &stopRecorder{}, cfg, allowServiceOptions(ServiceOptions{}))

	_, err := svc.Create(context.Background(), Scope{UserID: "user-2", BusinessID: "biz-a", AllBranches: true}, validCreateInput())
	if !errors.Is(err, ErrRolloutDenied) {
		t.Fatalf("disabled admission must fail closed, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("denied rollout must not consume capacity, got %d store calls", store.createCalls)
	}
}

func TestServiceCreateUsesHashedCognitoSubjectForInternalRollout(t *testing.T) {
	cfg := testConfig()
	cfg.AdmissionEnabled = true
	cfg.RolloutStage = "internal"
	cfg.RolloutInternalSubjectHashes = []string{
		"sha256:a82dbfadf4306ff5b431dd63bb680db4f6f00cd92583e349717838e2fb70fee0",
	}

	allowed := NewService(newMemoryStore(), &stopRecorder{}, cfg, allowServiceOptions(ServiceOptions{}))
	if _, err := allowed.Create(context.Background(), Scope{UserID: "internal-user", BusinessID: "biz-a", AllBranches: true}, validCreateInput()); err != nil {
		t.Fatalf("hashed internal subject must be admitted: %v", err)
	}

	store := newMemoryStore()
	denied := NewService(store, &stopRecorder{}, cfg, allowServiceOptions(ServiceOptions{}))
	if _, err := denied.Create(context.Background(), Scope{UserID: "other-user", BusinessID: "biz-a", AllBranches: true}, validCreateInput()); !errors.Is(err, ErrRolloutDenied) {
		t.Fatalf("subject absent from the hashed allowlist must be denied, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("denied internal subject must not consume capacity, got %d store calls", store.createCalls)
	}
}

func TestInternalRolloutHashesTheCanonicalPhonePoolIdentity(t *testing.T) {
	const canonicalPhoneIdentity = "ap-south-1_voicephone:phone-subject"
	cfg := testConfig()
	cfg.AdmissionEnabled = true
	cfg.RolloutStage = "internal"
	// Phone-pool authentication deliberately canonicalizes the identity as
	// <user-pool-id>:<sub>; hashing only the token's raw sub must not match.
	cfg.RolloutInternalSubjectHashes = []string{
		"sha256:0ad9e3b4da8d5b90add32509089baad50ad4f11e147a4b1fdfc25fdce75911d6",
	}
	if _, err := NewService(newMemoryStore(), &stopRecorder{}, cfg, allowServiceOptions(ServiceOptions{})).Create(
		context.Background(), Scope{UserID: canonicalPhoneIdentity, BusinessID: "biz-a", AllBranches: true}, validCreateInput(),
	); !errors.Is(err, ErrRolloutDenied) {
		t.Fatalf("a raw-sub hash must not admit a canonical phone-pool identity, got %v", err)
	}

	cfg.RolloutInternalSubjectHashes = []string{
		"sha256:47259dbd72f85b990fa440bfc7cbba646818319880eb99b5ef419a13c7c94d92",
	}
	if _, err := NewService(newMemoryStore(), &stopRecorder{}, cfg, allowServiceOptions(ServiceOptions{})).Create(
		context.Background(), Scope{UserID: canonicalPhoneIdentity, BusinessID: "biz-a", AllBranches: true}, validCreateInput(),
	); err != nil {
		t.Fatalf("the canonical phone-pool identity hash must be admitted: %v", err)
	}
}

func TestVoiceRolloutPercentageUsesStableUserAndBusinessBucket(t *testing.T) {
	tests := []struct {
		name      string
		stage     string
		userID    string
		business  string
		wantAllow bool
	}{
		{name: "five percent includes bucket one", stage: "5", userID: "user-2", business: "biz-a", wantAllow: true},
		{name: "five percent excludes boundary bucket five", stage: "5", userID: "user-6", business: "biz-a", wantAllow: false},
		{name: "twenty five percent includes bucket five", stage: "25", userID: "user-6", business: "biz-a", wantAllow: true},
		{name: "fifty percent excludes bucket ninety six", stage: "50", userID: "user-a", business: "biz-a", wantAllow: false},
		{name: "one hundred percent includes every valid scope", stage: "100", userID: "user-a", business: "biz-a", wantAllow: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.AdmissionEnabled = true
			cfg.RolloutStage = tc.stage
			store := newMemoryStore()
			svc := NewService(store, &stopRecorder{}, cfg, allowServiceOptions(ServiceOptions{}))

			_, err := svc.Create(context.Background(), Scope{UserID: tc.userID, BusinessID: tc.business, AllBranches: true}, validCreateInput())
			if tc.wantAllow && err != nil {
				t.Fatalf("expected admitted scope, got %v", err)
			}
			if !tc.wantAllow && !errors.Is(err, ErrRolloutDenied) {
				t.Fatalf("expected rollout denial, got %v", err)
			}
		})
	}
}

func TestServiceResumeHonorsClosedRolloutButCloseRemainsAvailable(t *testing.T) {
	store := newMemoryStore()
	cfg := testConfig()
	cfg.AdmissionEnabled = false
	cfg.RolloutStage = "disabled"
	svc := NewService(store, &stopRecorder{}, cfg, allowServiceOptions(ServiceOptions{}))
	scope := Scope{UserID: "user-2", BusinessID: "biz-a", AllBranches: true}
	value := existingResumableSession(svc.now(), scope)
	store.sessions[value.ID] = value

	metadata, err := svc.Get(context.Background(), scope, value.ID)
	if err != nil {
		t.Fatalf("disabled rollout must preserve session lookup: %v", err)
	}
	if metadata.Resumable {
		t.Fatal("disabled rollout must not advertise a session as resumable")
	}

	if _, err := svc.Resume(context.Background(), scope, value.ID); !errors.Is(err, ErrRolloutDenied) {
		t.Fatalf("disabled rollout must reject new attachment, got %v", err)
	}
	if err := svc.Close(context.Background(), scope, value.ID); err != nil {
		t.Fatalf("rollout disablement must preserve cleanup: %v", err)
	}
}

func existingResumableSession(now time.Time, scope Scope) *Session {
	return &Session{
		ID: "voice_rollout", RuntimeSessionID: "voice-session-rollout-1234567890123456",
		UserID: scope.UserID, BusinessID: scope.BusinessID,
		Status: StatusActive, RuntimeState: RuntimeStateRunning,
		ExpiresAt: now.Add(time.Hour), LeaseExpiresAt: now.Add(time.Minute),
	}
}
