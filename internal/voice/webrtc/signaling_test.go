package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/voice/protocol"
	"invoice-backend/internal/voice/runtime"
	voicesession "invoice-backend/internal/voice/session"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var signalingTestNow = time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)

type mutableSignalingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *mutableSignalingClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *mutableSignalingClock) Set(value time.Time) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = value
}

func TestNewSignalingServiceRequiresExactlyTwelveUniqueChannels(t *testing.T) {
	t.Parallel()

	config := SignalingConfig{
		RuntimeID:            "voice-runtime",
		AWSRegion:            MumbaiRegion,
		ProtocolVersion:      1,
		ChannelARNs:          testChannelARNs(11),
		InvocationTimeout:    2 * time.Second,
		GatherTimeout:        time.Second,
		AttachTimeout:        time.Second,
		ICEExpiryMargin:      time.Minute,
		MaxPeers:             2,
		MaxTrackedSessions:   4,
		MaxPendingCandidates: 8,
	}

	if _, err := NewSignalingService(config, SignalingDependencies{}); err == nil {
		t.Fatal("NewSignalingService accepted eleven KVS channels")
	}

	config.ChannelARNs = testChannelARNs(12)
	config.ChannelARNs[11] = config.ChannelARNs[0]
	if _, err := NewSignalingService(config, SignalingDependencies{}); err == nil {
		t.Fatal("NewSignalingService accepted a duplicate KVS channel")
	}
}

func TestSignalingConfigRejectsImpossibleICEExpiryMargin(t *testing.T) {
	config := validSignalingConfig(testChannelARNs(requiredKVSChannelCount))
	config.ICEExpiryMargin = maxSignalingCredentialTTL
	if _, err := validateSignalingConfig(config); !errors.Is(err, ErrInvalidSignalingConfig) {
		t.Fatalf("validateSignalingConfig() error = %v, want %v", err, ErrInvalidSignalingConfig)
	}
}

func TestNewSignalingServiceRequiresSTTBindingFactory(t *testing.T) {
	dependencies := SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{},
		Sessions:      &fakeSessionLookup{},
		ICE:           &fakeICECredentialSource{},
		Activities:    newFakeActivitySource(),
		Peers:         &fakePeerFactory{},
	}
	if _, err := NewSignalingService(validSignalingConfig(testChannelARNs(requiredKVSChannelCount)), dependencies); !errors.Is(err, ErrInvalidSignalingConfig) {
		t.Fatalf("NewSignalingService(nil STT binding factory) error = %v, want ErrInvalidSignalingConfig", err)
	}
	var typedNil *fakeSTTBindingFactory
	dependencies.STTBindings = typedNil
	if _, err := NewSignalingService(validSignalingConfig(testChannelARNs(requiredKVSChannelCount)), dependencies); !errors.Is(err, ErrInvalidSignalingConfig) {
		t.Fatalf("NewSignalingService(typed nil STT binding factory) error = %v, want ErrInvalidSignalingConfig", err)
	}
}

func TestSignalingAttachCreatesOnePersistentSTTBindingAndClosesOwnershipExactlyOnce(t *testing.T) {
	logicalSession := validSignalingSession()
	lifecycle := &lifecycleRecorder{}
	rawActivity := &countingActivityCloser{lifecycle: lifecycle}
	persistentContext := context.WithValue(context.Background(), struct{ name string }{"persistent"}, "context-canary")
	activities := &configuredActivitySource{ctx: persistentContext, activity: rawActivity}
	bindings := &fakeSTTBindingFactory{lifecycle: lifecycle}
	peerFactory := &fakePeerFactory{peer: newFakeSignalingPeer()}
	service := newTestSignalingService(t, testChannelARNs(requiredKVSChannelCount), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  activities,
		Peers:       peerFactory,
		STTBindings: bindings,
	})

	require.Equal(t, http.StatusOK, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)
	require.Equal(t, http.StatusOK, invokeSignaling(t, service, logicalSession, attachBody(2)).StatusCode, "refresh attach must reuse the existing peer binding")
	configs := bindings.configsSnapshot()
	require.Len(t, configs, 1)
	require.Equal(t, logicalSession.ID, configs[0].Session.ID)
	require.Equal(t, "en-IN", configs[0].Session.FallbackLanguage)
	require.Same(t, persistentContext, configs[0].Context)
	require.NotNil(t, configs[0].Activity)
	require.NotNil(t, peerFactory.config.STTBinding)
	require.Same(t, peerFactory.config.Context, configs[0].Context)

	require.NoError(t, service.Close())
	require.NoError(t, service.Close())
	require.Equal(t, 1, bindings.bindingsSnapshot()[0].closeCount(), "binding owner must close the raw binding once")
	require.Equal(t, 1, rawActivity.closeCount(), "raw persistent activity must close exactly once")
	require.Equal(t, []string{"binding.create", "binding.close", "activity.close"}, lifecycle.snapshot())
}

func TestSignalingSTTBindingConstructionFailureClosesActivityExactlyOnce(t *testing.T) {
	logicalSession := validSignalingSession()
	for _, test := range []struct {
		name    string
		factory *fakeSTTBindingFactory
	}{
		{
			name: "error after constructor consumed ownership",
			factory: &fakeSTTBindingFactory{
				err: errors.New("binding-construction-sensitive-canary"), closeActivityBeforeReturn: true,
			},
		},
		{name: "panic before ownership consumed", factory: &fakeSTTBindingFactory{panicValue: "binding-panic-sensitive-canary"}},
		{name: "nil binding", factory: &fakeSTTBindingFactory{returnNil: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rawActivity := &countingActivityCloser{}
			peerFactory := &fakePeerFactory{peer: newFakeSignalingPeer()}
			service := newTestSignalingService(t, testChannelARNs(requiredKVSChannelCount), SignalingDependencies{
				Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
					UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
				}},
				Sessions:    &fakeSessionLookup{value: logicalSession},
				ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
				Activities:  &configuredActivitySource{ctx: context.Background(), activity: rawActivity},
				Peers:       peerFactory,
				STTBindings: test.factory,
			})
			t.Cleanup(func() { _ = service.Close() })

			response := invokeSignaling(t, service, logicalSession, attachBody(1))
			require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
			assertSanitized(t, response.Body, "binding-construction-sensitive-canary", "binding-panic-sensitive-canary")
			require.Equal(t, 1, rawActivity.closeCount())
			require.Empty(t, peerFactory.calls, "peer creation must not run without a live binding")
		})
	}
}

func TestSignalingPeerCreationFailureClosesBindingBeforeActivity(t *testing.T) {
	logicalSession := validSignalingSession()
	lifecycle := &lifecycleRecorder{}
	rawActivity := &countingActivityCloser{lifecycle: lifecycle}
	bindings := &fakeSTTBindingFactory{lifecycle: lifecycle}
	service := newTestSignalingService(t, testChannelARNs(requiredKVSChannelCount), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  &configuredActivitySource{ctx: context.Background(), activity: rawActivity},
		Peers:       &fakePeerFactory{err: errors.New("peer-construction-sensitive-canary")},
		STTBindings: bindings,
	})
	t.Cleanup(func() { _ = service.Close() })

	response := invokeSignaling(t, service, logicalSession, attachBody(1))
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, 1, bindings.bindingsSnapshot()[0].closeCount())
	require.Equal(t, 1, rawActivity.closeCount())
	require.Equal(t, []string{"binding.create", "binding.close", "activity.close"}, lifecycle.snapshot())
}

func TestOwnedSTTBindingFencesConcurrentHandleAndClose(t *testing.T) {
	underlying := newBlockingSTTBinding()
	activity := &countingActivityCloser{}
	binding := &ownedSTTBinding{binding: underlying, activity: activity}

	handleResult := make(chan error, 1)
	go func() { handleResult <- binding.HandleOpus([]byte{0x01}) }()
	awaitSignal(t, underlying.handleStarted, time.Second, "binding HandleOpus entry")
	closeResult := make(chan error, 1)
	closeAttempted := make(chan struct{})
	go func() {
		close(closeAttempted)
		closeResult <- binding.Close()
	}()
	awaitSignal(t, closeAttempted, time.Second, "binding Close attempt")
	select {
	case <-underlying.closeStarted:
		t.Fatal("underlying binding closed while HandleOpus was in flight")
	default:
	}
	close(underlying.releaseHandle)
	require.NoError(t, <-handleResult)
	require.NoError(t, <-closeResult)
	require.Equal(t, 1, underlying.closeCount())
	require.Equal(t, 1, activity.closeCount())

	require.ErrorIs(t, binding.HandleOpus([]byte{0x02}), ErrPeerClosed)
	require.ErrorIs(t, binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechStarted}), ErrPeerClosed)
	require.NoError(t, binding.Close())
	require.Equal(t, 1, underlying.closeCount())
	require.Equal(t, 1, activity.closeCount())
}

