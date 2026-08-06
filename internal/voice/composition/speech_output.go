package composition

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sync"
	"time"
	"unicode/utf8"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/protocol"
	voicesession "invoice-backend/internal/voice/session"
	voicetelemetry "invoice-backend/internal/voice/telemetry"
	"invoice-backend/internal/voice/turn"
	"invoice-backend/internal/voice/webrtc"
)

const (
	defaultPlaybackAcknowledgementTimeout = 5 * time.Second
	maximumPlaybackAcknowledgementTimeout = 10 * time.Second
	finalTurnSinkCloseTimeout             = 2 * time.Second
)

var ErrSpeechOutputUnavailable = errors.New("voice composition speech output is unavailable")

// SessionTTSFactory creates a credential-owning provider boundary for one
// authenticated runtime session. It must not open a provider WebSocket; each
// active generation does that lazily through OpenTTS.
type SessionTTSFactory interface {
	NewSessionTTS(voicesession.Session) (sarvam.TTSOpener, error)
}

type SessionTTSFactoryFunc func(voicesession.Session) (sarvam.TTSOpener, error)

func (factory SessionTTSFactoryFunc) NewSessionTTS(value voicesession.Session) (sarvam.TTSOpener, error) {
	if factory == nil {
		return nil, ErrSpeechOutputUnavailable
	}
	return factory(value)
}

type SpeechEncoderFactory interface {
	NewEncoder() (audio.Encoder, error)
}

type SpeechEncoderFactoryFunc func() (audio.Encoder, error)

func (factory SpeechEncoderFactoryFunc) NewEncoder() (audio.Encoder, error) {
	if factory == nil {
		return nil, ErrSpeechOutputUnavailable
	}
	return factory()
}

// LibopusSpeechEncoderFactory is the production 16 kHz mono, 20 ms encoder
// factory. Builds without libopus support fail closed from NewEncoder.
type LibopusSpeechEncoderFactory struct{}

func (LibopusSpeechEncoderFactory) NewEncoder() (audio.Encoder, error) {
	return audio.NewLibopusEncoder()
}

type SpeechPacerFactory interface {
	NewPacer() (audio.FramePacer, error)
}

type SpeechPacerFactoryFunc func() (audio.FramePacer, error)

func (factory SpeechPacerFactoryFunc) NewPacer() (audio.FramePacer, error) {
	if factory == nil {
		return nil, ErrSpeechOutputUnavailable
	}
	return factory()
}

// FinalTurnSinkLease keeps SpeechOutput's data dependency narrow while giving
// the per-session factory an explicit cleanup hook for its bounded worker.
type FinalTurnSinkLease struct {
	Sink  voicesession.FinalTurnSink
	Close func(context.Context) error
}

type FinalTurnSinkFactory interface {
	NewFinalTurnSink(voicesession.Session) (FinalTurnSinkLease, error)
}

type FinalTurnSinkFactoryFunc func(voicesession.Session) (FinalTurnSinkLease, error)

func (factory FinalTurnSinkFactoryFunc) NewFinalTurnSink(value voicesession.Session) (FinalTurnSinkLease, error) {
	if factory == nil {
		return FinalTurnSinkLease{}, ErrSpeechOutputUnavailable
	}
	return factory(value)
}

// TurnTelemetryFactory creates one privacy-bounded aggregate recorder per
// client turn. Telemetry is optional for local/test composition and is wired
// by the production runtime without exposing session or user identifiers.
type TurnTelemetryFactory interface {
	NewTurnTelemetry() (voicetelemetry.RuntimeTurnTelemetry, error)
}

type TurnTelemetryFactoryFunc func() (voicetelemetry.RuntimeTurnTelemetry, error)

func (factory TurnTelemetryFactoryFunc) NewTurnTelemetry() (voicetelemetry.RuntimeTurnTelemetry, error) {
	if factory == nil {
		return nil, ErrSpeechOutputUnavailable
	}
	return factory()
}

// SpeechOutputFactory is process-scoped, but every dependency it returns is
// session-scoped and every TTS stream/encoder/pacer is generation-scoped.
type SpeechOutputFactory struct {
	TTS             SessionTTSFactory
	Encoders        SpeechEncoderFactory
	Pacers          SpeechPacerFactory
	FinalTurns      FinalTurnSinkFactory
	Telemetry       TurnTelemetryFactory
	PlaybackTimeout time.Duration
	Now             func() time.Time
}

func (factory SpeechOutputFactory) NewOutput(value voicesession.Session) (SessionOutput, error) {
	if nilInterface(factory.TTS) || nilInterface(factory.Encoders) ||
		(factory.Telemetry != nil && nilInterface(factory.Telemetry)) {
		return nil, ErrSessionOutputUnavailable
	}
	opener, err := safeNewSessionTTS(factory.TTS, cloneSession(value))
	if err != nil || nilInterface(opener) {
		return nil, ErrSessionOutputUnavailable
	}
	lease := FinalTurnSinkLease{}
	if value.ConsentTranscriptStorage {
		if nilInterface(factory.FinalTurns) {
			return nil, ErrSessionOutputUnavailable
		}
		lease, err = safeNewFinalTurnSink(factory.FinalTurns, cloneSession(value))
		if err != nil || nilInterface(lease.Sink) {
			return nil, ErrSessionOutputUnavailable
		}
	}
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session: value, TTS: opener, Encoders: factory.Encoders, Pacers: factory.Pacers,
		FinalTurns: lease.Sink, CloseFinalTurns: lease.Close,
		Telemetry:       factory.Telemetry,
		PlaybackTimeout: factory.PlaybackTimeout, Now: factory.Now,
	})
	if err != nil {
		_ = closeFinalTurnLease(lease.Close)
		return nil, ErrSessionOutputUnavailable
	}
	return output, nil
}

type SpeechOutputConfig struct {
	Session    voicesession.Session
	TTS        sarvam.TTSOpener
	Encoders   SpeechEncoderFactory
	Pacers     SpeechPacerFactory
	FinalTurns voicesession.FinalTurnSink
	Telemetry  TurnTelemetryFactory

	CloseFinalTurns func(context.Context) error
	PlaybackTimeout time.Duration
	Now             func() time.Time
}

// SpeechOutput is one authenticated peer's generation-fenced TTS, audio, and
// post-playback persistence boundary. It never retains or persists PCM/Opus
// after a generation is retired.
type SpeechOutput struct {
	mu sync.Mutex

	sessionID        string
	expiresAt        time.Time
	consent          bool
	turnSequence     int64
	controlSequence  int
	lastWireTurnID   int64
	pendingTurnID    int64
	pendingEnded     bool
	starting         bool
	setupFailureGen  uint64
	setupFailureTurn int64

	tts        sarvam.TTSOpener
	encoders   SpeechEncoderFactory
	pacers     SpeechPacerFactory
	finalTurns voicesession.FinalTurnSink
	telemetry  TurnTelemetryFactory
	closeFinal func(context.Context) error
	now        func() time.Time
	timeout    time.Duration
	bargeIn    *turn.BargeIn

	transport        webrtc.PeerTransport
	active           *speechGeneration
	pendingTelemetry *turnTelemetryState
	attached         bool
	failed           bool
	// durabilityDegraded is independent from realtime speech health. Once a
	// bounded final-turn enqueue fails, this session stops attempting durable
	// transcript writes but continues serving subsequent voice turns.
	durabilityDegraded bool
	closed             bool

	closeOnce sync.Once
	closeErr  error
}

