package webrtc

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"invoice-backend/internal/voice/protocol"
	"invoice-backend/internal/voice/runtime"
	voicesession "invoice-backend/internal/voice/session"
)

const (
	requiredKVSChannelCount     = 12
	maxSignalingRequestBytes    = 16 << 10
	maxSignalingSDPBytes        = 12 << 10
	maxSignalingCandidateBytes  = 2 << 10
	maxSignalingMidBytes        = 64
	maxSignalingUfragBytes      = 256
	maxSignalingPlatformBytes   = 16
	maxSignalingAppVersionBytes = 64
	maxLogicalSessionIDBytes    = 96
	maxKVSChannelARNBytes       = 512
	maxSignalingICEURIs         = 16
	maxSignalingICEFieldBytes   = 256
	maxSignalingCredentialTTL   = 5 * time.Minute
)

var (
	ErrInvalidSignalingConfig = errors.New("invalid voice signaling configuration")
	ErrSignalingUnauthorized  = errors.New("voice signaling authorization rejected")
	ErrSignalingClose         = errors.New("close voice signaling peers")
	errSignalingPeerPanicked  = errors.New("voice signaling peer operation failed")
	errSTTBindingFactoryPanic = errors.New("voice STT binding construction failed")
)

// TrustedIdentity contains only the claims the authorization boundary has
// validated. It deliberately excludes the bearer token and raw JWT claims.
type TrustedIdentity struct {
	UserID     string
	BusinessID string
	ClientID   string
}

// AuthorizationResolver validates the allowlisted Authorization value without
// exposing it to the signaling service's retained state.
type AuthorizationResolver interface {
	Resolve(context.Context, string) (TrustedIdentity, error)
}

// SessionLookup resolves a logical voice session through the tenant-safe
// service boundary.
type SessionLookup interface {
	Get(context.Context, voicesession.Scope, string) (*voicesession.Session, error)
}

// PersistentActivitySource roots peer lifetime in the runtime and keeps the
// runtime busy after the invocation that attached the peer has returned.
type PersistentActivitySource interface {
	AcquirePersistentActivity() (context.Context, io.Closer, error)
}

// STTBindingConfig is the immutable, per-peer input needed to compose a
// runtime.STTSessionBinding. Activity ownership transfers to Create; the
// returned binding must release it when closed.
type STTBindingConfig struct {
	Context  context.Context
	Session  voicesession.Session
	Activity io.Closer
}

// STTBindingFactory is implemented at backend composition by adapting
// runtime.NewSTTSessionBinding with provider, decoder, and final-handler
// dependencies. Signaling retains no provider credentials or transcript data.
type STTBindingFactory interface {
	Create(STTBindingConfig) (STTBinding, error)
}

var _ STTBinding = (*runtime.STTSessionBinding)(nil)

// SignalingPeer is the narrow peer lifecycle used by the invocation boundary.
type SignalingPeer interface {
	Answer(context.Context, string, bool) (string, error)
	AddCandidate(context.Context, ICECandidate) error
	RefreshICE(context.Context, TURNCredentials) error
	Close() error
	Done() <-chan struct{}
}

// PeerFactory makes Pion replaceable with a deterministic in-memory peer in
// boundary tests. Production defaults to NewPeer.
type PeerFactory interface {
	Create(PeerConfig) (SignalingPeer, error)
}

type directPeerFactory struct{}

func (directPeerFactory) Create(config PeerConfig) (SignalingPeer, error) {
	return NewPeer(config)
}

type SignalingConfig struct {
	RuntimeID            string
	AWSRegion            string
	ProtocolVersion      int
	ChannelARNs          []string
	InvocationTimeout    time.Duration
	GatherTimeout        time.Duration
	AttachTimeout        time.Duration
	ConnectTimeout       time.Duration
	RestartWindow        time.Duration
	ICEExpiryMargin      time.Duration
	MaxPeers             int
	MaxTrackedSessions   int
	MaxPendingCandidates int
	MaxCandidateBytes    int
	MaxControlQueue      int
	Now                  func() time.Time
}

type SignalingDependencies struct {
	Authorization AuthorizationResolver
	Sessions      SessionLookup
	ICE           ICECredentialSource
	Activities    PersistentActivitySource
	Peers         PeerFactory
	STTBindings   STTBindingFactory
	HandleControl func(context.Context, protocol.ControlMessage) error
}

type signalingConfig struct {
	runtimeID            string
	awsRegion            string
	protocolVersion      int
	channelARNs          []string
	invocationTimeout    time.Duration
	gatherTimeout        time.Duration
	attachTimeout        time.Duration
	connectTimeout       time.Duration
	restartWindow        time.Duration
	iceExpiryMargin      time.Duration
	maxPeers             int
	maxTrackedSessions   int
	maxPendingCandidates int
	maxCandidateBytes    int
	maxControlQueue      int
	now                  func() time.Time
}

// SignalingService owns bounded logical-session metadata and one opaque
// lifecycle binding per active peer. It never keeps Authorization, SDP,
// candidates, TURN response credentials, audio, or transcripts in its state.
type SignalingService struct {
	config        signalingConfig
	authorization AuthorizationResolver
	sessions      SessionLookup
	ice           ICECredentialSource
	activities    PersistentActivitySource
	peers         PeerFactory
	sttBindings   STTBindingFactory
	handleControl func(context.Context, protocol.ControlMessage) error

	mu          sync.Mutex
	closing     bool
	closeOnce   sync.Once
	done        chan struct{}
	watchers    sync.WaitGroup
	activePeers int
	states      map[signalingSessionKey]*signalingSessionState
	recency     *list.List
}

