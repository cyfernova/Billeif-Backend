// Package composition wires the authenticated WebRTC peer lifetime to the
// final-only Sarvam STT, chat, tenant-safe HTTP tools, and session output
// boundaries. It contains no provider credentials or durable data access.
package composition

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/protocol"
	voiceruntime "invoice-backend/internal/voice/runtime"
	voicesession "invoice-backend/internal/voice/session"
	"invoice-backend/internal/voice/tools"
	"invoice-backend/internal/voice/turn"
	"invoice-backend/internal/voice/webrtc"

	"github.com/google/uuid"
)

var (
	ErrInvalidFactoryConfig      = errors.New("voice composition factory configuration is invalid")
	ErrInvalidBindingConfig      = errors.New("voice composition session configuration is invalid")
	ErrSessionOutputUnavailable  = errors.New("voice composition session output is unavailable")
	ErrDecoderUnavailable        = errors.New("voice composition Opus decoder is unavailable")
	ErrToolConfiguration         = errors.New("voice composition tool boundary is unavailable")
	ErrOrchestratorUnavailable   = errors.New("voice composition orchestrator is unavailable")
	ErrSTTBindingUnavailable     = errors.New("voice composition STT binding is unavailable")
	ErrLeaseHeartbeatUnavailable = errors.New("voice composition lease heartbeat is unavailable")
	ErrBindingClosed             = errors.New("voice composition binding is closed")
	ErrBindingClose              = errors.New("voice composition binding cleanup failed")
	ErrPeerOutputUnavailable     = errors.New("voice composition peer output is unavailable")
)

// TurnFailure is the complete, non-sensitive failure vocabulary exposed to a
// session output. Provider errors, credentials, HTTP bodies, and tool details
// never cross this boundary.
type TurnFailure string

const (
	TurnFailureNone                 TurnFailure = ""
	TurnFailureCanceled             TurnFailure = "canceled"
	TurnFailureAuthorizationRefresh TurnFailure = "authorization_refresh_required"
	TurnFailureUnavailable          TurnFailure = "unavailable"
)

// SessionOutput is created once per authenticated peer. Task-specific output
// implementations may send control events and, in the speech path, consume
// the generation-fenced text through HandleTextDelta.
type SessionOutput interface {
	turn.TextDeltaHandler
	voiceruntime.RepeatRequestHandler
	AttachPeer(webrtc.PeerTransport) error
	HandlePeerControl(context.Context, protocol.ControlMessage) error
	HandleTurnResult(context.Context, turn.TurnResult, TurnFailure)
}

type generationOutput interface {
	BargeInController() *turn.BargeIn
}

type DecoderFactory interface {
	NewDecoder() (audio.Decoder, error)
}

type DecoderFactoryFunc func() (audio.Decoder, error)

func (factory DecoderFactoryFunc) NewDecoder() (audio.Decoder, error) {
	if factory == nil {
		return nil, ErrDecoderUnavailable
	}
	return factory()
}

type OutputFactory interface {
	NewOutput(voicesession.Session) (SessionOutput, error)
}

type OutputFactoryFunc func(voicesession.Session) (SessionOutput, error)

func (factory OutputFactoryFunc) NewOutput(value voicesession.Session) (SessionOutput, error) {
	if factory == nil {
		return nil, ErrSessionOutputUnavailable
	}
	return factory(value)
}

type WindowFactory interface {
	NewWindow(voicesession.Session) (turn.ConversationWindow, error)
}

type WindowFactoryFunc func(voicesession.Session) (turn.ConversationWindow, error)

func (factory WindowFactoryFunc) NewWindow(value voicesession.Session) (turn.ConversationWindow, error) {
	if factory == nil {
		return turn.ConversationWindow{}, ErrOrchestratorUnavailable
	}
	return factory(value)
}

// ToolGovernanceFactory binds the authenticated session to the durable
// governed-execution seam. A missing factory never falls back to direct tools.
type ToolGovernanceFactory interface {
	NewGovernedExecutor(voicesession.Session, turn.ToolExecutor) turn.ToolExecutor
}

type ToolGovernanceFactoryFunc func(voicesession.Session, turn.ToolExecutor) turn.ToolExecutor

func (factory ToolGovernanceFactoryFunc) NewGovernedExecutor(session voicesession.Session, executor turn.ToolExecutor) turn.ToolExecutor {
	if factory == nil {
		return nil
	}
	return factory(session, executor)
}

