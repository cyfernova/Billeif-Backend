package webrtc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/protocol"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/sdp/v3"
	pion "github.com/pion/webrtc/v4"
)

const opusPayloadType pion.PayloadType = 111

const (
	maxPeerSessionIDBytes  = 256
	maxPeerCandidates      = 64
	maxPeerCandidateBytes  = 2048
	maxPeerControlQueue    = 64
	maxPeerTURNURLs        = 16
	maxPeerCredentialBytes = 256
)

var (
	ErrInvalidPeerConfig      = errors.New("invalid WebRTC peer configuration")
	ErrPeerClosed             = errors.New("WebRTC peer is closed")
	ErrPeerInvalidDescription = errors.New("invalid WebRTC session description")
	ErrPeerInvalidCandidate   = errors.New("invalid WebRTC ICE candidate")
	ErrPeerState              = errors.New("invalid WebRTC peer state")
	ErrPeerCapacity           = errors.New("WebRTC peer capacity exceeded")
	ErrPeerGatherTimeout      = errors.New("WebRTC ICE gathering timed out")
	ErrPeerInvalidControl     = errors.New("invalid WebRTC control message")
	ErrPeerInvalidAudio       = errors.New("invalid WebRTC inbound audio")
	errControlHandlerPanic    = errors.New("voice control handler panicked")
	errSTTBindingPanic        = errors.New("voice STT binding panicked")
)

// STTBinding is the narrow per-peer bridge implemented by
// runtime.STTSessionBinding. HandleOpus is synchronous: implementations must
// consume the payload before returning and must not retain it.
type STTBinding interface {
	HandleOpus([]byte) error
	HandleControl(context.Context, protocol.ControlMessage) error
	Close() error
}

type PeerState string

const (
	PeerStateAwaitingOffer PeerState = "AWAITING_OFFER"
	PeerStateConnecting    PeerState = "CONNECTING"
	PeerStateConnected     PeerState = "CONNECTED"
	PeerStateReconnecting  PeerState = "RECONNECTING"
	PeerStateClosing       PeerState = "CLOSING"
	PeerStateClosed        PeerState = "CLOSED"
)

type ICECandidate struct {
	candidate        string
	sdpMid           *string
	sdpMLineIndex    *uint16
	usernameFragment *string
}

func (candidate ICECandidate) String() string {
	ufragFingerprint := ""
	if candidate.usernameFragment != nil {
		ufragFingerprint = hashString(*candidate.usernameFragment)
	}
	return fmt.Sprintf("ice_candidate{candidate_bytes=%d,ufrag_sha256=%s}", len(candidate.candidate), ufragFingerprint)
}

func (candidate ICECandidate) GoString() string {
	return candidate.String()
}

func (candidate ICECandidate) MarshalJSON() ([]byte, error) {
	type redactedCandidate struct {
		CandidateBytes int    `json:"candidate_bytes"`
		UfragSHA256    string `json:"ufrag_sha256,omitempty"`
	}
	ufragFingerprint := ""
	if candidate.usernameFragment != nil {
		ufragFingerprint = hashString(*candidate.usernameFragment)
	}
	return json.Marshal(redactedCandidate{CandidateBytes: len(candidate.candidate), UfragSHA256: ufragFingerprint})
}

func (candidate ICECandidate) MarshalXML(encoder *xml.Encoder, start xml.StartElement) error {
	type redactedCandidate struct {
		CandidateBytes int    `xml:"candidate_bytes"`
		UfragSHA256    string `xml:"ufrag_sha256,omitempty"`
	}
	if start.Name.Local == "" {
		start.Name.Local = "ice_candidate"
	}
	ufragFingerprint := ""
	if candidate.usernameFragment != nil {
		ufragFingerprint = hashString(*candidate.usernameFragment)
	}
	return encoder.EncodeElement(redactedCandidate{CandidateBytes: len(candidate.candidate), UfragSHA256: ufragFingerprint}, start)
}

func (candidate ICECandidate) GobEncode() ([]byte, error) {
	return candidate.MarshalJSON()
}

type PeerConfig struct {
	Context           context.Context
	SessionID         string
	TURNCredentials   TURNCredentials
	AttachTimeout     time.Duration
	GatherTimeout     time.Duration
	ConnectTimeout    time.Duration
	RestartWindow     time.Duration
	MaxCandidates     int
	MaxCandidateBytes int
	MaxControlQueue   int
	// HandleControl receives non-speech controls on the ordered worker with
	// the peer-derived context. An admitted callback may overlap or trigger
	// Close, must honor cancellation, and never commits its sequence after the
	// peer context is canceled. Speech boundaries route exclusively to STTBinding.
	HandleControl             func(context.Context, protocol.ControlMessage) error
	STTBinding                STTBinding
	allowLoopbackTURNForTests bool
}

type Peer struct {
	mu                   sync.Mutex
	signalMu             sync.Mutex
	state                PeerState
	pc                   *pion.PeerConnection
	config               PeerConfig
	remoteUfragHash      [sha256.Size]byte
	remotePasswordHash   [sha256.Size]byte
	hasRemoteUfrag       bool
	pendingCandidates    []pendingICECandidate
	candidateCounts      map[[sha256.Size]byte]int
	outboundAudio        *pion.TrackLocalStaticRTP
	inboundAudio         chan *pion.TrackRemote
	inboundTrackAccepted bool
	controlQueue         chan []byte
	controlTracker       *protocol.SequenceTracker
	controlChannel       *pion.DataChannel
	lastInboundSequence  int
	lastOutboundSequence int
	sendMu               sync.Mutex
	peerContext          context.Context
	cancelPeer           context.CancelFunc
	setRemoteDescription func(pion.SessionDescription) error
	addICECandidate      func(pion.ICECandidateInit) error
	pionLoggerFactory    logging.LoggerFactory
	connectTimer         *time.Timer
	restartTimer         *time.Timer
	restartDeadline      time.Time
	connectionEpoch      uint64
	done                 chan struct{}
	closeOne             sync.Once
	attach               *time.Timer
	mediaWorkers         sync.WaitGroup
}

type silentPionLoggerFactory struct{}

func (silentPionLoggerFactory) NewLogger(string) logging.LeveledLogger {
	return silentPionLogger{}
}