func TestSignalingAttachUsesTrustedSessionAndPersistedChannel(t *testing.T) {
	t.Parallel()

	channels := testChannelARNs(12)
	logicalSession := validSignalingSession()
	logicalSession.KVSChannelIndex = 7
	identity := TrustedIdentity{UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "mobile-client"}
	resolver := &fakeAuthorizationResolver{identity: identity}
	lookup := &fakeSessionLookup{value: logicalSession}
	credentials := TURNCredentials{
		uris:      []string{"turn:v-attach.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp"},
		username:  "attach-user-secret",
		password:  "attach-password-secret",
		ExpiresAt: signalingTestNow.Add(5 * time.Minute),
	}
	ice := &fakeICECredentialSource{credentials: credentials}
	activity := newFakeActivitySource()
	peer := newFakeSignalingPeer()
	factory := &fakePeerFactory{peer: peer, beforeCreate: func() {
		require.Len(t, activity.calls(), 1, "persistent activity must be acquired before peer creation")
	}}
	service := newTestSignalingService(t, channels, SignalingDependencies{
		Authorization: resolver,
		Sessions:      lookup,
		ICE:           ice,
		Activities:    activity,
		Peers:         factory,
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	invocationContext, cancelInvocation := context.WithCancel(context.Background())
	response, err := service.Invoke(invocationContext, runtime.InvocationRequest{
		Body: json.RawMessage(`{
			"type":"session.attach",
			"protocol_version":1,
			"session_id":"voice_01KTEST",
			"sequence":1,
			"client":{"platform":"android","app_version":"1.2.3"}
		}`),
		Authorization:    "Bearer authorization-canary",
		RuntimeSessionID: logicalSession.RuntimeSessionID,
		RuntimeID:        "voice-runtime",
		AWSRegion:        MumbaiRegion,
	})
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	assert.JSONEq(t, `{
		"type":"session.attached",
		"protocol_version":1,
		"session_id":"voice_01KTEST",
		"sequence":1,
		"ice_servers":[{
			"urls":["turn:v-attach.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp"],
			"username":"attach-user-secret",
			"credential":"attach-password-secret"
		}],
		"ice_expires_at":"2026-08-06T12:05:00Z",
		"ice_transport_policy":"relay"
	}`, string(response.Body))

	require.Equal(t, "Bearer authorization-canary", resolver.authorization)
	require.Equal(t, voicesession.Scope{
		UserID:      logicalSession.UserID,
		BusinessID:  logicalSession.BusinessID,
		AllBranches: true,
	}, lookup.scope)
	require.Equal(t, logicalSession.ID, lookup.sessionID)
	require.Equal(t, channels[7], ice.channelARN)
	require.NotNil(t, factory.config.Context)
	require.Equal(t, logicalSession.ID, factory.config.SessionID)
	require.Equal(t, credentials.uris, factory.config.TURNCredentials.uris)
	require.Equal(t, credentials.username, factory.config.TURNCredentials.username)
	require.Equal(t, credentials.password, factory.config.TURNCredentials.password)

	cancelInvocation()
	assert.NoError(t, factory.config.Context.Err(), "peer context must outlive one invocation")
}

func TestSignalingSelectsEveryPersistedKVSChannelWithoutRehashing(t *testing.T) {
	t.Parallel()

	channels := testChannelARNs(12)
	for index := range channels {
		index := index
		t.Run(fmt.Sprintf("index_%d", index), func(t *testing.T) {
			t.Parallel()
			logicalSession := validSignalingSession()
			logicalSession.KVSChannelIndex = index
			ice := &fakeICECredentialSource{credentials: validSignalingCredentials()}
			service := newTestSignalingService(t, channels, SignalingDependencies{
				Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
					UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
				}},
				Sessions:   &fakeSessionLookup{value: logicalSession},
				ICE:        ice,
				Activities: newFakeActivitySource(),
				Peers:      &fakePeerFactory{peer: newFakeSignalingPeer()},
			})
			t.Cleanup(func() { require.NoError(t, service.Close()) })

			require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)
			require.Equal(t, channels[index], ice.channelARN)
		})
	}
}

func TestSignalingOfferCandidateRestartFlowAndExactSequences(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	resolver := &fakeAuthorizationResolver{identity: TrustedIdentity{
		UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
	}}
	lookup := &fakeSessionLookup{value: logicalSession}
	peer := newFakeSignalingPeer()
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: resolver,
		Sessions:      lookup,
		ICE:           &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:    newFakeActivitySource(),
		Peers:         &fakePeerFactory{peer: peer},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)
	require.Equal(t, 204, invokeSignaling(t, service, logicalSession, candidateBody(logicalSession.ID, 2, "candidate-before-offer", "ufrag-one")).StatusCode)
	response := invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 3, "initial-offer"))
	require.Equal(t, 200, response.StatusCode)
	require.JSONEq(t, `{"type":"webrtc.answer","protocol_version":1,"session_id":"voice_01KTEST","sequence":3,"sdp":"answer-sdp"}`, string(response.Body))
	require.Equal(t, 204, invokeSignaling(t, service, logicalSession, candidateBody(logicalSession.ID, 4, "candidate-after-offer", "ufrag-one")).StatusCode)
	response = invokeSignaling(t, service, logicalSession, restartBody(logicalSession.ID, 5, "fresh-restart-offer"))
	require.Equal(t, 200, response.StatusCode)

	answers := peer.answerCalls()
	require.Equal(t, []fakeAnswerCall{
		{SDP: "initial-offer", Restart: false},
		{SDP: "fresh-restart-offer", Restart: true},
	}, answers)
	candidates := peer.candidateCalls()
	require.Len(t, candidates, 2)
	require.Equal(t, "candidate-before-offer", candidates[0].candidate)
	require.Equal(t, "candidate-after-offer", candidates[1].candidate)
	require.Equal(t, 5, resolver.calls, "Authorization must be resolved for every valid signaling request")
	require.Equal(t, 5, lookup.calls, "durable session must be resolved for every valid signaling request")

	require.Equal(t, 409, invokeSignaling(t, service, logicalSession, restartBody(logicalSession.ID, 5, "duplicate-sensitive-offer")).StatusCode)
	require.Equal(t, 409, invokeSignaling(t, service, logicalSession, restartBody(logicalSession.ID, 7, "out-of-order-sensitive-offer")).StatusCode)
}

func TestSignalingConcurrentSameSequenceRunsExactlyOneOperation(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	peer := newFakeSignalingPeer()
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:   &fakeSessionLookup{value: logicalSession},
		ICE:        &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities: newFakeActivitySource(),
		Peers:      &fakePeerFactory{peer: peer},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	start := make(chan struct{})
	statuses := make(chan int, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			response, err := service.Invoke(context.Background(), signalingInvocation(logicalSession, offerBody(logicalSession.ID, 2, "concurrent-offer")))
			if err != nil {
				statuses <- 0
				return
			}
			statuses <- response.StatusCode
		}()
	}
	close(start)
	group.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	require.Equal(t, 1, counts[200])
	require.Equal(t, 1, counts[409])
	require.Len(t, peer.answerCalls(), 1)
}

func TestSignalingStrictDecodeRejectsDuplicateFieldsBeforeAuthorization(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	resolver := &fakeAuthorizationResolver{identity: TrustedIdentity{
		UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
	}}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: resolver,
		Sessions:      &fakeSessionLookup{value: logicalSession},
		ICE:           &fakeICECredentialSource{},
		Activities:    newFakeActivitySource(),
		Peers:         &fakePeerFactory{peer: newFakeSignalingPeer()},
		STTBindings:   &fakeSTTBindingFactory{},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	response, err := service.Invoke(context.Background(), runtime.InvocationRequest{
		Body: json.RawMessage(`{
			"type":"session.attach",
			"protocol_version":1,
			"session_id":"voice_01KTEST",
			"sequence":1,
			"sequence":2,
			"client":{"platform":"android","app_version":"1.2.3"}
		}`),
	})
	require.NoError(t, err)
	require.Equal(t, 400, response.StatusCode)
	require.JSONEq(t, `{"error":"invalid request"}`, string(response.Body))
	require.Zero(t, resolver.calls, "ambiguous JSON must fail before a token is handled")
}

func TestSignalingStrictDecodeRejectsForbiddenNullFields(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	resolver := &fakeAuthorizationResolver{identity: TrustedIdentity{
		UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
	}}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: resolver,
		Sessions:      &fakeSessionLookup{value: logicalSession},
		ICE:           &fakeICECredentialSource{},
		Activities:    newFakeActivitySource(),
		Peers:         &fakePeerFactory{peer: newFakeSignalingPeer()},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	response, err := service.Invoke(context.Background(), runtime.InvocationRequest{
		Body: json.RawMessage(`{
			"type":"webrtc.offer",
			"protocol_version":1,
			"session_id":"voice_01KTEST",
			"sequence":1,
			"sdp":"fresh-offer",
			"client":null
		}`),
	})
	require.NoError(t, err)
	require.Equal(t, 400, response.StatusCode)
	require.Zero(t, resolver.calls, "type-invalid JSON must fail before a token is handled")
}

