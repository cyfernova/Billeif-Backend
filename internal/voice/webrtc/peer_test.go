package webrtc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/protocol"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/turn/v5"
	pion "github.com/pion/webrtc/v4"
)

func TestNewPeerRejectsUnsafeConfigurationBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PeerConfig)
	}{
		{name: "blank session", mutate: func(config *PeerConfig) { config.SessionID = " \t" }},
		{name: "oversized session", mutate: func(config *PeerConfig) { config.SessionID = strings.Repeat("s", 257) }},
		{name: "tcp relay", mutate: func(config *PeerConfig) { config.TURNCredentials.uris = []string{"turn:127.0.0.1:3478?transport=tcp"} }},
		{name: "secure tcp relay", mutate: func(config *PeerConfig) { config.TURNCredentials.uris = []string{"turns:127.0.0.1:5349?transport=tcp"} }},
		{name: "candidate byte ceiling", mutate: func(config *PeerConfig) { config.MaxCandidateBytes = 2049 }},
		{name: "candidate count ceiling", mutate: func(config *PeerConfig) { config.MaxCandidates = 65 }},
		{name: "control queue ceiling", mutate: func(config *PeerConfig) { config.MaxControlQueue = 65 }},
		{name: "gather HTTP budget", mutate: func(config *PeerConfig) { config.GatherTimeout = 10 * time.Second }},
		{name: "too many relay URLs", mutate: func(config *PeerConfig) {
			config.TURNCredentials.uris = make([]string, 17)
			for index := range config.TURNCredentials.uris {
				config.TURNCredentials.uris[index] = fmt.Sprintf("turn:127.0.0.1:%d?transport=udp", 4000+index)
			}
		}},
		{name: "oversized relay URL", mutate: func(config *PeerConfig) {
			config.TURNCredentials.uris = []string{"turn:127.0.0.1:3478?transport=udp" + strings.Repeat("x", 257)}
		}},
		{name: "oversized username", mutate: func(config *PeerConfig) { config.TURNCredentials.username = strings.Repeat("u", 257) }},
		{name: "oversized password", mutate: func(config *PeerConfig) { config.TURNCredentials.password = strings.Repeat("p", 257) }},
		{name: "non-printable username", mutate: func(config *PeerConfig) { config.TURNCredentials.username = "user\nsecret" }},
		{name: "username outside token grammar", mutate: func(config *PeerConfig) { config.TURNCredentials.username = "user:secret" }},
		{name: "password outside token grammar", mutate: func(config *PeerConfig) { config.TURNCredentials.password = "secret/value" }},
		{name: "duplicate relay URL", mutate: func(config *PeerConfig) {
			config.TURNCredentials.uris = []string{"turn:127.0.0.1:3478?transport=udp", "turn:127.0.0.1:3478?transport=udp"}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := testPeerConfig(context.Background())
			test.mutate(&config)
			peer, err := NewPeer(config)
			if peer != nil {
				_ = peer.Close()
			}
			if !errors.Is(err, ErrInvalidPeerConfig) {
				t.Fatalf("NewPeer() error = %v, want %v", err, ErrInvalidPeerConfig)
			}
		})
	}
}

func TestNewPeerRequiresMumbaiKVSUDP443OutsideLoopbackTests(t *testing.T) {
	config := testPeerConfig(context.Background())
	config.allowLoopbackTURNForTests = false
	if peer, err := NewPeer(config); peer != nil || !errors.Is(err, ErrInvalidPeerConfig) {
		if peer != nil {
			_ = peer.Close()
		}
		t.Fatalf("NewPeer(loopback without override) = (%v, %v), want invalid config", peer, err)
	}

	config.TURNCredentials.uris = []string{"turn:127-0-0-1.channel-1.kinesisvideo.ap-south-1.amazonaws.com:443?transport=udp"}
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer(Mumbai KVS UDP/443) error = %v", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewPeerForcesSilentPionLoggingDespiteEnvironment(t *testing.T) {
	t.Setenv("PION_LOG_TRACE", "all")
	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	if _, ok := peer.pionLoggerFactory.(silentPionLoggerFactory); !ok {
		t.Fatalf("Pion logger factory = %T, want silentPionLoggerFactory", peer.pionLoggerFactory)
	}
	logger := peer.pionLoggerFactory.NewLogger("secret-scope-canary")
	logger.Trace("secret-trace-canary")
	logger.Debugf("secret-debug-%s", "canary")
	logger.Errorf("secret-error-%s", "canary")
}

func TestNewPeerUsesPersistentContextForLifetime(t *testing.T) {
	persistentContext, cancelPersistent := context.WithCancel(context.Background())
	operationContext, cancelOperation := context.WithCancel(context.Background())
	peer, err := NewPeer(testPeerConfig(persistentContext))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	if got := peer.State(); got != PeerStateAwaitingOffer {
		t.Fatalf("State() = %q, want %q", got, PeerStateAwaitingOffer)
	}
	cancelOperation()
	select {
	case <-operationContext.Done():
	case <-time.After(time.Second):
		t.Fatal("operation context did not cancel")
	}
	select {
	case <-peer.Done():
		t.Fatal("operation context cancellation closed peer")
	case <-time.After(20 * time.Millisecond):
	}

	cancelPersistent()
	select {
	case <-peer.Done():
	case <-time.After(time.Second):
		t.Fatal("persistent context cancellation did not close peer")
	}
	if got := peer.State(); got != PeerStateClosed {
		t.Fatalf("State() after persistent cancellation = %q, want %q", got, PeerStateClosed)
	}
}

func TestPeerAnswerRejectsNonRelayAndInvalidMediaOffers(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
	}{
		{name: "host candidate", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "a=end-of-candidates\r\n", "a=candidate:1 1 udp 2130706431 127.0.0.1 50000 typ host\r\na=end-of-candidates\r\n", 1)},
		{name: "tcp relay candidate", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "a=end-of-candidates\r\n", "a=candidate:1 1 tcp 2130706431 127.0.0.1 50000 typ relay tcptype passive raddr 0.0.0.0 rport 0\r\na=end-of-candidates\r\n", 1)},
		{name: "video mline", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "m=application", "m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:video\r\na=sendrecv\r\nm=application", 1)},
		{name: "wrong audio direction", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "a=sendrecv", "a=recvonly", 1)},
		{name: "wrong opus rate", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "opus/48000/2", "opus/16000/1", 1)},
		{name: "missing application", sdp: strings.Split(testOfferSDP("RemoteUfragA"), "m=application")[0]},
		{name: "extra audio codec", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "m=audio 9 UDP/TLS/RTP/SAVPF 111", "m=audio 9 UDP/TLS/RTP/SAVPF 111 0\r\na=rtpmap:0 PCMU/8000", 1)},
		{name: "duplicate opus mapping", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "a=rtpmap:111 opus/48000/2", "a=rtpmap:111 opus/48000/2\r\na=rtpmap:111 opus/48000/2", 1)},
		{name: "duplicate media mid", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "a=mid:1", "a=mid:0", 1)},
		{name: "missing bundle", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "a=group:BUNDLE 0 1\r\n", "", 1)},
		{name: "missing rtcp mux", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "a=rtcp-mux\r\n", "", 1)},
		{name: "multiple audio tracks", sdp: strings.Replace(testOfferSDP("RemoteUfragA"), "a=msid:stream-1 track-1", "a=msid:stream-1 track-1\r\na=msid:stream-2 track-2", 1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			peer, err := NewPeer(testPeerConfig(context.Background()))
			if err != nil {
				t.Fatalf("NewPeer() error = %v", err)
			}
			t.Cleanup(func() { _ = peer.Close() })

			answer, err := peer.Answer(context.Background(), test.sdp, false)
			if answer != "" {
				t.Fatalf("Answer() SDP was not empty on invalid offer")
			}
			if !errors.Is(err, ErrPeerInvalidDescription) {
				t.Fatalf("Answer() error = %v, want %v", err, ErrPeerInvalidDescription)
			}
			if got := peer.State(); got != PeerStateAwaitingOffer {
				t.Fatalf("State() = %q after invalid offer, want %q", got, PeerStateAwaitingOffer)
			}
		})
	}
}

func TestValidateOfferDescriptionBoundsCandidatesAcrossMedia(t *testing.T) {
	const audioRelay = "a=candidate:1 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0\r\n"
	const applicationRelay = "a=candidate:2 1 udp 1677734909 127.0.0.1 50001 typ relay raddr 0.0.0.0 rport 0\r\n"
	const extraRelay = "a=candidate:3 1 udp 1677734908 127.0.0.1 50002 typ relay raddr 0.0.0.0 rport 0\r\n"

	atLimit := strings.Replace(testOfferSDP("BoundedUfrag"), "a=end-of-candidates\r\n", audioRelay+"a=end-of-candidates\r\n", 1)
	atLimit = strings.Replace(atLimit, "a=max-message-size:16384\r\n", "a=max-message-size:16384\r\n"+applicationRelay, 1)
	credentials, candidateCount, err := validateOfferDescription(atLimit, 2, 2048)
	if err != nil {
		t.Fatalf("validateOfferDescription(at limit) error = %v", err)
	}
	if credentials.ufragHash != sha256.Sum256([]byte("BoundedUfrag")) || candidateCount != 2 {
		t.Fatalf("validateOfferDescription(at limit) = (%x, %d), want expected ufrag hash and 2 candidates", credentials.ufragHash, candidateCount)
	}

	overLimit := strings.Replace(atLimit, "a=end-of-candidates\r\n", extraRelay+"a=end-of-candidates\r\n", 1)
	if _, _, err := validateOfferDescription(overLimit, 2, 2048); !errors.Is(err, ErrPeerCapacity) {
		t.Fatalf("validateOfferDescription(over limit) error = %v, want %v", err, ErrPeerCapacity)
	}
}