type silentPionLogger struct{}

func (silentPionLogger) Trace(string)          {}
func (silentPionLogger) Tracef(string, ...any) {}
func (silentPionLogger) Debug(string)          {}
func (silentPionLogger) Debugf(string, ...any) {}
func (silentPionLogger) Info(string)           {}
func (silentPionLogger) Infof(string, ...any)  {}
func (silentPionLogger) Warn(string)           {}
func (silentPionLogger) Warnf(string, ...any)  {}
func (silentPionLogger) Error(string)          {}
func (silentPionLogger) Errorf(string, ...any) {}

func (peer *Peer) String() string {
	if peer == nil {
		return "peer{nil}"
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	return fmt.Sprintf("peer{state=%s,pending_candidates=%d}", peer.state, len(peer.pendingCandidates))
}

func (peer *Peer) GoString() string {
	return peer.String()
}

func (peer *Peer) MarshalJSON() ([]byte, error) {
	if peer == nil {
		return []byte("null"), nil
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	type redactedPeer struct {
		State             PeerState `json:"state"`
		PendingCandidates int       `json:"pending_candidates"`
	}
	return json.Marshal(redactedPeer{State: peer.state, PendingCandidates: len(peer.pendingCandidates)})
}

type pendingICECandidate struct {
	value     pion.ICECandidateInit
	ufragHash [sha256.Size]byte
}

type answerStateOperation struct {
	priorState PeerState
	restart    bool
}

type iceCredentialHashes struct {
	ufragHash    [sha256.Size]byte
	passwordHash [sha256.Size]byte
}

type iceCredentialValues struct {
	ufrag    string
	password string
	present  bool
}

func NewPeer(config PeerConfig) (*Peer, error) {
	if err := validatePeerConfig(config); err != nil {
		return nil, err
	}

	mediaEngine := &pion.MediaEngine{}
	if err := mediaEngine.RegisterCodec(pion.RTPCodecParameters{
		RTPCodecCapability: pion.RTPCodecCapability{
			MimeType:     pion.MimeTypeOpus,
			ClockRate:    48000,
			Channels:     2,
			SDPFmtpLine:  "minptime=10;useinbandfec=1",
			RTCPFeedback: nil,
		},
		PayloadType: opusPayloadType,
	}, pion.RTPCodecTypeAudio); err != nil {
		return nil, ErrInvalidPeerConfig
	}

	settingEngine := pion.SettingEngine{}
	settingEngine.SetNetworkTypes([]pion.NetworkType{pion.NetworkTypeUDP4})
	settingEngine.SetSCTPMaxMessageSize(protocol.MaxControlMessageBytes)
	loggerFactory := silentPionLoggerFactory{}
	settingEngine.LoggerFactory = loggerFactory
	registry := &interceptor.Registry{}
	if err := pion.RegisterDefaultInterceptorsWithOptions(mediaEngine, registry, pion.WithInterceptorLoggerFactory(loggerFactory)); err != nil {
		return nil, ErrInvalidPeerConfig
	}
	api := pion.NewAPI(pion.WithMediaEngine(mediaEngine), pion.WithInterceptorRegistry(registry), pion.WithSettingEngine(settingEngine))
	pc, err := api.NewPeerConnection(pion.Configuration{
		ICEServers: []pion.ICEServer{{
			URLs:           append([]string(nil), config.TURNCredentials.uris...),
			Username:       config.TURNCredentials.username,
			Credential:     config.TURNCredentials.password,
			CredentialType: pion.ICECredentialTypePassword,
		}},
		ICETransportPolicy: pion.ICETransportPolicyRelay,
		BundlePolicy:       pion.BundlePolicyMaxBundle,
		RTCPMuxPolicy:      pion.RTCPMuxPolicyRequire,
	})
	if err != nil {
		return nil, ErrInvalidPeerConfig
	}

	outboundAudio, err := pion.NewTrackLocalStaticRTP(pion.RTPCodecCapability{
		MimeType: pion.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1",
	}, "runtime-audio", "runtime-stream")
	if err != nil {
		_ = pc.Close()
		return nil, ErrInvalidPeerConfig
	}
	storedConfig := config
	storedConfig.TURNCredentials = TURNCredentials{ExpiresAt: config.TURNCredentials.ExpiresAt}
	peerContext, cancelPeer := context.WithCancel(config.Context)
	peer := &Peer{
		state:             PeerStateAwaitingOffer,
		pc:                pc,
		config:            storedConfig,
		candidateCounts:   make(map[[sha256.Size]byte]int),
		outboundAudio:     outboundAudio,
		inboundAudio:      make(chan *pion.TrackRemote, 1),
		controlQueue:      make(chan []byte, config.MaxControlQueue),
		controlTracker:    protocol.NewSequenceTracker(1, config.MaxControlQueue),
		peerContext:       peerContext,
		cancelPeer:        cancelPeer,
		pionLoggerFactory: loggerFactory,
		done:              make(chan struct{}),
	}
	peer.setRemoteDescription = pc.SetRemoteDescription
	peer.addICECandidate = pc.AddICECandidate
	transceiver, err := pc.AddTransceiverFromTrack(outboundAudio, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionSendrecv})
	if err != nil {
		_ = pc.Close()
		return nil, ErrInvalidPeerConfig
	}
	peer.configureCallbacks()
	go peer.drainSenderRTCP(transceiver.Sender())
	go peer.runControlWorker()
	peer.attach = time.AfterFunc(config.AttachTimeout, func() { _ = peer.Close() })
	go func() {
		select {
		case <-config.Context.Done():
			_ = peer.Close()
		case <-peer.done:
		}
	}()
	return peer, nil
}

func (peer *Peer) RefreshICE(ctx context.Context, credentials TURNCredentials) error {
	peer.signalMu.Lock()
	defer peer.signalMu.Unlock()

	if ctx == nil || ctx.Err() != nil {
		return ErrPeerState
	}
	if err := validatePeerCredentials(credentials, peer.config.allowLoopbackTURNForTests); err != nil {
		return err
	}
	peer.mu.Lock()
	closed := peer.state == PeerStateClosing || peer.state == PeerStateClosed
	peer.mu.Unlock()
	if closed {
		return ErrPeerClosed
	}
	configuration := peer.pc.GetConfiguration()
	configuration.ICEServers = []pion.ICEServer{{
		URLs:           append([]string(nil), credentials.uris...),
		Username:       credentials.username,
		Credential:     credentials.password,
		CredentialType: pion.ICECredentialTypePassword,
	}}
	configuration.ICETransportPolicy = pion.ICETransportPolicyRelay
	if err := peer.pc.SetConfiguration(configuration); err != nil {
		return ErrPeerState
	}
	return nil
}

func (peer *Peer) AddCandidate(ctx context.Context, candidate ICECandidate) error {
	peer.signalMu.Lock()
	defer peer.signalMu.Unlock()

	if ctx == nil || ctx.Err() != nil {
		return ErrPeerState
	}
	init, ufragHash, err := peer.validateCandidate(candidate)
	if err != nil {
		return err
	}
	peer.mu.Lock()
	if peer.state == PeerStateClosing || peer.state == PeerStateClosed {
		peer.mu.Unlock()
		return ErrPeerClosed
	}
	if peer.candidateCounts[ufragHash] >= peer.config.MaxCandidates {
		peer.mu.Unlock()
		return ErrPeerCapacity
	}
	if peer.hasRemoteUfrag && peer.remoteUfragHash == ufragHash {
		peer.mu.Unlock()
		if err := peer.addICECandidate(init); err != nil {
			return ErrPeerInvalidCandidate
		}
		peer.mu.Lock()
		if peer.state == PeerStateClosing || peer.state == PeerStateClosed {
			peer.mu.Unlock()
			return ErrPeerClosed
		}
		peer.candidateCounts[ufragHash]++
		peer.mu.Unlock()
		return nil
	}
	if len(peer.pendingCandidates) >= peer.config.MaxCandidates {
		peer.mu.Unlock()
		return ErrPeerCapacity
	}
	peer.pendingCandidates = append(peer.pendingCandidates, pendingICECandidate{value: init, ufragHash: ufragHash})
	peer.candidateCounts[ufragHash]++
	peer.mu.Unlock()
	return nil
}

func (peer *Peer) validateCandidate(candidate ICECandidate) (pion.ICECandidateInit, [sha256.Size]byte, error) {
	if candidate.candidate == "" || len(candidate.candidate) > peer.config.MaxCandidateBytes ||
		candidate.sdpMid == nil || len(*candidate.sdpMid) > 64 || !validPeerToken(*candidate.sdpMid) ||
		candidate.sdpMLineIndex == nil || *candidate.sdpMLineIndex > 1 ||
		candidate.usernameFragment == nil || !validICECredential(*candidate.usernameFragment, 4) {
		return pion.ICECandidateInit{}, [sha256.Size]byte{}, ErrPeerInvalidCandidate
	}
	parsed, err := ice.UnmarshalCandidate(candidate.candidate)
	if err != nil || parsed.Type() != ice.CandidateTypeRelay || parsed.NetworkType() != ice.NetworkTypeUDP4 || parsed.Component() != ice.ComponentRTP {
		return pion.ICECandidateInit{}, [sha256.Size]byte{}, ErrPeerInvalidCandidate
	}
	mid := *candidate.sdpMid
	index := *candidate.sdpMLineIndex
	ufrag := *candidate.usernameFragment
	ufragExtensionCount := 0
	fields := strings.Fields(strings.TrimPrefix(candidate.candidate, "candidate:"))
	for index := 8; index < len(fields); index += 2 {
		if fields[index] == "ufrag" {
			ufragExtensionCount++
			if index+1 >= len(fields) || fields[index+1] != ufrag {
				return pion.ICECandidateInit{}, [sha256.Size]byte{}, ErrPeerInvalidCandidate
			}
		}
	}
	if ufragExtensionCount > 1 {
		return pion.ICECandidateInit{}, [sha256.Size]byte{}, ErrPeerInvalidCandidate
	}
	if ufragExtensionCount == 0 {
		if err := parsed.AddExtension(ice.CandidateExtension{Key: "ufrag", Value: ufrag}); err != nil {
			return pion.ICECandidateInit{}, [sha256.Size]byte{}, ErrPeerInvalidCandidate
		}
	}
	return pion.ICECandidateInit{
		Candidate:        "candidate:" + parsed.Marshal(),
		SDPMid:           &mid,
		SDPMLineIndex:    &index,
		UsernameFragment: &ufrag,
	}, sha256.Sum256([]byte(ufrag)), nil
}

func (peer *Peer) Answer(ctx context.Context, offerSDP string, restart bool) (string, error) {
	peer.signalMu.Lock()
	defer peer.signalMu.Unlock()

	if ctx == nil || ctx.Err() != nil {
		return "", ErrPeerState
	}
	credentials, embeddedCandidateCount, err := validateOfferDescription(offerSDP, peer.config.MaxCandidates, peer.config.MaxCandidateBytes)
	if err != nil {
		if errors.Is(err, ErrPeerCapacity) {
			return "", ErrPeerCapacity
		}
		return "", ErrPeerInvalidDescription
	}
	peer.mu.Lock()
	pending := make([]pendingICECandidate, 0, len(peer.pendingCandidates))
	for _, candidate := range peer.pendingCandidates {
		if candidate.ufragHash == credentials.ufragHash {
			pending = append(pending, candidate)
		}
	}
	peer.mu.Unlock()
	if embeddedCandidateCount+len(pending) > peer.config.MaxCandidates {
		return "", ErrPeerCapacity
	}
	operation, err := peer.beginAnswerState(restart, credentials)
	if err != nil {
		return "", err
	}

	if err := peer.setRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeOffer, SDP: offerSDP}); err != nil {
		peer.rollbackAnswerState(operation)
		return "", ErrPeerInvalidDescription
	}
	for _, candidate := range pending {
		if err := peer.addICECandidate(candidate.value); err != nil {
			go func() { _ = peer.Close() }()
			return "", ErrPeerInvalidCandidate
		}
	}
	candidateCount := embeddedCandidateCount + len(pending)
	for index := range pending {
		pending[index].value = pion.ICECandidateInit{}
		pending[index].ufragHash = [sha256.Size]byte{}
	}
	answer, err := peer.pc.CreateAnswer(nil)
	if err != nil {
		go func() { _ = peer.Close() }()
		return "", ErrPeerInvalidDescription
	}
	gathered := pion.GatheringCompletePromise(peer.pc)
	if err := peer.pc.SetLocalDescription(answer); err != nil {
		go func() { _ = peer.Close() }()
		return "", ErrPeerInvalidDescription
	}
	timer := time.NewTimer(peer.config.GatherTimeout)
	defer timer.Stop()
	select {
	case <-gathered:
	case <-ctx.Done():
		go func() { _ = peer.Close() }()
		return "", ErrPeerGatherTimeout
	case <-timer.C:
		go func() { _ = peer.Close() }()
		return "", ErrPeerGatherTimeout
	}
	localDescription := peer.pc.LocalDescription()
	if localDescription == nil {
		go func() { _ = peer.Close() }()
		return "", ErrPeerInvalidDescription
	}
	sanitizedAnswer, err := sanitizeGatheredRelayDescription(localDescription.SDP, peer.config.MaxCandidates, peer.config.MaxCandidateBytes)
	if err != nil {
		go func() { _ = peer.Close() }()
		if errors.Is(err, ErrPeerCapacity) {
			return "", ErrPeerCapacity
		}
		return "", ErrPeerInvalidDescription
	}

	if err := peer.completeAnswerState(operation, credentials, candidateCount); err != nil {
		return "", err
	}
	return sanitizedAnswer, nil
}