// MarshalJSON deliberately exposes no service internals. The service owns
// authorization collaborators and short-lived session metadata that must not
// be serialized into logs, diagnostics, or invocation responses.
func (*SignalingService) MarshalJSON() ([]byte, error) {
	return []byte("{}"), nil
}

type signalingSessionKey struct {
	logicalID string
	runtimeID string
}

type signalingSessionState struct {
	mu sync.Mutex

	key          signalingSessionKey
	lastSequence int64
	peer         SignalingPeer
	peerDone     <-chan struct{}
	peerStop     chan struct{}
	binding      STTBinding
	attachTimer  *time.Timer
	hasOffer     bool
	iceExpiresAt time.Time

	// The following fields are protected by SignalingService.mu.
	active  bool
	refs    int
	recency *list.Element
}

// onceActivityCloser makes the ownership-transfer edge safe even when the
// runtime activity implementation itself is not idempotent. A binding
// constructor may consume it before returning an error; signaling can still
// close the same wrapper on every failure path.
type onceActivityCloser struct {
	once     sync.Once
	activity io.Closer
	err      error
}

func (closer *onceActivityCloser) Close() error {
	if closer == nil {
		return nil
	}
	closer.once.Do(func() {
		if closer.activity != nil {
			closer.err = closer.activity.Close()
		}
		closer.activity = nil
	})
	return closer.err
}

// ownedSTTBinding is the single lifecycle object shared by the real Peer and
// signaling's fallback cleanup. It stops STT/decoder work first, then releases
// the persistent runtime activity, with both operations fenced exactly once.
type ownedSTTBinding struct {
	mu       sync.RWMutex
	binding  STTBinding
	activity io.Closer
	closed   bool

	closeOnce sync.Once
	closeErr  error
}

func (binding *ownedSTTBinding) HandleOpus(payload []byte) error {
	if binding == nil {
		return ErrPeerClosed
	}
	binding.mu.RLock()
	defer binding.mu.RUnlock()
	if binding.closed || binding.binding == nil {
		return ErrPeerClosed
	}
	return binding.binding.HandleOpus(payload)
}

func (binding *ownedSTTBinding) HandleControl(ctx context.Context, message protocol.ControlMessage) error {
	if binding == nil {
		return ErrPeerClosed
	}
	binding.mu.RLock()
	defer binding.mu.RUnlock()
	if binding.closed || binding.binding == nil {
		return ErrPeerClosed
	}
	return binding.binding.HandleControl(ctx, message)
}

func (binding *ownedSTTBinding) Close() error {
	if binding == nil {
		return nil
	}
	binding.closeOnce.Do(func() {
		binding.mu.Lock()
		defer binding.mu.Unlock()
		binding.closed = true
		bindingError := safeCloseSTTBinding(binding.binding)
		activityError := safeCloseActivity(binding.activity)
		binding.closeErr = errors.Join(bindingError, activityError)
	})
	return binding.closeErr
}

type signalingRequest struct {
	Type            protocol.EventType  `json:"type"`
	ProtocolVersion int                 `json:"protocol_version"`
	SessionID       string              `json:"session_id"`
	Sequence        int64               `json:"sequence"`
	Client          *signalingClient    `json:"client,omitempty"`
	SDP             *string             `json:"sdp,omitempty"`
	Candidate       *signalingCandidate `json:"candidate,omitempty"`
	present         signalingFieldPresence
}

type signalingFieldPresence struct {
	client    bool
	sdp       bool
	candidate bool
}

type signalingClient struct {
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

type signalingCandidate struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdp_mid"`
	SDPMLineIndex    *uint16 `json:"sdp_mline_index"`
	UsernameFragment *string `json:"username_fragment"`
}

type signalingFailure struct {
	status int
	code   string
}

func (failure *signalingFailure) Error() string { return failure.code }

func NewSignalingService(config SignalingConfig, dependencies SignalingDependencies) (*SignalingService, error) {
	normalized, err := validateSignalingConfig(config)
	if err != nil {
		return nil, err
	}
	if dependencies.Authorization == nil || dependencies.Sessions == nil || dependencies.ICE == nil || dependencies.Activities == nil || isNilInterface(dependencies.STTBindings) {
		return nil, ErrInvalidSignalingConfig
	}
	if dependencies.Peers == nil {
		dependencies.Peers = directPeerFactory{}
	}
	if dependencies.HandleControl == nil {
		dependencies.HandleControl = func(context.Context, protocol.ControlMessage) error { return nil }
	}
	return &SignalingService{
		config:        normalized,
		authorization: dependencies.Authorization,
		sessions:      dependencies.Sessions,
		ice:           dependencies.ICE,
		activities:    dependencies.Activities,
		peers:         dependencies.Peers,
		sttBindings:   dependencies.STTBindings,
		handleControl: dependencies.HandleControl,
		states:        make(map[signalingSessionKey]*signalingSessionState, normalized.maxTrackedSessions),
		recency:       list.New(),
		done:          make(chan struct{}),
	}, nil
}