func TestSignalingStrictRequestShapeAndBounds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "unknown top level", body: `{"type":"webrtc.offer","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"sdp":"offer","audio":"encoded"}`, wantStatus: 400},
		{name: "case variant common key", body: `{"Type":"webrtc.offer","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"sdp":"offer"}`, wantStatus: 400},
		{name: "case colliding common key", body: `{"type":"webrtc.offer","Type":"webrtc.offer","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"sdp":"offer"}`, wantStatus: 400},
		{name: "unknown nested", body: `{"type":"session.attach","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"client":{"platform":"android","app_version":"1.2.3","token":"secret"}}`, wantStatus: 400},
		{name: "case variant client key", body: `{"type":"session.attach","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"client":{"Platform":"android","app_version":"1.2.3"}}`, wantStatus: 400},
		{name: "case variant candidate key", body: `{"type":"webrtc.candidate","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"candidate":{"Candidate":"candidate","sdp_mid":"0","sdp_mline_index":0,"username_fragment":"ufrag"}}`, wantStatus: 400},
		{name: "trailing object", body: attachBody(1) + `{}`, wantStatus: 400},
		{name: "top level array", body: `[]`, wantStatus: 400},
		{name: "zero sequence", body: strings.ReplaceAll(attachBody(1), `"sequence":1`, `"sequence":0`), wantStatus: 400},
		{name: "offer missing sdp", body: `{"type":"webrtc.offer","protocol_version":1,"session_id":"voice_01KTEST","sequence":1}`, wantStatus: 400},
		{name: "restart missing fresh sdp", body: `{"type":"webrtc.restart","protocol_version":1,"session_id":"voice_01KTEST","sequence":1}`, wantStatus: 400},
		{name: "candidate missing ufrag", body: `{"type":"webrtc.candidate","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"candidate":{"candidate":"candidate","sdp_mid":"0","sdp_mline_index":0}}`, wantStatus: 400},
		{name: "candidate invalid mline", body: `{"type":"webrtc.candidate","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"candidate":{"candidate":"candidate","sdp_mid":"0","sdp_mline_index":2,"username_fragment":"ufrag"}}`, wantStatus: 400},
		{name: "attach forbids sdp", body: `{"type":"session.attach","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"client":{"platform":"android","app_version":"1.2.3"},"sdp":"forbidden"}`, wantStatus: 400},
		{name: "offer forbids candidate", body: `{"type":"webrtc.offer","protocol_version":1,"session_id":"voice_01KTEST","sequence":1,"sdp":"offer","candidate":null}`, wantStatus: 400},
		{name: "outer body", body: `{"padding":"` + strings.Repeat("x", maxSignalingRequestBytes) + `"}`, wantStatus: 413},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			logicalSession := validSignalingSession()
			resolver := &fakeAuthorizationResolver{identity: TrustedIdentity{
				UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
			}}
			service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
				Authorization: resolver,
				Sessions:      &fakeSessionLookup{value: logicalSession},
				ICE:           &fakeICECredentialSource{},
				Activities:    newFakeActivitySource(),
				Peers:         &fakePeerFactory{peer: newFakeSignalingPeer()},
			})
			t.Cleanup(func() { require.NoError(t, service.Close()) })
			response := invokeSignaling(t, service, logicalSession, testCase.body)
			require.Equal(t, testCase.wantStatus, response.StatusCode)
			require.Zero(t, resolver.calls)
			assertSanitized(t, response.Body, "encoded", "secret", "forbidden", "candidate", "offer")
		})
	}
}

func TestSignalingRejectsShutdownBeforeHandlingSensitiveDependencies(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	resolver := &fakeAuthorizationResolver{identity: TrustedIdentity{
		UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
	}}
	lookup := &fakeSessionLookup{value: logicalSession}
	ice := &fakeICECredentialSource{}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: resolver,
		Sessions:      lookup,
		ICE:           ice,
		Activities:    newFakeActivitySource(),
		Peers:         &fakePeerFactory{peer: newFakeSignalingPeer()},
	})
	require.NoError(t, service.Close())

	response := invokeSignaling(t, service, logicalSession, attachBody(1))
	require.Equal(t, 503, response.StatusCode)
	require.Zero(t, resolver.calls)
	require.Zero(t, lookup.calls)
	require.Zero(t, ice.calls)
}

func TestSignalingReservesCapacityBeforeRequestingCredentials(t *testing.T) {
	t.Parallel()

	first := validSignalingSession()
	lookup := &fakeSessionLookup{value: first}
	resolver := &fakeAuthorizationResolver{identity: TrustedIdentity{
		UserID: first.UserID, BusinessID: first.BusinessID, ClientID: "client",
	}}
	ice := &fakeICECredentialSource{credentials: validSignalingCredentials()}
	config := validSignalingConfig(testChannelARNs(12))
	config.MaxPeers = 1
	service, err := NewSignalingService(config, SignalingDependencies{
		Authorization: resolver,
		Sessions:      lookup,
		ICE:           ice,
		Activities:    newFakeActivitySource(),
		Peers:         &fakePeerFactory{peer: newFakeSignalingPeer()},
		STTBindings:   &fakeSTTBindingFactory{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	response := invokeSignaling(t, service, first, attachBody(1))
	require.Equal(t, 200, response.StatusCode)

	second := validSignalingSession()
	second.ID = "voice_01KSECOND"
	second.RuntimeSessionID = "voice-session-01KSECOND00000000000000000"
	lookup.set(second)
	response = invokeSignaling(t, service, second, strings.ReplaceAll(attachBody(1), first.ID, second.ID))
	require.Equal(t, 429, response.StatusCode)
	require.Equal(t, 1, ice.calls, "full peer capacity must not spend a KVS credential request")
}

func TestSignalingMapsBoundedSDPAndCandidateViolationsToPayloadTooLarge(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	resolver := &fakeAuthorizationResolver{identity: TrustedIdentity{
		UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
	}}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: resolver,
		Sessions:      &fakeSessionLookup{value: logicalSession},
		ICE:           &fakeICECredentialSource{},
		Activities:    newFakeActivitySource(),
		Peers:         &fakePeerFactory{peer: newFakeSignalingPeer()},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	oversizedSDP, err := json.Marshal(map[string]any{
		"type": "webrtc.offer", "protocol_version": 1, "session_id": logicalSession.ID, "sequence": 1,
		"sdp": strings.Repeat("s", maxSignalingSDPBytes+1),
	})
	require.NoError(t, err)
	response := invokeSignaling(t, service, logicalSession, string(oversizedSDP))
	require.Equal(t, 413, response.StatusCode)

	oversizedCandidate, err := json.Marshal(map[string]any{
		"type": "webrtc.candidate", "protocol_version": 1, "session_id": logicalSession.ID, "sequence": 1,
		"candidate": map[string]any{
			"candidate": strings.Repeat("c", maxSignalingCandidateBytes+1), "sdp_mid": "0", "sdp_mline_index": 0, "username_fragment": "ufrag",
		},
	})
	require.NoError(t, err)
	response = invokeSignaling(t, service, logicalSession, string(oversizedCandidate))
	require.Equal(t, 413, response.StatusCode)
	require.Zero(t, resolver.calls, "oversized fields must fail before Authorization is handled")
}

func TestSignalingClosesActivityWhenPeerFactoryPanicsOrReturnsClosedPeer(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		factory *fakePeerFactory
	}{
		{name: "factory panic", factory: &fakePeerFactory{panicValue: "factory-sensitive-canary"}},
		{name: "already closed", factory: &fakePeerFactory{peer: closedFakeSignalingPeer()}},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			logicalSession := validSignalingSession()
			activity := newFakeActivitySource()
			service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
				Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
					UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
				}},
				Sessions:   &fakeSessionLookup{value: logicalSession},
				ICE:        &fakeICECredentialSource{credentials: validSignalingCredentials()},
				Activities: activity,
				Peers:      testCase.factory,
			})
			t.Cleanup(func() { require.NoError(t, service.Close()) })

			var response runtime.InvocationResponse
			require.NotPanics(t, func() {
				response = invokeSignaling(t, service, logicalSession, attachBody(1))
			})
			require.Equal(t, 503, response.StatusCode)
			select {
			case <-activity.closer.closed:
			default:
				t.Fatal("failed attach leaked persistent activity")
			}
			assertSanitized(t, response.Body, "factory-sensitive-canary")
		})
	}
}

