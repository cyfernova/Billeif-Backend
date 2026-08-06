package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/composition"
	"invoice-backend/internal/voice/protocol"
	voiceruntime "invoice-backend/internal/voice/runtime"
	voicesession "invoice-backend/internal/voice/session"
	"invoice-backend/internal/voice/tools"
	"invoice-backend/internal/voice/webrtc"
)

const (
	runtimeRegion             = "ap-south-1"
	runtimeKVSChannelCount    = 12
	runtimeInvocationTimeout  = 12 * time.Second
	runtimeGatherTimeout      = 3 * time.Second
	runtimeAttachTimeout      = 15 * time.Second
	runtimeConnectTimeout     = 5 * time.Second
	runtimeRestartWindow      = 8 * time.Second
	runtimeICEExpiryMargin    = 30 * time.Second
	runtimeFirstOutputTimeout = 4 * time.Second
	runtimeTurnTimeout        = 20 * time.Second
	runtimeSTTFinalTimeout    = 8 * time.Second
	runtimeSTTWarmTimeout     = 30 * time.Second
)

var ErrInvalidRuntimeEnvironment = errors.New("invalid AgentCore voice runtime environment")

type environmentLookup func(string) (string, bool)

// runtimeEnvironment contains only non-secret identifiers and bounded policy.
// The Sarvam credential itself is never accepted from process environment.
type runtimeEnvironment struct {
	runtimeID       string
	region          string
	apiOrigin       string
	sarvamSecretARN string
	phonePoolID     string
	phoneClientID   string
	phoneRegion     string
	sessionTable    string
	channelARNs     []string
	protocolVersion int
	globalLimit     int64
	perUserLimit    int64
	leaseDuration   time.Duration
	maxDuration     time.Duration
	rotateAfter     time.Duration
}

type runtimeDependencies struct {
	Authorization webrtc.AuthorizationResolver
	Sessions      webrtc.SessionLookup
	LeaseRenewer  voicesession.LeaseRenewer
	ICE           webrtc.ICECredentialSource
	STT           sarvam.STTOpener
	Chat          sarvam.ChatStreamer
	Outputs       composition.OutputFactory
	Decoders      composition.DecoderFactory
	Peers         webrtc.PeerFactory
	Metrics       webrtc.SignalingMetrics
	Closers       []io.Closer
}