func (peer *Peer) beginAnswerState(restart bool, credentials iceCredentialHashes) (answerStateOperation, error) {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	operation := answerStateOperation{priorState: peer.state, restart: restart}
	if peer.state == PeerStateClosing || peer.state == PeerStateClosed {
		return answerStateOperation{}, ErrPeerClosed
	}
	if restart {
		if !peer.hasRemoteUfrag || peer.remoteUfragHash == credentials.ufragHash || peer.remotePasswordHash == credentials.passwordHash ||
			(peer.state != PeerStateConnected && peer.state != PeerStateReconnecting) {
			return answerStateOperation{}, ErrPeerState
		}
		peer.state = PeerStateReconnecting
		peer.startRestartTimerLocked()
	} else {
		if peer.state != PeerStateAwaitingOffer {
			return answerStateOperation{}, ErrPeerState
		}
		peer.state = PeerStateConnecting
		peer.startConnectTimerLocked()
	}
	return operation, nil
}

func (peer *Peer) rollbackAnswerState(operation answerStateOperation) {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.state == PeerStateClosing || peer.state == PeerStateClosed || peer.state == PeerStateConnected {
		return
	}
	if operation.restart && operation.priorState == PeerStateReconnecting && !peer.restartDeadline.IsZero() {
		peer.state = PeerStateReconnecting
		if peer.restartTimer == nil {
			peer.armRestartTimerLocked(peer.restartDeadline)
		}
		return
	}
	peer.connectionEpoch++
	peer.stopConnectionTimersLocked()
	peer.state = operation.priorState
}