func TestSignalingMapsPeerErrorsAndReusesFailedSequence(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	peer := newFakeSignalingPeer()
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  newFakeActivitySource(),
		Peers:       &fakePeerFactory{peer: peer},
		STTBindings: &fakeSTTBindingFactory{},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	peer.setAnswerError(ErrPeerInvalidDescription)
	response := invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "invalid-sensitive-sdp"))
	require.Equal(t, 400, response.StatusCode)
	assertSanitized(t, response.Body, "invalid-sensitive-sdp")

	peer.setAnswerError(nil)
	response = invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "corrected-offer"))
	require.Equal(t, 200, response.StatusCode, "failed operation must not consume sequence 2")
	response = invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "duplicate-offer"))
	require.Equal(t, 409, response.StatusCode)
}

func TestSignalingMapsStoppedInactiveSessionToGone(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	logicalSession.Status = voicesession.StatusClosed
	logicalSession.RuntimeState = voicesession.RuntimeStateStopped
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:   &fakeSessionLookup{value: logicalSession},
		ICE:        &fakeICECredentialSource{},
		Activities: newFakeActivitySource(),
		Peers:      &fakePeerFactory{peer: newFakeSignalingPeer()},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	response := invokeSignaling(t, service, logicalSession, attachBody(1))
	require.Equal(t, 410, response.StatusCode)
}

func TestSignalingDurableSessionAndTrustedRuntimeFences(t *testing.T) {
	t.Parallel()

	providerCanary := "provider-sensitive-canary"
	testCases := []struct {
		name             string
		wantStatus       int
		mutateSession    func(*voicesession.Session)
		mutateInvocation func(*runtime.InvocationRequest)
		identity         *TrustedIdentity
		authorizationErr error
		lookupErr        error
	}{
		{name: "authorization rejected", wantStatus: 404, authorizationErr: ErrSignalingUnauthorized},
		{name: "authorization dependency", wantStatus: 503, authorizationErr: errors.New(providerCanary)},
		{name: "missing trusted client", wantStatus: 404, identity: &TrustedIdentity{UserID: "user-123", BusinessID: "business-456"}},
		{name: "session not found", wantStatus: 404, lookupErr: voicesession.ErrNotFound},
		{name: "session dependency", wantStatus: 503, lookupErr: errors.New(providerCanary)},
		{name: "ownership mismatch", wantStatus: 404, mutateSession: func(value *voicesession.Session) { value.UserID = "other-user" }},
		{name: "logical id mismatch", wantStatus: 404, mutateSession: func(value *voicesession.Session) { value.ID = "voice_other" }},
		{name: "runtime header mismatch", wantStatus: 409, mutateInvocation: func(value *runtime.InvocationRequest) {
			value.RuntimeSessionID = "voice-session-other-runtime-000000000000"
		}},
		{name: "runtime resource mismatch", wantStatus: 409, mutateInvocation: func(value *runtime.InvocationRequest) { value.RuntimeID = "other-runtime" }},
		{name: "region mismatch", wantStatus: 409, mutateInvocation: func(value *runtime.InvocationRequest) { value.AWSRegion = "us-east-1" }},
		{name: "closing", wantStatus: 410, mutateSession: func(value *voicesession.Session) { value.Status = voicesession.StatusClosing }},
		{name: "runtime stopped", wantStatus: 410, mutateSession: func(value *voicesession.Session) { value.RuntimeState = voicesession.RuntimeStateStopped }},
		{name: "session expired", wantStatus: 410, mutateSession: func(value *voicesession.Session) { value.ExpiresAt = signalingTestNow }},
		{name: "lease expired", wantStatus: 410, mutateSession: func(value *voicesession.Session) { value.LeaseExpiresAt = signalingTestNow }},
		{name: "protocol mismatch", wantStatus: 409, mutateSession: func(value *voicesession.Session) { value.ProtocolVersion = 2 }},
		{name: "negative channel", wantStatus: 409, mutateSession: func(value *voicesession.Session) { value.KVSChannelIndex = -1 }},
		{name: "channel out of range", wantStatus: 409, mutateSession: func(value *voicesession.Session) { value.KVSChannelIndex = 12 }},
		{name: "client platform mismatch", wantStatus: 409, mutateSession: func(value *voicesession.Session) { value.ClientPlatform = "ios" }},
		{name: "client version mismatch", wantStatus: 409, mutateSession: func(value *voicesession.Session) { value.ClientAppVersion = "9.9.9" }},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			logicalSession := validSignalingSession()
			if testCase.mutateSession != nil {
				testCase.mutateSession(logicalSession)
			}
			identity := TrustedIdentity{UserID: "user-123", BusinessID: "business-456", ClientID: "client"}
			if testCase.identity != nil {
				identity = *testCase.identity
			}
			resolver := &fakeAuthorizationResolver{identity: identity, err: testCase.authorizationErr}
			lookup := &fakeSessionLookup{value: logicalSession, err: testCase.lookupErr}
			service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
				Authorization: resolver,
				Sessions:      lookup,
				ICE:           &fakeICECredentialSource{credentials: validSignalingCredentials()},
				Activities:    newFakeActivitySource(),
				Peers:         &fakePeerFactory{peer: newFakeSignalingPeer()},
			})
			t.Cleanup(func() { require.NoError(t, service.Close()) })

			invocation := signalingInvocation(validSignalingSession(), attachBody(1))
			if testCase.mutateInvocation != nil {
				testCase.mutateInvocation(&invocation)
			}
			response, err := service.Invoke(context.Background(), invocation)
			require.NoError(t, err)
			require.Equal(t, testCase.wantStatus, response.StatusCode)
			assertSanitized(t, response.Body,
				providerCanary, invocation.Authorization, string(invocation.Body), logicalSession.RuntimeSessionID,
			)
		})
	}
}

func TestSignalingRefreshesActivePeerWithoutCreatingAnotherActivity(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	peer := newFakeSignalingPeer()
	ice := &fakeICECredentialSource{credentials: validSignalingCredentials()}
	activity := newFakeActivitySource()
	factory := &fakePeerFactory{peer: peer}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:   &fakeSessionLookup{value: logicalSession},
		ICE:        ice,
		Activities: activity,
		Peers:      factory,
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "initial-offer")).StatusCode)

	refreshed := validSignalingCredentials()
	refreshed.username = "refreshed-user-secret"
	refreshed.password = "refreshed-password-secret"
	ice.setCredentials(refreshed)
	response := invokeSignaling(t, service, logicalSession, attachBody(3))
	require.Equal(t, 200, response.StatusCode)
	assert.Contains(t, string(response.Body), "refreshed-user-secret")
	require.Len(t, peer.refreshes(), 1)
	require.Equal(t, refreshed.username, peer.refreshes()[0].username)
	require.Len(t, activity.calls(), 1)
	require.Len(t, factory.calls, 1)
}

func TestSignalingDelegatesCandidateBoundsPerICEGeneration(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	peer := newFakeSignalingPeer()
	config := validSignalingConfig(testChannelARNs(12))
	config.MaxPendingCandidates = 1
	service, err := NewSignalingService(config, SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  newFakeActivitySource(),
		Peers:       &fakePeerFactory{peer: peer},
		STTBindings: &fakeSTTBindingFactory{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)
	require.Equal(t, 204, invokeSignaling(t, service, logicalSession, candidateBody(logicalSession.ID, 2, "candidate-a", "ufrag-a")).StatusCode)
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 3, "initial-offer")).StatusCode)
	require.Equal(t, 204, invokeSignaling(t, service, logicalSession, candidateBody(logicalSession.ID, 4, "candidate-b", "ufrag-b")).StatusCode,
		"a fresh ICE generation must not inherit signaling's prior candidate count")
}

func TestNewSignalingServiceRejectsPeerLimitsThatWouldFailEveryAttach(t *testing.T) {
	t.Parallel()

	dependencies := SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{}, Sessions: &fakeSessionLookup{}, ICE: &fakeICECredentialSource{},
		Activities: newFakeActivitySource(), Peers: &fakePeerFactory{},
	}
	config := validSignalingConfig(testChannelARNs(12))
	config.MaxPendingCandidates = 65
	if _, err := NewSignalingService(config, dependencies); !errors.Is(err, ErrInvalidSignalingConfig) {
		t.Fatalf("MaxPendingCandidates=65 error = %v, want invalid config", err)
	}
	config = validSignalingConfig(testChannelARNs(12))
	config.MaxControlQueue = 65
	if _, err := NewSignalingService(config, dependencies); !errors.Is(err, ErrInvalidSignalingConfig) {
		t.Fatalf("MaxControlQueue=65 error = %v, want invalid config", err)
	}
}

func TestSignalingAttachTimeoutAndCloseReleasePersistentActivity(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	peer := newFakeSignalingPeer()
	activity := newFakeActivitySource()
	config := validSignalingConfig(testChannelARNs(12))
	config.AttachTimeout = 20 * time.Millisecond
	service, err := NewSignalingService(config, SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  activity,
		Peers:       &fakePeerFactory{peer: peer},
		STTBindings: &fakeSTTBindingFactory{},
	})
	require.NoError(t, err)
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	select {
	case <-peer.Done():
	case <-time.After(time.Second):
		t.Fatal("unoffered peer was not closed at attach timeout")
	}
	select {
	case <-activity.closer.closed:
	case <-time.After(time.Second):
		t.Fatal("attach timeout did not release persistent activity")
	}
	require.NoError(t, service.Close())
	require.NoError(t, service.Close(), "Close must remain idempotent")
}