func validateSignalingConfig(config SignalingConfig) (signalingConfig, error) {
	if !safeBoundedValue(config.RuntimeID, 256) || config.AWSRegion != MumbaiRegion ||
		config.ProtocolVersion != protocol.ProtocolVersion ||
		config.InvocationTimeout <= 0 || config.InvocationTimeout >= 15*time.Second ||
		config.GatherTimeout <= 0 || config.GatherTimeout >= 10*time.Second || config.GatherTimeout >= config.InvocationTimeout ||
		config.AttachTimeout <= 0 || config.ConnectTimeout <= 0 || config.RestartWindow <= 0 ||
		config.ICEExpiryMargin <= 0 || config.ICEExpiryMargin >= maxSignalingCredentialTTL ||
		config.MaxPeers <= 0 || config.MaxTrackedSessions < config.MaxPeers || config.MaxPendingCandidates <= 0 ||
		config.MaxPendingCandidates > maxPeerCandidates || config.MaxCandidateBytes <= 0 || config.MaxCandidateBytes > maxPeerCandidateBytes ||
		config.MaxControlQueue <= 0 || config.MaxControlQueue > maxPeerControlQueue ||
		!validKVSChannelARNs(config.AWSRegion, config.ChannelARNs) {
		return signalingConfig{}, ErrInvalidSignalingConfig
	}
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return signalingConfig{
		runtimeID:            config.RuntimeID,
		awsRegion:            config.AWSRegion,
		protocolVersion:      config.ProtocolVersion,
		channelARNs:          append([]string(nil), config.ChannelARNs...),
		invocationTimeout:    config.InvocationTimeout,
		gatherTimeout:        config.GatherTimeout,
		attachTimeout:        config.AttachTimeout,
		connectTimeout:       config.ConnectTimeout,
		restartWindow:        config.RestartWindow,
		iceExpiryMargin:      config.ICEExpiryMargin,
		maxPeers:             config.MaxPeers,
		maxTrackedSessions:   config.MaxTrackedSessions,
		maxPendingCandidates: config.MaxPendingCandidates,
		maxCandidateBytes:    config.MaxCandidateBytes,
		maxControlQueue:      config.MaxControlQueue,
		now:                  now,
	}, nil
}

func validKVSChannelARNs(region string, channelARNs []string) bool {
	if len(channelARNs) != requiredKVSChannelCount {
		return false
	}
	seen := make(map[string]struct{}, requiredKVSChannelCount)
	for _, channelARN := range channelARNs {
		if !validKVSChannelARN(region, channelARN) {
			return false
		}
		if _, duplicate := seen[channelARN]; duplicate {
			return false
		}
		seen[channelARN] = struct{}{}
	}
	return true
}