func TestValidateOfferDescriptionRequiresUnambiguousSharedICECredentials(t *testing.T) {
	const ufrag = "ValidUfrag123"
	const password = "ValidPasswordValue123456789"
	valid := testOfferSDPWithICECredentials(ufrag, password)
	sessionOnly := strings.ReplaceAll(valid, "a=ice-ufrag:"+ufrag+"\r\n", "")
	sessionOnly = strings.ReplaceAll(sessionOnly, "a=ice-pwd:"+password+"\r\n", "")
	sessionOnly = strings.Replace(sessionOnly, "t=0 0\r\n", "t=0 0\r\na=ice-ufrag:"+ufrag+"\r\na=ice-pwd:"+password+"\r\n", 1)
	tests := []struct {
		name    string
		offer   string
		wantErr error
	}{
		{name: "media credentials", offer: valid},
		{name: "session credentials", offer: sessionOnly},
		{name: "duplicate media ufrag", offer: strings.Replace(valid, "a=ice-ufrag:"+ufrag+"\r\n", "a=ice-ufrag:"+ufrag+"\r\na=ice-ufrag:"+ufrag+"\r\n", 1), wantErr: ErrPeerInvalidDescription},
		{name: "duplicate media password", offer: strings.Replace(valid, "a=ice-pwd:"+password+"\r\n", "a=ice-pwd:"+password+"\r\na=ice-pwd:"+password+"\r\n", 1), wantErr: ErrPeerInvalidDescription},
		{name: "missing paired password", offer: strings.Replace(valid, "a=ice-pwd:"+password+"\r\n", "", 1), wantErr: ErrPeerInvalidDescription},
		{name: "short ufrag", offer: strings.ReplaceAll(valid, "a=ice-ufrag:"+ufrag, "a=ice-ufrag:abc"), wantErr: ErrPeerInvalidDescription},
		{name: "short password", offer: strings.ReplaceAll(valid, "a=ice-pwd:"+password, "a=ice-pwd:short"), wantErr: ErrPeerInvalidDescription},
		{name: "invalid ufrag character", offer: strings.ReplaceAll(valid, "a=ice-ufrag:"+ufrag, "a=ice-ufrag:invalid-ufrag"), wantErr: ErrPeerInvalidDescription},
		{name: "invalid password character", offer: strings.ReplaceAll(valid, "a=ice-pwd:"+password, "a=ice-pwd:InvalidPassword_Value123"), wantErr: ErrPeerInvalidDescription},
		{name: "bundled media mismatch", offer: replaceLast(valid, "a=ice-pwd:"+password, "a=ice-pwd:DifferentPasswordValue12345"), wantErr: ErrPeerInvalidDescription},
		{name: "session media selection mismatch", offer: strings.Replace(valid, "t=0 0\r\n", "t=0 0\r\na=ice-ufrag:OtherUfrag123\r\na=ice-pwd:OtherPasswordValue123456\r\n", 1), wantErr: ErrPeerInvalidDescription},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credentials, _, err := validateOfferDescription(test.offer, maxPeerCandidates, maxPeerCandidateBytes)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("validateOfferDescription() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && (credentials.ufragHash != sha256.Sum256([]byte(ufrag)) || credentials.passwordHash != sha256.Sum256([]byte(password))) {
				t.Fatalf("credential hashes = %#v, want hashes of the effective shared ICE credentials", credentials)
			}
		})
	}
}

func TestCandidateDescriptionValidationRejectsNonRTPComponents(t *testing.T) {
	componentTwoOffer := strings.Replace(
		testOfferSDP("ComponentUfrag"),
		"a=end-of-candidates\r\n",
		"a=candidate:1 2 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0\r\na=end-of-candidates\r\n",
		1,
	)
	if _, _, err := validateOfferDescription(componentTwoOffer, 1, 2048); !errors.Is(err, ErrPeerInvalidDescription) {
		t.Fatalf("validateOfferDescription(component 2) error = %v, want %v", err, ErrPeerInvalidDescription)
	}
	if err := validateGatheredRelayDescription(componentTwoOffer); !errors.Is(err, ErrPeerInvalidDescription) {
		t.Fatalf("validateGatheredRelayDescription(component 2) error = %v, want %v", err, ErrPeerInvalidDescription)
	}
}

func TestSanitizeGatheredRelayDescriptionRemovesOnlyRelayRTCPComponent(t *testing.T) {
	componentOne := "a=candidate:1 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0\r\n"
	componentTwo := "a=candidate:2 2 udp 1677734909 127.0.0.1 50001 typ relay raddr 0.0.0.0 rport 0\r\n"
	gathered := strings.Replace(testOfferSDP("LocalUfrag"), "a=end-of-candidates\r\n", componentOne+componentTwo+"a=end-of-candidates\r\n", 1)
	sanitized, err := sanitizeGatheredRelayDescription(gathered, 2, 2048)
	if err != nil {
		t.Fatalf("sanitizeGatheredRelayDescription() error = %v", err)
	}
	if !strings.Contains(sanitized, componentOne) || strings.Contains(sanitized, componentTwo) {
		t.Fatalf("sanitized SDP did not retain only component 1 candidates:\n%s", sanitized)
	}
	if err := validateGatheredRelayDescription(sanitized); err != nil {
		t.Fatalf("validateGatheredRelayDescription(sanitized) error = %v", err)
	}

	hostComponentTwo := strings.Replace(testOfferSDP("LocalUfrag"), "a=end-of-candidates\r\n", "a=candidate:3 2 udp 2130706431 127.0.0.1 50002 typ host\r\na=end-of-candidates\r\n", 1)
	if _, err := sanitizeGatheredRelayDescription(hostComponentTwo, 2, 2048); !errors.Is(err, ErrPeerInvalidDescription) {
		t.Fatalf("sanitizeGatheredRelayDescription(host component 2) error = %v, want %v", err, ErrPeerInvalidDescription)
	}
}

func TestValidateOfferDescriptionRejectsSessionLevelCandidates(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		wantErr   error
	}{
		{
			name:      "relay UDP component one",
			candidate: "a=candidate:1 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0\r\n",
			wantErr:   ErrPeerInvalidDescription,
		},
		{
			name:      "relay UDP component two",
			candidate: "a=candidate:2 2 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0\r\n",
			wantErr:   ErrPeerInvalidDescription,
		},
		{
			name:      "host UDP",
			candidate: "a=candidate:3 1 udp 2130706431 127.0.0.1 50000 typ host\r\n",
			wantErr:   ErrPeerInvalidDescription,
		},
		{
			name:      "relay TCP",
			candidate: "a=candidate:4 1 tcp 1677734910 127.0.0.1 50000 typ relay tcptype passive raddr 0.0.0.0 rport 0\r\n",
			wantErr:   ErrPeerInvalidDescription,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offer := strings.Replace(testOfferSDP("SessionCandidateUfrag"), "t=0 0\r\n", "t=0 0\r\n"+test.candidate, 1)
			_, candidateCount, err := validateOfferDescription(offer, 1, 2048)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("validateOfferDescription() error = %v, want %v", err, test.wantErr)
			}
			_ = candidateCount
		})
	}
}