func TestSignalingBoundsTrackedTombstonesAndPreservesActiveState(t *testing.T) {
	t.Parallel()

	lookup := &fakeSessionLookup{}
	config := validSignalingConfig(testChannelARNs(12))
	config.MaxPeers = 1
	config.MaxTrackedSessions = 2
	service, err := NewSignalingService(config, SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{UserID: "user-123", BusinessID: "business-456", ClientID: "client"}},
		Sessions:      lookup,
		ICE:           &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:    newFakeActivitySource(),
		Peers:         &fakePeerFactory{peer: newFakeSignalingPeer()},
		STTBindings:   &fakeSTTBindingFactory{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	for index := 0; index < 8; index++ {
		logicalSession := validSignalingSession()
		logicalSession.ID = fmt.Sprintf("voice_tombstone_%d", index)
		logicalSession.RuntimeSessionID = fmt.Sprintf("voice-session-tombstone-%02d-0000000000", index)
		lookup.set(logicalSession)
		response := invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 1, "offer"))
		require.Equal(t, 409, response.StatusCode)
		service.mu.Lock()
		require.LessOrEqual(t, len(service.states), 2)
		service.mu.Unlock()
	}
}

func TestSignalingSanitizesDependencyAndPeerPanics(t *testing.T) {
	t.Parallel()

	panicCanary := "panic-sensitive-canary"
	logicalSession := validSignalingSession()
	testCases := []struct {
		name         string
		dependencies func() SignalingDependencies
		prepare      func(*SignalingService)
		body         string
	}{
		{
			name: "authorization",
			dependencies: func() SignalingDependencies {
				return validSignalingDependencies(logicalSession, newFakeSignalingPeer(), panicCanary, nil, nil, nil)
			},
			body: attachBody(1),
		},
		{
			name: "session",
			dependencies: func() SignalingDependencies {
				return validSignalingDependencies(logicalSession, newFakeSignalingPeer(), nil, panicCanary, nil, nil)
			},
			body: attachBody(1),
		},
		{
			name: "ice",
			dependencies: func() SignalingDependencies {
				return validSignalingDependencies(logicalSession, newFakeSignalingPeer(), nil, nil, panicCanary, nil)
			},
			body: attachBody(1),
		},
		{
			name: "activity",
			dependencies: func() SignalingDependencies {
				return validSignalingDependencies(logicalSession, newFakeSignalingPeer(), nil, nil, nil, panicCanary)
			},
			body: attachBody(1),
		},
		{
			name: "peer operation",
			dependencies: func() SignalingDependencies {
				peer := newFakeSignalingPeer()
				peer.answerPanic = panicCanary
				return validSignalingDependencies(logicalSession, peer, nil, nil, nil, nil)
			},
			prepare: func(service *SignalingService) {
				require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)
			},
			body: offerBody(logicalSession.ID, 2, "offer-sensitive-canary"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := newTestSignalingService(t, testChannelARNs(12), testCase.dependencies())
			t.Cleanup(func() { require.NoError(t, service.Close()) })
			if testCase.prepare != nil {
				testCase.prepare(service)
			}
			var response runtime.InvocationResponse
			require.NotPanics(t, func() { response = invokeSignaling(t, service, logicalSession, testCase.body) })
			require.Equal(t, 503, response.StatusCode)
			assertSanitized(t, response.Body, panicCanary, "offer-sensitive-canary")
		})
	}
}

func TestSignalingPeerMethodPanicIsTerminalAndReleasesCapacity(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	panickingPeer := newFakeSignalingPeer()
	panickingPeer.answerPanic = "peer-panic-sensitive-canary"
	healthyPeer := newFakeSignalingPeer()
	activities := &multiActivitySource{}
	config := validSignalingConfig(testChannelARNs(12))
	config.MaxPeers = 1
	service, err := NewSignalingService(config, SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  activities,
		Peers:       &fakePeerFactory{peers: []SignalingPeer{panickingPeer, healthyPeer}},
		STTBindings: &fakeSTTBindingFactory{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	response := invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "panic-offer-canary"))
	require.Equal(t, 503, response.StatusCode)
	assertSanitized(t, response.Body, "peer-panic-sensitive-canary", "panic-offer-canary")
	require.Equal(t, 0, signalingActivePeers(service))
	require.Equal(t, int64(1), signalingLastSequence(t, service, logicalSession))
	select {
	case <-activities.snapshot()[0].closed:
	default:
		t.Fatal("panicking peer retained persistent activity")
	}

	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(2)).StatusCode)
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 3, "healthy-offer")).StatusCode)
}

func TestSignalingPostAnswerCancellationRetiresMutatedPeer(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	mutatedPeer := newFakeSignalingPeer()
	healthyPeer := newFakeSignalingPeer()
	activities := &multiActivitySource{}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:   &fakeSessionLookup{value: logicalSession},
		ICE:        &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities: activities,
		Peers:      &fakePeerFactory{peers: []SignalingPeer{mutatedPeer, healthyPeer}},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	operationContext, cancelOperation := context.WithCancel(context.Background())
	mutatedPeer.afterAnswer = cancelOperation
	response, err := service.Invoke(operationContext, signalingInvocation(logicalSession, offerBody(logicalSession.ID, 2, "cancelled-offer-canary")))
	require.NoError(t, err)
	require.Equal(t, 503, response.StatusCode)
	require.Equal(t, 0, signalingActivePeers(service))
	require.Equal(t, int64(1), signalingLastSequence(t, service, logicalSession))
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(2)).StatusCode)
}

func TestSignalingFailureOutputAndServiceFormattingNeverLeakSecrets(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	ice := &fakeICECredentialSource{err: errors.New("provider-turn-password-canary")}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:   &fakeSessionLookup{value: logicalSession},
		ICE:        ice,
		Activities: newFakeActivitySource(),
		Peers:      &fakePeerFactory{peer: newFakeSignalingPeer()},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	response := invokeSignaling(t, service, logicalSession, attachBody(1))
	require.Equal(t, 503, response.StatusCode)
	assertSanitized(t, response.Body,
		"authorization-sensitive-canary", "provider-turn-password-canary", "voice-pool-a", "arn:aws", "voice-session-",
	)
	assertSanitized(t, []byte(fmt.Sprintf("%v %#v %+v", service, service, service)),
		"authorization", "provider", "voice-pool", "arn:aws", "turn-user-secret", "turn-password-secret",
	)
	encoded, err := json.Marshal(service)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(encoded))
}

func TestSignalingRejectsInvalidICECredentialsWithoutStartingActivity(t *testing.T) {
	t.Parallel()

	providerCanary := "ice-provider-sensitive-canary"
	testCases := []struct {
		name        string
		credentials TURNCredentials
		err         error
	}{
		{name: "provider failure", credentials: validSignalingCredentials(), err: errors.New(providerCanary)},
		{name: "expiry margin equality", credentials: func() TURNCredentials {
			value := validSignalingCredentials()
			value.ExpiresAt = signalingTestNow.Add(time.Minute)
			return value
		}()},
		{name: "bad uri", credentials: func() TURNCredentials {
			value := validSignalingCredentials()
			value.uris = []string{"turn:attacker.invalid:443?transport=udp"}
			return value
		}()},
		{name: "missing username", credentials: func() TURNCredentials {
			value := validSignalingCredentials()
			value.username = ""
			return value
		}()},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			logicalSession := validSignalingSession()
			activity := newFakeActivitySource()
			service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
				Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
					UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
				}},
				Sessions:   &fakeSessionLookup{value: logicalSession},
				ICE:        &fakeICECredentialSource{credentials: testCase.credentials, err: testCase.err},
				Activities: activity,
				Peers:      &fakePeerFactory{peer: newFakeSignalingPeer()},
			})
			t.Cleanup(func() { require.NoError(t, service.Close()) })
			response := invokeSignaling(t, service, logicalSession, attachBody(1))
			require.Equal(t, 503, response.StatusCode)
			require.Empty(t, activity.calls())
			assertSanitized(t, response.Body, providerCanary, "attacker.invalid", testCase.credentials.username, testCase.credentials.password)
		})
	}
}