type FactoryConfig struct {
	STT                   sarvam.STTOpener
	Chat                  sarvam.ChatStreamer
	LeaseRenewer          voicesession.LeaseRenewer
	Tools                 tools.Config
	Decoders              DecoderFactory
	Outputs               OutputFactory
	Windows               WindowFactory
	ToolGovernance        ToolGovernanceFactory
	RequireToolGovernance bool

	LeaseDuration          time.Duration
	LeaseHeartbeatInterval time.Duration
	LeaseWriteTimeout      time.Duration
	LeaseNow               func() time.Time

	MaxTokens          int
	FirstOutputTimeout time.Duration
	TotalTimeout       time.Duration
	STTFinalTimeout    time.Duration
	STTWarmTimeout     time.Duration
}

// BindingFactory is the concrete backend implementation of the signaling
// layer's per-peer factory. Its dependencies are process-lifetime clients;
// every Create call constructs isolated codec, tools, turn, and output state.
type BindingFactory struct {
	stt                   sarvam.STTOpener
	chat                  sarvam.ChatStreamer
	leaseRenewer          voicesession.LeaseRenewer
	tools                 tools.Config
	decoders              DecoderFactory
	outputs               OutputFactory
	windows               WindowFactory
	toolGovernance        ToolGovernanceFactory
	requireToolGovernance bool

	leaseDuration          time.Duration
	leaseHeartbeatInterval time.Duration
	leaseWriteTimeout      time.Duration
	leaseNow               func() time.Time

	maxTokens          int
	firstOutputTimeout time.Duration
	totalTimeout       time.Duration
	sttFinalTimeout    time.Duration
	sttWarmTimeout     time.Duration
}

func NewBindingFactory(config FactoryConfig) (*BindingFactory, error) {
	if nilInterface(config.STT) || nilInterface(config.Chat) ||
		nilInterface(config.LeaseRenewer) || nilInterface(config.Decoders) || nilInterface(config.Outputs) {
		return nil, ErrInvalidFactoryConfig
	}
	windows := config.Windows
	if windows == nil {
		windows = WindowFactoryFunc(defaultConversationWindow)
	} else if nilInterface(windows) {
		return nil, ErrInvalidFactoryConfig
	}
	leaseDuration := config.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = 2 * time.Minute
	}
	heartbeatInterval := config.LeaseHeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = leaseDuration / 4
		if heartbeatInterval > voicesession.DefaultLeaseHeartbeatWriteInterval {
			heartbeatInterval = voicesession.DefaultLeaseHeartbeatWriteInterval
		}
	}
	writeTimeout := config.LeaseWriteTimeout
	if writeTimeout == 0 {
		writeTimeout = 2 * time.Second
	}
	if leaseDuration < 30*time.Second || leaseDuration > 5*time.Minute || heartbeatInterval <= 0 ||
		heartbeatInterval > voicesession.DefaultLeaseHeartbeatWriteInterval || heartbeatInterval >= leaseDuration ||
		writeTimeout <= 0 || writeTimeout > 5*time.Second {
		return nil, ErrInvalidFactoryConfig
	}
	return &BindingFactory{
		stt: config.STT, chat: config.Chat, leaseRenewer: config.LeaseRenewer, tools: config.Tools,
		decoders: config.Decoders, outputs: config.Outputs, windows: windows,
		toolGovernance: config.ToolGovernance, requireToolGovernance: config.RequireToolGovernance,
		leaseDuration: leaseDuration, leaseHeartbeatInterval: heartbeatInterval,
		leaseWriteTimeout: writeTimeout, leaseNow: config.LeaseNow,
		maxTokens: config.MaxTokens, firstOutputTimeout: config.FirstOutputTimeout,
		totalTimeout: config.TotalTimeout, sttFinalTimeout: config.STTFinalTimeout,
		sttWarmTimeout: config.STTWarmTimeout,
	}, nil
}