type speechGeneration struct {
	id       uint64
	turnID   int64
	sequence int64
	ctx      context.Context
	final    turn.FinalTranscript

	stream   sarvam.TTSStream
	playback *audio.GenerationPlayback
	encoder  audio.Encoder
	chunker  *turn.SpeechChunker

	writeMu sync.Mutex
	mu      sync.Mutex

	readDone      chan struct{}
	ackWake       chan struct{}
	readErr       error
	providerFinal bool
	cancelled     bool
	cancelSent    bool
	answerSent    bool
	textFallback  bool
	lifecycleDone bool
	result        turn.TurnResult

	llmFirstToken     time.Time
	ttsFirstAudio     time.Time
	clientFirstAudio  time.Time
	clientCompleted   time.Time
	ttsRequestStarted time.Time
	playbackStarted   bool
	playbackComplete  bool
	durabilityFailed  bool
	ttsCharacters     int64
	telemetry         *turnTelemetryState

	closeOnce sync.Once
}

// turnTelemetryState serializes callbacks from the STT, chat, TTS, playback,
// and control goroutines. It also guarantees strict timestamp monotonicity
// even when the injected/system clock has coarse resolution. Any telemetry
// error disables only that turn's telemetry and never the realtime path.
type turnTelemetryState struct {
	mu       sync.Mutex
	recorder voicetelemetry.RuntimeTurnTelemetry
	last     time.Time
	terminal bool
	invalid  bool
}

func (state *turnTelemetryState) record(at time.Time, callback func(voicetelemetry.RuntimeTurnTelemetry, time.Time) error) {
	if state == nil || callback == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.terminal || state.invalid || nilInterface(state.recorder) {
		return
	}
	at = state.nextTime(at)
	if safeTelemetryRecord(state.recorder, at, callback) != nil {
		state.invalid = true
		return
	}
	state.last = at
}

func (state *turnTelemetryState) interrupt(startedAt, stoppedAt time.Time) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.terminal || state.invalid || nilInterface(state.recorder) {
		return
	}
	startedAt = state.nextTime(startedAt)
	if !stoppedAt.After(startedAt) {
		stoppedAt = startedAt.Add(time.Nanosecond)
	}
	if safeTelemetryInterrupt(state.recorder, startedAt, stoppedAt) != nil {
		state.invalid = true
		return
	}
	state.last = stoppedAt
}

func (state *turnTelemetryState) durabilityFailure() {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.terminal || state.invalid || nilInterface(state.recorder) {
		return
	}
	safeTelemetryDurabilityFailure(state.recorder)
}

func (state *turnTelemetryState) finish(
	at time.Time,
	measurements voicetelemetry.TurnMeasurements,
	callback func(voicetelemetry.RuntimeTurnTelemetry, time.Time, voicetelemetry.TurnMeasurements) error,
) {
	if state == nil || callback == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.terminal || state.invalid || nilInterface(state.recorder) {
		return
	}
	at = state.nextTime(at)
	state.terminal = true
	_ = safeTelemetryFinish(state.recorder, at, measurements, callback)
}

func (state *turnTelemetryState) nextTime(at time.Time) time.Time {
	at = at.UTC()
	if at.IsZero() {
		if state.last.IsZero() {
			return time.Unix(0, 1).UTC()
		}
		return state.last.Add(time.Nanosecond)
	}
	if !state.last.IsZero() && !at.After(state.last) {
		return state.last.Add(time.Nanosecond)
	}
	return at
}

func NewSpeechOutput(config SpeechOutputConfig) (*SpeechOutput, error) {
	if !validSpeechSession(config.Session) || nilInterface(config.TTS) || nilInterface(config.Encoders) ||
		(config.Pacers != nil && nilInterface(config.Pacers)) ||
		(config.Session.ConsentTranscriptStorage && nilInterface(config.FinalTurns)) ||
		(config.Telemetry != nil && nilInterface(config.Telemetry)) {
		return nil, ErrSpeechOutputUnavailable
	}
	timeout := config.PlaybackTimeout
	if timeout == 0 {
		timeout = defaultPlaybackAcknowledgementTimeout
	}
	if timeout < time.Millisecond || timeout > maximumPlaybackAcknowledgementTimeout {
		return nil, ErrSpeechOutputUnavailable
	}
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SpeechOutput{
		sessionID: config.Session.ID, expiresAt: config.Session.ExpiresAt,
		consent: config.Session.ConsentTranscriptStorage, turnSequence: config.Session.TurnSequence,
		tts: config.TTS, encoders: config.Encoders, pacers: config.Pacers,
		finalTurns: config.FinalTurns, telemetry: config.Telemetry, closeFinal: config.CloseFinalTurns,
		now: now, timeout: timeout,
		bargeIn: turn.NewBargeInAfter(uint64(config.Session.GenerationID)),
	}, nil
}

func (output *SpeechOutput) BargeInController() *turn.BargeIn {
	if output == nil {
		return nil
	}
	return output.bargeIn
}