func TestSignalingPeerErrorStatusMatrix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid description", err: ErrPeerInvalidDescription, wantStatus: 400},
		{name: "invalid candidate", err: ErrPeerInvalidCandidate, wantStatus: 400},
		{name: "state", err: ErrPeerState, wantStatus: 409},
		{name: "closed", err: ErrPeerClosed, wantStatus: 409},
		{name: "capacity", err: ErrPeerCapacity, wantStatus: 429},
		{name: "gather timeout", err: ErrPeerGatherTimeout, wantStatus: 503},
		{name: "dependency", err: errors.New("peer-provider-sensitive-canary"), wantStatus: 503},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			logicalSession := validSignalingSession()
			peer := newFakeSignalingPeer()
			peer.answerErr = testCase.err
			service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
				Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
					UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
				}},
				Sessions:   &fakeSessionLookup{value: logicalSession},
				ICE:        &fakeICECredentialSource{credentials: validSignalingCredentials()},
				Activities: newFakeActivitySource(),
				Peers:      &fakePeerFactory{peer: peer},
			})
			t.Cleanup(func() { require.NoError(t, service.Close()) })
			require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)
			response := invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "sdp-sensitive-canary"))
			require.Equal(t, testCase.wantStatus, response.StatusCode)
			assertSanitized(t, response.Body, "sdp-sensitive-canary", "peer-provider-sensitive-canary")
		})
	}
}

func TestSignalingCloseClosesEveryPeerAndSanitizesCloseErrors(t *testing.T) {
	t.Parallel()

	first := validSignalingSession()
	second := validSignalingSession()
	second.ID = "voice_01KCLOSESECOND"
	second.RuntimeSessionID = "voice-session-01KCLOSESECOND00000000000000"
	lookup := &fakeSessionLookup{value: first}
	firstPeer := newFakeSignalingPeer()
	firstPeer.closeErr = errors.New("close-provider-sensitive-canary")
	secondPeer := newFakeSignalingPeer()
	activities := &multiActivitySource{}
	factory := &fakePeerFactory{peers: []SignalingPeer{firstPeer, secondPeer}}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{UserID: first.UserID, BusinessID: first.BusinessID, ClientID: "client"}},
		Sessions:      lookup,
		ICE:           &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:    activities,
		Peers:         factory,
	})
	require.Equal(t, 200, invokeSignaling(t, service, first, attachBody(1)).StatusCode)
	lookup.set(second)
	require.Equal(t, 200, invokeSignaling(t, service, second, strings.ReplaceAll(attachBody(1), first.ID, second.ID)).StatusCode)

	closeErr := service.Close()
	require.Error(t, closeErr)
	assertSanitized(t, []byte(closeErr.Error()), "close-provider-sensitive-canary")
	require.NoError(t, service.Close(), "subsequent Close must be idempotent")
	select {
	case <-firstPeer.Done():
	default:
		t.Fatal("first peer was not closed")
	}
	select {
	case <-secondPeer.Done():
	default:
		t.Fatal("second peer was not closed")
	}
	for _, closer := range activities.snapshot() {
		select {
		case <-closer.closed:
		default:
			t.Fatal("Close leaked persistent activity")
		}
	}
}

func TestSignalingDoesNotCommitSuccessAfterPeerClosesDuringOperation(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"answer", "candidate"} {
		operation := operation
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			logicalSession := validSignalingSession()
			peer := newFakeSignalingPeer()
			if operation == "answer" {
				peer.afterAnswer = func() { _ = peer.Close() }
			} else {
				peer.afterCandidate = func() { _ = peer.Close() }
			}
			service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
				Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
					UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
				}},
				Sessions:   &fakeSessionLookup{value: logicalSession},
				ICE:        &fakeICECredentialSource{credentials: validSignalingCredentials()},
				Activities: newFakeActivitySource(),
				Peers:      &fakePeerFactory{peer: peer},
			})
			t.Cleanup(func() { require.NoError(t, service.Close()) })
			require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

			body := offerBody(logicalSession.ID, 2, "offer")
			if operation == "candidate" {
				body = candidateBody(logicalSession.ID, 2, "candidate-sensitive-canary", "ufrag")
			}
			response := invokeSignaling(t, service, logicalSession, body)
			require.Equal(t, 503, response.StatusCode)
			assertSanitized(t, response.Body, "candidate-sensitive-canary")
			require.Equal(t, int64(1), signalingLastSequence(t, service, logicalSession))
		})
	}
}