// Create consumes Activity on every path. Authorization remains owned and
// scrubbed by signaling; the tools binding receives only its narrow capability.
func (factory *BindingFactory) Create(config webrtc.STTBindingConfig) (_ webrtc.STTBinding, resultErr error) {
	if nilInterface(config.Activity) {
		return nil, ErrInvalidBindingConfig
	}
	activityOwned := true
	defer func() {
		if activityOwned {
			_ = safeClose(config.Activity)
		}
		if recover() != nil {
			resultErr = ErrInvalidBindingConfig
		}
	}()
	if factory == nil || config.Context == nil || config.Context.Err() != nil ||
		nilInterface(config.Authorization) || !validSession(config.Session) {
		return nil, ErrInvalidBindingConfig
	}

	output, err := safeNewOutput(factory.outputs, cloneSession(config.Session))
	if err != nil || nilInterface(output) {
		return nil, ErrSessionOutputUnavailable
	}
	outputOwned := true
	defer func() {
		if outputOwned {
			_ = safeClose(output)
		}
	}()

	decoder, err := safeNewDecoder(factory.decoders)
	if err != nil || nilInterface(decoder) {
		return nil, ErrDecoderUnavailable
	}
	decoderOwned := true
	defer func() {
		if decoderOwned {
			_ = safeClose(decoder)
		}
	}()

	sessionBinding, err := tools.NewSessionBinding(
		config.Authorization, config.Session.BusinessID, config.Session.BranchID,
	)
	if err != nil {
		return nil, ErrToolConfiguration
	}
	registry, err := tools.NewRegistry(factory.tools, sessionBinding)
	if err != nil {
		_ = sessionBinding.Close()
		return nil, ErrToolConfiguration
	}
	registryOwned := true
	defer func() {
		if registryOwned {
			_ = registry.Close()
		}
	}()

	window, err := safeNewWindow(factory.windows, cloneSession(config.Session))
	if err != nil {
		return nil, ErrOrchestratorUnavailable
	}
	var executor turn.ToolExecutor = registry
	if config.Session.BranchID != "" {
		// Existing invoice/customer operations are business-wide. Until those
		// public APIs expose branch-aware reads, do not advertise unusable tools.
		executor = nil
	} else if !nilInterface(factory.toolGovernance) {
		executor = factory.toolGovernance.NewGovernedExecutor(cloneSession(config.Session), registry)
		if nilInterface(executor) {
			executor = nil
		}
	} else if factory.requireToolGovernance {
		executor = nil
	}
	orchestrator, err := turn.NewOrchestrator(turn.OrchestratorConfig{
		Context: config.Context, Chat: factory.chat, Tools: executor, Window: window,
		TextHandler: output, BargeIn: safeOutputBargeIn(output), MaxTokens: factory.maxTokens,
		FirstOutputTimeout: factory.firstOutputTimeout, TotalTimeout: factory.totalTimeout,
	})
	if err != nil {
		return nil, ErrOrchestratorUnavailable
	}
	orchestratorOwned := true
	defer func() {
		if orchestratorOwned {
			_ = orchestrator.Close()
		}
	}()

	finalHandler := &finalTranscriptHandler{orchestrator: orchestrator, output: output}
	runtimeBinding, err := voiceruntime.NewSTTSessionBinding(voiceruntime.STTSessionBindingConfig{
		STT: voiceruntime.STTSessionConfig{
			Context: config.Context, Opener: factory.stt,
			FallbackLanguage: config.Session.FallbackLanguage,
			FinalTimeout:     factory.sttFinalTimeout, WarmTimeout: factory.sttWarmTimeout,
			FinalTranscriptHandler: finalHandler, RepeatRequestHandler: output,
		},
		Decoder: decoder, Activity: config.Activity,
	})
	// NewSTTSessionBinding consumes both decoder and activity whether it
	// succeeds or reaches its nested STT-construction failure path.
	decoderOwned = false
	activityOwned = false
	if err != nil || runtimeBinding == nil {
		return nil, ErrSTTBindingUnavailable
	}
	runtimeBindingOwned := true
	defer func() {
		if runtimeBindingOwned {
			_ = safeClose(runtimeBinding)
		}
	}()
	heartbeat, err := voicesession.NewLeaseHeartbeat(voicesession.LeaseHeartbeatConfig{
		Context: config.Context, Renewer: factory.leaseRenewer, Session: cloneSession(config.Session),
		LeaseDuration: factory.leaseDuration, MinWriteInterval: factory.leaseHeartbeatInterval,
		WriteTimeout: factory.leaseWriteTimeout, Now: factory.leaseNow,
	})
	if err != nil || heartbeat == nil {
		return nil, ErrLeaseHeartbeatUnavailable
	}
	heartbeatOwned := true
	defer func() {
		if heartbeatOwned {
			_ = heartbeat.Close()
		}
	}()

	binding := &Binding{
		stt: runtimeBinding, orchestrator: orchestrator, registry: registry, output: output, heartbeat: heartbeat,
	}
	orchestratorOwned = false
	registryOwned = false
	outputOwned = false
	runtimeBindingOwned = false
	heartbeatOwned = false
	go binding.watchHeartbeat()
	return binding, nil
}