func loadRuntimeEnvironment(lookup environmentLookup) (runtimeEnvironment, error) {
	if lookup == nil {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	required := func(name string, maximum int) (string, error) {
		value, ok := lookup(name)
		if !ok || !safeEnvironmentValue(value, maximum) {
			return "", ErrInvalidRuntimeEnvironment
		}
		return value, nil
	}
	region, err := required("AWS_REGION", 32)
	if err != nil || region != runtimeRegion {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	runtimeID, err := required("RUNTIME_ID", 256)
	if err != nil || !safeRuntimeName(runtimeID) {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	apiOrigin, err := required("BILLEIF_API_ORIGIN", 2<<10)
	if err != nil || !validBilleifAPIOrigin(region, apiOrigin) {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	secretARN, err := required("SARVAM_SECRET_ARN", 512)
	if err != nil || !validSarvamSecretARN(region, secretARN) {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	phoneRegion, err := required("COGNITO_PHONE_REGION", 32)
	if err != nil || phoneRegion != region {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	phonePoolID, err := required("COGNITO_PHONE_USER_POOL_ID", 256)
	if err != nil || !safeCognitoValue(phonePoolID) {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	phoneClientID, err := required("COGNITO_PHONE_CLIENT_ID", 256)
	if err != nil || !safeCognitoValue(phoneClientID) {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	sessionTable, err := required("VOICE_SESSIONS_TABLE_NAME", 255)
	if err != nil || !safeTableName(sessionTable) {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	rawChannelARNs, err := required("VOICE_KVS_CHANNEL_ARNS", 16<<10)
	if err != nil {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	channelARNs := strings.Split(rawChannelARNs, ",")
	if len(channelARNs) != runtimeKVSChannelCount {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	for _, channelARN := range channelARNs {
		if !safeEnvironmentValue(channelARN, 512) {
			return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
		}
	}
	channelCount, err := requiredPositiveInt(lookup, "VOICE_KVS_CHANNEL_COUNT", runtimeKVSChannelCount)
	if err != nil || channelCount != runtimeKVSChannelCount {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	protocolVersion, err := requiredPositiveInt(lookup, "VOICE_PROTOCOL_VERSION", 16)
	if err != nil || protocolVersion != protocol.ProtocolVersion {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	globalLimit, err := requiredPositiveInt64(lookup, "VOICE_GLOBAL_CAPACITY_LIMIT", 10_000)
	if err != nil {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	perUserLimit, err := requiredPositiveInt64(lookup, "VOICE_PER_USER_CAPACITY_LIMIT", globalLimit)
	if err != nil || perUserLimit > globalLimit {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	leaseDuration, err := requiredDuration(lookup, "VOICE_SESSION_LEASE_DURATION", 30*time.Second, 5*time.Minute)
	if err != nil {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	maxDuration, err := requiredDuration(lookup, "VOICE_SESSION_MAX_DURATION", 5*time.Minute, 60*time.Minute)
	if err != nil {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	rotateAfter, err := requiredDuration(lookup, "VOICE_SESSION_ROTATE_AFTER", 5*time.Minute, maxDuration)
	if err != nil || rotateAfter >= maxDuration {
		return runtimeEnvironment{}, ErrInvalidRuntimeEnvironment
	}
	return runtimeEnvironment{
		runtimeID: runtimeID, region: region, apiOrigin: apiOrigin, sarvamSecretARN: secretARN,
		phonePoolID: phonePoolID, phoneClientID: phoneClientID, phoneRegion: phoneRegion,
		sessionTable: sessionTable, channelARNs: append([]string(nil), channelARNs...),
		protocolVersion: protocolVersion, globalLimit: globalLimit, perUserLimit: perUserLimit,
		leaseDuration: leaseDuration, maxDuration: maxDuration, rotateAfter: rotateAfter,
	}, nil
}

func newRuntimeApplication(environment runtimeEnvironment, dependencies runtimeDependencies) (*voiceruntime.Server, *http.Server, error) {
	slot := &invocationHandlerSlot{}
	orderedCloser := &runtimeOrderedCloser{slot: slot, dependencies: append([]io.Closer(nil), dependencies.Closers...)}
	ownershipTransferred := false
	defer func() {
		if !ownershipTransferred {
			_ = orderedCloser.Close()
		}
	}()
	if !validRuntimeDependencies(dependencies) {
		return nil, nil, ErrInvalidRuntimeEnvironment
	}
	heartbeatInterval := environment.leaseDuration / 4
	if heartbeatInterval > voicesession.DefaultLeaseHeartbeatWriteInterval {
		heartbeatInterval = voicesession.DefaultLeaseHeartbeatWriteInterval
	}
	bindingFactory, err := composition.NewBindingFactory(composition.FactoryConfig{
		STT: dependencies.STT, Chat: dependencies.Chat,
		LeaseRenewer: dependencies.LeaseRenewer, LeaseDuration: environment.leaseDuration,
		LeaseHeartbeatInterval: heartbeatInterval, LeaseWriteTimeout: 2 * time.Second,
		Tools:    tools.Config{Origin: environment.apiOrigin, Timeout: 3 * time.Second},
		Decoders: dependencies.Decoders, Outputs: dependencies.Outputs,
		MaxTokens: 180, FirstOutputTimeout: runtimeFirstOutputTimeout, TotalTimeout: runtimeTurnTimeout,
		STTFinalTimeout: runtimeSTTFinalTimeout, STTWarmTimeout: runtimeSTTWarmTimeout,
	})
	if err != nil {
		return nil, nil, ErrInvalidRuntimeEnvironment
	}

	application := voiceruntime.NewServer(voiceruntime.Config{
		RuntimeID: environment.runtimeID, AWSRegion: environment.region,
	}, slot, orderedCloser)

	signaling, err := webrtc.NewSignalingService(webrtc.SignalingConfig{
		RuntimeID: environment.runtimeID, AWSRegion: environment.region, ProtocolVersion: environment.protocolVersion,
		ChannelARNs: environment.channelARNs, InvocationTimeout: runtimeInvocationTimeout,
		GatherTimeout: runtimeGatherTimeout, AttachTimeout: runtimeAttachTimeout, ConnectTimeout: runtimeConnectTimeout,
		RestartWindow: runtimeRestartWindow, ICEExpiryMargin: runtimeICEExpiryMargin,
		MaxPeers: int(environment.globalLimit), MaxTrackedSessions: int(environment.globalLimit * 2),
		MaxPendingCandidates: 64, MaxCandidateBytes: 2 << 10, MaxControlQueue: 64,
	}, webrtc.SignalingDependencies{
		Authorization: dependencies.Authorization, Sessions: dependencies.Sessions, ICE: dependencies.ICE,
		Activities: application, Peers: dependencies.Peers, STTBindings: bindingFactory,
		Metrics: dependencies.Metrics,
	})
	if err != nil || slot.bind(signaling) != nil {
		_ = application.Shutdown(context.Background())
		return nil, nil, ErrInvalidRuntimeEnvironment
	}

	httpServer := &http.Server{
		Addr: runtimeAddress, Handler: application,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	ownershipTransferred = true
	return application, httpServer, nil
}

type invocationHandlerSlot struct {
	mu      sync.RWMutex
	handler voiceruntime.InvocationHandler
}

func (slot *invocationHandlerSlot) bind(handler voiceruntime.InvocationHandler) error {
	if slot == nil || handler == nil {
		return ErrInvalidRuntimeEnvironment
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.handler != nil {
		return ErrInvalidRuntimeEnvironment
	}
	slot.handler = handler
	return nil
}

func (slot *invocationHandlerSlot) Invoke(ctx context.Context, request voiceruntime.InvocationRequest) (voiceruntime.InvocationResponse, error) {
	if slot == nil {
		return voiceruntime.InvocationResponse{StatusCode: http.StatusServiceUnavailable, Body: json.RawMessage(`{"error":"runtime unavailable"}`)}, nil
	}
	slot.mu.RLock()
	handler := slot.handler
	slot.mu.RUnlock()
	if handler == nil {
		return voiceruntime.InvocationResponse{StatusCode: http.StatusServiceUnavailable, Body: json.RawMessage(`{"error":"runtime unavailable"}`)}, nil
	}
	return handler.Invoke(ctx, request)
}

func (slot *invocationHandlerSlot) Close() error {
	if slot == nil {
		return nil
	}
	slot.mu.Lock()
	handler := slot.handler
	slot.handler = nil
	slot.mu.Unlock()
	closer, _ := handler.(io.Closer)
	if closer == nil {
		return nil
	}
	return closer.Close()
}

type runtimeOrderedCloser struct {
	once         sync.Once
	slot         *invocationHandlerSlot
	dependencies []io.Closer
	err          error
}

func (closer *runtimeOrderedCloser) Close() error {
	if closer == nil {
		return nil
	}
	closer.once.Do(func() {
		var failures []error
		if err := closer.slot.Close(); err != nil {
			failures = append(failures, err)
		}
		for _, dependency := range closer.dependencies {
			if !nilRuntimeInterface(dependency) {
				if err := dependency.Close(); err != nil {
					failures = append(failures, err)
				}
			}
		}
		closer.err = errors.Join(failures...)
	})
	return closer.err
}

func validRuntimeDependencies(value runtimeDependencies) bool {
	return !nilRuntimeInterface(value.Authorization) && !nilRuntimeInterface(value.Sessions) &&
		!nilRuntimeInterface(value.LeaseRenewer) &&
		!nilRuntimeInterface(value.ICE) && !nilRuntimeInterface(value.STT) &&
		!nilRuntimeInterface(value.Chat) && !nilRuntimeInterface(value.Outputs) && !nilRuntimeInterface(value.Decoders) &&
		!nilRuntimeInterface(value.Metrics)
}

func nilRuntimeInterface(value any) bool {
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

func safeEnvironmentValue(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func safeRuntimeName(value string) bool {
	if !safeEnvironmentValue(value, 256) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' {
			continue
		}
		return false
	}
	return true
}

func safeCognitoValue(value string) bool {
	if !safeEnvironmentValue(value, 256) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("_-.", character) {
			continue
		}
		return false
	}
	return true
}

func safeTableName(value string) bool {
	if len(value) < 3 || !safeEnvironmentValue(value, 255) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("_-.", character) {
			continue
		}
		return false
	}
	return true
}

func validBilleifAPIOrigin(region, value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return false
	}
	host := parsed.Hostname()
	suffix := ".execute-api." + region + ".amazonaws.com"
	prefix := strings.TrimSuffix(host, suffix)
	if prefix == host || prefix == "" || strings.Contains(prefix, ".") || net.ParseIP(host) != nil || !safeLowerDNSLabel(prefix) {
		return false
	}
	stage := strings.TrimPrefix(parsed.Path, "/")
	return stage != "" && !strings.Contains(stage, "/") && safeStageName(stage)
}

func validSarvamSecretARN(region, value string) bool {
	parts := strings.SplitN(value, ":", 7)
	if len(parts) != 7 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "secretsmanager" ||
		parts[3] != region || len(parts[4]) != 12 || parts[5] != "secret" || parts[6] == "" {
		return false
	}
	for _, digit := range parts[4] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return safeEnvironmentValue(parts[6], 256)
}

func safeLowerDNSLabel(value string) bool {
	if value == "" || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return false
	}
	return true
}

func safeStageName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("_$-", character) {
			continue
		}
		return false
	}
	return true
}

func requiredPositiveInt(lookup environmentLookup, name string, maximum int) (int, error) {
	value, ok := lookup(name)
	if !ok || !safeEnvironmentValue(value, 16) {
		return 0, ErrInvalidRuntimeEnvironment
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 || parsed > maximum {
		return 0, ErrInvalidRuntimeEnvironment
	}
	return parsed, nil
}

func requiredPositiveInt64(lookup environmentLookup, name string, maximum int64) (int64, error) {
	value, ok := lookup(name)
	if !ok || !safeEnvironmentValue(value, 20) {
		return 0, ErrInvalidRuntimeEnvironment
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || parsed > maximum {
		return 0, ErrInvalidRuntimeEnvironment
	}
	return parsed, nil
}

func requiredDuration(lookup environmentLookup, name string, minimum, maximum time.Duration) (time.Duration, error) {
	value, ok := lookup(name)
	if !ok || !safeEnvironmentValue(value, 32) {
		return 0, ErrInvalidRuntimeEnvironment
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, ErrInvalidRuntimeEnvironment
	}
	return parsed, nil
}

func (environment runtimeEnvironment) String() string {
	return fmt.Sprintf("voice runtime environment{region=%s,channels=%d}", environment.region, len(environment.channelARNs))
}