func TestSignalingBoundsFinalEncodedResponseBeforeCommittingSequence(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	oversizedPeer := newFakeSignalingPeer()
	oversizedPeer.answer = strings.Repeat(`\`, maxSignalingSDPBytes)
	healthyPeer := newFakeSignalingPeer()
	activities := &multiActivitySource{}
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:   &fakeSessionLookup{value: logicalSession},
		ICE:        &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities: activities,
		Peers:      &fakePeerFactory{peers: []SignalingPeer{oversizedPeer, healthyPeer}},
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	response := invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "offer"))
	require.Equal(t, 503, response.StatusCode)
	require.LessOrEqual(t, len(response.Body), runtime.MaxInvocationBodySize)
	require.Equal(t, int64(1), signalingLastSequence(t, service, logicalSession))
	require.Equal(t, 0, signalingActivePeers(service), "post-answer response failure must retire the mutated peer")

	response = invokeSignaling(t, service, logicalSession, attachBody(2))
	require.Equal(t, 200, response.StatusCode, "oversized encoded response must leave sequence 2 reusable for re-attach")
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 3, "corrected-offer")).StatusCode)
}

func TestSignalingCloseDoesNotWaitForeverForBrokenPeerDone(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	peer := newFakeSignalingPeer()
	peer.leaveDoneOpen = true
	peer.closeErr = errors.New("broken-close-sensitive-canary")
	service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  newFakeActivitySource(),
		Peers:       &fakePeerFactory{peer: peer},
		STTBindings: &fakeSTTBindingFactory{},
	})
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	closed := make(chan error, 1)
	go func() { closed <- service.Close() }()
	select {
	case err := <-closed:
		require.Error(t, err)
		assertSanitized(t, []byte(err.Error()), "broken-close-sensitive-canary")
	case <-time.After(time.Second):
		t.Fatal("Close stranded a watcher on a broken peer Done channel")
	}
}

func TestSignalingRequiresFreshICEBeforeOfferAndPreservesSequenceForRefresh(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	clock := &mutableSignalingClock{now: signalingTestNow}
	peer := newFakeSignalingPeer()
	ice := &fakeICECredentialSource{credentials: validSignalingCredentials()}
	config := validSignalingConfig(testChannelARNs(12))
	config.Now = clock.Now
	service, err := NewSignalingService(config, SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         ice,
		Activities:  newFakeActivitySource(),
		Peers:       &fakePeerFactory{peer: peer},
		STTBindings: &fakeSTTBindingFactory{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	clock.Set(signalingTestNow.Add(4 * time.Minute))
	response := invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "stale-credential-offer-canary"))
	require.Equal(t, 409, response.StatusCode)
	require.Empty(t, peer.answerCalls())
	require.Equal(t, int64(1), signalingLastSequence(t, service, logicalSession))
	assertSanitized(t, response.Body, "stale-credential-offer-canary")

	refreshed := validSignalingCredentials()
	refreshed.ExpiresAt = clock.Now().Add(5 * time.Minute)
	ice.setCredentials(refreshed)
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(2)).StatusCode)
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 3, "fresh-offer")).StatusCode)
}

func TestSignalingRechecksICEFreshnessAfterAnswerGathering(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	clock := &mutableSignalingClock{now: signalingTestNow}
	peer := newFakeSignalingPeer()
	peer.afterAnswer = func() { clock.Set(signalingTestNow.Add(4 * time.Minute)) }
	config := validSignalingConfig(testChannelARNs(12))
	config.Now = clock.Now
	service, err := NewSignalingService(config, SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  newFakeActivitySource(),
		Peers:       &fakePeerFactory{peer: peer},
		STTBindings: &fakeSTTBindingFactory{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	require.Equal(t, 200, invokeSignaling(t, service, logicalSession, attachBody(1)).StatusCode)

	response := invokeSignaling(t, service, logicalSession, offerBody(logicalSession.ID, 2, "slow-gather-offer-canary"))
	require.Equal(t, 409, response.StatusCode)
	require.Equal(t, int64(1), signalingLastSequence(t, service, logicalSession))
	assertSanitized(t, response.Body, "slow-gather-offer-canary")
}

func TestSignalingBoundsCredentialSourceBeforePeerOrResponse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		mutate func(*TURNCredentials)
	}{
		{name: "too many uris", mutate: func(value *TURNCredentials) {
			value.uris = make([]string, 17)
			for index := range value.uris {
				value.uris[index] = fmt.Sprintf("turn:v-%02d.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp", index)
			}
		}},
		{name: "username too long", mutate: func(value *TURNCredentials) { value.username = strings.Repeat("u", 257) }},
		{name: "password too long", mutate: func(value *TURNCredentials) { value.password = strings.Repeat("p", 257) }},
		{name: "username control character", mutate: func(value *TURNCredentials) { value.username = "user\ncredential-canary" }},
		{name: "password unicode", mutate: func(value *TURNCredentials) { value.password = "password-☃-canary" }},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			logicalSession := validSignalingSession()
			credentials := validSignalingCredentials()
			testCase.mutate(&credentials)
			activity := newFakeActivitySource()
			factory := &fakePeerFactory{peer: newFakeSignalingPeer()}
			service := newTestSignalingService(t, testChannelARNs(12), SignalingDependencies{
				Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
					UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
				}},
				Sessions:   &fakeSessionLookup{value: logicalSession},
				ICE:        &fakeICECredentialSource{credentials: credentials},
				Activities: activity,
				Peers:      factory,
			})
			t.Cleanup(func() { require.NoError(t, service.Close()) })
			response := invokeSignaling(t, service, logicalSession, attachBody(1))
			require.Equal(t, 503, response.StatusCode)
			require.Empty(t, activity.calls())
			require.Empty(t, factory.calls)
			assertSanitized(t, response.Body, credentials.username, credentials.password, "credential-canary", "password-")
		})
	}
}

func testChannelARNs(count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = "arn:aws:kinesisvideo:ap-south-1:123456789012:channel/voice-pool-" + string(rune('a'+index)) + "/1720000000"
	}
	return values
}

func validSignalingSession() *voicesession.Session {
	return &voicesession.Session{
		ID:               "voice_01KTEST",
		RuntimeSessionID: "voice-session-01KTEST00000000000000000000",
		UserID:           "user-123",
		BusinessID:       "business-456",
		Status:           voicesession.StatusActive,
		RuntimeState:     voicesession.RuntimeStateRunning,
		ProtocolVersion:  protocol.ProtocolVersion,
		KVSChannelIndex:  0,
		FallbackLanguage: "en-IN",
		ClientPlatform:   "android",
		ClientAppVersion: "1.2.3",
		ExpiresAt:        signalingTestNow.Add(30 * time.Minute),
		LeaseExpiresAt:   signalingTestNow.Add(5 * time.Minute),
	}
}

func validSignalingCredentials() TURNCredentials {
	return TURNCredentials{
		uris:      []string{"turn:v-signal.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp"},
		username:  "turn-user-secret",
		password:  "turn-password-secret",
		ExpiresAt: signalingTestNow.Add(5 * time.Minute),
	}
}

func invokeSignaling(t *testing.T, service *SignalingService, logicalSession *voicesession.Session, body string) runtime.InvocationResponse {
	t.Helper()
	response, err := service.Invoke(context.Background(), signalingInvocation(logicalSession, body))
	require.NoError(t, err)
	return response
}

func signalingInvocation(logicalSession *voicesession.Session, body string) runtime.InvocationRequest {
	return runtime.InvocationRequest{
		Body:             json.RawMessage(body),
		Authorization:    "Bearer authorization-sensitive-canary",
		RuntimeSessionID: logicalSession.RuntimeSessionID,
		RuntimeID:        "voice-runtime",
		AWSRegion:        MumbaiRegion,
	}
}

func signalingLastSequence(t *testing.T, service *SignalingService, logicalSession *voicesession.Session) int64 {
	t.Helper()
	key := signalingSessionKey{logicalID: logicalSession.ID, runtimeID: logicalSession.RuntimeSessionID}
	service.mu.Lock()
	state := service.states[key]
	service.mu.Unlock()
	require.NotNil(t, state)
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.lastSequence
}

func signalingActivePeers(service *SignalingService) int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.activePeers
}

func attachBody(sequence int) string {
	return fmt.Sprintf(`{"type":"session.attach","protocol_version":1,"session_id":"voice_01KTEST","sequence":%d,"client":{"platform":"android","app_version":"1.2.3"}}`, sequence)
}

func offerBody(sessionID string, sequence int, sdp string) string {
	encoded, _ := json.Marshal(map[string]any{
		"type": "webrtc.offer", "protocol_version": 1, "session_id": sessionID, "sequence": sequence, "sdp": sdp,
	})
	return string(encoded)
}

func restartBody(sessionID string, sequence int, sdp string) string {
	encoded, _ := json.Marshal(map[string]any{
		"type": "webrtc.restart", "protocol_version": 1, "session_id": sessionID, "sequence": sequence, "sdp": sdp,
	})
	return string(encoded)
}

func candidateBody(sessionID string, sequence int, candidate, ufrag string) string {
	encoded, _ := json.Marshal(map[string]any{
		"type": "webrtc.candidate", "protocol_version": 1, "session_id": sessionID, "sequence": sequence,
		"candidate": map[string]any{
			"candidate": candidate, "sdp_mid": "0", "sdp_mline_index": 0, "username_fragment": ufrag,
		},
	})
	return string(encoded)
}

func validSignalingConfig(channels []string) SignalingConfig {
	return SignalingConfig{
		RuntimeID:            "voice-runtime",
		AWSRegion:            MumbaiRegion,
		ProtocolVersion:      protocol.ProtocolVersion,
		ChannelARNs:          channels,
		InvocationTimeout:    2 * time.Second,
		GatherTimeout:        time.Second,
		AttachTimeout:        time.Second,
		ConnectTimeout:       time.Second,
		RestartWindow:        time.Second,
		ICEExpiryMargin:      time.Minute,
		MaxPeers:             2,
		MaxTrackedSessions:   4,
		MaxPendingCandidates: 8,
		MaxCandidateBytes:    2048,
		MaxControlQueue:      8,
		Now:                  func() time.Time { return signalingTestNow },
	}
}

func newTestSignalingService(t *testing.T, channels []string, dependencies SignalingDependencies) *SignalingService {
	t.Helper()
	if dependencies.STTBindings == nil {
		dependencies.STTBindings = &fakeSTTBindingFactory{}
	}
	service, err := NewSignalingService(validSignalingConfig(channels), dependencies)
	require.NoError(t, err)
	return service
}

func validSignalingDependencies(logicalSession *voicesession.Session, peer SignalingPeer, authorizationPanic, sessionPanic, icePanic, activityPanic any) SignalingDependencies {
	return SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{
			identity:   TrustedIdentity{UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client"},
			panicValue: authorizationPanic,
		},
		Sessions:    &fakeSessionLookup{value: logicalSession, panicValue: sessionPanic},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials(), panicValue: icePanic},
		Activities:  &fakeActivitySource{ctx: context.Background(), closer: newRecordingCloser(), panicValue: activityPanic},
		Peers:       &fakePeerFactory{peer: peer},
		STTBindings: &fakeSTTBindingFactory{},
	}
}

type lifecycleRecorder struct {
	mu     sync.Mutex
	events []string
}

func (recorder *lifecycleRecorder) record(event string) {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	recorder.events = append(recorder.events, event)
	recorder.mu.Unlock()
}

func (recorder *lifecycleRecorder) snapshot() []string {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]string(nil), recorder.events...)
}

type countingActivityCloser struct {
	mu        sync.Mutex
	calls     int
	lifecycle *lifecycleRecorder
}

func (closer *countingActivityCloser) Close() error {
	closer.mu.Lock()
	closer.calls++
	closer.mu.Unlock()
	closer.lifecycle.record("activity.close")
	return nil
}

func (closer *countingActivityCloser) closeCount() int {
	closer.mu.Lock()
	defer closer.mu.Unlock()
	return closer.calls
}

type configuredActivitySource struct {
	ctx      context.Context
	activity io.Closer
}

func (source *configuredActivitySource) AcquirePersistentActivity() (context.Context, io.Closer, error) {
	return source.ctx, source.activity, nil
}

type fakeSTTBindingFactory struct {
	mu sync.Mutex

	configs                   []STTBindingConfig
	bindings                  []*fakeSignalingSTTBinding
	err                       error
	panicValue                any
	returnNil                 bool
	closeActivityBeforeReturn bool
	lifecycle                 *lifecycleRecorder
}

func (factory *fakeSTTBindingFactory) Create(config STTBindingConfig) (STTBinding, error) {
	if factory == nil {
		panic("typed nil fake STT binding factory")
	}
	factory.mu.Lock()
	factory.configs = append(factory.configs, config)
	err := factory.err
	panicValue := factory.panicValue
	returnNil := factory.returnNil
	closeActivityBeforeReturn := factory.closeActivityBeforeReturn
	lifecycle := factory.lifecycle
	factory.mu.Unlock()
	lifecycle.record("binding.create")
	if closeActivityBeforeReturn {
		_ = config.Activity.Close()
	}
	if panicValue != nil {
		panic(panicValue)
	}
	if returnNil || err != nil {
		return nil, err
	}
	binding := &fakeSignalingSTTBinding{activity: config.Activity, lifecycle: lifecycle}
	factory.mu.Lock()
	factory.bindings = append(factory.bindings, binding)
	factory.mu.Unlock()
	return binding, nil
}

func (factory *fakeSTTBindingFactory) configsSnapshot() []STTBindingConfig {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return append([]STTBindingConfig(nil), factory.configs...)
}

func (factory *fakeSTTBindingFactory) bindingsSnapshot() []*fakeSignalingSTTBinding {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return append([]*fakeSignalingSTTBinding(nil), factory.bindings...)
}

type fakeSignalingSTTBinding struct {
	mu sync.Mutex

	activity  io.Closer
	lifecycle *lifecycleRecorder
	closes    int
}

func (*fakeSignalingSTTBinding) HandleOpus([]byte) error { return nil }

func (*fakeSignalingSTTBinding) HandleControl(context.Context, protocol.ControlMessage) error {
	return nil
}

func (binding *fakeSignalingSTTBinding) Close() error {
	binding.mu.Lock()
	binding.closes++
	activity := binding.activity
	binding.mu.Unlock()
	binding.lifecycle.record("binding.close")
	if activity != nil {
		return activity.Close()
	}
	return nil
}

func (binding *fakeSignalingSTTBinding) closeCount() int {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	return binding.closes
}

type blockingSTTBinding struct {
	handleStarted chan struct{}
	releaseHandle chan struct{}
	closeStarted  chan struct{}

	mu     sync.Mutex
	closes int
}

func newBlockingSTTBinding() *blockingSTTBinding {
	return &blockingSTTBinding{
		handleStarted: make(chan struct{}),
		releaseHandle: make(chan struct{}),
		closeStarted:  make(chan struct{}),
	}
}

func (binding *blockingSTTBinding) HandleOpus([]byte) error {
	close(binding.handleStarted)
	<-binding.releaseHandle
	return nil
}

func (*blockingSTTBinding) HandleControl(context.Context, protocol.ControlMessage) error {
	return nil
}

func (binding *blockingSTTBinding) Close() error {
	close(binding.closeStarted)
	binding.mu.Lock()
	binding.closes++
	binding.mu.Unlock()
	return nil
}

func (binding *blockingSTTBinding) closeCount() int {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	return binding.closes
}

type fakeAuthorizationResolver struct {
	mu            sync.Mutex
	identity      TrustedIdentity
	err           error
	authorization string
	calls         int
	panicValue    any
}

func (resolver *fakeAuthorizationResolver) Resolve(_ context.Context, authorization string) (TrustedIdentity, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if resolver.panicValue != nil {
		panic(resolver.panicValue)
	}
	resolver.authorization = authorization
	resolver.calls++
	return resolver.identity, resolver.err
}

type fakeSessionLookup struct {
	mu         sync.Mutex
	value      *voicesession.Session
	err        error
	scope      voicesession.Scope
	sessionID  string
	calls      int
	panicValue any
}

func (lookup *fakeSessionLookup) set(value *voicesession.Session) {
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	lookup.value = value
}

func (lookup *fakeSessionLookup) Get(_ context.Context, scope voicesession.Scope, sessionID string) (*voicesession.Session, error) {
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	if lookup.panicValue != nil {
		panic(lookup.panicValue)
	}
	lookup.scope = scope
	lookup.sessionID = sessionID
	lookup.calls++
	if lookup.value == nil {
		return nil, lookup.err
	}
	cloned := *lookup.value
	return &cloned, lookup.err
}

type fakeICECredentialSource struct {
	mu          sync.Mutex
	credentials TURNCredentials
	err         error
	channelARN  string
	calls       int
	panicValue  any
}

func (source *fakeICECredentialSource) GetTURN(_ context.Context, channelARN string) (TURNCredentials, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.panicValue != nil {
		panic(source.panicValue)
	}
	source.channelARN = channelARN
	source.calls++
	return source.credentials, source.err
}

func (source *fakeICECredentialSource) setCredentials(credentials TURNCredentials) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.credentials = credentials
}

type recordingCloser struct {
	once   sync.Once
	closed chan struct{}
}

func newRecordingCloser() *recordingCloser {
	return &recordingCloser{closed: make(chan struct{})}
}

func (closer *recordingCloser) Close() error {
	closer.once.Do(func() { close(closer.closed) })
	return nil
}

type fakeActivitySource struct {
	mu         sync.Mutex
	ctx        context.Context
	closer     *recordingCloser
	err        error
	order      []string
	panicValue any
}

type multiActivitySource struct {
	mu      sync.Mutex
	closers []*recordingCloser
}

func (source *multiActivitySource) AcquirePersistentActivity() (context.Context, io.Closer, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	closer := newRecordingCloser()
	source.closers = append(source.closers, closer)
	return context.Background(), closer, nil
}

func (source *multiActivitySource) snapshot() []*recordingCloser {
	source.mu.Lock()
	defer source.mu.Unlock()
	return append([]*recordingCloser(nil), source.closers...)
}

func newFakeActivitySource() *fakeActivitySource {
	return &fakeActivitySource{ctx: context.Background(), closer: newRecordingCloser()}
}

func (source *fakeActivitySource) AcquirePersistentActivity() (context.Context, io.Closer, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.panicValue != nil {
		panic(source.panicValue)
	}
	source.order = append(source.order, "activity")
	return source.ctx, source.closer, source.err
}

func (source *fakeActivitySource) calls() []string {
	source.mu.Lock()
	defer source.mu.Unlock()
	return append([]string(nil), source.order...)
}

type fakePeerFactory struct {
	mu           sync.Mutex
	peer         SignalingPeer
	err          error
	config       PeerConfig
	calls        []string
	panicValue   any
	beforeCreate func()
	peers        []SignalingPeer
	nextPeer     int
}

func (factory *fakePeerFactory) Create(config PeerConfig) (SignalingPeer, error) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.panicValue != nil {
		panic(factory.panicValue)
	}
	if factory.beforeCreate != nil {
		factory.beforeCreate()
	}
	factory.config = config
	factory.calls = append(factory.calls, "peer")
	if len(factory.peers) > 0 {
		if factory.nextPeer >= len(factory.peers) {
			return nil, errors.New("fake peer queue exhausted")
		}
		peer := factory.peers[factory.nextPeer]
		factory.nextPeer++
		return peer, factory.err
	}
	return factory.peer, factory.err
}

type fakeSignalingPeer struct {
	mu             sync.Mutex
	done           chan struct{}
	closeOnce      sync.Once
	answers        []fakeAnswerCall
	answer         string
	answerErr      error
	candidates     []ICECandidate
	candidateErr   error
	refreshCalls   []TURNCredentials
	refreshErr     error
	answerPanic    any
	closeErr       error
	afterAnswer    func()
	afterCandidate func()
	leaveDoneOpen  bool
}

type fakeAnswerCall struct {
	SDP     string
	Restart bool
}

func newFakeSignalingPeer() *fakeSignalingPeer {
	return &fakeSignalingPeer{done: make(chan struct{}), answer: "answer-sdp"}
}

func closedFakeSignalingPeer() *fakeSignalingPeer {
	peer := newFakeSignalingPeer()
	_ = peer.Close()
	return peer
}

func (peer *fakeSignalingPeer) Answer(_ context.Context, sdp string, restart bool) (string, error) {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.answerPanic != nil {
		panic(peer.answerPanic)
	}
	peer.answers = append(peer.answers, fakeAnswerCall{SDP: sdp, Restart: restart})
	if peer.afterAnswer != nil {
		peer.afterAnswer()
	}
	return peer.answer, peer.answerErr
}

func (peer *fakeSignalingPeer) AddCandidate(_ context.Context, candidate ICECandidate) error {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	peer.candidates = append(peer.candidates, candidate)
	if peer.afterCandidate != nil {
		peer.afterCandidate()
	}
	return peer.candidateErr
}

func (peer *fakeSignalingPeer) RefreshICE(_ context.Context, credentials TURNCredentials) error {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	peer.refreshCalls = append(peer.refreshCalls, credentials)
	return peer.refreshErr
}

func (peer *fakeSignalingPeer) refreshes() []TURNCredentials {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	return append([]TURNCredentials(nil), peer.refreshCalls...)
}

func (peer *fakeSignalingPeer) setAnswerError(err error) {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	peer.answerErr = err
}

func (peer *fakeSignalingPeer) answerCalls() []fakeAnswerCall {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	return append([]fakeAnswerCall(nil), peer.answers...)
}

func (peer *fakeSignalingPeer) candidateCalls() []ICECandidate {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	return append([]ICECandidate(nil), peer.candidates...)
}

func (peer *fakeSignalingPeer) Close() error {
	peer.closeOnce.Do(func() {
		if !peer.leaveDoneOpen {
			close(peer.done)
		}
	})
	return peer.closeErr
}

func (peer *fakeSignalingPeer) Done() <-chan struct{} {
	return peer.done
}

var _ SignalingPeer = (*fakeSignalingPeer)(nil)

func assertSanitized(t *testing.T, body []byte, canaries ...string) {
	t.Helper()
	for _, canary := range canaries {
		if canary == "" {
			continue
		}
		assert.NotContains(t, strings.ToLower(string(body)), strings.ToLower(canary))
	}
}