func (output *SpeechOutput) AttachPeer(transport webrtc.PeerTransport) error {
	if output == nil || nilInterface(transport) {
		return ErrPeerOutputUnavailable
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.closed || output.failed || output.attached {
		return ErrPeerOutputUnavailable
	}
	output.transport = transport
	output.attached = true
	return nil
}

func (output *SpeechOutput) BeginTextGeneration(
	ctx context.Context,
	generation uint64,
	final turn.FinalTranscript,
) error {
	if output == nil || !validOutputContext(ctx) || generation == 0 ||
		!validControlOutputText(final.Text) || !validSpeechLanguage(final.ResponseLanguage) {
		return ErrSpeechOutputUnavailable
	}
	var transport webrtc.PeerTransport
	var turnID int64
	var persistenceSequence int64
	var telemetryState *turnTelemetryState
	if err := output.bargeIn.WithGeneration(generation, func() error {
		output.mu.Lock()
		defer output.mu.Unlock()
		if output.closed || output.failed || !output.attached || nilInterface(output.transport) || output.active != nil ||
			output.starting || output.turnSequence == math.MaxInt64 || output.lastWireTurnID == math.MaxInt64 {
			return ErrSpeechOutputUnavailable
		}
		transport = output.transport
		persistenceSequence = output.turnSequence + 1
		turnID = output.lastWireTurnID + 1
		if turnID < persistenceSequence {
			turnID = persistenceSequence
		}
		if output.pendingTurnID != 0 {
			if !output.pendingEnded || output.pendingTurnID <= output.lastWireTurnID {
				return ErrSpeechOutputUnavailable
			}
			turnID = output.pendingTurnID
			telemetryState = output.pendingTelemetry
		}
		output.starting = true
		return nil
	}); err != nil {
		return ErrSpeechOutputUnavailable
	}
	installed := false
	defer func() {
		if !installed {
			output.mu.Lock()
			output.starting = false
			output.setupFailureGen = generation
			output.setupFailureTurn = turnID
			output.mu.Unlock()
		}
	}()

	stream, err := safeOpenSpeechTTS(output.tts, ctx, final.ResponseLanguage)
	if err != nil || nilInterface(stream) {
		return ErrSpeechOutputUnavailable
	}
	if err := output.bargeIn.BindTTS(generation, stream); err != nil {
		return ErrSpeechOutputUnavailable
	}
	encoder, err := safeNewSpeechEncoder(output.encoders)
	if err != nil || nilInterface(encoder) {
		_ = output.bargeIn.Abort(generation, ErrSpeechOutputUnavailable)
		return ErrSpeechOutputUnavailable
	}
	pacer, err := safeNewSpeechPacer(output.pacers)
	if err != nil {
		_ = safeCloseSpeechResource(encoder)
		_ = output.bargeIn.Abort(generation, ErrSpeechOutputUnavailable)
		return ErrSpeechOutputUnavailable
	}
	playback, err := audio.NewGenerationPlayback(audio.GenerationPlaybackConfig{
		Context: ctx, Generation: generation, Encoder: encoder, Sender: transport, Pacer: pacer,
		Guard: output.bargeIn.WithGeneration,
	})
	if err != nil {
		_ = safeCloseSpeechResource(encoder)
		_ = output.bargeIn.Abort(generation, ErrSpeechOutputUnavailable)
		return ErrSpeechOutputUnavailable
	}
	if err := output.bargeIn.BindAudio(generation, playback.Cancel); err != nil {
		_ = playback.Close()
		_ = safeCloseSpeechResource(encoder)
		return ErrSpeechOutputUnavailable
	}

	state := &speechGeneration{
		id: generation, turnID: turnID, sequence: persistenceSequence,
		ctx: ctx, final: cloneSpeechFinalTranscript(final),
		stream: stream, playback: playback, encoder: encoder, chunker: turn.NewSpeechChunker(),
		readDone: make(chan struct{}), ackWake: make(chan struct{}, 1),
		telemetry: telemetryState,
	}
	if err := output.bargeIn.WithGeneration(generation, func() error {
		output.mu.Lock()
		defer output.mu.Unlock()
		if output.closed || output.failed || output.active != nil {
			return ErrSpeechOutputUnavailable
		}
		if output.pendingTurnID != 0 && output.pendingTurnID != turnID {
			return ErrSpeechOutputUnavailable
		}
		output.active = state
		output.setupFailureGen = 0
		output.setupFailureTurn = 0
		output.lastWireTurnID = turnID
		if output.pendingTurnID == turnID {
			output.pendingTurnID = 0
			output.pendingEnded = false
			output.pendingTelemetry = nil
		}
		output.starting = false
		return nil
	}); err != nil {
		_ = output.bargeIn.Abort(generation, ErrSpeechOutputUnavailable)
		state.closeResources()
		return ErrSpeechOutputUnavailable
	}
	installed = true
	state.telemetry.record(final.STTFinalAt, func(recorder voicetelemetry.RuntimeTurnTelemetry, at time.Time) error {
		return recorder.STTFinalReceived(at)
	})
	if err := output.sendAuthoritativeTurnControls(ctx, state); err != nil {
		output.failGeneration(state, ErrSpeechOutputUnavailable)
		return ErrSpeechOutputUnavailable
	}
	if state.telemetry != nil {
		state.telemetry.record(output.now(), func(recorder voicetelemetry.RuntimeTurnTelemetry, at time.Time) error {
			return recorder.LLMRequestStarted(at)
		})
	}
	go output.readTTS(state)
	return nil
}

func (output *SpeechOutput) HandleTextDelta(ctx context.Context, generation uint64, delta string) error {
	state, err := output.activeGeneration(generation)
	if err != nil || !validOutputContext(ctx) || !validControlOutputDelta(delta) {
		return ErrSpeechOutputUnavailable
	}
	if firstTokenAt, first := state.markLLMFirstToken(output.now); first {
		state.telemetry.record(firstTokenAt, func(recorder voicetelemetry.RuntimeTurnTelemetry, at time.Time) error {
			return recorder.LLMFirstToken(at)
		})
	}
	state.writeMu.Lock()
	if state.speechUnavailable() {
		state.writeMu.Unlock()
		return nil
	}
	chunks, err := state.chunker.Write(delta)
	if err == nil {
		for _, chunk := range chunks {
			if err = state.writeChunk(ctx, chunk, output.now); err != nil {
				break
			}
		}
	}
	state.writeMu.Unlock()
	if err != nil {
		if ctx.Err() != nil || state.ctx.Err() != nil {
			return output.generationContextError(state, err)
		}
		// Speech synthesis is best-effort once the LLM turn is admitted. Preserve
		// generation fencing, stop partial audio, and keep collecting the final
		// visible answer so the client can fall back to text.
		output.failSpeech(state, ErrSpeechOutputUnavailable)
	}
	return nil
}

func (output *SpeechOutput) CompleteTextGeneration(
	ctx context.Context,
	generation uint64,
	result turn.TurnResult,
) error {
	state, err := output.activeGeneration(generation)
	if err != nil || !validOutputContext(ctx) || result.GenerationID != generation || !equalSpeechFinalTranscript(result.Transcript, state.final) ||
		!validControlOutputText(result.Text) {
		return ErrSpeechOutputUnavailable
	}
	if err := output.sendFinalAnswer(ctx, state, result.Text); err != nil {
		output.failGeneration(state, ErrSpeechOutputUnavailable)
		return ErrSpeechOutputUnavailable
	}
	state.writeMu.Lock()
	if !state.speechUnavailable() {
		chunks, flushErr := state.chunker.Flush()
		err = flushErr
		if err == nil {
			for _, chunk := range chunks {
				if err = state.writeChunk(ctx, chunk, output.now); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = safeFlushSpeechTTS(state.stream, ctx)
		}
	}
	state.writeMu.Unlock()
	if err != nil {
		if ctx.Err() != nil || state.ctx.Err() != nil {
			return output.generationContextError(state, err)
		}
		output.failSpeech(state, ErrSpeechOutputUnavailable)
	}
	if state.speechUnavailable() {
		state.completeTextFallback(result, output.now())
		return nil
	}

	select {
	case <-state.readDone:
	case <-ctx.Done():
		return output.generationContextError(state, ctx.Err())
	}
	if err := state.readerError(); err != nil || !state.hasProviderFinal() {
		if ctx.Err() != nil || state.ctx.Err() != nil {
			return output.generationContextError(state, err)
		}
		output.failSpeech(state, ErrSpeechOutputUnavailable)
		state.completeTextFallback(result, output.now())
		return nil
	}
	if err := state.playback.Finish(ctx); err != nil {
		if ctx.Err() != nil || state.ctx.Err() != nil {
			return output.generationContextError(state, err)
		}
		output.failSpeech(state, ErrSpeechOutputUnavailable)
		state.completeTextFallback(result, output.now())
		return nil
	}

	waitContext, cancelWait := context.WithTimeout(ctx, output.timeout)
	defer cancelWait()
	for !state.playbackAcknowledged() {
		select {
		case <-state.ackWake:
		case <-waitContext.Done():
			output.failGeneration(state, ErrSpeechOutputUnavailable)
			return output.generationContextError(state, waitContext.Err())
		}
	}
	state.mu.Lock()
	state.lifecycleDone = true
	state.result = cloneTurnResult(result)
	state.mu.Unlock()
	return nil
}

func (output *SpeechOutput) HandlePeerControl(ctx context.Context, message protocol.ControlMessage) error {
	if output == nil || !validOutputContext(ctx) {
		return ErrBindingClosed
	}
	switch message.Type {
	case protocol.EventInterrupt:
		generation, ok := messageGeneration(message)
		turnID, validTurn := messageTurn(message)
		if !ok || !validTurn {
			return ErrPeerOutputUnavailable
		}
		return output.interrupt(ctx, generation, turnID)
	case protocol.EventSpeechStarted:
		turnID, ok := messageTurn(message)
		if !ok {
			return ErrPeerOutputUnavailable
		}
		return output.speechStarted(ctx, turnID)
	case protocol.EventSpeechEnded:
		turnID, ok := messageTurn(message)
		if !ok {
			return ErrPeerOutputUnavailable
		}
		return output.speechEnded(turnID)
	case protocol.EventPlaybackStarted, protocol.EventPlaybackCompleted:
		generation, ok := messageGeneration(message)
		turnID, validTurn := messageTurn(message)
		if !ok || !validTurn {
			return ErrPeerOutputUnavailable
		}
		return output.playbackAcknowledgement(message.Type, generation, turnID)
	default:
		return nil
	}
}

func (output *SpeechOutput) HandleTurnResult(ctx context.Context, result turn.TurnResult, failure TurnFailure) {
	if output == nil || !validOutputContext(ctx) {
		return
	}
	output.mu.Lock()
	state := output.active
	if state == nil {
		var pendingTelemetry *turnTelemetryState
		failedTurnID := int64(0)
		if output.setupFailureGen == result.GenerationID {
			failedTurnID = output.setupFailureTurn
			output.setupFailureGen = 0
			output.setupFailureTurn = 0
		}
		if failedTurnID != 0 && output.pendingTurnID == failedTurnID {
			pendingTelemetry = output.pendingTelemetry
			if failedTurnID > output.lastWireTurnID {
				output.lastWireTurnID = failedTurnID
			}
			output.pendingTurnID = 0
			output.pendingEnded = false
			output.pendingTelemetry = nil
		}
		output.mu.Unlock()
		finishPendingTurnTelemetry(pendingTelemetry, output.now(), result, failure)
		output.sendFailureControl(ctx, result.GenerationID, failure)
		return
	}
	if state.id != result.GenerationID {
		output.mu.Unlock()
		output.sendFailureControl(ctx, result.GenerationID, failure)
		return
	}
	output.active = nil
	output.mu.Unlock()

	telemetryFailure := failure
	telemetrySuccess := false
	switch failure {
	case TurnFailureNone:
		if !state.successfulResult(result) {
			telemetryFailure = TurnFailureUnavailable
			output.markFailed()
			_ = output.send(ctx, protocol.ControlMessage{
				Type: protocol.EventError, ErrorCode: "voice_unavailable", Message: "Voice response is temporarily unavailable.",
			})
			break
		}
		telemetrySuccess = true
		persistenceAccepted := false
		if sink, available := output.durabilitySink(); output.consent && available && state.durablePlaybackCompleted() {
			finalTurn := output.finalTurn(state, result)
			if err := safeEnqueueFinalTurn(sink, finalTurn); err != nil {
				state.mu.Lock()
				state.durabilityFailed = true
				state.mu.Unlock()
				state.telemetry.durabilityFailure()
				output.disableDurability()
			} else {
				persistenceAccepted = true
			}
		}
		if persistenceAccepted {
			output.mu.Lock()
			output.turnSequence = state.sequence
			output.mu.Unlock()
		}
		generationID, ok := controlGeneration(result.GenerationID)
		if ok {
			_ = output.send(ctx, protocol.ControlMessage{
				Type: protocol.EventTurnCompleted, GenerationID: &generationID, TurnID: &state.turnID,
			})
		}
	case TurnFailureCanceled:
		output.sendCancellationOnce(ctx, state)
	case TurnFailureAuthorizationRefresh:
		_ = output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventError, ErrorCode: "authorization_refresh_required", Message: "Authorization refresh is required.",
		})
	default:
		_ = output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventError, ErrorCode: "voice_unavailable", Message: "Voice response is temporarily unavailable.",
		})
	}
	measurements := state.telemetryMeasurements(result, telemetryFailure)
	terminalAt := time.Time{}
	if state.telemetry != nil {
		terminalAt = output.now()
	}
	if telemetrySuccess {
		state.telemetry.finish(terminalAt, measurements, func(
			recorder voicetelemetry.RuntimeTurnTelemetry,
			at time.Time,
			value voicetelemetry.TurnMeasurements,
		) error {
			return recorder.Complete(at, value)
		})
	} else if telemetryFailure == TurnFailureCanceled {
		state.telemetry.finish(terminalAt, measurements, func(
			recorder voicetelemetry.RuntimeTurnTelemetry,
			at time.Time,
			value voicetelemetry.TurnMeasurements,
		) error {
			return recorder.Cancel(at, value)
		})
	} else {
		state.telemetry.finish(terminalAt, measurements, func(
			recorder voicetelemetry.RuntimeTurnTelemetry,
			at time.Time,
			value voicetelemetry.TurnMeasurements,
		) error {
			return recorder.Fail(at, value)
		})
	}
	state.closeResources()
}