func (peer *Peer) completeAnswerState(_ answerStateOperation, credentials iceCredentialHashes, candidateCount int) error {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.state == PeerStateClosing || peer.state == PeerStateClosed {
		return ErrPeerClosed
	}
	peer.remoteUfragHash = credentials.ufragHash
	peer.remotePasswordHash = credentials.passwordHash
	peer.hasRemoteUfrag = true
	peer.clearPendingCandidatesLocked()
	peer.candidateCounts = map[[sha256.Size]byte]int{credentials.ufragHash: candidateCount}
	if peer.attach != nil {
		peer.attach.Stop()
		peer.attach = nil
	}
	return nil
}

func (peer *Peer) clearPendingCandidatesLocked() {
	for index := range peer.pendingCandidates {
		peer.pendingCandidates[index].value = pion.ICECandidateInit{}
		peer.pendingCandidates[index].ufragHash = [sha256.Size]byte{}
	}
	peer.pendingCandidates = nil
}

func validateGatheredRelayDescription(rawSDP string) error {
	description := &sdp.SessionDescription{}
	if err := description.UnmarshalString(rawSDP); err != nil {
		return ErrPeerInvalidDescription
	}
	candidateCount := 0
	for _, attribute := range description.Attributes {
		if attribute.Key == sdp.AttrKeyCandidate {
			return ErrPeerInvalidDescription
		}
	}
	for _, media := range description.MediaDescriptions {
		for _, attribute := range media.Attributes {
			if attribute.Key == sdp.AttrKeyCandidate {
				candidateCount++
				if validateRemoteCandidates([]sdp.Attribute{attribute}) != nil {
					return ErrPeerInvalidDescription
				}
			}
		}
	}
	if candidateCount == 0 {
		return ErrPeerInvalidDescription
	}
	return nil
}

func sanitizeGatheredRelayDescription(rawSDP string, maxCandidates, maxCandidateBytes int) (string, error) {
	if rawSDP == "" || maxCandidates <= 0 || maxCandidateBytes <= 0 {
		return "", ErrPeerInvalidDescription
	}
	description := &sdp.SessionDescription{}
	if err := description.UnmarshalString(rawSDP); err != nil || attributeCount(description.Attributes, sdp.AttrKeyCandidate) != 0 {
		return "", ErrPeerInvalidDescription
	}
	candidateCount := 0
	for _, media := range description.MediaDescriptions {
		filtered := make([]sdp.Attribute, 0, len(media.Attributes))
		for _, attribute := range media.Attributes {
			if attribute.Key != sdp.AttrKeyCandidate {
				filtered = append(filtered, attribute)
				continue
			}
			if len("candidate:")+len(attribute.Value) > maxCandidateBytes {
				return "", ErrPeerCapacity
			}
			candidate, err := ice.UnmarshalCandidate(attribute.Value)
			if err != nil || candidate.Type() != ice.CandidateTypeRelay || candidate.NetworkType() != ice.NetworkTypeUDP4 {
				return "", ErrPeerInvalidDescription
			}
			switch candidate.Component() {
			case ice.ComponentRTP:
				candidateCount++
				if candidateCount > maxCandidates {
					return "", ErrPeerCapacity
				}
				filtered = append(filtered, attribute)
			case 2:
				// Pion v4 currently gathers RTCP candidates even when rtcp-mux is
				// required. They are not signaled because this peer accepts only RTP.
			default:
				return "", ErrPeerInvalidDescription
			}
		}
		media.Attributes = filtered
	}
	marshaled, err := description.Marshal()
	if err != nil || len(marshaled) > 12*1024 {
		return "", ErrPeerInvalidDescription
	}
	sanitized := string(marshaled)
	if err := validateGatheredRelayDescription(sanitized); err != nil {
		return "", err
	}
	return sanitized, nil
}

