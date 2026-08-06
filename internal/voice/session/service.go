package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	appVersionPattern    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	policyVersionPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

type ServiceOptions struct {
	Now     func() time.Time
	NewULID func() string
}

type Service struct {
	store   Store
	stopper RuntimeStopper
	config  Config
	now     func() time.Time
	newULID func() string
}

func NewService(store Store, stopper RuntimeStopper, cfg Config, options ServiceOptions) *Service {
	cfg = normalizeConfig(cfg)
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	newULID := options.NewULID
	if newULID == nil {
		newULID = func() string {
			id, err := generateULID(now())
			if err != nil {
				return ""
			}
			return id
		}
	}
	return &Service{store: store, stopper: stopper, config: cfg, now: now, newULID: newULID}
}

func normalizeConfig(cfg Config) Config {
	if cfg.AgentRuntimeQualifier == "" {
		cfg.AgentRuntimeQualifier = "PROD"
	}
	if cfg.ProtocolVersion == 0 {
		cfg.ProtocolVersion = 1
	}
	if cfg.KVSChannelCount == 0 {
		cfg.KVSChannelCount = 12
	}
	if cfg.MaxDuration == 0 {
		cfg.MaxDuration = 55 * time.Minute
	}
	if cfg.RotateAfter == 0 {
		cfg.RotateAfter = 52 * time.Minute
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = 2 * time.Minute
	}
	if cfg.IdempotencyTTL == 0 {
		cfg.IdempotencyTTL = 24 * time.Hour
	}
	if cfg.LeaseIndexName == "" {
		cfg.LeaseIndexName = "gsi2"
	}
	if cfg.GlobalCapacityLimit == 0 {
		cfg.GlobalCapacityLimit = 100
	}
	if cfg.PerUserCapacityLimit == 0 {
		cfg.PerUserCapacityLimit = 1
	}
	return cfg
}

func (s *Service) Create(ctx context.Context, scope Scope, input CreateInput) (*Session, error) {
	if s == nil || s.store == nil || !s.config.Enabled() {
		return nil, ErrUnavailable
	}
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	if err := s.validateCreate(input); err != nil {
		return nil, err
	}
	branchID := strings.TrimSpace(input.BranchID)
	if !scope.AllBranches && branchID == "" {
		return nil, fmt.Errorf("%w: branch_id is required for restricted branch access", ErrInvalidRequest)
	}
	if !scopeAllowsBranch(scope, branchID) {
		return nil, ErrBranchForbidden
	}
	now := s.now().UTC()
	ulid := s.newULID()
	if len(ulid) != 26 {
		return nil, fmt.Errorf("generate voice session id: %w", ErrUnavailable)
	}
	sessionID := "voice_" + ulid
	runtimeSessionID := "voice-session-" + ulid
	if len(runtimeSessionID) < 33 || len(runtimeSessionID) > 256 {
		return nil, fmt.Errorf("generate runtime session id: %w", ErrUnavailable)
	}
	requestHash, err := canonicalRequestHash(input)
	if err != nil {
		return nil, fmt.Errorf("hash create request: %w", err)
	}
	created := &Session{
		ID: sessionID, RuntimeSessionID: runtimeSessionID,
		UserID: scope.UserID, BusinessID: scope.BusinessID, BranchID: branchID,
		Status: StatusActive, RuntimeState: RuntimeStateRunning,
		ProtocolVersion: s.config.ProtocolVersion, KVSChannelIndex: stableChannel(sessionID, s.config.KVSChannelCount),
		PreferredLanguage: input.PreferredLanguage, FallbackLanguage: input.FallbackLanguage, CurrentLanguage: input.PreferredLanguage,
		ConsentTranscriptStorage: *input.Consent.TranscriptStorage,
		ConsentAudioRecording:    *input.Consent.AudioRecording,
		ConsentPolicyVersion:     input.Consent.PolicyVersion,
		ClientPlatform:           input.Client.Platform, ClientAppVersion: input.Client.AppVersion,
		CreatedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(s.config.LeaseDuration),
		ExpiresAt: now.Add(s.config.MaxDuration), RotateAt: now.Add(s.config.RotateAfter),
	}
	stored, _, err := s.store.Create(ctx, CreateRecord{
		Session: created, IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash,
		IdempotencyExpiresAt: now.Add(s.config.IdempotencyTTL),
	})
	if err != nil {
		return nil, err
	}
	setResumable(stored, now)
	return stored, nil
}