func (output *SpeechOutput) RequestRepeat(ctx context.Context) {
	if !validOutputContext(ctx) || output == nil {
		return
	}
	output.mu.Lock()
	telemetryState := output.pendingTelemetry
	if output.pendingTurnID > output.lastWireTurnID {
		output.lastWireTurnID = output.pendingTurnID
	}
	output.pendingTurnID = 0
	output.pendingEnded = false
	output.pendingTelemetry = nil
	output.mu.Unlock()
	telemetryState.finish(output.now(), voicetelemetry.TurnMeasurements{
		ErrorClass: "repeat_required",
	}, func(
		recorder voicetelemetry.RuntimeTurnTelemetry,
		at time.Time,
		value voicetelemetry.TurnMeasurements,
	) error {
		return recorder.Cancel(at, value)
	})
	_ = output.send(ctx, protocol.ControlMessage{
		Type: protocol.EventError, ErrorCode: "repeat_required", Message: "Please repeat that.",
	})
}

func finishPendingTurnTelemetry(
	state *turnTelemetryState,
	at time.Time,
	result turn.TurnResult,
	failure TurnFailure,
) {
	if state == nil {
		return
	}
	if failure == TurnFailureNone {
		failure = TurnFailureUnavailable
	}
	measurements := voicetelemetry.TurnMeasurements{
		ErrorClass: string(failure),
		Usage: voicetelemetry.Usage{
			InputTokens: int64(result.Usage.PromptTokens), OutputTokens: int64(result.Usage.CompletionTokens),
		},
	}
	if failure == TurnFailureCanceled {
		state.finish(at, measurements, func(
			recorder voicetelemetry.RuntimeTurnTelemetry,
			finishedAt time.Time,
			value voicetelemetry.TurnMeasurements,
		) error {
			return recorder.Cancel(finishedAt, value)
		})
		return
	}
	state.finish(at, measurements, func(
		recorder voicetelemetry.RuntimeTurnTelemetry,
		finishedAt time.Time,
		value voicetelemetry.TurnMeasurements,
	) error {
		return recorder.Fail(finishedAt, value)
	})
}