func (peer *Peer) configureCallbacks() {
	peer.pc.OnTrack(func(track *pion.TrackRemote, _ *pion.RTPReceiver) {
		peer.handleInboundTrack(track)
	})
	peer.pc.OnDataChannel(func(channel *pion.DataChannel) {
		if !peer.acceptControlDataChannel(channel) {
			go func() {
				_ = channel.Close()
				_ = peer.Close()
			}()
		}
	})
	peer.pc.OnConnectionStateChange(peer.handleConnectionState)
}

func (peer *Peer) handleInboundTrack(track *pion.TrackRemote) {
	peer.mu.Lock()
	if peer.inboundTrackAccepted || peer.state == PeerStateClosing || peer.state == PeerStateClosed {
		peer.mu.Unlock()
		go func() { _ = peer.Close() }()
		return
	}
	if track == nil {
		peer.mu.Unlock()
		go func() { _ = peer.Close() }()
		return
	}
	codec := track.Codec().RTPCodecCapability
	if track.Kind() != pion.RTPCodecTypeAudio || !strings.EqualFold(codec.MimeType, pion.MimeTypeOpus) || codec.ClockRate != 48000 || codec.Channels != 2 {
		peer.mu.Unlock()
		go func() { _ = peer.Close() }()
		return
	}
	peer.inboundTrackAccepted = true
	binding := peer.config.STTBinding
	if binding != nil {
		peer.mediaWorkers.Add(1)
	}
	peer.mu.Unlock()
	if binding != nil {
		go peer.readInboundOpus(track)
		return
	}
	select {
	case peer.inboundAudio <- track:
	default:
		peer.closeAsynchronously()
	}
}

func (peer *Peer) readInboundOpus(track *pion.TrackRemote) {
	defer peer.mediaWorkers.Done()
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			if peer.peerContext.Err() == nil {
				peer.closeAsynchronously()
			}
			return
		}
		if packet == nil || peer.dispatchInboundOpus(packet.Payload) != nil {
			peer.closeAsynchronously()
			return
		}
	}
}

func (peer *Peer) dispatchInboundOpus(payload []byte) error {
	if len(payload) == 0 || len(payload) > audio.MaxOpusPacketBytes {
		return ErrPeerInvalidAudio
	}
	peer.mu.Lock()
	binding := peer.config.STTBinding
	closing := peer.state == PeerStateClosing || peer.state == PeerStateClosed
	peer.mu.Unlock()
	if closing || binding == nil {
		return ErrPeerClosed
	}
	return safeHandleOpus(binding, payload)
}

func (peer *Peer) acceptControlDataChannel(channel *pion.DataChannel) bool {
	if channel == nil || channel.Label() != protocol.DataChannelLabel || !channel.Ordered() ||
		channel.MaxPacketLifeTime() != nil || channel.MaxRetransmits() != nil || channel.Negotiated() || channel.Protocol() != "" {
		return false
	}
	peer.mu.Lock()
	if peer.controlChannel != nil || peer.state == PeerStateClosing || peer.state == PeerStateClosed {
		peer.mu.Unlock()
		return false
	}
	peer.controlChannel = channel
	peer.mu.Unlock()
	channel.OnMessage(peer.enqueueControlMessage)
	channel.OnClose(func() { go func() { _ = peer.Close() }() })
	return true
}

func (peer *Peer) enqueueControlMessage(message pion.DataChannelMessage) {
	if !message.IsString || len(message.Data) == 0 || len(message.Data) > protocol.MaxControlMessageBytes {
		go func() { _ = peer.Close() }()
		return
	}
	frame := append([]byte(nil), message.Data...)
	select {
	case peer.controlQueue <- frame:
	default:
		go func() { _ = peer.Close() }()
	}
}

func (peer *Peer) runControlWorker() {
	for {
		select {
		case <-peer.done:
			return
		case <-peer.peerContext.Done():
			return
		case frame := <-peer.controlQueue:
			if peer.peerContext.Err() != nil {
				clear(frame)
				return
			}
			if !hasExactControlJSONKeys(frame) {
				go func() { _ = peer.Close() }()
				return
			}
			message, err := protocol.DecodeControlMessage(frame, protocol.ClientToRuntime, nil)
			if err != nil || message.SessionID != peer.config.SessionID || message.Sequence <= peer.lastInboundSequence {
				go func() { _ = peer.Close() }()
				return
			}
			if peer.config.STTBinding != nil && (message.Type == protocol.EventSpeechStarted || message.Type == protocol.EventSpeechEnded) {
				if err := safeHandleSTTControl(peer.config.STTBinding, peer.peerContext, message); err != nil {
					peer.closeAsynchronously()
					return
				}
			} else if err := safeHandleControl(peer.config.HandleControl, peer.peerContext, message); err != nil {
				if errors.Is(err, errControlHandlerPanic) {
					peer.closeAsynchronously()
					return
				}
				if peer.peerContext.Err() != nil {
					return
				}
				continue
			}
			if peer.peerContext.Err() != nil {
				return
			}
			if err := peer.controlTracker.Accept(message.SessionID, protocol.ClientToRuntime, message.Sequence); err != nil {
				go func() { _ = peer.Close() }()
				return
			}
			peer.lastInboundSequence = message.Sequence
		}
	}
}