func (s *Service) Get(ctx context.Context, scope Scope, sessionID string) (*Session, error) {
	if s == nil || s.store == nil {
		return nil, ErrUnavailable
	}
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	if !validSessionID(sessionID) {
		return nil, ErrNotFound
	}
	stored, err := s.store.Get(ctx, scope, sessionID)
	if err != nil {
		return nil, err
	}
	setResumable(stored, s.now().UTC())
	return stored, nil
}

func (s *Service) Resume(ctx context.Context, scope Scope, sessionID string) (*Session, error) {
	stored, err := s.Get(ctx, scope, sessionID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if stored.Status != StatusActive || !stored.ExpiresAt.After(now) || !stored.LeaseExpiresAt.After(now) {
		return nil, ErrNotResumable
	}
	newRuntimeID := ""
	if stored.RuntimeState == RuntimeStateStopped {
		ulid := s.newULID()
		if len(ulid) != 26 {
			return nil, fmt.Errorf("rotate runtime session id: %w", ErrUnavailable)
		}
		newRuntimeID = "voice-session-" + ulid
	}
	resumed, err := s.store.Resume(ctx, ResumeRecord{
		Scope: scope, SessionID: sessionID, ExpectedBranchID: stored.BranchID, OldRuntimeSessionID: stored.RuntimeSessionID,
		ExpectedRuntimeState:   stored.RuntimeState,
		ExpectedLeaseExpiresAt: stored.LeaseExpiresAt,
		NewRuntimeSessionID:    newRuntimeID, LeaseExpiresAt: boundedLease(now, stored.ExpiresAt, s.config.LeaseDuration), UpdatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	setResumable(resumed, now)
	return resumed, nil
}

func (s *Service) Close(ctx context.Context, scope Scope, sessionID string) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	if err := validateScope(scope); err != nil {
		return err
	}
	if !validSessionID(sessionID) {
		return ErrNotFound
	}
	now := s.now().UTC()
	stored, state, err := s.store.Release(ctx, scope, sessionID, now)
	if err != nil {
		return err
	}
	if state.AlreadyClosed || !state.ShouldStop {
		return nil
	}
	if s.stopper == nil {
		return fmt.Errorf("stop AgentCore runtime session: %w", ErrUnavailable)
	}
	if err := s.stopper.StopRuntimeSession(ctx, RuntimeTarget{
		AgentRuntimeARN: s.config.AgentRuntimeARN, RuntimeSessionID: stored.RuntimeSessionID, Qualifier: s.config.AgentRuntimeQualifier,
	}); err != nil {
		return fmt.Errorf("stop AgentCore runtime session: %w", err)
	}
	if err := s.store.MarkClosed(ctx, scope, sessionID, stored.BranchID, now); err != nil {
		return fmt.Errorf("mark voice session closed: %w", err)
	}
	return nil
}

func (s *Service) ExpiredLeases(ctx context.Context, before time.Time, limit int32) ([]*Session, error) {
	if s == nil || s.store == nil {
		return nil, ErrUnavailable
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return s.store.ExpiredLeases(ctx, before.UTC(), limit)
}

func (s *Service) CreateResponse(value *Session) CreateResponse {
	return CreateResponse{
		SessionID: value.ID, RuntimeSessionID: value.RuntimeSessionID,
		AgentRuntimeARN: s.config.AgentRuntimeARN, AgentRuntimeQualifier: s.config.AgentRuntimeQualifier,
		KVSChannelIndex: value.KVSChannelIndex, ProtocolVersion: value.ProtocolVersion,
		ExpiresAt: value.ExpiresAt, RotateAt: value.RotateAt, SpokenLanguages: append([]string(nil), SpokenLanguages...),
	}
}

func SessionMetadata(value *Session) SessionResponse {
	return SessionResponse{
		SessionID: value.ID, RuntimeSessionID: value.RuntimeSessionID, BranchID: value.BranchID,
		Status: value.Status, ProtocolVersion: value.ProtocolVersion, KVSChannelIndex: value.KVSChannelIndex,
		PreferredLanguage: value.PreferredLanguage, FallbackLanguage: value.FallbackLanguage, CurrentLanguage: value.CurrentLanguage,
		TurnSequence: value.TurnSequence, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		LeaseExpiresAt: value.LeaseExpiresAt, ExpiresAt: value.ExpiresAt, RotateAt: value.RotateAt,
		ClosedAt: value.ClosedAt, Resumable: value.Resumable,
	}
}

func (s *Service) validateCreate(input CreateInput) error {
	if _, err := uuid.Parse(input.IdempotencyKey); err != nil {
		return fmt.Errorf("%w: idempotency_key must be a UUID", ErrInvalidRequest)
	}
	if strings.TrimSpace(input.BranchID) != "" {
		if _, err := uuid.Parse(input.BranchID); err != nil {
			return fmt.Errorf("%w: branch_id must be a UUID", ErrInvalidRequest)
		}
	}
	if !supportedLanguage(input.PreferredLanguage) || !supportedLanguage(input.FallbackLanguage) {
		return fmt.Errorf("%w: language is not supported", ErrInvalidRequest)
	}
	if input.Consent.TranscriptStorage == nil {
		return fmt.Errorf("%w: transcript_storage consent is required", ErrInvalidRequest)
	}
	if input.Consent.AudioRecording == nil || *input.Consent.AudioRecording {
		return fmt.Errorf("%w: audio_recording must be false", ErrInvalidRequest)
	}
	if !policyVersionPattern.MatchString(input.Consent.PolicyVersion) {
		return fmt.Errorf("%w: consent policy_version is invalid", ErrInvalidRequest)
	}
	if _, err := time.Parse("2006-01-02", input.Consent.PolicyVersion); err != nil {
		return fmt.Errorf("%w: consent policy_version is invalid", ErrInvalidRequest)
	}
	if input.Client.Platform != "ios" && input.Client.Platform != "android" {
		return fmt.Errorf("%w: client platform must be ios or android", ErrInvalidRequest)
	}
	if len(input.Client.AppVersion) > 64 || !appVersionPattern.MatchString(input.Client.AppVersion) {
		return fmt.Errorf("%w: client app_version is invalid", ErrInvalidRequest)
	}
	if input.Client.ProtocolVersion != s.config.ProtocolVersion || input.Client.ProtocolVersion != 1 {
		return fmt.Errorf("%w: protocol_version must be 1", ErrInvalidRequest)
	}
	return nil
}

func validateScope(scope Scope) error {
	if strings.TrimSpace(scope.UserID) == "" || strings.TrimSpace(scope.BusinessID) == "" {
		return fmt.Errorf("%w: authenticated business scope is required", ErrInvalidRequest)
	}
	return nil
}

func scopeAllowsBranch(scope Scope, branchID string) bool {
	if scope.AllBranches {
		return true
	}
	if branchID == "" {
		return false
	}
	for _, allowedBranchID := range scope.AllowedBranchIDs {
		if branchID == allowedBranchID {
			return true
		}
	}
	return false
}

func validSessionID(value string) bool {
	return strings.HasPrefix(value, "voice_") && len(value) >= len("voice_")+1 && len(value) <= 96
}

func supportedLanguage(value string) bool {
	for _, language := range SpokenLanguages {
		if value == language {
			return true
		}
	}
	return false
}

func canonicalRequestHash(input CreateInput) (string, error) {
	canonical := struct {
		BranchID          string       `json:"branch_id"`
		PreferredLanguage string       `json:"preferred_language"`
		FallbackLanguage  string       `json:"fallback_language"`
		Consent           ConsentInput `json:"consent"`
		Client            ClientInput  `json:"client"`
	}{strings.TrimSpace(input.BranchID), input.PreferredLanguage, input.FallbackLanguage, input.Consent, input.Client}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum), nil
}

func stableChannel(sessionID string, channelCount int) int {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(sessionID))
	return int(hash.Sum64() % uint64(channelCount))
}

func boundedLease(now, expiresAt time.Time, duration time.Duration) time.Time {
	lease := now.Add(duration)
	if lease.After(expiresAt) {
		return expiresAt
	}
	return lease
}

func setResumable(value *Session, now time.Time) {
	if value != nil {
		value.Resumable = value.Status == StatusActive && value.ExpiresAt.After(now) && value.LeaseExpiresAt.After(now)
	}
}

func cloneSession(value *Session) *Session {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.ClosedAt != nil {
		closed := *value.ClosedAt
		cloned.ClosedAt = &closed
	}
	return &cloned
}

func generateULID(now time.Time) (string, error) {
	var raw [16]byte
	millis := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		raw[i] = byte(millis)
		millis >>= 8
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", err
	}
	value := new(big.Int).SetBytes(raw[:])
	base := big.NewInt(32)
	encoded := make([]byte, 26)
	mod := new(big.Int)
	for i := len(encoded) - 1; i >= 0; i-- {
		value.DivMod(value, base, mod)
		encoded[i] = ulidAlphabet[mod.Int64()]
	}
	return string(encoded), nil
}