func (output *SpeechOutput) Close() error {
	if output == nil {
		return nil
	}
	output.closeOnce.Do(func() {
		output.mu.Lock()
		output.closed = true
		state := output.active
		pendingTelemetry := output.pendingTelemetry
		output.active = nil
		output.pendingTelemetry = nil
		output.pendingTurnID = 0
		output.pendingEnded = false
		output.setupFailureGen = 0
		output.setupFailureTurn = 0
		output.transport = nil
		output.attached = false
		output.mu.Unlock()
		if output.bargeIn.Close() != nil {
			output.closeErr = ErrBindingClose
		}
		if state != nil {
			state.telemetry.finish(output.now(), state.telemetryMeasurements(state.result, TurnFailureCanceled), func(
				recorder voicetelemetry.RuntimeTurnTelemetry,
				at time.Time,
				value voicetelemetry.TurnMeasurements,
			) error {
				return recorder.Cancel(at, value)
			})
			state.closeResources()
		}
		pendingTelemetry.finish(output.now(), voicetelemetry.TurnMeasurements{
			ErrorClass: "session_closed",
		}, func(
			recorder voicetelemetry.RuntimeTurnTelemetry,
			at time.Time,
			value voicetelemetry.TurnMeasurements,
		) error {
			return recorder.Cancel(at, value)
		})
		if output.closeFinal != nil && closeFinalTurnLease(output.closeFinal) != nil {
			output.closeErr = ErrBindingClose
		}
		output.mu.Lock()
		output.sessionID = ""
		output.tts = nil
		output.encoders = nil
		output.pacers = nil
		output.finalTurns = nil
		output.telemetry = nil
		output.closeFinal = nil
		output.mu.Unlock()
	})
	return output.closeErr
}

func (*SpeechOutput) String() string   { return "voice speech output{redacted}" }
func (*SpeechOutput) GoString() string { return "voice speech output{redacted}" }
func (*SpeechOutput) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{}{})
}

func (output *SpeechOutput) activeGeneration(generation uint64) (*speechGeneration, error) {
	if output == nil || generation == 0 {
		return nil, ErrSpeechOutputUnavailable
	}
	var state *speechGeneration
	err := output.bargeIn.WithGeneration(generation, func() error {
		output.mu.Lock()
		defer output.mu.Unlock()
		if output.closed || output.failed || output.active == nil || output.active.id != generation {
			return ErrSpeechOutputUnavailable
		}
		state = output.active
		return nil
	})
	if err != nil || state == nil {
		return nil, ErrSpeechOutputUnavailable
	}
	return state, nil
}

func (output *SpeechOutput) readTTS(state *speechGeneration) {
	defer close(state.readDone)
	for {
		event, err := safeNextSpeechTTS(state.stream, state.ctx)
		if err != nil {
			if state.ctx.Err() == nil {
				output.failSpeech(state, ErrSpeechOutputUnavailable)
			}
			return
		}
		if event.Final {
			if len(event.PCM16) != 0 {
				output.failSpeech(state, ErrSpeechOutputUnavailable)
				return
			}
			state.mu.Lock()
			state.providerFinal = true
			state.mu.Unlock()
			return
		}
		if len(event.PCM16) == 0 || len(event.PCM16)%2 != 0 || event.ContentType != "audio/raw" {
			output.failSpeech(state, ErrSpeechOutputUnavailable)
			return
		}
		firstAudio := state.markTTSFirstAudio(output.now())
		if firstAudio {
			state.mu.Lock()
			firstAudioAt := state.ttsFirstAudio
			state.mu.Unlock()
			state.telemetry.record(firstAudioAt, func(recorder voicetelemetry.RuntimeTurnTelemetry, at time.Time) error {
				return recorder.TTSFirstAudio(at)
			})
			_ = output.sendGenerationControl(state.ctx, state.id, protocol.ControlMessage{
				Type: protocol.EventAgentState, GenerationID: generationPointer(state.id),
				TurnID: &state.turnID, State: "speaking",
			})
		}
		if err := state.playback.PushPCM16LE(state.ctx, event.PCM16); err != nil {
			clear(event.PCM16)
			if state.ctx.Err() == nil {
				output.failSpeech(state, ErrSpeechOutputUnavailable)
			}
			return
		}
		clear(event.PCM16)
	}
}

func (output *SpeechOutput) speechStarted(ctx context.Context, turnID int64) error {
	output.mu.Lock()
	if output.closed || output.failed || turnID <= output.lastWireTurnID || output.pendingTurnID != 0 {
		output.mu.Unlock()
		return ErrPeerOutputUnavailable
	}
	speechStartedAt := time.Time{}
	if output.telemetry != nil {
		speechStartedAt = output.now()
	}
	state := output.active
	starting := output.starting
	output.mu.Unlock()

	if state != nil {
		if err := output.interrupt(ctx, state.id, state.turnID); err != nil {
			return err
		}
	} else if starting {
		generation := output.bargeIn.Current()
		if generation != 0 {
			if _, err := output.bargeIn.Interrupt(generation); err != nil &&
				!errors.Is(err, turn.ErrNoActiveVoiceGeneration) && !errors.Is(err, turn.ErrStaleVoiceGeneration) {
				return ErrPeerOutputUnavailable
			}
		}
	}

	telemetryState := safeNewTurnTelemetry(output.telemetry)
	telemetryState.record(speechStartedAt, func(recorder voicetelemetry.RuntimeTurnTelemetry, at time.Time) error {
		return recorder.SpeechStarted(at)
	})

	output.mu.Lock()
	defer output.mu.Unlock()
	if output.closed || output.failed || turnID <= output.lastWireTurnID || output.pendingTurnID != 0 {
		return ErrPeerOutputUnavailable
	}
	output.pendingTurnID = turnID
	output.pendingEnded = false
	output.pendingTelemetry = telemetryState
	return nil
}

func (output *SpeechOutput) speechEnded(turnID int64) error {
	output.mu.Lock()
	if output.closed || output.failed || output.pendingTurnID != turnID || output.pendingEnded {
		output.mu.Unlock()
		return ErrPeerOutputUnavailable
	}
	output.pendingEnded = true
	telemetryState := output.pendingTelemetry
	output.mu.Unlock()
	speechEndedAt := time.Time{}
	if telemetryState != nil {
		speechEndedAt = output.now()
	}
	telemetryState.record(speechEndedAt, func(recorder voicetelemetry.RuntimeTurnTelemetry, at time.Time) error {
		return recorder.SpeechEnded(at)
	})
	return nil
}