func hasExactControlJSONKeys(frame []byte) bool {
	allowed := map[string]struct{}{
		"type": {}, "protocol_version": {}, "session_id": {}, "sequence": {}, "turn_id": {}, "generation_id": {},
		"client_monotonic_ms": {}, "state": {}, "text": {}, "detected_language": {}, "language_probability": {},
		"error_code": {}, "message": {}, "rotate_at": {}, "ice_servers": {},
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return false
	}
	seen := make(map[string]struct{}, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return false
		}
		if _, permitted := allowed[key]; !permitted {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return false
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func safeHandleControl(handler func(context.Context, protocol.ControlMessage) error, ctx context.Context, message protocol.ControlMessage) (err error) {
	defer func() {
		if recover() != nil {
			err = errControlHandlerPanic
		}
	}()
	return handler(ctx, message)
}

func safeHandleSTTControl(binding STTBinding, ctx context.Context, message protocol.ControlMessage) (err error) {
	defer func() {
		if recover() != nil {
			err = errSTTBindingPanic
		}
	}()
	return binding.HandleControl(ctx, message)
}

func safeHandleOpus(binding STTBinding, payload []byte) (err error) {
	defer func() {
		if recover() != nil {
			err = errSTTBindingPanic
		}
	}()
	return binding.HandleOpus(payload)
}

func safeCloseSTTBinding(binding STTBinding) (err error) {
	defer func() {
		if recover() != nil {
			err = errSTTBindingPanic
		}
	}()
	if binding == nil {
		return nil
	}
	return binding.Close()
}

func (peer *Peer) closeAsynchronously() {
	go func() { _ = peer.Close() }()
}

func (peer *Peer) SendControl(message protocol.ControlMessage) error {
	peer.sendMu.Lock()
	defer peer.sendMu.Unlock()
	if message.SessionID != peer.config.SessionID || message.Sequence <= peer.lastOutboundSequence {
		return ErrPeerInvalidControl
	}
	if err := protocol.ValidateControlMessage(message, protocol.RuntimeToClient); err != nil {
		return ErrPeerInvalidControl
	}
	frame, err := json.Marshal(message)
	if err != nil || len(frame) > protocol.MaxControlMessageBytes {
		return ErrPeerInvalidControl
	}
	peer.mu.Lock()
	channel := peer.controlChannel
	closed := peer.state == PeerStateClosing || peer.state == PeerStateClosed
	peer.mu.Unlock()
	if closed {
		return ErrPeerClosed
	}
	if channel == nil || channel.ReadyState() != pion.DataChannelStateOpen {
		return ErrPeerState
	}
	if err := channel.SendText(string(frame)); err != nil {
		return ErrPeerState
	}
	if err := peer.controlTracker.Accept(message.SessionID, protocol.RuntimeToClient, message.Sequence); err != nil {
		return ErrPeerInvalidControl
	}
	peer.lastOutboundSequence = message.Sequence
	return nil
}

func (peer *Peer) InboundAudio() <-chan *pion.TrackRemote {
	return peer.inboundAudio
}

func (peer *Peer) OutboundAudio() *pion.TrackLocalStaticRTP {
	return peer.outboundAudio
}

func (peer *Peer) drainSenderRTCP(sender *pion.RTPSender) {
	for {
		if _, _, err := sender.ReadRTCP(); err != nil {
			return
		}
	}
}

func (peer *Peer) handleConnectionState(state pion.PeerConnectionState) {
	peer.mu.Lock()
	if peer.state == PeerStateClosing || peer.state == PeerStateClosed {
		peer.mu.Unlock()
		return
	}
	closePeer := false
	switch state {
	case pion.PeerConnectionStateConnected:
		peer.connectionEpoch++
		peer.state = PeerStateConnected
		peer.stopConnectionTimersLocked()
		if peer.attach != nil {
			peer.attach.Stop()
			peer.attach = nil
		}
	case pion.PeerConnectionStateDisconnected:
		if peer.state != PeerStateClosing && peer.state != PeerStateClosed && peer.state != PeerStateReconnecting {
			peer.state = PeerStateReconnecting
			peer.startRestartTimerLocked()
		}
	case pion.PeerConnectionStateFailed, pion.PeerConnectionStateClosed:
		closePeer = peer.state != PeerStateClosing && peer.state != PeerStateClosed
	}
	peer.mu.Unlock()
	if closePeer {
		go func() { _ = peer.Close() }()
	}
}

func (peer *Peer) startConnectTimerLocked() {
	if peer.connectTimer != nil {
		return
	}
	peer.connectionEpoch++
	epoch := peer.connectionEpoch
	peer.connectTimer = time.AfterFunc(peer.config.ConnectTimeout, func() {
		peer.mu.Lock()
		shouldClose := peer.connectionEpoch == epoch && peer.state == PeerStateConnecting
		peer.mu.Unlock()
		if shouldClose {
			_ = peer.Close()
		}
	})
}

func (peer *Peer) startRestartTimerLocked() {
	if peer.restartTimer != nil {
		return
	}
	peer.armRestartTimerLocked(time.Now().Add(peer.config.RestartWindow))
}

func (peer *Peer) armRestartTimerLocked(deadline time.Time) {
	peer.connectionEpoch++
	epoch := peer.connectionEpoch
	peer.restartDeadline = deadline
	remaining := time.Until(deadline)
	if remaining < 0 {
		remaining = 0
	}
	peer.restartTimer = time.AfterFunc(remaining, func() {
		peer.mu.Lock()
		shouldClose := peer.connectionEpoch == epoch && peer.state == PeerStateReconnecting && !time.Now().Before(peer.restartDeadline)
		peer.mu.Unlock()
		if shouldClose {
			_ = peer.Close()
		}
	})
}

func (peer *Peer) stopConnectionTimersLocked() {
	if peer.connectTimer != nil {
		peer.connectTimer.Stop()
		peer.connectTimer = nil
	}
	if peer.restartTimer != nil {
		peer.restartTimer.Stop()
		peer.restartTimer = nil
	}
	peer.restartDeadline = time.Time{}
}

func validateOfferDescription(rawSDP string, maxCandidates, maxCandidateBytes int) (iceCredentialHashes, int, error) {
	if rawSDP == "" || len(rawSDP) > 12*1024 || maxCandidates <= 0 || maxCandidateBytes <= 0 {
		return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
	}
	description := &sdp.SessionDescription{}
	if err := description.UnmarshalString(rawSDP); err != nil || len(description.MediaDescriptions) != 2 {
		return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
	}

	var audioCount, applicationCount int
	candidateCount := 0
	bundledMIDs, ok := exactBundledMIDs(description.Attributes)
	if !ok {
		return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
	}
	observedMIDs := make(map[string]struct{}, 2)
	if attributeCount(description.Attributes, sdp.AttrKeyCandidate) != 0 {
		return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
	}
	sessionCredentials, err := exactICECredentials(description.Attributes)
	if err != nil {
		return iceCredentialHashes{}, 0, err
	}
	sharedCredentials := iceCredentialValues{}
	for _, media := range description.MediaDescriptions {
		if media.MediaName.Port.Value == 0 {
			return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
		}
		mediaCandidateCount, err := validateRemoteCandidatesBounded(media.Attributes, maxCandidateBytes)
		if err != nil {
			return iceCredentialHashes{}, 0, err
		}
		candidateCount += mediaCandidateCount
		if candidateCount > maxCandidates {
			return iceCredentialHashes{}, 0, ErrPeerCapacity
		}
		mediaCredentials, err := exactICECredentials(media.Attributes)
		if err != nil {
			return iceCredentialHashes{}, 0, err
		}
		effectiveCredentials := sessionCredentials
		if mediaCredentials.present {
			if sessionCredentials.present && !sameICECredentials(sessionCredentials, mediaCredentials) {
				return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
			}
			effectiveCredentials = mediaCredentials
		}
		if !effectiveCredentials.present {
			return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
		}
		if sharedCredentials.present && !sameICECredentials(sharedCredentials, effectiveCredentials) {
			return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
		}
		sharedCredentials = effectiveCredentials
		mid, ok := exactAttributeValue(media.Attributes, "mid")
		if !ok {
			return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
		}
		if _, duplicate := observedMIDs[mid]; duplicate {
			return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
		}
		observedMIDs[mid] = struct{}{}
		switch media.MediaName.Media {
		case "audio":
			audioCount++
			if !hasOnlySendRecvDirection(media.Attributes) || !hasOpus48kStereo(media) ||
				attributeCount(media.Attributes, sdp.AttrKeyRTCPMux) != 1 || attributeCount(media.Attributes, "msid") != 1 ||
				attributeCount(media.Attributes, "ssrc-group") != 0 {
				return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
			}
		case "application":
			applicationCount++
			if !hasOnlySendRecvDirection(media.Attributes) || strings.Join(media.MediaName.Protos, "/") != "UDP/DTLS/SCTP" || len(media.MediaName.Formats) != 1 || media.MediaName.Formats[0] != "webrtc-datachannel" {
				return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
			}
		default:
			return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
		}
	}
	if audioCount != 1 || applicationCount != 1 || !sharedCredentials.present || !sameStringSet(observedMIDs, bundledMIDs) {
		return iceCredentialHashes{}, 0, ErrPeerInvalidDescription
	}
	for _, media := range description.MediaDescriptions {
		if err := validateCandidateUfragExtensions(media.Attributes, sharedCredentials.ufrag); err != nil {
			return iceCredentialHashes{}, 0, err
		}
	}
	return iceCredentialHashes{
		ufragHash:    sha256.Sum256([]byte(sharedCredentials.ufrag)),
		passwordHash: sha256.Sum256([]byte(sharedCredentials.password)),
	}, candidateCount, nil
}

func exactBundledMIDs(attributes []sdp.Attribute) (map[string]struct{}, bool) {
	var bundleFields []string
	for _, attribute := range attributes {
		if attribute.Key != "group" {
			continue
		}
		fields := strings.Fields(attribute.Value)
		if len(fields) != 3 || fields[0] != "BUNDLE" || bundleFields != nil || fields[1] == fields[2] {
			return nil, false
		}
		bundleFields = fields
	}
	if bundleFields == nil {
		return nil, false
	}
	return map[string]struct{}{bundleFields[1]: {}, bundleFields[2]: {}}, true
}

func exactAttributeValue(attributes []sdp.Attribute, key string) (string, bool) {
	value := ""
	count := 0
	for _, attribute := range attributes {
		if attribute.Key == key {
			value = attribute.Value
			count++
		}
	}
	return value, count == 1 && value != ""
}

func attributeCount(attributes []sdp.Attribute, key string) int {
	count := 0
	for _, attribute := range attributes {
		if attribute.Key == key {
			count++
		}
	}
	return count
}

func exactICECredentials(attributes []sdp.Attribute) (iceCredentialValues, error) {
	var ufrag, password string
	var ufragCount, passwordCount int
	for _, attribute := range attributes {
		switch attribute.Key {
		case "ice-ufrag":
			ufrag = attribute.Value
			ufragCount++
		case "ice-pwd":
			password = attribute.Value
			passwordCount++
		}
	}
	if ufragCount > 1 || passwordCount > 1 || (ufragCount == 0) != (passwordCount == 0) {
		return iceCredentialValues{}, ErrPeerInvalidDescription
	}
	if ufragCount == 0 {
		return iceCredentialValues{}, nil
	}
	if !validICECredential(ufrag, 4) || !validICECredential(password, 22) {
		return iceCredentialValues{}, ErrPeerInvalidDescription
	}
	return iceCredentialValues{ufrag: ufrag, password: password, present: true}, nil
}

func sameICECredentials(first, second iceCredentialValues) bool {
	return first.present == second.present && first.ufrag == second.ufrag && first.password == second.password
}

func validICECredential(value string, minimumBytes int) bool {
	if len(value) < minimumBytes || len(value) > 256 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '+' && character != '/' {
			return false
		}
	}
	return true
}

func validPeerToken(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '.' && character != '-' {
			return false
		}
	}
	return true
}