func TestValidateOfferDescriptionFencesEmbeddedCandidateICEGeneration(t *testing.T) {
	tests := []struct {
		name       string
		extensions string
		wantErr    error
	}{
		{name: "no candidate ufrag"},
		{name: "matching candidate ufrag", extensions: " ufrag OfferGeneration"},
		{name: "stale candidate ufrag", extensions: " ufrag PriorGeneration", wantErr: ErrPeerInvalidDescription},
		{name: "duplicate matching candidate ufrag", extensions: " ufrag OfferGeneration ufrag OfferGeneration", wantErr: ErrPeerInvalidDescription},
		{name: "conflicting duplicate candidate ufrag", extensions: " ufrag OfferGeneration ufrag PriorGeneration", wantErr: ErrPeerInvalidDescription},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offer := strings.Replace(
				testOfferSDP("OfferGeneration"),
				"a=end-of-candidates\r\n",
				"a=candidate:1 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0"+test.extensions+"\r\na=end-of-candidates\r\n",
				1,
			)
			_, _, err := validateOfferDescription(offer, 1, 2048)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("validateOfferDescription() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestPeerAnswerCountsQueuedAndEmbeddedCandidatesTogether(t *testing.T) {
	config := testPeerConfig(context.Background())
	config.MaxCandidates = 1
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	ufrag := "CombinedLimitUfrag"
	mid := "0"
	index := uint16(0)
	if err := peer.AddCandidate(context.Background(), ICECandidate{
		candidate:        "candidate:queued 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0 ufrag " + ufrag,
		sdpMid:           &mid,
		sdpMLineIndex:    &index,
		usernameFragment: &ufrag,
	}); err != nil {
		t.Fatalf("AddCandidate(queued) error = %v", err)
	}
	offer := strings.Replace(
		testOfferSDP(ufrag),
		"a=end-of-candidates\r\n",
		"a=candidate:embedded 1 udp 1677734909 127.0.0.1 50001 typ relay raddr 0.0.0.0 rport 0\r\na=end-of-candidates\r\n",
		1,
	)
	if answer, err := peer.Answer(context.Background(), offer, false); answer != "" || !errors.Is(err, ErrPeerCapacity) {
		t.Fatalf("Answer(combined over limit) = (%q, %v), want empty answer and %v", answer, err, ErrPeerCapacity)
	}
	if got := peer.State(); got != PeerStateAwaitingOffer {
		t.Fatalf("State() = %q after capacity rejection, want %q", got, PeerStateAwaitingOffer)
	}
}

func TestPeerAddCandidateQueuesOnlyBoundedRelayUDP4Candidates(t *testing.T) {
	config := testPeerConfig(context.Background())
	config.MaxCandidates = 2
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	ufrag := "FutureUfrag"
	mid := "0"
	index := uint16(0)
	valid := ICECandidate{
		candidate:        "candidate:1 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0 ufrag " + ufrag,
		sdpMid:           &mid,
		sdpMLineIndex:    &index,
		usernameFragment: &ufrag,
	}
	if err := peer.AddCandidate(context.Background(), valid); err != nil {
		t.Fatalf("AddCandidate(valid) error = %v", err)
	}
	valid.candidate = "candidate:2 1 udp 1677734909 127.0.0.1 50001 typ relay raddr 0.0.0.0 rport 0 ufrag " + ufrag
	if err := peer.AddCandidate(context.Background(), valid); err != nil {
		t.Fatalf("AddCandidate(second valid) error = %v", err)
	}
	valid.candidate = "candidate:3 1 udp 1677734908 127.0.0.1 50002 typ relay raddr 0.0.0.0 rport 0 ufrag " + ufrag
	if err := peer.AddCandidate(context.Background(), valid); !errors.Is(err, ErrPeerCapacity) {
		t.Fatalf("AddCandidate(over capacity) error = %v, want %v", err, ErrPeerCapacity)
	}

	invalid := []ICECandidate{
		{candidate: "candidate:4 1 udp 2130706431 127.0.0.1 50003 typ host", sdpMid: &mid, sdpMLineIndex: &index, usernameFragment: &ufrag},
		{candidate: "candidate:5 1 tcp 1677734907 127.0.0.1 50004 typ relay tcptype passive raddr 0.0.0.0 rport 0", sdpMid: &mid, sdpMLineIndex: &index, usernameFragment: &ufrag},
		{candidate: "candidate:6 1 udp 1677734906 127.0.0.1 50005 typ relay raddr 0.0.0.0 rport 0", sdpMid: &mid, sdpMLineIndex: &index},
		{candidate: strings.Repeat("x", config.MaxCandidateBytes+1), sdpMid: &mid, sdpMLineIndex: &index, usernameFragment: &ufrag},
		{candidate: "candidate:7 2 udp 1677734905 127.0.0.1 50006 typ relay raddr 0.0.0.0 rport 0", sdpMid: &mid, sdpMLineIndex: &index, usernameFragment: &ufrag},
		{candidate: "candidate:8 1 udp 1677734904 127.0.0.1 50007 typ relay raddr 0.0.0.0 rport 0 ufrag DifferentUfrag", sdpMid: &mid, sdpMLineIndex: &index, usernameFragment: &ufrag},
	}
	badMID := "0 1"
	badUfrag := "future ufrag"
	invalid = append(invalid,
		ICECandidate{candidate: "candidate:9 1 udp 1677734903 127.0.0.1 50008 typ relay raddr 0.0.0.0 rport 0", sdpMid: &badMID, sdpMLineIndex: &index, usernameFragment: &ufrag},
		ICECandidate{candidate: "candidate:10 1 udp 1677734902 127.0.0.1 50009 typ relay raddr 0.0.0.0 rport 0", sdpMid: &mid, sdpMLineIndex: &index, usernameFragment: &badUfrag},
	)
	for _, candidate := range invalid {
		if err := peer.AddCandidate(context.Background(), candidate); !errors.Is(err, ErrPeerInvalidCandidate) {
			t.Fatalf("AddCandidate(invalid) error = %v, want %v", err, ErrPeerInvalidCandidate)
		}
	}
}

func TestPeerAddCandidateFencesQueuedAndActiveICEGenerations(t *testing.T) {
	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	activeUfrag := "ActiveUfrag"
	futureUfrag := "FutureUfrag"
	mid := "0"
	index := uint16(0)
	peer.mu.Lock()
	peer.hasRemoteUfrag = true
	peer.remoteUfragHash = sha256.Sum256([]byte(activeUfrag))
	peer.mu.Unlock()
	var added atomic.Int32
	peer.addICECandidate = func(candidate pion.ICECandidateInit) error {
		added.Add(1)
		if candidate.UsernameFragment == nil || *candidate.UsernameFragment != activeUfrag {
			t.Fatalf("active candidate ufrag = %v", candidate.UsernameFragment)
		}
		if !strings.Contains(candidate.Candidate, " ufrag "+activeUfrag) {
			t.Fatalf("active candidate did not normalize the pointer ufrag into raw candidate")
		}
		return nil
	}
	active := ICECandidate{
		candidate:        "candidate:1 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0",
		sdpMid:           &mid,
		sdpMLineIndex:    &index,
		usernameFragment: &activeUfrag,
	}
	if err := peer.AddCandidate(context.Background(), active); err != nil {
		t.Fatalf("AddCandidate(active) error = %v", err)
	}
	future := ICECandidate{
		candidate:        "candidate:2 1 udp 1677734909 127.0.0.1 50001 typ relay raddr 0.0.0.0 rport 0 ufrag " + futureUfrag,
		sdpMid:           &mid,
		sdpMLineIndex:    &index,
		usernameFragment: &futureUfrag,
	}
	if err := peer.AddCandidate(context.Background(), future); err != nil {
		t.Fatalf("AddCandidate(future) error = %v", err)
	}
	if got := added.Load(); got != 1 {
		t.Fatalf("Pion AddICECandidate calls = %d, want active generation only", got)
	}
	peer.mu.Lock()
	pending := append([]pendingICECandidate(nil), peer.pendingCandidates...)
	peer.mu.Unlock()
	if len(pending) != 1 || pending[0].ufragHash != sha256.Sum256([]byte(futureUfrag)) {
		t.Fatalf("pending candidates = %#v, want only future generation", pending)
	}
}

func TestPeerAddCandidateCannotCommitAfterConcurrentClose(t *testing.T) {
	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	ufrag := "ActiveUfrag"
	mid := "0"
	index := uint16(0)
	peer.mu.Lock()
	peer.hasRemoteUfrag = true
	peer.remoteUfragHash = sha256.Sum256([]byte(ufrag))
	peer.mu.Unlock()
	entered := make(chan struct{})
	release := make(chan struct{})
	peer.addICECandidate = func(pion.ICECandidateInit) error {
		close(entered)
		<-release
		return nil
	}
	result := make(chan error, 1)
	go func() {
		result <- peer.AddCandidate(context.Background(), ICECandidate{
			candidate:        "candidate:1 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0 ufrag " + ufrag,
			sdpMid:           &mid,
			sdpMLineIndex:    &index,
			usernameFragment: &ufrag,
		})
	}()
	awaitSignal(t, entered, time.Second, "Pion candidate add entered")
	if err := peer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	close(release)
	select {
	case err := <-result:
		if !errors.Is(err, ErrPeerClosed) {
			t.Fatalf("AddCandidate() error = %v, want %v", err, ErrPeerClosed)
		}
	case <-time.After(time.Second):
		t.Fatal("AddCandidate did not return after Close")
	}
}

func TestPeerHalfTrickleAnswerConnectsOnlyThroughLoopbackTURN(t *testing.T) {
	credentials := startLoopbackTURN(t)
	client := newLoopbackPeerConnection(t, credentials)
	t.Cleanup(func() { _ = client.Close() })

	clientTrack, err := pion.NewTrackLocalStaticRTP(pion.RTPCodecCapability{
		MimeType: pion.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1",
	}, "client-audio", "client-stream")
	if err != nil {
		t.Fatalf("NewTrackLocalStaticRTP() error = %v", err)
	}
	if _, err := client.AddTransceiverFromTrack(clientTrack, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionSendrecv}); err != nil {
		t.Fatalf("AddTransceiverFromTrack() error = %v", err)
	}
	ordered := true
	negotiated := false
	clientControl, err := client.CreateDataChannel(protocol.DataChannelLabel, &pion.DataChannelInit{Ordered: &ordered, Negotiated: &negotiated})
	if err != nil {
		t.Fatalf("CreateDataChannel() error = %v", err)
	}
	controlOpen := make(chan struct{})
	runtimeControls := make(chan []byte, 1)
	clientControl.OnOpen(func() { close(controlOpen) })
	clientControl.OnMessage(func(message pion.DataChannelMessage) { runtimeControls <- append([]byte(nil), message.Data...) })
	clientInbound := make(chan *pion.TrackRemote, 1)
	client.OnTrack(func(track *pion.TrackRemote, _ *pion.RTPReceiver) { clientInbound <- track })

	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer() error = %v", err)
	}
	gathered := pion.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription(offer) error = %v", err)
	}
	awaitSignal(t, gathered, 3*time.Second, "client ICE gathering")
	localOffer := client.LocalDescription()
	if localOffer == nil {
		t.Fatal("client local offer is nil")
	}
	assertSDPHasOnlyRelayUDPCandidates(t, localOffer.SDP)
	trickleCandidates := extractRelayRTPICECandidates(t, localOffer.SDP)
	signalingOffer := stripICECandidates(localOffer.SDP)
	if _, _, err := validateOfferDescription(signalingOffer, maxPeerCandidates, maxPeerCandidateBytes); err != nil {
		t.Fatalf("loopback offer failed peer policy validation: %v", err)
	}

	config := testPeerConfig(context.Background())
	config.TURNCredentials = credentials
	config.GatherTimeout = 3 * time.Second
	config.ConnectTimeout = 3 * time.Second
	config.RestartWindow = 5 * time.Second
	binding := newRecordingPeerSTTBinding()
	config.STTBinding = binding
	acceptedControl := make(chan protocol.ControlMessage, 1)
	var handlerCalls atomic.Int32
	config.HandleControl = func(_ context.Context, message protocol.ControlMessage) error {
		call := handlerCalls.Add(1)
		if message.Type == protocol.EventHeartbeat && call == 1 {
			return errors.New("state rejected")
		}
		if message.Type == protocol.EventHeartbeat {
			acceptedControl <- message
		}
		return nil
	}
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	if err := peer.AddCandidate(context.Background(), trickleCandidates[0]); err != nil {
		t.Fatalf("AddCandidate(before remote description) error = %v", err)
	}
	answer, err := peer.Answer(context.Background(), signalingOffer, false)
	if err != nil {
		t.Fatalf("Answer() error = %v", err)
	}
	if answer == "" {
		t.Fatal("Answer() returned empty SDP")
	}
	assertSDPHasOnlyRelayUDPCandidates(t, answer)
	if !strings.Contains(answer, "a=max-message-size:16384\r\n") {
		t.Fatal("answer did not advertise the 16 KiB DataChannel message ceiling")
	}
	postRemoteCandidate := trickleCandidates[0]
	if len(trickleCandidates) > 1 {
		postRemoteCandidate = trickleCandidates[1]
	}
	if err := peer.AddCandidate(context.Background(), postRemoteCandidate); err != nil {
		t.Fatalf("AddCandidate(after remote description) error = %v", err)
	}
	peer.mu.Lock()
	trickledCandidateCount := peer.candidateCounts[sha256.Sum256([]byte(offerICEUfrag(signalingOffer)))]
	peer.mu.Unlock()
	if trickledCandidateCount != 2 {
		t.Fatalf("recorded trickled candidate count = %d, want 2", trickledCandidateCount)
	}
	if err := client.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatalf("SetRemoteDescription(answer) error = %v", err)
	}
	awaitPeerState(t, peer, PeerStateConnected, 5*time.Second)
	assertSelectedPairIsRelayUDP(t, client)
	assertSelectedPairIsRelayUDP(t, peer.pc)
	awaitSignal(t, controlOpen, 3*time.Second, "control DataChannel open")

	clientPacket := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: uint8(opusPayloadType), SequenceNumber: 1, Timestamp: 960, SSRC: 101}, Payload: []byte{0xf8, 0xff, 0xfe}}
	if err := clientTrack.WriteRTP(clientPacket); err != nil {
		t.Fatalf("client WriteRTP() error = %v", err)
	}
	select {
	case payload := <-binding.opus:
		if !bytes.Equal(payload, clientPacket.Payload) {
			t.Fatalf("binding Opus payload = %x, want exact RTP payload %x", payload, clientPacket.Payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("production peer track reader did not route inbound Opus to STT binding")
	}
	select {
	case track := <-peer.InboundAudio():
		t.Fatalf("configured production binding exposed a competing inbound track reader: %v", track)
	default:
	}

	firstRuntimePayload := []byte{0xf8, 0xff, 0xfe}
	secondRuntimePayload := []byte{0xf8, 0xff, 0xfd}
	if err := peer.SendOpus(firstRuntimePayload); err != nil {
		t.Fatalf("SendOpus(first) error = %v", err)
	}
	if err := peer.SendOpus(secondRuntimePayload); err != nil {
		t.Fatalf("SendOpus(second) error = %v", err)
	}
	clientTrackRemote := awaitRemoteTrack(t, clientInbound, "client inbound Opus")
	firstRuntimePacket := readRTPPacketWithTimeout(t, clientTrackRemote, "first client inbound RTP")
	secondRuntimePacket := readRTPPacketWithTimeout(t, clientTrackRemote, "second client inbound RTP")
	if !bytes.Equal(firstRuntimePacket.Payload, firstRuntimePayload) || !bytes.Equal(secondRuntimePacket.Payload, secondRuntimePayload) {
		t.Fatalf("runtime Opus payloads = (%x, %x), want (%x, %x)", firstRuntimePacket.Payload, secondRuntimePacket.Payload, firstRuntimePayload, secondRuntimePayload)
	}
	if secondRuntimePacket.SequenceNumber != firstRuntimePacket.SequenceNumber+1 {
		t.Fatalf("runtime RTP sequences = (%d, %d), want +1", firstRuntimePacket.SequenceNumber, secondRuntimePacket.SequenceNumber)
	}
	if secondRuntimePacket.Timestamp != firstRuntimePacket.Timestamp+audio.RTPTimePerFrame {
		t.Fatalf("runtime RTP timestamps = (%d, %d), want +%d", firstRuntimePacket.Timestamp, secondRuntimePacket.Timestamp, audio.RTPTimePerFrame)
	}
	runtimeMessage := protocol.ControlMessage{Type: protocol.EventSessionReady, ProtocolVersion: protocol.ProtocolVersion, SessionID: "voice-session-1", Sequence: 1}
	if err := peer.SendControl(runtimeMessage); err != nil {
		t.Fatalf("SendControl() error = %v", err)
	}
	select {
	case frame := <-runtimeControls:
		decoded, err := protocol.DecodeControlMessage(frame, protocol.RuntimeToClient, nil)
		if err != nil || decoded.Sequence != 1 || decoded.Type != protocol.EventSessionReady {
			t.Fatalf("runtime control decode = (%#v, %v)", decoded, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not receive runtime control")
	}

	control := `{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":1}`
	if err := clientControl.SendText(control); err != nil {
		t.Fatalf("SendText(first attempt) error = %v", err)
	}
	if err := clientControl.SendText(control); err != nil {
		t.Fatalf("SendText(retry) error = %v", err)
	}
	select {
	case message := <-acceptedControl:
		if message.Sequence != 1 || message.SessionID != "voice-session-1" {
			t.Fatalf("accepted control = %#v", message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("state-corrected control retry was not accepted")
	}
	for sequence, eventType := range []protocol.EventType{protocol.EventSpeechStarted, protocol.EventSpeechEnded} {
		frame := fmt.Sprintf(`{"type":%q,"protocol_version":1,"session_id":"voice-session-1","sequence":%d,"turn_id":1,"client_monotonic_ms":%d}`, eventType, sequence+2, sequence+1)
		if err := clientControl.SendText(frame); err != nil {
			t.Fatalf("SendText(%s) error = %v", eventType, err)
		}
		select {
		case message := <-binding.controls:
			if message.Type != eventType || message.Sequence != sequence+2 {
				t.Fatalf("binding control = %#v, want %s sequence %d", message, eventType, sequence+2)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("binding did not receive ordered %s control", eventType)
		}
	}
	if _, err := peer.Answer(context.Background(), signalingOffer, true); !errors.Is(err, ErrPeerState) {
		t.Fatalf("Answer(same-ufrag restart) error = %v, want %v", err, ErrPeerState)
	}
	restartOffer, err := client.CreateOffer(&pion.OfferOptions{ICERestart: true})
	if err != nil {
		t.Fatalf("CreateOffer(ICE restart) error = %v", err)
	}
	restartGathered := pion.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(restartOffer); err != nil {
		t.Fatalf("SetLocalDescription(restart) error = %v", err)
	}
	awaitSignal(t, restartGathered, 3*time.Second, "client restart ICE gathering")
	gatheredRestartOffer := client.LocalDescription()
	if gatheredRestartOffer == nil || offerICEUfrag(gatheredRestartOffer.SDP) == offerICEUfrag(localOffer.SDP) {
		t.Fatal("client ICE restart did not produce a changed ufrag")
	}
	restartCandidates := extractRelayRTPICECandidates(t, gatheredRestartOffer.SDP)
	restartSignalingOffer := stripICECandidates(gatheredRestartOffer.SDP)
	if err := peer.AddCandidate(context.Background(), restartCandidates[0]); err != nil {
		t.Fatalf("AddCandidate(before restart remote description) error = %v", err)
	}
	restartAnswer, err := peer.Answer(context.Background(), restartSignalingOffer, true)
	if err != nil {
		t.Fatalf("Answer(fresh ICE restart) error = %v", err)
	}
	assertSDPHasOnlyRelayUDPCandidates(t, restartAnswer)
	postRestartCandidate := restartCandidates[0]
	if len(restartCandidates) > 1 {
		postRestartCandidate = restartCandidates[1]
	}
	if err := peer.AddCandidate(context.Background(), postRestartCandidate); err != nil {
		t.Fatalf("AddCandidate(after restart remote description) error = %v", err)
	}
	if err := client.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: restartAnswer}); err != nil {
		t.Fatalf("SetRemoteDescription(restart answer) error = %v", err)
	}
	awaitPeerState(t, peer, PeerStateConnected, 5*time.Second)
	assertSelectedPairIsRelayUDP(t, client)
	assertSelectedPairIsRelayUDP(t, peer.pc)

	binding.setOpusError(errors.New("offline STT decode failure"))
	clientPacket.SequenceNumber++
	clientPacket.Timestamp += 960
	if err := clientTrack.WriteRTP(clientPacket); err != nil {
		t.Fatalf("client WriteRTP(binding failure) error = %v", err)
	}
	awaitSignal(t, peer.Done(), 3*time.Second, "STT binding failure closes peer")
	if got := handlerCalls.Load(); got != 2 {
		t.Fatalf("generic handler calls = %d, want 2 heartbeat attempts; speech controls must route exclusively to the binding", got)
	}
	if got := binding.closeCalls.Load(); got != 1 {
		t.Fatalf("binding Close() calls = %d, want 1", got)
	}
}

func TestPeerSTTBindingControlFailureAndPanicFailClosed(t *testing.T) {
	for _, test := range []struct {
		name       string
		controlErr error
		panicValue any
	}{
		{name: "error", controlErr: errors.New("binding control rejected")},
		{name: "panic", panicValue: "binding-control-panic-canary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			binding := newRecordingPeerSTTBinding()
			binding.controlErr = test.controlErr
			binding.controlPanic = test.panicValue
			config := testPeerConfig(context.Background())
			config.STTBinding = binding
			peer, err := NewPeer(config)
			if err != nil {
				t.Fatalf("NewPeer() error = %v", err)
			}
			t.Cleanup(func() { _ = peer.Close() })

			peer.enqueueControlMessage(pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"speech.started","protocol_version":1,"session_id":"voice-session-1","sequence":1,"turn_id":1,"client_monotonic_ms":1}`)})
			awaitSignal(t, peer.Done(), time.Second, "STT binding control failure closes peer")
			if got := binding.closeCalls.Load(); got != 1 {
				t.Fatalf("binding Close() calls = %d, want 1", got)
			}
		})
	}
}

func TestPeerSendOpusRejectsInvalidPayloadAndDisconnectedPeer(t *testing.T) {
	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	for _, payload := range [][]byte{nil, make([]byte, audio.MaxOpusPacketBytes+1)} {
		if err := peer.SendOpus(payload); !errors.Is(err, ErrPeerInvalidAudio) {
			t.Fatalf("SendOpus(%d bytes) error = %v, want ErrPeerInvalidAudio", len(payload), err)
		}
	}
	if err := peer.SendOpus([]byte{0xf8, 0xff, 0xfe}); !errors.Is(err, ErrPeerState) {
		t.Fatalf("SendOpus(disconnected) error = %v, want ErrPeerState", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := peer.SendOpus([]byte{0xf8, 0xff, 0xfe}); !errors.Is(err, ErrPeerState) {
		t.Fatalf("SendOpus(closed) error = %v, want ErrPeerState", err)
	}
}

func TestPeerSTTBindingOpusPayloadBoundsAndPanicContainment(t *testing.T) {
	binding := newRecordingPeerSTTBinding()
	config := testPeerConfig(context.Background())
	config.STTBinding = binding
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	for _, payload := range [][]byte{nil, make([]byte, audio.MaxOpusPacketBytes+1)} {
		if err := peer.dispatchInboundOpus(payload); !errors.Is(err, ErrPeerInvalidAudio) {
			t.Fatalf("dispatchInboundOpus(%d bytes) error = %v, want ErrPeerInvalidAudio", len(payload), err)
		}
	}
	select {
	case payload := <-binding.opus:
		t.Fatalf("invalid Opus payload reached binding: %d bytes", len(payload))
	default:
	}

	minimum := []byte{0x7f}
	if err := peer.dispatchInboundOpus(minimum); err != nil {
		t.Fatalf("dispatchInboundOpus(minimum) error = %v", err)
	}
	select {
	case payload := <-binding.opus:
		if !bytes.Equal(payload, minimum) {
			t.Fatal("minimum Opus payload changed at binding boundary")
		}
	default:
		t.Fatal("minimum valid Opus payload did not reach binding")
	}

	maximum := bytes.Repeat([]byte{0xa5}, audio.MaxOpusPacketBytes)
	if err := peer.dispatchInboundOpus(maximum); err != nil {
		t.Fatalf("dispatchInboundOpus(maximum) error = %v", err)
	}
	select {
	case payload := <-binding.opus:
		if !bytes.Equal(payload, maximum) {
			t.Fatal("maximum Opus payload changed at binding boundary")
		}
	default:
		t.Fatal("maximum valid Opus payload did not reach binding")
	}

	binding.mu.Lock()
	binding.opusPanic = "binding-opus-panic-canary"
	binding.mu.Unlock()
	if err := peer.dispatchInboundOpus([]byte{0x01}); !errors.Is(err, errSTTBindingPanic) {
		t.Fatalf("dispatchInboundOpus(panicking binding) error = %v, want contained panic", err)
	}
}

func TestPeerDuplicateAcceptedControlStillClosesBeforeRedispatch(t *testing.T) {
	var calls atomic.Int32
	config := testPeerConfig(context.Background())
	config.HandleControl = func(context.Context, protocol.ControlMessage) error {
		calls.Add(1)
		return nil
	}
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	frame := pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":1}`)}
	peer.enqueueControlMessage(frame)
	deadline := time.Now().Add(time.Second)
	for calls.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("initial handler calls = %d, want 1", got)
	}
	peer.enqueueControlMessage(frame)
	awaitSignal(t, peer.Done(), time.Second, "duplicate accepted control closes peer")
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls after duplicate = %d, want 1", got)
	}
}

func TestPeerGenericControlHandlerCanSynchronouslyClosePeer(t *testing.T) {
	handlerReturned := make(chan error, 1)
	config := testPeerConfig(context.Background())
	var peer *Peer
	config.HandleControl = func(context.Context, protocol.ControlMessage) error {
		err := peer.Close()
		handlerReturned <- err
		return err
	}
	var err error
	peer, err = NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	peer.enqueueControlMessage(pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":1}`)})
	select {
	case err := <-handlerReturned:
		if err != nil {
			t.Fatalf("handler Peer.Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("generic control handler deadlocked while synchronously closing its peer")
	}
	awaitSignal(t, peer.Done(), time.Second, "synchronous handler peer close")
	if got := peer.lastInboundSequence; got != 0 {
		t.Fatalf("closed peer committed inbound sequence = %d, want 0", got)
	}
}

func TestPeerRefreshICEReplacesServersWithoutRetainingCredentialsOrRelaxingRelayPolicy(t *testing.T) {
	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	refreshed := TURNCredentials{
		uris:      []string{"turn:127.0.0.1:3479?transport=udp"},
		username:  "fresh-user",
		password:  "fresh-password",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := peer.RefreshICE(context.Background(), refreshed); err != nil {
		t.Fatalf("RefreshICE() error = %v", err)
	}
	configuration := peer.pc.GetConfiguration()
	if configuration.ICETransportPolicy != pion.ICETransportPolicyRelay {
		t.Fatalf("ICETransportPolicy = %v, want relay", configuration.ICETransportPolicy)
	}
	if len(configuration.ICEServers) != 1 || len(configuration.ICEServers[0].URLs) != 1 || configuration.ICEServers[0].URLs[0] != "turn:127.0.0.1:3479?transport=udp" {
		t.Fatalf("refreshed ICE server URLs = %#v, want one replacement", configuration.ICEServers)
	}
	if len(peer.config.TURNCredentials.uris) != 0 || peer.config.TURNCredentials.username != "" || peer.config.TURNCredentials.password != "" {
		t.Fatal("peer retained raw TURN credentials outside Pion configuration")
	}

	invalid := refreshed
	invalid.uris = []string{"turn:127.0.0.1:3479?transport=tcp"}
	if err := peer.RefreshICE(context.Background(), invalid); !errors.Is(err, ErrInvalidPeerConfig) {
		t.Fatalf("RefreshICE(invalid) error = %v, want %v", err, ErrInvalidPeerConfig)
	}
}

func TestPeerAnswerStateFencePreservesEarlyConnectedCallback(t *testing.T) {
	config := testPeerConfig(context.Background())
	config.ConnectTimeout = 25 * time.Millisecond
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	credentials := iceCredentialHashes{
		ufragHash:    sha256.Sum256([]byte("FreshUfrag")),
		passwordHash: sha256.Sum256([]byte("FreshPasswordValue123456")),
	}
	operation, err := peer.beginAnswerState(false, credentials)
	if err != nil {
		t.Fatalf("beginAnswerState() error = %v", err)
	}
	peer.handleConnectionState(pion.PeerConnectionStateConnected)
	if err := peer.completeAnswerState(operation, credentials, 0); err != nil {
		t.Fatalf("completeAnswerState() error = %v", err)
	}
	time.Sleep(2 * config.ConnectTimeout)
	if got := peer.State(); got != PeerStateConnected {
		t.Fatalf("State() = %q after early connected callback, want %q", got, PeerStateConnected)
	}
	select {
	case <-peer.Done():
		t.Fatal("stale connect timeout closed an already connected peer")
	default:
	}
}

func TestPeerConnectionCallbackCannotResurrectTerminalState(t *testing.T) {
	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	peer.mu.Lock()
	peer.state = PeerStateClosing
	peer.mu.Unlock()
	peer.handleConnectionState(pion.PeerConnectionStateConnected)
	if got := peer.State(); got != PeerStateClosing {
		t.Fatalf("State() = %q after callback during close, want %q", got, PeerStateClosing)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestPeerRejectedRestartOfferPreservesOriginalDisconnectDeadline(t *testing.T) {
	config := testPeerConfig(context.Background())
	config.RestartWindow = 60 * time.Millisecond
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	peer.mu.Lock()
	peer.hasRemoteUfrag = true
	peer.remoteUfragHash = sha256.Sum256([]byte("OldUfrag"))
	peer.remotePasswordHash = sha256.Sum256([]byte("OldPasswordValue123456789"))
	peer.state = PeerStateConnected
	peer.mu.Unlock()
	peer.handleConnectionState(pion.PeerConnectionStateDisconnected)
	startedAt := time.Now()
	time.Sleep(15 * time.Millisecond)
	peer.setRemoteDescription = func(pion.SessionDescription) error { return errors.New("synthetic Pion rejection") }
	if _, err := peer.Answer(context.Background(), testOfferSDP("FreshUfrag"), true); !errors.Is(err, ErrPeerInvalidDescription) {
		t.Fatalf("Answer(rejected restart) error = %v, want %v", err, ErrPeerInvalidDescription)
	}
	select {
	case <-peer.Done():
		if elapsed := time.Since(startedAt); elapsed > 2*config.RestartWindow {
			t.Fatalf("restart deadline extended after rejected offer: elapsed %s", elapsed)
		}
	case <-time.After(2 * config.RestartWindow):
		t.Fatal("rejected restart offer canceled the original disconnect deadline")
	}
}

func TestPeerRestartRequiresBothICECredentialHashesToChange(t *testing.T) {
	oldCredentials := iceCredentialHashes{
		ufragHash:    sha256.Sum256([]byte("OldUfrag")),
		passwordHash: sha256.Sum256([]byte("OldPasswordValue123456789")),
	}
	tests := []struct {
		name        string
		credentials iceCredentialHashes
		wantErr     error
	}{
		{
			name: "changed ufrag only",
			credentials: iceCredentialHashes{
				ufragHash:    sha256.Sum256([]byte("NewUfrag")),
				passwordHash: oldCredentials.passwordHash,
			},
			wantErr: ErrPeerState,
		},
		{
			name: "changed password only",
			credentials: iceCredentialHashes{
				ufragHash:    oldCredentials.ufragHash,
				passwordHash: sha256.Sum256([]byte("NewPasswordValue123456789")),
			},
			wantErr: ErrPeerState,
		},
		{
			name: "both changed",
			credentials: iceCredentialHashes{
				ufragHash:    sha256.Sum256([]byte("NewUfrag")),
				passwordHash: sha256.Sum256([]byte("NewPasswordValue123456789")),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			peer, err := NewPeer(testPeerConfig(context.Background()))
			if err != nil {
				t.Fatalf("NewPeer() error = %v", err)
			}
			t.Cleanup(func() { _ = peer.Close() })
			peer.mu.Lock()
			peer.state = PeerStateConnected
			peer.hasRemoteUfrag = true
			peer.remoteUfragHash = oldCredentials.ufragHash
			peer.remotePasswordHash = oldCredentials.passwordHash
			peer.mu.Unlock()

			_, err = peer.beginAnswerState(true, test.credentials)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("beginAnswerState(restart) error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil && peer.State() != PeerStateConnected {
				t.Fatalf("rejected restart state = %q, want %q", peer.State(), PeerStateConnected)
			}
		})
	}
}

func TestPeerAndCandidateFormattingRedactsRawSignalingMaterial(t *testing.T) {
	config := testPeerConfig(context.Background())
	config.TURNCredentials.username = "secret-user-canary"
	config.TURNCredentials.password = "secret-password-canary"
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	candidateText := "candidate:secretfoundation 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0 ufrag SecretUfragCanary"
	ufrag := "SecretUfragCanary"
	mid := "0"
	index := uint16(0)
	candidate := ICECandidate{candidate: candidateText, sdpMid: &mid, sdpMLineIndex: &index, usernameFragment: &ufrag}
	if err := peer.AddCandidate(context.Background(), candidate); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}

	serializedCandidate, err := json.Marshal(candidate)
	if err != nil {
		t.Fatalf("json.Marshal(candidate) error = %v", err)
	}
	serializedXML, err := xml.Marshal(candidate)
	if err != nil {
		t.Fatalf("xml.Marshal(candidate) error = %v", err)
	}
	var serializedGob bytes.Buffer
	if err := gob.NewEncoder(&serializedGob).Encode(candidate); err != nil {
		t.Fatalf("gob.Encode(candidate) error = %v", err)
	}
	typeOf := reflect.TypeOf(candidate)
	for fieldIndex := 0; fieldIndex < typeOf.NumField(); fieldIndex++ {
		if typeOf.Field(fieldIndex).IsExported() {
			t.Fatalf("ICECandidate.%s unexpectedly exports raw signaling material", typeOf.Field(fieldIndex).Name)
		}
	}
	outputs := []string{
		fmt.Sprintf("%v", peer), fmt.Sprintf("%+v", peer), fmt.Sprintf("%#v", peer),
		fmt.Sprintf("%v", candidate), fmt.Sprintf("%+v", candidate), fmt.Sprintf("%#v", candidate),
		string(serializedCandidate), string(serializedXML), serializedGob.String(),
	}
	for _, output := range outputs {
		for _, secret := range []string{"secret-user-canary", "secret-password-canary", "secretfoundation", "SecretUfragCanary"} {
			if strings.Contains(output, secret) {
				t.Fatalf("formatted peer material leaked secret %q", secret)
			}
		}
	}
}

func TestPeerCloseIsIdempotentAndClearsPendingCandidateMaterial(t *testing.T) {
	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	ufrag := "PendingUfrag"
	mid := "0"
	index := uint16(0)
	if err := peer.AddCandidate(context.Background(), ICECandidate{
		candidate:        "candidate:1 1 udp 1677734910 127.0.0.1 50000 typ relay raddr 0.0.0.0 rport 0 ufrag " + ufrag,
		sdpMid:           &mid,
		sdpMLineIndex:    &index,
		usernameFragment: &ufrag,
	}); err != nil {
		t.Fatalf("AddCandidate() error = %v", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("Close(second) error = %v", err)
	}
	peer.mu.Lock()
	pendingCount := len(peer.pendingCandidates)
	peer.mu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("pending candidates after Close = %d, want 0", pendingCount)
	}
	if got := peer.State(); got != PeerStateClosed {
		t.Fatalf("State() = %q, want %q", got, PeerStateClosed)
	}
}

func TestPeerRejectsSecondInboundTrackAfterFirstWasConsumed(t *testing.T) {
	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	peer.mu.Lock()
	peer.inboundTrackAccepted = true
	peer.mu.Unlock()
	peer.handleInboundTrack(nil)
	awaitSignal(t, peer.Done(), time.Second, "duplicate inbound track closes peer")
}

func TestPeerContainsControlHandlerPanicAndCloses(t *testing.T) {
	config := testPeerConfig(context.Background())
	config.HandleControl = func(context.Context, protocol.ControlMessage) error { panic("handler-secret-canary") }
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	peer.enqueueControlMessage(pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":1}`)})
	awaitSignal(t, peer.Done(), time.Second, "control handler panic closes peer")
}

func TestPeerConnectionFailureAndDisconnectTimersAreFenced(t *testing.T) {
	t.Run("reconnection cancels stale timer", func(t *testing.T) {
		config := testPeerConfig(context.Background())
		config.RestartWindow = 35 * time.Millisecond
		peer, err := NewPeer(config)
		if err != nil {
			t.Fatalf("NewPeer() error = %v", err)
		}
		t.Cleanup(func() { _ = peer.Close() })
		peer.mu.Lock()
		peer.state = PeerStateConnected
		peer.mu.Unlock()
		peer.handleConnectionState(pion.PeerConnectionStateDisconnected)
		if got := peer.State(); got != PeerStateReconnecting {
			t.Fatalf("State() = %q, want %q", got, PeerStateReconnecting)
		}
		peer.handleConnectionState(pion.PeerConnectionStateConnected)
		time.Sleep(2 * config.RestartWindow)
		if got := peer.State(); got != PeerStateConnected {
			t.Fatalf("State() = %q after stale timer, want %q", got, PeerStateConnected)
		}
		select {
		case <-peer.Done():
			t.Fatal("stale disconnect timer closed reconnected peer")
		default:
		}
	})

	t.Run("disconnect timeout closes", func(t *testing.T) {
		config := testPeerConfig(context.Background())
		config.RestartWindow = 25 * time.Millisecond
		peer, err := NewPeer(config)
		if err != nil {
			t.Fatalf("NewPeer() error = %v", err)
		}
		peer.mu.Lock()
		peer.state = PeerStateConnected
		peer.mu.Unlock()
		peer.handleConnectionState(pion.PeerConnectionStateDisconnected)
		awaitSignal(t, peer.Done(), time.Second, "disconnect timeout")
	})

	t.Run("failed closes immediately", func(t *testing.T) {
		peer, err := NewPeer(testPeerConfig(context.Background()))
		if err != nil {
			t.Fatalf("NewPeer() error = %v", err)
		}
		peer.handleConnectionState(pion.PeerConnectionStateFailed)
		awaitSignal(t, peer.Done(), time.Second, "failed state closes peer")
	})
}

func TestPeerAcceptsOnlyOneExactRemoteControlDataChannel(t *testing.T) {
	ordered := true
	unordered := false
	negotiated := true
	maxRetransmits := uint16(0)
	protocolName := "unexpected"
	tests := []struct {
		name  string
		label string
		init  *pion.DataChannelInit
	}{
		{name: "wrong label", label: "wrong", init: &pion.DataChannelInit{Ordered: &ordered}},
		{name: "unordered", label: protocol.DataChannelLabel, init: &pion.DataChannelInit{Ordered: &unordered}},
		{name: "unreliable", label: protocol.DataChannelLabel, init: &pion.DataChannelInit{Ordered: &ordered, MaxRetransmits: &maxRetransmits}},
		{name: "negotiated", label: protocol.DataChannelLabel, init: &pion.DataChannelInit{Ordered: &ordered, Negotiated: &negotiated}},
		{name: "unexpected protocol", label: protocol.DataChannelLabel, init: &pion.DataChannelInit{Ordered: &ordered, Protocol: &protocolName}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			peer, err := NewPeer(testPeerConfig(context.Background()))
			if err != nil {
				t.Fatalf("NewPeer() error = %v", err)
			}
			t.Cleanup(func() { _ = peer.Close() })
			client, err := pion.NewPeerConnection(pion.Configuration{})
			if err != nil {
				t.Fatalf("NewPeerConnection() error = %v", err)
			}
			t.Cleanup(func() { _ = client.Close() })
			channel, err := client.CreateDataChannel(test.label, test.init)
			if err != nil {
				t.Fatalf("CreateDataChannel() error = %v", err)
			}
			if peer.acceptControlDataChannel(channel) {
				t.Fatal("invalid DataChannel was accepted")
			}
		})
	}

	peer, err := NewPeer(testPeerConfig(context.Background()))
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	client, err := pion.NewPeerConnection(pion.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	first, err := client.CreateDataChannel(protocol.DataChannelLabel, &pion.DataChannelInit{Ordered: &ordered})
	if err != nil {
		t.Fatalf("CreateDataChannel(first) error = %v", err)
	}
	second, err := client.CreateDataChannel(protocol.DataChannelLabel, &pion.DataChannelInit{Ordered: &ordered})
	if err != nil {
		t.Fatalf("CreateDataChannel(second) error = %v", err)
	}
	if !peer.acceptControlDataChannel(first) {
		t.Fatal("exact first control DataChannel was rejected")
	}
	if peer.acceptControlDataChannel(second) {
		t.Fatal("duplicate control DataChannel was accepted")
	}
}

func TestPeerRejectsUnsafeControlFramesAndBoundsCallbackQueue(t *testing.T) {
	validWrongSession := []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"other-session","sequence":1}`)
	tests := []struct {
		name    string
		message pion.DataChannelMessage
	}{
		{name: "binary", message: pion.DataChannelMessage{IsString: false, Data: []byte("binary")}},
		{name: "empty", message: pion.DataChannelMessage{IsString: true}},
		{name: "oversized", message: pion.DataChannelMessage{IsString: true, Data: bytes.Repeat([]byte("x"), protocol.MaxControlMessageBytes+1)}},
		{name: "malformed", message: pion.DataChannelMessage{IsString: true, Data: []byte("{")}},
		{name: "unknown field", message: pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":1,"secret":true}`)}},
		{name: "case variant field", message: pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","Sequence":1}`)}},
		{name: "duplicate field", message: pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":1,"sequence":2}`)}},
		{name: "wrong session", message: pion.DataChannelMessage{IsString: true, Data: validWrongSession}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var handlerCalls atomic.Int32
			config := testPeerConfig(context.Background())
			config.HandleControl = func(context.Context, protocol.ControlMessage) error { handlerCalls.Add(1); return nil }
			peer, err := NewPeer(config)
			if err != nil {
				t.Fatalf("NewPeer() error = %v", err)
			}
			t.Cleanup(func() { _ = peer.Close() })
			peer.enqueueControlMessage(test.message)
			awaitSignal(t, peer.Done(), time.Second, "unsafe control closes peer")
			if got := handlerCalls.Load(); got != 0 {
				t.Fatalf("handler calls = %d, want 0", got)
			}
		})
	}

	t.Run("out of order", func(t *testing.T) {
		accepted := make(chan struct{}, 1)
		config := testPeerConfig(context.Background())
		config.HandleControl = func(context.Context, protocol.ControlMessage) error { accepted <- struct{}{}; return nil }
		peer, err := NewPeer(config)
		if err != nil {
			t.Fatalf("NewPeer() error = %v", err)
		}
		t.Cleanup(func() { _ = peer.Close() })
		peer.enqueueControlMessage(pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":2}`)})
		awaitSignal(t, accepted, time.Second, "first control accepted")
		peer.enqueueControlMessage(pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":1}`)})
		awaitSignal(t, peer.Done(), time.Second, "out-of-order control closes peer")
	})

	t.Run("queue overflow", func(t *testing.T) {
		entered := make(chan struct{})
		config := testPeerConfig(context.Background())
		config.MaxControlQueue = 1
		config.HandleControl = func(ctx context.Context, _ protocol.ControlMessage) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}
		peer, err := NewPeer(config)
		if err != nil {
			t.Fatalf("NewPeer() error = %v", err)
		}
		t.Cleanup(func() { _ = peer.Close() })
		controlFrame := func(sequence int) pion.DataChannelMessage {
			return pion.DataChannelMessage{IsString: true, Data: []byte(fmt.Sprintf(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-session-1","sequence":%d}`, sequence))}
		}
		peer.enqueueControlMessage(controlFrame(1))
		awaitSignal(t, entered, time.Second, "handler entered")
		peer.enqueueControlMessage(controlFrame(2))
		peer.enqueueControlMessage(controlFrame(3))
		awaitSignal(t, peer.Done(), time.Second, "control queue overflow closes peer")
	})
}

func startLoopbackTURN(t *testing.T) TURNCredentials {
	t.Helper()
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket(loopback TURN) error = %v", err)
	}
	const username = "loopback-user"
	const password = "loopback-password"
	const realm = "loopback.invalid"
	server, err := turn.NewServer(turn.ServerConfig{
		Realm: realm,
		AuthHandler: func(attributes *turn.RequestAttributes) (string, []byte, bool) {
			if attributes.Username != username {
				return "", nil, false
			}
			return username, turn.GenerateAuthKey(username, realm, password), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: listener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"),
				Address:      "127.0.0.1",
			},
			PermissionHandler: func(_ net.Addr, peerIP net.IP) bool { return peerIP.IsLoopback() },
		}},
	})
	if err != nil {
		_ = listener.Close()
		t.Fatalf("turn.NewServer() error = %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	return TURNCredentials{
		uris:      []string{fmt.Sprintf("turn:%s?transport=udp", listener.LocalAddr())},
		username:  username,
		password:  password,
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func newLoopbackPeerConnection(t *testing.T, credentials TURNCredentials) *pion.PeerConnection {
	t.Helper()
	mediaEngine := &pion.MediaEngine{}
	if err := mediaEngine.RegisterCodec(pion.RTPCodecParameters{
		RTPCodecCapability: pion.RTPCodecCapability{MimeType: pion.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"},
		PayloadType:        opusPayloadType,
	}, pion.RTPCodecTypeAudio); err != nil {
		t.Fatalf("RegisterCodec() error = %v", err)
	}
	registry := &interceptor.Registry{}
	if err := pion.RegisterDefaultInterceptors(mediaEngine, registry); err != nil {
		t.Fatalf("RegisterDefaultInterceptors() error = %v", err)
	}
	settings := pion.SettingEngine{}
	settings.SetNetworkTypes([]pion.NetworkType{pion.NetworkTypeUDP4})
	api := pion.NewAPI(pion.WithMediaEngine(mediaEngine), pion.WithInterceptorRegistry(registry), pion.WithSettingEngine(settings))
	peerConnection, err := api.NewPeerConnection(pion.Configuration{
		ICEServers:         []pion.ICEServer{{URLs: credentials.uris, Username: credentials.username, Credential: credentials.password, CredentialType: pion.ICECredentialTypePassword}},
		ICETransportPolicy: pion.ICETransportPolicyRelay,
		BundlePolicy:       pion.BundlePolicyMaxBundle,
		RTCPMuxPolicy:      pion.RTCPMuxPolicyRequire,
	})
	if err != nil {
		t.Fatalf("NewPeerConnection() error = %v", err)
	}
	return peerConnection
}

func awaitSignal(t *testing.T, signal <-chan struct{}, timeout time.Duration, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func awaitPeerState(t *testing.T, peer *Peer, expected PeerState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if peer.State() == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("peer state = %q, want %q", peer.State(), expected)
}

func assertSDPHasOnlyRelayUDPCandidates(t *testing.T, rawSDP string) {
	t.Helper()
	candidateCount := 0
	for _, line := range strings.Split(rawSDP, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "a=candidate:") {
			continue
		}
		candidateCount++
		parts := strings.Fields(line)
		if len(parts) < 8 || !strings.EqualFold(parts[2], "udp") || parts[7] != "relay" {
			t.Fatalf("SDP candidate was not relay-only UDP: %q", line)
		}
	}
	if candidateCount == 0 {
		t.Fatal("SDP contained no gathered relay candidates")
	}
}

func extractRelayRTPICECandidates(t *testing.T, rawSDP string) []ICECandidate {
	t.Helper()
	description := &sdp.SessionDescription{}
	if err := description.UnmarshalString(rawSDP); err != nil {
		t.Fatalf("parse SDP candidates error = %v", err)
	}
	candidates := make([]ICECandidate, 0)
	for mediaIndex, media := range description.MediaDescriptions {
		mid, hasMID := exactAttributeValue(media.Attributes, "mid")
		ufrag, hasUfrag := exactAttributeValue(media.Attributes, "ice-ufrag")
		if !hasMID || !hasUfrag || mediaIndex > int(^uint16(0)) {
			continue
		}
		for _, attribute := range media.Attributes {
			if attribute.Key != sdp.AttrKeyCandidate {
				continue
			}
			parsed, err := ice.UnmarshalCandidate(attribute.Value)
			if err != nil || parsed.Type() != ice.CandidateTypeRelay || parsed.NetworkType() != ice.NetworkTypeUDP4 || parsed.Component() != ice.ComponentRTP {
				continue
			}
			candidateMID := mid
			candidateIndex := uint16(mediaIndex)
			candidateUfrag := ufrag
			candidates = append(candidates, ICECandidate{
				candidate:        "candidate:" + attribute.Value,
				sdpMid:           &candidateMID,
				sdpMLineIndex:    &candidateIndex,
				usernameFragment: &candidateUfrag,
			})
		}
	}
	if len(candidates) == 0 {
		t.Fatal("SDP contained no trickle-compatible relay UDP RTP candidate")
	}
	return candidates
}

func stripICECandidates(rawSDP string) string {
	lines := strings.Split(rawSDP, "\r\n")
	filtered := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "a=candidate:") || line == "a=end-of-candidates" {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\r\n")
}

func replaceLast(value, old, replacement string) string {
	index := strings.LastIndex(value, old)
	if index < 0 {
		return value
	}
	return value[:index] + replacement + value[index+len(old):]
}

func assertSelectedPairIsRelayUDP(t *testing.T, peerConnection *pion.PeerConnection) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		pair, err := peerConnection.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
		lastErr = err
		if err == nil && pair != nil {
			if pair.Local.Typ != pion.ICECandidateTypeRelay || pair.Remote.Typ != pion.ICECandidateTypeRelay ||
				pair.Local.Protocol != pion.ICEProtocolUDP || pair.Remote.Protocol != pion.ICEProtocolUDP {
				t.Fatalf("selected pair was not relay-only UDP: %#v", pair)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for selected relay-only UDP pair: %v", lastErr)
}

func offerICEUfrag(rawSDP string) string {
	for _, line := range strings.Split(rawSDP, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "a=ice-ufrag:") {
			return strings.TrimPrefix(line, "a=ice-ufrag:")
		}
	}
	return ""
}

func awaitRemoteTrack(t *testing.T, tracks <-chan *pion.TrackRemote, description string) *pion.TrackRemote {
	t.Helper()
	select {
	case track := <-tracks:
		return track
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}

func readRTPPacketWithTimeout(t *testing.T, track *pion.TrackRemote, description string) *rtp.Packet {
	t.Helper()
	type readResult struct {
		packet *rtp.Packet
		err    error
	}
	result := make(chan readResult, 1)
	go func() {
		packet, _, err := track.ReadRTP()
		result <- readResult{packet: packet, err: err}
	}()
	select {
	case value := <-result:
		if value.err != nil {
			t.Fatalf("%s error = %v", description, value.err)
		}
		return value.packet
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}

func testOfferSDP(ufrag string) string {
	return testOfferSDPWithICECredentials(ufrag, "RemotePasswordValue123456789")
}

func testOfferSDPWithICECredentials(ufrag, password string) string {
	return "v=0\r\n" +
		"o=- 1 1 IN IP4 127.0.0.1\r\n" +
		"s=-\r\n" +
		"t=0 0\r\n" +
		"a=group:BUNDLE 0 1\r\n" +
		"a=msid-semantic:WMS *\r\n" +
		"m=audio 9 UDP/TLS/RTP/SAVPF 111\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=mid:0\r\n" +
		"a=sendrecv\r\n" +
		"a=ice-ufrag:" + ufrag + "\r\n" +
		"a=ice-pwd:" + password + "\r\n" +
		"a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00\r\n" +
		"a=setup:actpass\r\n" +
		"a=rtcp-mux\r\n" +
		"a=rtpmap:111 opus/48000/2\r\n" +
		"a=fmtp:111 minptime=10;useinbandfec=1\r\n" +
		"a=msid:stream-1 track-1\r\n" +
		"a=end-of-candidates\r\n" +
		"m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=mid:1\r\n" +
		"a=sendrecv\r\n" +
		"a=ice-ufrag:" + ufrag + "\r\n" +
		"a=ice-pwd:" + password + "\r\n" +
		"a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00\r\n" +
		"a=setup:actpass\r\n" +
		"a=sctp-port:5000\r\n" +
		"a=max-message-size:16384\r\n" +
		"a=end-of-candidates\r\n"
}

func testPeerConfig(ctx context.Context) PeerConfig {
	return PeerConfig{
		Context:   ctx,
		SessionID: "voice-session-1",
		TURNCredentials: TURNCredentials{
			uris:      []string{"turn:127.0.0.1:3478?transport=udp"},
			username:  "test-user",
			password:  "test-password",
			ExpiresAt: time.Now().Add(time.Hour),
		},
		AttachTimeout:             time.Second,
		GatherTimeout:             200 * time.Millisecond,
		ConnectTimeout:            time.Second,
		RestartWindow:             time.Second,
		MaxCandidates:             8,
		MaxCandidateBytes:         2048,
		MaxControlQueue:           8,
		HandleControl:             func(context.Context, protocol.ControlMessage) error { return nil },
		allowLoopbackTURNForTests: true,
	}
}

type recordingPeerSTTBinding struct {
	mu sync.Mutex

	opus         chan []byte
	controls     chan protocol.ControlMessage
	opusErr      error
	opusPanic    any
	controlErr   error
	controlPanic any
	closeErr     error
	closePanic   any
	closeCalls   atomic.Int32
}

func newRecordingPeerSTTBinding() *recordingPeerSTTBinding {
	return &recordingPeerSTTBinding{
		opus:     make(chan []byte, 8),
		controls: make(chan protocol.ControlMessage, 8),
	}
}

func (binding *recordingPeerSTTBinding) HandleOpus(payload []byte) error {
	binding.mu.Lock()
	panicValue := binding.opusPanic
	err := binding.opusErr
	binding.mu.Unlock()
	if panicValue != nil {
		panic(panicValue)
	}
	if err != nil {
		return err
	}
	binding.opus <- append([]byte(nil), payload...)
	return nil
}

func (binding *recordingPeerSTTBinding) HandleControl(_ context.Context, message protocol.ControlMessage) error {
	binding.mu.Lock()
	panicValue := binding.controlPanic
	err := binding.controlErr
	binding.mu.Unlock()
	if panicValue != nil {
		panic(panicValue)
	}
	if err != nil {
		return err
	}
	binding.controls <- message
	return nil
}

func (binding *recordingPeerSTTBinding) Close() error {
	binding.closeCalls.Add(1)
	binding.mu.Lock()
	panicValue := binding.closePanic
	err := binding.closeErr
	binding.mu.Unlock()
	if panicValue != nil {
		panic(panicValue)
	}
	return err
}

func (binding *recordingPeerSTTBinding) setOpusError(err error) {
	binding.mu.Lock()
	binding.opusErr = err
	binding.mu.Unlock()
}