func (output *SpeechOutput) interrupt(ctx context.Context, generation uint64, turnID int64) error {
	output.mu.Lock()
	state := output.active
	output.mu.Unlock()
	current := output.bargeIn.Current()
	if state == nil || state.id != generation {
		if generation <= current {
			return nil
		}
		return ErrPeerOutputUnavailable
	}
	if state.turnID != turnID {
		return ErrPeerOutputUnavailable
	}
	interruptedAt := time.Time{}
	if state.telemetry != nil {
		interruptedAt = output.now()
	}
	if _, err := output.bargeIn.Interrupt(generation); err != nil {
		if errors.Is(err, turn.ErrStaleVoiceGeneration) || errors.Is(err, turn.ErrNoActiveVoiceGeneration) {
			return nil
		}
		return ErrPeerOutputUnavailable
	}
	if state.telemetry != nil {
		state.telemetry.interrupt(interruptedAt, output.now())
	}
	state.mu.Lock()
	state.cancelled = true
	state.mu.Unlock()
	output.sendCancellationOnce(ctx, state)
	return nil
}

func (output *SpeechOutput) playbackAcknowledgement(event protocol.EventType, generation uint64, turnID int64) error {
	output.mu.Lock()
	state := output.active
	output.mu.Unlock()
	current := output.bargeIn.Current()
	if state == nil || state.id != generation {
		if generation <= current {
			return nil
		}
		return ErrPeerOutputUnavailable
	}
	if state.turnID != turnID {
		return ErrPeerOutputUnavailable
	}
	state.mu.Lock()
	if state.cancelled || state.ttsFirstAudio.IsZero() {
		state.mu.Unlock()
		return ErrPeerOutputUnavailable
	}
	var clientFirstAudio time.Time
	switch event {
	case protocol.EventPlaybackStarted:
		if state.playbackStarted {
			state.mu.Unlock()
			return ErrPeerOutputUnavailable
		}
		state.playbackStarted = true
		state.clientFirstAudio = output.now()
		clientFirstAudio = state.clientFirstAudio
	case protocol.EventPlaybackCompleted:
		if !state.playbackStarted || state.playbackComplete {
			state.mu.Unlock()
			return ErrPeerOutputUnavailable
		}
		state.playbackComplete = true
		state.clientCompleted = output.now()
	default:
		state.mu.Unlock()
		return ErrPeerOutputUnavailable
	}
	state.mu.Unlock()
	if !clientFirstAudio.IsZero() {
		state.telemetry.record(clientFirstAudio, func(recorder voicetelemetry.RuntimeTurnTelemetry, at time.Time) error {
			return recorder.ClientFirstAudioPlayed(at)
		})
	}
	state.signalAcknowledgement()
	return nil
}

func (output *SpeechOutput) failGeneration(state *speechGeneration, cause error) {
	if state == nil {
		return
	}
	state.setReaderError(cause)
	_ = output.bargeIn.Abort(state.id, cause)
}

// failSpeech retires only the synthesis/playback side of an otherwise valid
// generation. The LLM context deliberately stays active so a provider close
// can still produce one bounded final answer for the client's text fallback.
func (output *SpeechOutput) failSpeech(state *speechGeneration, cause error) {
	if state == nil {
		return
	}
	state.setReaderError(cause)
	if state.playback != nil {
		_ = state.playback.Cancel(state.id, cause)
	}
	if state.stream != nil {
		_ = state.stream.Close()
	}
	state.signalAcknowledgement()
}

func (output *SpeechOutput) generationContextError(state *speechGeneration, fallback error) error {
	if state != nil && state.ctx != nil {
		if cause := context.Cause(state.ctx); cause != nil {
			return cause
		}
	}
	if fallback != nil && errors.Is(fallback, turn.ErrTurnInterrupted) {
		return turn.ErrTurnInterrupted
	}
	return ErrSpeechOutputUnavailable
}

func (output *SpeechOutput) sendGenerationControl(
	ctx context.Context,
	generation uint64,
	message protocol.ControlMessage,
) error {
	return output.bargeIn.WithGeneration(generation, func() error {
		return output.send(ctx, message)
	})
}

func (output *SpeechOutput) sendFinalAnswer(ctx context.Context, state *speechGeneration, text string) error {
	if state == nil || !validControlOutputText(text) {
		return ErrPeerOutputUnavailable
	}
	state.mu.Lock()
	if state.answerSent || state.cancelled {
		state.mu.Unlock()
		return ErrPeerOutputUnavailable
	}
	state.mu.Unlock()
	if err := output.sendGenerationControl(ctx, state.id, protocol.ControlMessage{
		Type: protocol.EventAnswerFinal, GenerationID: generationPointer(state.id),
		TurnID: &state.turnID, Text: text,
	}); err != nil {
		return err
	}
	state.mu.Lock()
	state.answerSent = true
	state.mu.Unlock()
	return nil
}

func (output *SpeechOutput) sendAuthoritativeTurnControls(ctx context.Context, state *speechGeneration) error {
	if state == nil {
		return ErrPeerOutputUnavailable
	}
	return output.bargeIn.WithGeneration(state.id, func() error {
		detectedLanguage := state.final.ProviderLanguage
		if detectedLanguage == "" {
			detectedLanguage = state.final.DetectedLanguage
		}
		var probability *float64
		if state.final.LanguageProbability != nil && detectedLanguage != "" {
			value := *state.final.LanguageProbability
			probability = &value
		}
		if err := output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventTranscriptFinal, TurnID: &state.turnID,
			Text: state.final.Text, DetectedLanguage: detectedLanguage, LanguageProbability: probability,
		}); err != nil {
			return err
		}
		if err := output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventLanguageSelected, TurnID: &state.turnID, Language: state.final.ResponseLanguage,
		}); err != nil {
			return err
		}
		return output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventTurnStarted, GenerationID: generationPointer(state.id), TurnID: &state.turnID,
		})
	})
}

func (output *SpeechOutput) send(ctx context.Context, message protocol.ControlMessage) (resultErr error) {
	if output == nil || !validOutputContext(ctx) {
		return ErrPeerOutputUnavailable
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.closed || !output.attached || nilInterface(output.transport) || output.controlSequence == math.MaxInt {
		return ErrPeerOutputUnavailable
	}
	message.ProtocolVersion = protocol.ProtocolVersion
	message.SessionID = output.sessionID
	message.Sequence = output.controlSequence + 1
	defer func() {
		if recover() != nil {
			resultErr = ErrPeerOutputUnavailable
		}
	}()
	if err := output.transport.SendControl(message); err != nil {
		return ErrPeerOutputUnavailable
	}
	output.controlSequence++
	return nil
}

func (output *SpeechOutput) sendCancellationOnce(ctx context.Context, state *speechGeneration) {
	state.mu.Lock()
	if state.cancelSent {
		state.mu.Unlock()
		return
	}
	state.cancelSent = true
	state.mu.Unlock()
	generationID, ok := controlGeneration(state.id)
	if ok {
		_ = output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventTurnCancelled, GenerationID: &generationID,
			TurnID: &state.turnID, ErrorCode: "canceled",
		})
	}
}