func sameStringSet(first, second map[string]struct{}) bool {
	if len(first) != len(second) {
		return false
	}
	for value := range first {
		if _, ok := second[value]; !ok {
			return false
		}
	}
	return true
}

func validateRemoteCandidates(attributes []sdp.Attribute) error {
	_, err := validateRemoteCandidatesBounded(attributes, maxPeerCandidateBytes)
	return err
}

func validateRemoteCandidatesBounded(attributes []sdp.Attribute, maxCandidateBytes int) (int, error) {
	candidateCount := 0
	for _, attribute := range attributes {
		if attribute.Key != sdp.AttrKeyCandidate {
			continue
		}
		candidateCount++
		if len("candidate:")+len(attribute.Value) > maxCandidateBytes {
			return 0, ErrPeerCapacity
		}
		candidate, err := ice.UnmarshalCandidate(attribute.Value)
		if err != nil || candidate.Type() != ice.CandidateTypeRelay || candidate.NetworkType() != ice.NetworkTypeUDP4 || candidate.Component() != ice.ComponentRTP {
			return 0, ErrPeerInvalidDescription
		}
	}
	return candidateCount, nil
}

func validateCandidateUfragExtensions(attributes []sdp.Attribute, expectedUfrag string) error {
	for _, attribute := range attributes {
		if attribute.Key != sdp.AttrKeyCandidate {
			continue
		}
		candidate, err := ice.UnmarshalCandidate(attribute.Value)
		if err != nil {
			return ErrPeerInvalidDescription
		}
		ufragCount := 0
		for _, extension := range candidate.Extensions() {
			if !strings.EqualFold(extension.Key, "ufrag") {
				continue
			}
			ufragCount++
			if extension.Key != "ufrag" || extension.Value != expectedUfrag || ufragCount > 1 {
				return ErrPeerInvalidDescription
			}
		}
	}
	return nil
}