// Binding owns all session-scoped components except the Authorization vault,
// which signaling scrubs immediately after Binding.Close returns.
type Binding struct {
	stt          *voiceruntime.STTSessionBinding
	orchestrator *turn.Orchestrator
	registry     *tools.Registry
	output       SessionOutput
	heartbeat    *voicesession.LeaseHeartbeat
	attachMu     sync.Mutex
	attached     bool
	closed       atomic.Bool
	closeOnce    sync.Once
	closeErr     error
}

func (binding *Binding) AttachPeer(transport webrtc.PeerTransport) error {
	if binding == nil || binding.closed.Load() || nilInterface(transport) {
		return ErrPeerOutputUnavailable
	}
	binding.attachMu.Lock()
	defer binding.attachMu.Unlock()
	if binding.closed.Load() || binding.attached {
		return ErrPeerOutputUnavailable
	}
	if err := safeAttachOutput(binding.output, transport); err != nil {
		return ErrPeerOutputUnavailable
	}
	binding.attached = true
	return nil
}

func (binding *Binding) HandlePeerControl(ctx context.Context, message protocol.ControlMessage) error {
	if binding == nil || binding.closed.Load() || ctx == nil || ctx.Err() != nil {
		return ErrBindingClosed
	}
	if message.Type == protocol.EventHeartbeat {
		if binding.heartbeat == nil || binding.heartbeat.Heartbeat() != nil {
			_ = binding.Close()
			return ErrBindingClosed
		}
	}
	if err := safeHandlePeerControl(binding.output, ctx, message); err != nil {
		return err
	}
	if message.Type == protocol.EventSessionClose {
		if err := binding.Close(); err != nil {
			return err
		}
		// PersistentActivity cancellation stops workers, while this explicit
		// sentinel tells the peer worker to close its PeerConnection and retire
		// signaling state immediately even if no more media arrives.
		return webrtc.ErrSignalingClose
	}
	return nil
}

func (binding *Binding) HandleOpus(payload []byte) error {
	if binding == nil || binding.closed.Load() || binding.stt == nil {
		return ErrBindingClosed
	}
	return binding.stt.HandleOpus(payload)
}

func (binding *Binding) HandleControl(ctx context.Context, message protocol.ControlMessage) error {
	if binding == nil || binding.closed.Load() || binding.stt == nil {
		return ErrBindingClosed
	}
	if message.Type == protocol.EventSpeechStarted || message.Type == protocol.EventSpeechEnded {
		if err := safeHandlePeerControl(binding.output, ctx, message); err != nil {
			return err
		}
	}
	return binding.stt.HandleControl(ctx, message)
}

func (binding *Binding) Close() error {
	if binding == nil {
		return nil
	}
	binding.closeOnce.Do(func() {
		binding.closed.Store(true)
		var failures int
		if safeClose(binding.heartbeat) != nil {
			failures++
		}
		if safeClose(binding.orchestrator) != nil {
			failures++
		}
		binding.attachMu.Lock()
		if safeClose(binding.output) != nil {
			failures++
		}
		binding.attachMu.Unlock()
		for _, closer := range []any{binding.registry, binding.stt} {
			if safeClose(closer) != nil {
				failures++
			}
		}
		if failures != 0 {
			binding.closeErr = ErrBindingClose
		}
	})
	return binding.closeErr
}

func (binding *Binding) watchHeartbeat() {
	if binding == nil || binding.heartbeat == nil {
		return
	}
	<-binding.heartbeat.Done()
	if binding.heartbeat.Err() != nil {
		_ = binding.Close()
	}
}

type finalTranscriptHandler struct {
	orchestrator *turn.Orchestrator
	output       SessionOutput
}

func (handler *finalTranscriptHandler) HandleFinalTranscript(ctx context.Context, final voiceruntime.STTFinalTranscript) {
	if handler == nil || handler.orchestrator == nil || nilInterface(handler.output) {
		return
	}
	result, err := handler.orchestrator.HandleFinal(ctx, turn.FinalTranscript{
		Text: final.Text, ProviderLanguage: final.ProviderLanguage,
		DetectedLanguage: final.DetectedLanguage, ResponseLanguage: final.ResponseLanguage,
		SpeechEndedAt: final.SpeechEndedAt, STTFinalAt: final.STTFinalAt,
		LanguageProbability: final.LanguageProbability,
	})
	safeHandleTurnResult(handler.output, ctx, result, classifyTurnFailure(err))
}