func (output *SpeechOutput) sendFailureControl(ctx context.Context, generation uint64, failure TurnFailure) {
	generationID, validGeneration := controlGeneration(generation)
	switch failure {
	case TurnFailureCanceled:
		if validGeneration {
			_ = output.send(ctx, protocol.ControlMessage{
				Type: protocol.EventTurnCancelled, GenerationID: &generationID, ErrorCode: "canceled",
			})
		}
	case TurnFailureAuthorizationRefresh:
		_ = output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventError, ErrorCode: "authorization_refresh_required", Message: "Authorization refresh is required.",
		})
	case TurnFailureUnavailable:
		_ = output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventError, ErrorCode: "voice_unavailable", Message: "Voice response is temporarily unavailable.",
		})
	}
}

func (output *SpeechOutput) finalTurn(state *speechGeneration, result turn.TurnResult) voicesession.FinalTurn {
	state.mu.Lock()
	llmFirstToken := state.llmFirstToken
	ttsFirstAudio := state.ttsFirstAudio
	clientFirstAudio := state.clientFirstAudio
	completedAt := state.clientCompleted
	ttsCharacters := state.ttsCharacters
	state.mu.Unlock()
	inputTokens := int64(result.Usage.PromptTokens)
	outputTokens := int64(result.Usage.CompletionTokens)
	totalTokens := int64(result.Usage.TotalTokens)
	tools := make([]voicesession.FinalTurnToolOutcome, len(result.Tools))
	for index, outcome := range result.Tools {
		tools[index] = voicesession.FinalTurnToolOutcome{
			Name: outcome.Name, Success: outcome.Success, ErrorCode: string(outcome.ErrorCode),
		}
	}
	return voicesession.FinalTurn{
		SessionID: output.sessionID, Sequence: state.sequence, GenerationID: int64(state.id),
		Transcript: result.Transcript.Text, ProviderLanguage: result.Transcript.ProviderLanguage,
		SelectedLanguage: result.Transcript.ResponseLanguage, Response: result.Text, Tools: tools,
		Timings: voicesession.FinalTurnTimings{
			SpeechEndedAt: result.Transcript.SpeechEndedAt, STTFinalAt: result.Transcript.STTFinalAt,
			LLMFirstTokenAt: llmFirstToken, TTSFirstAudioAt: ttsFirstAudio, ClientFirstAudioAt: clientFirstAudio,
		},
		Usage: voicesession.FinalTurnUsage{
			InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens,
			TTSCharacters: &ttsCharacters,
		},
		CompletedAt: completedAt, ExpiresAt: output.expiresAt,
	}
}

func (state *speechGeneration) telemetryMeasurements(
	result turn.TurnResult,
	failure TurnFailure,
) voicetelemetry.TurnMeasurements {
	state.mu.Lock()
	ttsCharacters := state.ttsCharacters
	state.mu.Unlock()
	errorClass := string(failure)
	if failure == TurnFailureNone {
		errorClass = ""
	}
	return voicetelemetry.TurnMeasurements{
		ErrorClass: errorClass,
		Usage: voicetelemetry.Usage{
			InputTokens: int64(result.Usage.PromptTokens), OutputTokens: int64(result.Usage.CompletionTokens),
			TTSCharacters: ttsCharacters,
		},
	}
}

func (output *SpeechOutput) markFailed() {
	output.mu.Lock()
	output.failed = true
	output.mu.Unlock()
}

func (output *SpeechOutput) durabilitySink() (voicesession.FinalTurnSink, bool) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.closed || output.durabilityDegraded || nilInterface(output.finalTurns) {
		return nil, false
	}
	return output.finalTurns, true
}

func (output *SpeechOutput) disableDurability() {
	output.mu.Lock()
	output.durabilityDegraded = true
	// Keep closeFinal: it still owns and drains any turns accepted before the
	// failure. Dropping only the enqueue capability prevents later sequence
	// gaps while preserving bounded cleanup.
	output.finalTurns = nil
	output.mu.Unlock()
}

func (state *speechGeneration) writeChunk(ctx context.Context, chunk string, now func() time.Time) error {
	if chunk == "" {
		return ErrSpeechOutputUnavailable
	}
	if state.telemetry != nil {
		ttsRequestAt := now()
		if state.markTTSRequestStarted(ttsRequestAt) {
			state.telemetry.record(ttsRequestAt, func(recorder voicetelemetry.RuntimeTurnTelemetry, at time.Time) error {
				return recorder.TTSRequestStarted(at)
			})
		}
	}
	if err := safeWriteSpeechTTS(state.stream, ctx, chunk); err != nil {
		return err
	}
	state.mu.Lock()
	state.ttsCharacters += int64(utf8.RuneCountInString(chunk))
	state.mu.Unlock()
	return nil
}

func (state *speechGeneration) markLLMFirstToken(now func() time.Time) (time.Time, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.llmFirstToken.IsZero() {
		state.llmFirstToken = now()
		return state.llmFirstToken, true
	}
	return state.llmFirstToken, false
}

func (state *speechGeneration) markTTSRequestStarted(at time.Time) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.ttsRequestStarted.IsZero() {
		state.ttsRequestStarted = at
		return true
	}
	return false
}

func (state *speechGeneration) markTTSFirstAudio(at time.Time) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.ttsFirstAudio.IsZero() {
		return false
	}
	state.ttsFirstAudio = at
	return true
}

func (state *speechGeneration) setReaderError(err error) {
	if err == nil {
		return
	}
	state.mu.Lock()
	if state.readErr == nil {
		state.readErr = err
	}
	state.mu.Unlock()
}

func (state *speechGeneration) readerError() error {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.readErr
}

func (state *speechGeneration) speechUnavailable() bool {
	return state.readerError() != nil
}

func (state *speechGeneration) hasProviderFinal() bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.providerFinal
}

func (state *speechGeneration) playbackAcknowledged() bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.playbackStarted && state.playbackComplete && !state.cancelled
}

func (state *speechGeneration) durablePlaybackCompleted() bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.lifecycleDone && !state.cancelled && !state.textFallback &&
		state.providerFinal && state.playbackStarted && state.playbackComplete
}

func (state *speechGeneration) signalAcknowledgement() {
	select {
	case state.ackWake <- struct{}{}:
	default:
	}
}

func (state *speechGeneration) completeTextFallback(result turn.TurnResult, completedAt time.Time) {
	state.mu.Lock()
	state.textFallback = true
	state.lifecycleDone = true
	state.result = cloneTurnResult(result)
	if state.clientCompleted.IsZero() {
		state.clientCompleted = completedAt
	}
	state.mu.Unlock()
}

func (state *speechGeneration) successfulResult(result turn.TurnResult) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.lifecycleDone || state.cancelled || !state.answerSent ||
		state.result.GenerationID != result.GenerationID ||
		!equalSpeechFinalTranscript(state.result.Transcript, result.Transcript) || state.result.Text != result.Text {
		return false
	}
	return state.textFallback || (state.playbackStarted && state.playbackComplete && state.providerFinal)
}

func (state *speechGeneration) closeResources() {
	if state == nil {
		return
	}
	state.closeOnce.Do(func() {
		if state.stream != nil {
			_ = state.stream.Close()
		}
		if state.playback != nil {
			_ = state.playback.Close()
		}
		_ = safeCloseSpeechResource(state.encoder)
		state.mu.Lock()
		state.final.Text = ""
		state.result.Text = ""
		state.mu.Unlock()
	})
}