func hasOnlySendRecvDirection(attributes []sdp.Attribute) bool {
	directionCount := 0
	for _, attribute := range attributes {
		switch attribute.Key {
		case sdp.AttrKeySendRecv:
			directionCount++
		case sdp.AttrKeySendOnly, sdp.AttrKeyRecvOnly, sdp.AttrKeyInactive:
			return false
		}
	}
	return directionCount == 1
}

func hasOpus48kStereo(media *sdp.MediaDescription) bool {
	if len(media.MediaName.Formats) != 1 || media.MediaName.Formats[0] != fmt.Sprint(opusPayloadType) {
		return false
	}
	rtpMapCount := 0
	formatParametersCount := 0
	for _, attribute := range media.Attributes {
		switch attribute.Key {
		case "rtpmap":
			rtpMapCount++
			if !strings.EqualFold(strings.TrimSpace(attribute.Value), "111 opus/48000/2") {
				return false
			}
		case "fmtp":
			formatParametersCount++
			if strings.TrimSpace(attribute.Value) != "111 minptime=10;useinbandfec=1" {
				return false
			}
		}
	}
	return rtpMapCount == 1 && formatParametersCount == 1
}

func validatePeerConfig(config PeerConfig) error {
	if config.Context == nil || strings.TrimSpace(config.SessionID) == "" || len(config.SessionID) > maxPeerSessionIDBytes ||
		config.AttachTimeout <= 0 || config.GatherTimeout <= 0 || config.GatherTimeout >= 10*time.Second || config.ConnectTimeout <= 0 ||
		config.RestartWindow <= 0 || config.MaxCandidates <= 0 || config.MaxCandidates > maxPeerCandidates ||
		config.MaxCandidateBytes <= 0 || config.MaxCandidateBytes > maxPeerCandidateBytes ||
		config.MaxControlQueue <= 0 || config.MaxControlQueue > maxPeerControlQueue || config.HandleControl == nil {
		return ErrInvalidPeerConfig
	}
	return validatePeerCredentials(config.TURNCredentials, config.allowLoopbackTURNForTests)
}

func validatePeerCredentials(credentials TURNCredentials, allowLoopback bool) error {
	if len(credentials.username) > maxPeerCredentialBytes || !validPeerToken(credentials.username) ||
		len(credentials.password) > maxPeerCredentialBytes || !validPeerToken(credentials.password) ||
		len(credentials.uris) == 0 || len(credentials.uris) > maxPeerTURNURLs || !credentials.ExpiresAt.After(time.Now()) {
		return ErrInvalidPeerConfig
	}
	if !allowLoopback {
		if ValidateTURNCredentials(MumbaiRegion, time.Now(), time.Nanosecond, credentials) != nil {
			return ErrInvalidPeerConfig
		}
		return nil
	}
	seenURLs := make(map[string]struct{}, len(credentials.uris))
	for _, rawURL := range credentials.uris {
		if len(rawURL) > maxPeerCredentialBytes || !printableASCII(rawURL) {
			return ErrInvalidPeerConfig
		}
		if _, duplicate := seenURLs[rawURL]; duplicate {
			return ErrInvalidPeerConfig
		}
		seenURLs[rawURL] = struct{}{}
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "turn" || parsed.Opaque == "" || parsed.Host != "" || parsed.RawQuery != "transport=udp" || strings.Contains(parsed.Opaque, "@") {
			return ErrInvalidPeerConfig
		}
		host, portText, err := net.SplitHostPort(parsed.Opaque)
		port, portErr := strconv.ParseUint(portText, 10, 16)
		if err != nil || portErr != nil || host == "" || port == 0 || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return ErrInvalidPeerConfig
		}
	}
	return nil
}

func printableASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

func (peer *Peer) State() PeerState {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	return peer.state
}

func (peer *Peer) Done() <-chan struct{} {
	return peer.done
}

func (peer *Peer) Close() error {
	var closeErr error
	peer.closeOne.Do(func() {
		peer.mu.Lock()
		peer.state = PeerStateClosing
		peer.connectionEpoch++
		if peer.attach != nil {
			peer.attach.Stop()
			peer.attach = nil
		}
		peer.stopConnectionTimersLocked()
		peer.clearPendingCandidatesLocked()
		peer.candidateCounts = nil
		peer.mu.Unlock()

		peer.cancelPeer()
		draining := true
		for draining {
			select {
			case frame := <-peer.controlQueue:
				clear(frame)
			default:
				draining = false
			}
		}
		peerConnectionError := peer.pc.Close()
		peer.mediaWorkers.Wait()
		bindingError := safeCloseSTTBinding(peer.config.STTBinding)
		peer.mu.Lock()
		peer.controlChannel = nil
		peer.state = PeerStateClosed
		peer.mu.Unlock()
		close(peer.done)
		closeErr = errors.Join(peerConnectionError, bindingError)
	})
	return closeErr
}