func validKVSChannelARN(region, value string) bool {
	if len(value) == 0 || len(value) > maxKVSChannelARNBytes || strings.TrimSpace(value) != value || !isPrintableASCII(value) {
		return false
	}
	parts := strings.SplitN(value, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "kinesisvideo" || parts[3] != region || !allDigits(parts[4], 12) {
		return false
	}
	resource := strings.Split(parts[5], "/")
	return len(resource) == 3 && resource[0] == "channel" && safeChannelName(resource[1]) && decimalLengthBetween(resource[2], 1, 32)
}

func safeChannelName(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func allDigits(value string, exactLength int) bool {
	if len(value) != exactLength {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func decimalLengthBetween(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func isPrintableASCII(value string) bool {
	for _, character := range value {
		if character < 0x21 || character > 0x7e || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

// Invoke implements runtime.InvocationHandler and always maps boundary errors
// into a small sanitized status vocabulary.
func (service *SignalingService) Invoke(ctx context.Context, invocation runtime.InvocationRequest) (response runtime.InvocationResponse, invokeErr error) {
	defer func() {
		if recover() != nil {
			response = sanitizedSignalingError(http.StatusServiceUnavailable)
			invokeErr = nil
		}
	}()
	if service == nil {
		return sanitizedSignalingError(http.StatusServiceUnavailable), nil
	}
	if service.isClosing() {
		return sanitizedSignalingError(http.StatusServiceUnavailable), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	invocationContext, cancel := context.WithTimeout(ctx, service.config.invocationTimeout)
	defer cancel()

	response, invokeErr = service.invoke(invocationContext, invocation)
	if invokeErr != nil {
		var failure *signalingFailure
		if errors.As(invokeErr, &failure) {
			return sanitizedSignalingError(failure.status), nil
		}
		return sanitizedSignalingError(http.StatusServiceUnavailable), nil
	}
	return response, nil
}

func (service *SignalingService) invoke(ctx context.Context, invocation runtime.InvocationRequest) (runtime.InvocationResponse, error) {
	request, err := decodeSignalingRequest(invocation.Body)
	if err != nil {
		return runtime.InvocationResponse{}, err
	}

	identity, err := service.authorization.Resolve(ctx, invocation.Authorization)
	if err != nil {
		if errors.Is(err, ErrSignalingUnauthorized) {
			return runtime.InvocationResponse{}, fail(http.StatusNotFound)
		}
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	if !validTrustedIdentity(identity) {
		return runtime.InvocationResponse{}, fail(http.StatusNotFound)
	}

	logicalSession, err := service.sessions.Get(ctx, voicesession.Scope{
		UserID:      identity.UserID,
		BusinessID:  identity.BusinessID,
		AllBranches: true,
	}, request.SessionID)
	if err != nil {
		if errors.Is(err, voicesession.ErrNotFound) || errors.Is(err, voicesession.ErrBranchForbidden) {
			return runtime.InvocationResponse{}, fail(http.StatusNotFound)
		}
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	if err := service.validateDurableSession(invocation, request, identity, logicalSession); err != nil {
		return runtime.InvocationResponse{}, err
	}

	key := signalingSessionKey{logicalID: logicalSession.ID, runtimeID: logicalSession.RuntimeSessionID}
	state, err := service.acquireState(key)
	if err != nil {
		return runtime.InvocationResponse{}, err
	}
	defer service.releaseState(state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if request.Sequence != state.lastSequence+1 {
		return runtime.InvocationResponse{}, fail(http.StatusConflict)
	}

	switch request.Type {
	case protocol.InvocationSessionAttach:
		return service.attach(ctx, state, request, logicalSession)
	case protocol.InvocationWebRTCOffer:
		return service.answer(ctx, state, request, false)
	case protocol.InvocationWebRTCRestart:
		return service.answer(ctx, state, request, true)
	case protocol.InvocationWebRTCCandidate:
		return service.addCandidate(ctx, state, request)
	default:
		return runtime.InvocationResponse{}, fail(http.StatusBadRequest)
	}
}

func decodeSignalingRequest(body json.RawMessage) (signalingRequest, error) {
	if len(body) == 0 {
		return signalingRequest{}, fail(http.StatusBadRequest)
	}
	if len(body) > maxSignalingRequestBytes {
		return signalingRequest{}, fail(http.StatusRequestEntityTooLarge)
	}
	if err := rejectDuplicateJSONFields(body); err != nil {
		return signalingRequest{}, fail(http.StatusBadRequest)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request signalingRequest
	if err := decoder.Decode(&request); err != nil {
		return signalingRequest{}, fail(http.StatusBadRequest)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return signalingRequest{}, fail(http.StatusBadRequest)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return signalingRequest{}, fail(http.StatusBadRequest)
	}
	if !validExactSignalingKeys(fields) {
		return signalingRequest{}, fail(http.StatusBadRequest)
	}
	_, request.present.client = fields["client"]
	_, request.present.sdp = fields["sdp"]
	_, request.present.candidate = fields["candidate"]
	if err := validateSignalingRequest(request); err != nil {
		return signalingRequest{}, err
	}
	return request, nil
}

func validExactSignalingKeys(fields map[string]json.RawMessage) bool {
	for key := range fields {
		switch key {
		case "type", "protocol_version", "session_id", "sequence", "client", "sdp", "candidate":
		default:
			return false
		}
	}
	if rawClient, present := fields["client"]; present && !bytes.Equal(bytes.TrimSpace(rawClient), []byte("null")) {
		if !rawObjectHasOnlyKeys(rawClient, "platform", "app_version") {
			return false
		}
	}
	if rawCandidate, present := fields["candidate"]; present && !bytes.Equal(bytes.TrimSpace(rawCandidate), []byte("null")) {
		if !rawObjectHasOnlyKeys(rawCandidate, "candidate", "sdp_mid", "sdp_mline_index", "username_fragment") {
			return false
		}
	}
	return true
}

func rawObjectHasOnlyKeys(raw json.RawMessage, allowed ...string) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return false
	}
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	for key := range object {
		if _, ok := allowedKeys[key]; !ok {
			return false
		}
	}
	return true
}

func rejectDuplicateJSONFields(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate JSON object key")
			}
			seen[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
		return nil
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
		return nil
	default:
		return errors.New("invalid JSON delimiter")
	}
}

func validateSignalingRequest(request signalingRequest) error {
	if request.ProtocolVersion != protocol.ProtocolVersion || request.Sequence <= 0 ||
		len(request.SessionID) == 0 || len(request.SessionID) > maxLogicalSessionIDBytes ||
		!strings.HasPrefix(request.SessionID, "voice_") || strings.TrimSpace(request.SessionID) != request.SessionID || !isPrintableASCII(request.SessionID) {
		return fail(http.StatusBadRequest)
	}
	switch request.Type {
	case protocol.InvocationSessionAttach:
		if !request.present.client || request.present.sdp || request.present.candidate || request.Client == nil ||
			(request.Client.Platform != "ios" && request.Client.Platform != "android") {
			return fail(http.StatusBadRequest)
		}
		if len(request.Client.Platform) > maxSignalingPlatformBytes || len(request.Client.AppVersion) > maxSignalingAppVersionBytes {
			return fail(http.StatusRequestEntityTooLarge)
		}
		if !safeBoundedValue(request.Client.Platform, maxSignalingPlatformBytes) || !safeBoundedValue(request.Client.AppVersion, maxSignalingAppVersionBytes) {
			return fail(http.StatusBadRequest)
		}
	case protocol.InvocationWebRTCOffer, protocol.InvocationWebRTCRestart:
		if request.present.client || request.present.candidate || !request.present.sdp || request.SDP == nil || len(*request.SDP) == 0 {
			return fail(http.StatusBadRequest)
		}
		if len(*request.SDP) > maxSignalingSDPBytes {
			return fail(http.StatusRequestEntityTooLarge)
		}
	case protocol.InvocationWebRTCCandidate:
		if request.present.client || request.present.sdp || !request.present.candidate || request.Candidate == nil {
			return fail(http.StatusBadRequest)
		}
		if len(request.Candidate.Candidate) > maxSignalingCandidateBytes ||
			(request.Candidate.SDPMid != nil && len(*request.Candidate.SDPMid) > maxSignalingMidBytes) ||
			(request.Candidate.UsernameFragment != nil && len(*request.Candidate.UsernameFragment) > maxSignalingUfragBytes) {
			return fail(http.StatusRequestEntityTooLarge)
		}
		if !validSignalingCandidate(*request.Candidate) {
			return fail(http.StatusBadRequest)
		}
	default:
		return fail(http.StatusBadRequest)
	}
	return nil
}

func validSignalingCandidate(candidate signalingCandidate) bool {
	return len(candidate.Candidate) > 0 && len(candidate.Candidate) <= maxSignalingCandidateBytes &&
		candidate.SDPMid != nil && safeBoundedValue(*candidate.SDPMid, maxSignalingMidBytes) &&
		candidate.SDPMLineIndex != nil && *candidate.SDPMLineIndex <= 1 &&
		candidate.UsernameFragment != nil && safeBoundedValue(*candidate.UsernameFragment, maxSignalingUfragBytes)
}

func safeBoundedValue(value string, limit int) bool {
	return len(value) > 0 && len(value) <= limit && strings.TrimSpace(value) == value && isPrintableASCII(value)
}

func validTrustedIdentity(identity TrustedIdentity) bool {
	return safeBoundedValue(identity.UserID, 256) && safeBoundedValue(identity.BusinessID, 256) && safeBoundedValue(identity.ClientID, 256)
}

func (service *SignalingService) validateDurableSession(invocation runtime.InvocationRequest, request signalingRequest, identity TrustedIdentity, value *voicesession.Session) error {
	if value == nil {
		return fail(http.StatusServiceUnavailable)
	}
	if value.ID != request.SessionID || value.UserID != identity.UserID || value.BusinessID != identity.BusinessID {
		return fail(http.StatusNotFound)
	}
	if invocation.RuntimeID != service.config.runtimeID || invocation.AWSRegion != service.config.awsRegion ||
		invocation.RuntimeSessionID == "" || invocation.RuntimeSessionID != value.RuntimeSessionID {
		return fail(http.StatusConflict)
	}
	now := service.config.now().UTC()
	if value.Status != voicesession.StatusActive || value.RuntimeState != voicesession.RuntimeStateRunning ||
		!value.ExpiresAt.After(now) || !value.LeaseExpiresAt.After(now) {
		return fail(http.StatusGone)
	}
	if value.ProtocolVersion != service.config.protocolVersion || value.KVSChannelIndex < 0 || value.KVSChannelIndex >= len(service.config.channelARNs) {
		return fail(http.StatusConflict)
	}
	if request.Type == protocol.InvocationSessionAttach &&
		(request.Client.Platform != value.ClientPlatform || request.Client.AppVersion != value.ClientAppVersion) {
		return fail(http.StatusConflict)
	}
	return nil
}

func (service *SignalingService) acquireState(key signalingSessionKey) (*signalingSessionState, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closing {
		return nil, fail(http.StatusServiceUnavailable)
	}
	if state := service.states[key]; state != nil {
		state.refs++
		service.recency.MoveToFront(state.recency)
		return state, nil
	}
	if len(service.states) >= service.config.maxTrackedSessions && !service.evictInactiveState() {
		return nil, fail(http.StatusTooManyRequests)
	}
	state := &signalingSessionState{key: key, refs: 1}
	state.recency = service.recency.PushFront(key)
	service.states[key] = state
	return state, nil
}

func (service *SignalingService) evictInactiveState() bool {
	for element := service.recency.Back(); element != nil; element = element.Prev() {
		key := element.Value.(signalingSessionKey)
		state := service.states[key]
		if state != nil && !state.active && state.refs == 0 {
			delete(service.states, key)
			service.recency.Remove(element)
			return true
		}
	}
	return false
}

func (service *SignalingService) releaseState(state *signalingSessionState) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if state.refs > 0 {
		state.refs--
	}
}

func (service *SignalingService) attach(ctx context.Context, state *signalingSessionState, request signalingRequest, logicalSession *voicesession.Session) (runtime.InvocationResponse, error) {
	if state.peer != nil {
		return service.refreshActivePeer(ctx, state, request, logicalSession)
	}
	if err := service.reservePeer(state); err != nil {
		return runtime.InvocationResponse{}, err
	}
	reserved := true
	defer func() {
		if reserved {
			service.releasePeerReservation(state)
		}
	}()
	credentials, err := service.ice.GetTURN(ctx, service.config.channelARNs[logicalSession.KVSChannelIndex])
	if err != nil || !validSignalingTURNCredentials(service.config.awsRegion, service.config.now().UTC(), service.config.iceExpiryMargin, credentials) {
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}

	peerContext, rawActivity, err := service.activities.AcquirePersistentActivity()
	if err != nil || peerContext == nil || isNilInterface(rawActivity) || peerContext.Err() != nil {
		if !isNilInterface(rawActivity) {
			_ = safeCloseActivity(rawActivity)
		}
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	activity := &onceActivityCloser{activity: rawActivity}
	binding, bindingErr := safelyCreateSTTBinding(service.sttBindings, STTBindingConfig{
		Context:  peerContext,
		Session:  cloneSignalingSession(logicalSession),
		Activity: activity,
	})
	if bindingErr != nil || isNilInterface(binding) {
		if !isNilInterface(binding) {
			_ = safeCloseSTTBinding(&ownedSTTBinding{binding: binding, activity: activity})
		} else {
			_ = safeCloseActivity(activity)
		}
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	ownedBinding := &ownedSTTBinding{binding: binding, activity: activity}
	peer, err := safelyCreatePeer(service.peers, PeerConfig{
		Context:           peerContext,
		SessionID:         logicalSession.ID,
		TURNCredentials:   credentials,
		AttachTimeout:     service.config.attachTimeout,
		GatherTimeout:     service.config.gatherTimeout,
		ConnectTimeout:    service.config.connectTimeout,
		RestartWindow:     service.config.restartWindow,
		MaxCandidates:     service.config.maxPendingCandidates,
		MaxCandidateBytes: service.config.maxCandidateBytes,
		MaxControlQueue:   service.config.maxControlQueue,
		HandleControl:     service.handleControl,
		STTBinding:        ownedBinding,
	})
	done, doneErr := safePeerDone(peer)
	if err != nil || doneErr != nil || peer == nil || done == nil {
		if peer != nil {
			_ = safeClosePeer(peer)
		}
		_ = safeCloseSTTBinding(ownedBinding)
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	cleanupPeer := true
	defer func() {
		if cleanupPeer {
			_ = safeClosePeer(peer)
			_ = safeCloseSTTBinding(ownedBinding)
		}
	}()
	if channelClosed(done) || service.isClosing() || peerContext.Err() != nil ||
		!validSignalingTURNCredentials(service.config.awsRegion, service.config.now().UTC(), service.config.iceExpiryMargin, credentials) {
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}

	body, err := marshalAttachedResponse(request, credentials)
	if err != nil {
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	if !validSignalingResponseBody(body) || channelClosed(done) || service.isClosing() || peerContext.Err() != nil || ctx.Err() != nil {
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	state.peer = peer
	state.peerDone = done
	state.peerStop = make(chan struct{})
	state.binding = ownedBinding
	state.hasOffer = false
	state.iceExpiresAt = credentials.ExpiresAt.UTC()
	state.lastSequence = request.Sequence
	state.attachTimer = time.AfterFunc(service.config.attachTimeout, func() {
		service.expireUnofferedPeer(state, peer)
	})
	reserved = false
	cleanupPeer = false
	service.watchers.Add(1)
	go service.watchPeer(state, peer, done, state.peerStop)
	return runtime.InvocationResponse{StatusCode: http.StatusOK, Body: body}, nil
}

func (service *SignalingService) refreshActivePeer(ctx context.Context, state *signalingSessionState, request signalingRequest, logicalSession *voicesession.Session) (runtime.InvocationResponse, error) {
	peer := state.peer
	if channelClosed(state.peerDone) {
		return runtime.InvocationResponse{}, fail(http.StatusConflict)
	}
	credentials, err := service.ice.GetTURN(ctx, service.config.channelARNs[logicalSession.KVSChannelIndex])
	if err != nil || !validSignalingTURNCredentials(service.config.awsRegion, service.config.now().UTC(), service.config.iceExpiryMargin, credentials) {
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	if err := safelyRefreshICE(peer, ctx, credentials); err != nil {
		if errors.Is(err, errSignalingPeerPanicked) {
			service.retirePeerLocked(state, peer)
		}
		return runtime.InvocationResponse{}, mapPeerError(err)
	}
	if !validSignalingTURNCredentials(service.config.awsRegion, service.config.now().UTC(), service.config.iceExpiryMargin, credentials) {
		service.retirePeerLocked(state, peer)
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	body, err := marshalAttachedResponse(request, credentials)
	if err != nil {
		service.retirePeerLocked(state, peer)
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	if !validSignalingResponseBody(body) || channelClosed(state.peerDone) || service.isClosing() || ctx.Err() != nil {
		service.retirePeerLocked(state, peer)
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	state.iceExpiresAt = credentials.ExpiresAt.UTC()
	state.lastSequence = request.Sequence
	return runtime.InvocationResponse{StatusCode: http.StatusOK, Body: body}, nil
}

func safelyCreatePeer(factory PeerFactory, config PeerConfig) (peer SignalingPeer, err error) {
	defer func() {
		if recover() != nil {
			peer = nil
			err = ErrInvalidPeerConfig
		}
	}()
	return factory.Create(config)
}

func safelyCreateSTTBinding(factory STTBindingFactory, config STTBindingConfig) (binding STTBinding, err error) {
	defer func() {
		if recover() != nil {
			binding = nil
			err = errSTTBindingFactoryPanic
		}
	}()
	return factory.Create(config)
}

func cloneSignalingSession(session *voicesession.Session) voicesession.Session {
	if session == nil {
		return voicesession.Session{}
	}
	cloned := *session
	if session.ClosedAt != nil {
		closedAt := *session.ClosedAt
		cloned.ClosedAt = &closedAt
	}
	return cloned
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func safePeerDone(peer SignalingPeer) (done <-chan struct{}, err error) {
	defer func() {
		if recover() != nil {
			done = nil
			err = ErrSignalingClose
		}
	}()
	if peer == nil {
		return nil, ErrSignalingClose
	}
	return peer.Done(), nil
}

func channelClosed(done <-chan struct{}) bool {
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func validSignalingTURNCredentials(region string, now time.Time, expiryMargin time.Duration, credentials TURNCredentials) bool {
	if ValidateTURNCredentials(region, now, expiryMargin, credentials) != nil || len(credentials.uris) > maxSignalingICEURIs ||
		len(credentials.username) > maxSignalingICEFieldBytes || len(credentials.password) > maxSignalingICEFieldBytes ||
		!validICECredentialToken(credentials.username) || !validICECredentialToken(credentials.password) ||
		credentials.ExpiresAt.After(now.Add(maxSignalingCredentialTTL)) {
		return false
	}
	for _, uri := range credentials.uris {
		if len(uri) > maxSignalingICEFieldBytes {
			return false
		}
	}
	return true
}

func validICECredentialToken(value string) bool {
	if len(value) == 0 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') && character != '_' && character != '.' && character != '-' {
			return false
		}
	}
	return true
}

func marshalAttachedResponse(request signalingRequest, credentials TURNCredentials) (json.RawMessage, error) {
	type iceServerWire struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username"`
		Credential string   `json:"credential"`
	}
	type attachedWire struct {
		Type               protocol.EventType `json:"type"`
		ProtocolVersion    int                `json:"protocol_version"`
		SessionID          string             `json:"session_id"`
		Sequence           int64              `json:"sequence"`
		ICEServers         []iceServerWire    `json:"ice_servers"`
		ICEExpiresAt       time.Time          `json:"ice_expires_at"`
		ICETransportPolicy string             `json:"ice_transport_policy"`
	}
	return json.Marshal(attachedWire{
		Type:            protocol.ResponseSessionAttached,
		ProtocolVersion: protocol.ProtocolVersion,
		SessionID:       request.SessionID,
		Sequence:        request.Sequence,
		ICEServers: []iceServerWire{{
			URLs:       append([]string(nil), credentials.uris...),
			Username:   credentials.username,
			Credential: credentials.password,
		}},
		ICEExpiresAt:       credentials.ExpiresAt.UTC(),
		ICETransportPolicy: "relay",
	})
}

func (service *SignalingService) reservePeer(state *signalingSessionState) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closing {
		return fail(http.StatusServiceUnavailable)
	}
	if state.active || service.activePeers >= service.config.maxPeers {
		return fail(http.StatusTooManyRequests)
	}
	state.active = true
	service.activePeers++
	return nil
}

func (service *SignalingService) releasePeerReservation(state *signalingSessionState) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if state.active {
		state.active = false
		if service.activePeers > 0 {
			service.activePeers--
		}
	}
}

func (service *SignalingService) answer(ctx context.Context, state *signalingSessionState, request signalingRequest, restart bool) (runtime.InvocationResponse, error) {
	if state.peer == nil || (!restart && state.hasOffer) || (restart && !state.hasOffer) {
		return runtime.InvocationResponse{}, fail(http.StatusConflict)
	}
	if !state.iceExpiresAt.After(service.config.now().UTC().Add(service.config.iceExpiryMargin)) {
		return runtime.InvocationResponse{}, fail(http.StatusConflict)
	}
	answer, err := safelyAnswer(state.peer, ctx, *request.SDP, restart)
	if err != nil {
		if errors.Is(err, errSignalingPeerPanicked) {
			service.retirePeerLocked(state, state.peer)
		}
		return runtime.InvocationResponse{}, mapPeerError(err)
	}
	if len(answer) == 0 || len(answer) > maxSignalingSDPBytes {
		service.retirePeerLocked(state, state.peer)
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	body, err := json.Marshal(struct {
		Type            protocol.EventType `json:"type"`
		ProtocolVersion int                `json:"protocol_version"`
		SessionID       string             `json:"session_id"`
		Sequence        int64              `json:"sequence"`
		SDP             string             `json:"sdp"`
	}{
		Type:            protocol.ResponseWebRTCAnswer,
		ProtocolVersion: protocol.ProtocolVersion,
		SessionID:       request.SessionID,
		Sequence:        request.Sequence,
		SDP:             answer,
	})
	if err != nil {
		service.retirePeerLocked(state, state.peer)
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	if !state.iceExpiresAt.After(service.config.now().UTC().Add(service.config.iceExpiryMargin)) {
		service.retirePeerLocked(state, state.peer)
		return runtime.InvocationResponse{}, fail(http.StatusConflict)
	}
	if !validSignalingResponseBody(body) || channelClosed(state.peerDone) || service.isClosing() || ctx.Err() != nil {
		service.retirePeerLocked(state, state.peer)
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	if !restart {
		state.hasOffer = true
		if state.attachTimer != nil {
			state.attachTimer.Stop()
			state.attachTimer = nil
		}
	}
	state.lastSequence = request.Sequence
	return runtime.InvocationResponse{StatusCode: http.StatusOK, Body: body}, nil
}

func (service *SignalingService) addCandidate(ctx context.Context, state *signalingSessionState, request signalingRequest) (runtime.InvocationResponse, error) {
	if state.peer == nil {
		return runtime.InvocationResponse{}, fail(http.StatusConflict)
	}
	candidate := ICECandidate{
		candidate:        request.Candidate.Candidate,
		sdpMid:           cloneStringPointer(request.Candidate.SDPMid),
		sdpMLineIndex:    cloneUint16Pointer(request.Candidate.SDPMLineIndex),
		usernameFragment: cloneStringPointer(request.Candidate.UsernameFragment),
	}
	if err := safelyAddCandidate(state.peer, ctx, candidate); err != nil {
		if errors.Is(err, errSignalingPeerPanicked) {
			service.retirePeerLocked(state, state.peer)
		}
		return runtime.InvocationResponse{}, mapPeerError(err)
	}
	if channelClosed(state.peerDone) || service.isClosing() || ctx.Err() != nil {
		service.retirePeerLocked(state, state.peer)
		return runtime.InvocationResponse{}, fail(http.StatusServiceUnavailable)
	}
	state.lastSequence = request.Sequence
	return runtime.InvocationResponse{StatusCode: http.StatusNoContent}, nil
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneUint16Pointer(value *uint16) *uint16 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func mapPeerError(err error) error {
	switch {
	case errors.Is(err, ErrPeerInvalidDescription), errors.Is(err, ErrPeerInvalidCandidate):
		return fail(http.StatusBadRequest)
	case errors.Is(err, ErrPeerClosed), errors.Is(err, ErrPeerState):
		return fail(http.StatusConflict)
	case errors.Is(err, ErrPeerCapacity):
		return fail(http.StatusTooManyRequests)
	default:
		return fail(http.StatusServiceUnavailable)
	}
}

func (service *SignalingService) expireUnofferedPeer(state *signalingSessionState, peer SignalingPeer) {
	state.mu.Lock()
	shouldClose := state.peer == peer && !state.hasOffer
	state.mu.Unlock()
	if shouldClose {
		_ = safeClosePeer(peer)
		service.detachPeer(state, peer)
	}
}

func (service *SignalingService) watchPeer(state *signalingSessionState, peer SignalingPeer, done, stop <-chan struct{}) {
	defer service.watchers.Done()
	select {
	case <-done:
		service.detachPeer(state, peer)
	case <-stop:
	case <-service.done:
	}
}

// retirePeerLocked removes a terminal peer after an operation mutated or
// panicked. The caller holds state.mu, so the sequence remains reusable while
// the capacity/activity release is atomic with peer removal.
func (service *SignalingService) retirePeerLocked(state *signalingSessionState, peer SignalingPeer) {
	if state.peer != peer || peer == nil {
		return
	}
	if state.attachTimer != nil {
		state.attachTimer.Stop()
		state.attachTimer = nil
	}
	binding := state.binding
	stop := state.peerStop
	state.peer = nil
	state.peerDone = nil
	state.peerStop = nil
	state.binding = nil
	state.hasOffer = false
	state.iceExpiresAt = time.Time{}
	if stop != nil {
		close(stop)
	}
	_ = safeClosePeer(peer)
	_ = safeCloseSTTBinding(binding)
	service.releasePeerReservation(state)
}

func (service *SignalingService) detachPeer(state *signalingSessionState, peer SignalingPeer) {
	state.mu.Lock()
	if state.peer != peer {
		state.mu.Unlock()
		return
	}
	if state.attachTimer != nil {
		state.attachTimer.Stop()
		state.attachTimer = nil
	}
	binding := state.binding
	stop := state.peerStop
	state.peer = nil
	state.peerDone = nil
	state.peerStop = nil
	state.binding = nil
	state.hasOffer = false
	state.iceExpiresAt = time.Time{}
	state.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	if binding != nil {
		_ = safeCloseSTTBinding(binding)
	}
	service.releasePeerReservation(state)
}

// Close rejects new work, closes every peer, and releases persistent activity.
// It is safe to call more than once.
func (service *SignalingService) Close() error {
	if service == nil {
		return nil
	}
	service.mu.Lock()
	service.closing = true
	service.closeOnce.Do(func() { close(service.done) })
	states := make([]*signalingSessionState, 0, len(service.states))
	for _, state := range service.states {
		states = append(states, state)
	}
	service.mu.Unlock()

	closeFailed := false
	for _, state := range states {
		state.mu.Lock()
		peer := state.peer
		state.mu.Unlock()
		if peer == nil {
			continue
		}
		if err := safeClosePeer(peer); err != nil {
			closeFailed = true
		}
		service.detachPeer(state, peer)
	}
	service.watchers.Wait()
	if closeFailed {
		return ErrSignalingClose
	}
	return nil
}

func safelyAnswer(peer SignalingPeer, ctx context.Context, offer string, restart bool) (answer string, err error) {
	defer func() {
		if recover() != nil {
			answer = ""
			err = errSignalingPeerPanicked
		}
	}()
	return peer.Answer(ctx, offer, restart)
}

func safelyAddCandidate(peer SignalingPeer, ctx context.Context, candidate ICECandidate) (err error) {
	defer func() {
		if recover() != nil {
			err = errSignalingPeerPanicked
		}
	}()
	return peer.AddCandidate(ctx, candidate)
}

func safelyRefreshICE(peer SignalingPeer, ctx context.Context, credentials TURNCredentials) (err error) {
	defer func() {
		if recover() != nil {
			err = errSignalingPeerPanicked
		}
	}()
	return peer.RefreshICE(ctx, credentials)
}

func validSignalingResponseBody(body json.RawMessage) bool {
	return len(body) > 0 && len(body) <= runtime.MaxInvocationBodySize && json.Valid(body)
}

func safeClosePeer(peer SignalingPeer) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrSignalingClose
		}
	}()
	if peer == nil {
		return nil
	}
	if peer.Close() != nil {
		return ErrSignalingClose
	}
	return nil
}

func safeCloseActivity(activity io.Closer) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrSignalingClose
		}
	}()
	if activity == nil {
		return nil
	}
	if activity.Close() != nil {
		return ErrSignalingClose
	}
	return nil
}

func (service *SignalingService) String() string {
	if service == nil {
		return "voice_signaling_service{nil}"
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	return fmt.Sprintf("voice_signaling_service{tracked_sessions=%d,active_peers=%d,closing=%t}", len(service.states), service.activePeers, service.closing)
}

func (service *SignalingService) isClosing() bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.closing
}

func (service *SignalingService) GoString() string { return service.String() }

func fail(status int) error {
	return &signalingFailure{status: status, code: signalingErrorCode(status)}
}

func sanitizedSignalingError(status int) runtime.InvocationResponse {
	body, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: signalingErrorCode(status)})
	return runtime.InvocationResponse{StatusCode: status, Body: body}
}

func signalingErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid request"
	case http.StatusNotFound:
		return "not found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusGone:
		return "session unavailable"
	case http.StatusRequestEntityTooLarge:
		return "request too large"
	case http.StatusTooManyRequests:
		return "capacity unavailable"
	default:
		return "service unavailable"
	}
}