func validSpeechSession(value voicesession.Session) bool {
	return safeIdentifier(value.ID, 128) && value.TurnSequence >= 0 && value.GenerationID >= 0 &&
		(!value.ConsentTranscriptStorage || !value.ExpiresAt.IsZero())
}

func validSpeechLanguage(language string) bool {
	selection := turn.SelectResponseLanguage(language, "")
	return selection.ProviderLanguage == language && selection.ResponseLanguage == language && !selection.UsedFallback
}

func generationPointer(generation uint64) *int64 {
	value, ok := controlGeneration(generation)
	if !ok {
		return nil
	}
	return &value
}

func messageGeneration(message protocol.ControlMessage) (uint64, bool) {
	return func() (uint64, bool) {
		if message.GenerationID == nil || *message.GenerationID <= 0 {
			return 0, false
		}
		return uint64(*message.GenerationID), true
	}()
}

func messageTurn(message protocol.ControlMessage) (int64, bool) {
	if message.TurnID == nil || *message.TurnID <= 0 {
		return 0, false
	}
	return *message.TurnID, true
}

func cloneTurnResult(result turn.TurnResult) turn.TurnResult {
	result.Transcript = cloneSpeechFinalTranscript(result.Transcript)
	result.Tools = append([]turn.ToolOutcome(nil), result.Tools...)
	return result
}

func cloneSpeechFinalTranscript(final turn.FinalTranscript) turn.FinalTranscript {
	if final.LanguageProbability != nil {
		probability := *final.LanguageProbability
		final.LanguageProbability = &probability
	}
	return final
}

func equalSpeechFinalTranscript(left, right turn.FinalTranscript) bool {
	if left.Text != right.Text || left.ProviderLanguage != right.ProviderLanguage ||
		left.DetectedLanguage != right.DetectedLanguage || left.ResponseLanguage != right.ResponseLanguage ||
		!left.SpeechEndedAt.Equal(right.SpeechEndedAt) || !left.STTFinalAt.Equal(right.STTFinalAt) {
		return false
	}
	if left.LanguageProbability == nil || right.LanguageProbability == nil {
		return left.LanguageProbability == nil && right.LanguageProbability == nil
	}
	return *left.LanguageProbability == *right.LanguageProbability
}

func safeNewSessionTTS(factory SessionTTSFactory, value voicesession.Session) (opener sarvam.TTSOpener, err error) {
	defer func() {
		if recover() != nil {
			opener = nil
			err = ErrSpeechOutputUnavailable
		}
	}()
	return factory.NewSessionTTS(value)
}

func safeNewTurnTelemetry(factory TurnTelemetryFactory) (state *turnTelemetryState) {
	if factory == nil || nilInterface(factory) {
		return nil
	}
	defer func() {
		if recover() != nil {
			state = nil
		}
	}()
	recorder, err := factory.NewTurnTelemetry()
	if err != nil || nilInterface(recorder) {
		return nil
	}
	return &turnTelemetryState{recorder: recorder}
}

func safeTelemetryRecord(
	recorder voicetelemetry.RuntimeTurnTelemetry,
	at time.Time,
	callback func(voicetelemetry.RuntimeTurnTelemetry, time.Time) error,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrSpeechOutputUnavailable
		}
	}()
	return callback(recorder, at)
}

func safeTelemetryInterrupt(
	recorder voicetelemetry.RuntimeTurnTelemetry,
	startedAt time.Time,
	stoppedAt time.Time,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrSpeechOutputUnavailable
		}
	}()
	return recorder.Interrupt(startedAt, stoppedAt)
}

func safeTelemetryDurabilityFailure(recorder voicetelemetry.RuntimeTurnTelemetry) {
	defer func() { _ = recover() }()
	recorder.DurabilityFailure()
}

func safeTelemetryFinish(
	recorder voicetelemetry.RuntimeTurnTelemetry,
	at time.Time,
	measurements voicetelemetry.TurnMeasurements,
	callback func(voicetelemetry.RuntimeTurnTelemetry, time.Time, voicetelemetry.TurnMeasurements) error,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrSpeechOutputUnavailable
		}
	}()
	return callback(recorder, at, measurements)
}

func safeNewFinalTurnSink(factory FinalTurnSinkFactory, value voicesession.Session) (lease FinalTurnSinkLease, err error) {
	defer func() {
		if recover() != nil {
			lease = FinalTurnSinkLease{}
			err = ErrSpeechOutputUnavailable
		}
	}()
	return factory.NewFinalTurnSink(value)
}

func safeOpenSpeechTTS(opener sarvam.TTSOpener, ctx context.Context, language string) (stream sarvam.TTSStream, err error) {
	defer func() {
		if recover() != nil {
			stream = nil
			err = ErrSpeechOutputUnavailable
		}
	}()
	return opener.OpenTTS(ctx, language)
}

func safeNewSpeechEncoder(factory SpeechEncoderFactory) (encoder audio.Encoder, err error) {
	defer func() {
		if recover() != nil {
			encoder = nil
			err = ErrSpeechOutputUnavailable
		}
	}()
	return factory.NewEncoder()
}

func safeNewSpeechPacer(factory SpeechPacerFactory) (pacer audio.FramePacer, err error) {
	if factory == nil {
		return nil, nil
	}
	defer func() {
		if recover() != nil {
			pacer = nil
			err = ErrSpeechOutputUnavailable
		}
	}()
	return factory.NewPacer()
}

func safeWriteSpeechTTS(stream sarvam.TTSStream, ctx context.Context, chunk string) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrSpeechOutputUnavailable
		}
	}()
	return stream.WriteText(ctx, chunk)
}

func safeFlushSpeechTTS(stream sarvam.TTSStream, ctx context.Context) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrSpeechOutputUnavailable
		}
	}()
	return stream.Flush(ctx)
}

func safeNextSpeechTTS(stream sarvam.TTSStream, ctx context.Context) (event sarvam.TTSEvent, err error) {
	defer func() {
		if recover() != nil {
			event = sarvam.TTSEvent{}
			err = ErrSpeechOutputUnavailable
		}
	}()
	return stream.Next(ctx)
}

func safeEnqueueFinalTurn(sink voicesession.FinalTurnSink, value voicesession.FinalTurn) (err error) {
	if nilInterface(sink) {
		return ErrSpeechOutputUnavailable
	}
	defer func() {
		if recover() != nil {
			err = ErrSpeechOutputUnavailable
		}
	}()
	return sink.EnqueueAfterPlayback(value)
}

func safeCloseSpeechResource(value any) (err error) {
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

func closeFinalTurnLease(closeLease func(context.Context) error) (err error) {
	if closeLease == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), finalTurnSinkCloseTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			err = ErrBindingClose
		}
	}()
	return closeLease(ctx)
}

var (
	_ OutputFactory              = SpeechOutputFactory{}
	_ SessionOutput              = (*SpeechOutput)(nil)
	_ turn.TextGenerationHandler = (*SpeechOutput)(nil)
)