func classifyTurnFailure(err error) TurnFailure {
	if err == nil {
		return TurnFailureNone
	}
	if errors.Is(err, turn.ErrToolAuthorizationRefresh) {
		return TurnFailureAuthorizationRefresh
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, turn.ErrOrchestratorClosed) || errors.Is(err, turn.ErrTurnInterrupted) {
		return TurnFailureCanceled
	}
	return TurnFailureUnavailable
}

func defaultConversationWindow(value voicesession.Session) (turn.ConversationWindow, error) {
	scope := "all branches"
	if value.BranchID != "" {
		scope = "one authorized branch; business-wide tools are unavailable"
	}
	return turn.ConversationWindow{
		SystemPolicy:    "You are Billeif's concise voice assistant. Help the caller with their current business workflow and never claim an action succeeded without trusted tool data.",
		BusinessContext: "The caller and business were authenticated before this voice peer was admitted. Current data scope: " + scope + ".",
	}, nil
}

func validSession(value voicesession.Session) bool {
	return safeIdentifier(value.ID, 128) && safeIdentifier(value.RuntimeSessionID, 256) &&
		safeIdentifier(value.UserID, 256) && canonicalUUID(value.BusinessID) &&
		(value.BranchID == "" || canonicalUUID(value.BranchID)) &&
		value.Status == voicesession.StatusActive && value.RuntimeState == voicesession.RuntimeStateRunning && !value.CapacityReleased &&
		!value.UpdatedAt.IsZero() && !value.LeaseExpiresAt.IsZero() && !value.ExpiresAt.IsZero() &&
		!value.LeaseExpiresAt.After(value.ExpiresAt)
}

func safeIdentifier(value string, maximum int) bool {
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

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func cloneSession(value voicesession.Session) voicesession.Session {
	cloned := value
	if value.ClosedAt != nil {
		closedAt := *value.ClosedAt
		cloned.ClosedAt = &closedAt
	}
	return cloned
}

func safeNewDecoder(factory DecoderFactory) (decoder audio.Decoder, err error) {
	defer func() {
		if recover() != nil {
			decoder = nil
			err = ErrDecoderUnavailable
		}
	}()
	return factory.NewDecoder()
}

func safeNewOutput(factory OutputFactory, value voicesession.Session) (output SessionOutput, err error) {
	defer func() {
		if recover() != nil {
			output = nil
			err = ErrSessionOutputUnavailable
		}
	}()
	return factory.NewOutput(value)
}

func safeNewWindow(factory WindowFactory, value voicesession.Session) (window turn.ConversationWindow, err error) {
	defer func() {
		if recover() != nil {
			window = turn.ConversationWindow{}
			err = ErrOrchestratorUnavailable
		}
	}()
	return factory.NewWindow(value)
}

func safeHandleTurnResult(output SessionOutput, ctx context.Context, result turn.TurnResult, failure TurnFailure) {
	defer func() { _ = recover() }()
	output.HandleTurnResult(ctx, result, failure)
}

func safeAttachOutput(output SessionOutput, transport webrtc.PeerTransport) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrPeerOutputUnavailable
		}
	}()
	return output.AttachPeer(transport)
}

func safeHandlePeerControl(output SessionOutput, ctx context.Context, message protocol.ControlMessage) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrPeerOutputUnavailable
		}
	}()
	if nilInterface(output) {
		return ErrPeerOutputUnavailable
	}
	return output.HandlePeerControl(ctx, message)
}

func safeOutputBargeIn(output SessionOutput) (controller *turn.BargeIn) {
	source, ok := output.(generationOutput)
	if !ok || nilInterface(source) {
		return nil
	}
	defer func() {
		if recover() != nil {
			controller = nil
		}
	}()
	return source.BargeInController()
}

func safeClose(value any) (err error) {
	if nilInterface(value) {
		return nil
	}
	closer, ok := value.(io.Closer)
	if !ok || nilInterface(closer) {
		return nil
	}
	defer func() {
		if recover() != nil {
			err = ErrBindingClose
		}
	}()
	return closer.Close()
}

func nilInterface(value any) bool {
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

var (
	_ webrtc.STTBindingFactory            = (*BindingFactory)(nil)
	_ webrtc.STTBinding                   = (*Binding)(nil)
	_ webrtc.PeerAttachment               = (*Binding)(nil)
	_ webrtc.PeerControlHandler           = (*Binding)(nil)
	_ voiceruntime.FinalTranscriptHandler = (*finalTranscriptHandler)(nil)
)
