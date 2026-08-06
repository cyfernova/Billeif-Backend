package runtime

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/turn"
)

const (
	STTPCMFrameBytes    = 640
	STTPreRollFrames    = 25
	STTWriteQueueFrames = 25

	DefaultSTTFinalTimeout = 8 * time.Second
	DefaultSTTWarmTimeout  = 30 * time.Second
	MaxSTTFinalTimeout     = DefaultSTTFinalTimeout
	MaxSTTWarmTimeout      = DefaultSTTWarmTimeout
)

var (
	ErrSTTSessionContextRequired = errors.New("voice runtime STT session context is required")
	ErrSTTOpenerRequired         = errors.New("voice runtime STT opener is required")
	ErrSTTFinalHandlerRequired   = errors.New("voice runtime STT final handler is required")
	ErrSTTRepeatHandlerRequired  = errors.New("voice runtime STT repeat handler is required")
	ErrSTTInvalidTimeout         = errors.New("voice runtime STT timeout is outside the allowed bounds")
	ErrSTTInvalidPCMFrame        = errors.New("voice runtime STT requires one 20 ms PCM16 frame")
	ErrSTTSessionClosed          = errors.New("voice runtime STT session is closed")
	ErrSTTTurnInProgress         = errors.New("voice runtime STT turn is already in progress")
	ErrSTTNoActiveTurn           = errors.New("voice runtime STT has no active turn")
	ErrSTTSpeechAlreadyEnded     = errors.New("voice runtime STT speech has already ended")
	ErrSTTWriteQueueFull         = errors.New("voice runtime STT write queue is full")
)

var (
	errSTTProviderFailure = errors.New("voice runtime STT provider failure")
	errSTTFinalTimeout    = errors.New("voice runtime STT final result timed out")
	errSTTEmptyFinal      = errors.New("voice runtime STT final transcript is empty")
)

// STTFinalTranscript is the single authoritative transcript accepted for one
// speech epoch. ProviderLanguage is retained for recording; only the canonical
// DetectedLanguage and ResponseLanguage are safe to drive spoken output.
type STTFinalTranscript struct {
	Text                string
	ProviderLanguage    string
	DetectedLanguage    string
	ResponseLanguage    string
	LanguageProbability *float64
	UsedFallback        bool
}

// FinalTranscriptHandler receives an admitted authoritative final outside all
// controller locks. A handler admitted just before Close may overlap Close;
// it receives the session-derived context, must honor cancellation, and may
// call Close without deadlocking.
type FinalTranscriptHandler interface {
	HandleFinalTranscript(context.Context, STTFinalTranscript)
}

type FinalTranscriptHandlerFunc func(context.Context, STTFinalTranscript)

func (handler FinalTranscriptHandlerFunc) HandleFinalTranscript(ctx context.Context, final STTFinalTranscript) {
	handler(ctx, final)
}

// RepeatRequestHandler follows the same cancellation and Close semantics as a
// final handler. Provider events and partial transcripts never invoke either
// handler.
type RepeatRequestHandler interface {
	RequestRepeat(context.Context)
}

type RepeatRequestHandlerFunc func(context.Context)

func (handler RepeatRequestHandlerFunc) RequestRepeat(ctx context.Context) {
	handler(ctx)
}

type STTTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type STTClock interface {
	NewTimer(time.Duration) STTTimer
}

type STTSessionConfig struct {
	Context context.Context
	Opener  sarvam.STTOpener

	FallbackLanguage string
	FinalTimeout     time.Duration
	WarmTimeout      time.Duration
	Clock            STTClock

	FinalTranscriptHandler FinalTranscriptHandler
	RepeatRequestHandler   RepeatRequestHandler
}

type pcmSTTFrame [STTPCMFrameBytes]byte

type sttTurn struct {
	epoch uint64

	ctx    context.Context
	cancel context.CancelFunc

	preRoll      [STTPreRollFrames]pcmSTTFrame
	preRollCount int
	frames       chan pcmSTTFrame
	ended        bool
	speechEnded  bool
	failed       bool
	stream       sarvam.STTStream
}

type warmSTTLease struct {
	timer  STTTimer
	cancel chan struct{}
	once   sync.Once
}

func (lease *warmSTTLease) stop() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		close(lease.cancel)
		if lease.timer != nil {
			lease.timer.Stop()
		}
	})
}

// STTSession serializes final-only provider turns for one voice session.
type STTSession struct {
	mu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc

	opener           sarvam.STTOpener
	fallbackLanguage string
	finalTimeout     time.Duration
	warmTimeout      time.Duration
	clock            STTClock
	onFinal          FinalTranscriptHandler
	onRepeat         RepeatRequestHandler

	preRoll      [STTPreRollFrames]pcmSTTFrame
	preRollHead  int
	preRollCount int

	epoch       uint64
	current     *sttTurn
	lastEnded   bool
	pendingEnd  bool
	warmStream  sarvam.STTStream
	warmEpoch   uint64
	warmLease   *warmSTTLease
	closed      bool
	workers     sync.WaitGroup
	closeOnce   sync.Once
	closeResult error
}

func NewSTTSession(config STTSessionConfig) (*STTSession, error) {
	if config.Context == nil {
		return nil, ErrSTTSessionContextRequired
	}
	if config.Context.Err() != nil {
		return nil, ErrSTTSessionClosed
	}
	if nilInterface(config.Opener) {
		return nil, ErrSTTOpenerRequired
	}
	if nilInterface(config.FinalTranscriptHandler) {
		return nil, ErrSTTFinalHandlerRequired
	}
	if nilInterface(config.RepeatRequestHandler) {
		return nil, ErrSTTRepeatHandlerRequired
	}

	finalTimeout, err := boundedSTTTimeout(config.FinalTimeout, DefaultSTTFinalTimeout, MaxSTTFinalTimeout)
	if err != nil {
		return nil, err
	}
	warmTimeout, err := boundedSTTTimeout(config.WarmTimeout, DefaultSTTWarmTimeout, MaxSTTWarmTimeout)
	if err != nil {
		return nil, err
	}
	clock := config.Clock
	if clock == nil {
		clock = realSTTClock{}
	} else if nilInterface(clock) {
		return nil, ErrSTTInvalidTimeout
	}

	ctx, cancel := context.WithCancel(config.Context)
	session := &STTSession{
		ctx:              ctx,
		cancel:           cancel,
		opener:           config.Opener,
		fallbackLanguage: config.FallbackLanguage,
		finalTimeout:     finalTimeout,
		warmTimeout:      warmTimeout,
		clock:            clock,
		onFinal:          config.FinalTranscriptHandler,
		onRepeat:         config.RepeatRequestHandler,
	}
	go func() {
		<-ctx.Done()
		_ = session.Close()
	}()
	return session, nil
}

// PushPCM16 copies exactly one 20 ms, 16 kHz mono PCM16LE frame into fixed
// session storage. Idle frames feed only the next turn's pre-roll; active-turn
// frames feed only that turn's bounded live queue.
func (session *STTSession) PushPCM16(pcm []byte) error {
	if len(pcm) != STTPCMFrameBytes {
		return ErrSTTInvalidPCMFrame
	}
	var frame pcmSTTFrame
	copy(frame[:], pcm)

	session.mu.Lock()
	if session.closed || session.ctx.Err() != nil {
		session.mu.Unlock()
		clear(frame[:])
		return ErrSTTSessionClosed
	}
	turnState := session.current
	if turnState == nil {
		if session.pendingEnd {
			session.mu.Unlock()
			clear(frame[:])
			return nil
		}
		session.pushPreRollLocked(frame)
		session.mu.Unlock()
		clear(frame[:])
		return nil
	}
	if turnState.ended {
		session.mu.Unlock()
		clear(frame[:])
		return ErrSTTSpeechAlreadyEnded
	}

	select {
	case turnState.frames <- frame:
		session.mu.Unlock()
		clear(frame[:])
		return nil
	default:
		if !turnState.failed {
			turnState.failed = true
			turnState.ended = true
			session.lastEnded = true
			turnState.cancel()
			close(turnState.frames)
		}
		session.mu.Unlock()
		clear(frame[:])
		return ErrSTTWriteQueueFull
	}
}

// SpeechStarted snapshots and clears the 500 ms pre-roll, claims a live warm
// stream if available, and starts exactly one provider turn.
func (session *STTSession) SpeechStarted() error {
	session.mu.Lock()
	if session.closed || session.ctx.Err() != nil {
		session.mu.Unlock()
		return ErrSTTSessionClosed
	}
	if session.current != nil || session.pendingEnd {
		session.mu.Unlock()
		return ErrSTTTurnInProgress
	}

	session.epoch++
	turnContext, cancelTurn := context.WithCancel(session.ctx)
	turnState := &sttTurn{
		epoch:  session.epoch,
		ctx:    turnContext,
		cancel: cancelTurn,
		frames: make(chan pcmSTTFrame, STTWriteQueueFrames),
		stream: session.warmStream,
	}
	turnState.preRollCount = session.snapshotPreRollLocked(turnState.preRoll[:])
	session.clearPreRollLocked()

	oldWarmLease := session.warmLease
	session.warmLease = nil
	session.warmStream = nil
	session.warmEpoch++
	session.current = turnState
	session.lastEnded = false
	session.workers.Add(1)
	session.mu.Unlock()

	oldWarmLease.stop()
	go session.runSTTTurn(turnState)
	return nil
}

// SpeechEnded closes the active turn's input exactly once. Its worker drains
// accepted live frames before issuing exactly one provider flush.
func (session *STTSession) SpeechEnded() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.ctx.Err() != nil {
		return ErrSTTSessionClosed
	}
	if session.current == nil {
		if session.pendingEnd {
			session.pendingEnd = false
			session.lastEnded = true
			session.clearPreRollLocked()
			return nil
		}
		if session.lastEnded {
			return ErrSTTSpeechAlreadyEnded
		}
		return ErrSTTNoActiveTurn
	}
	if session.current.ended {
		if session.current.failed && !session.current.speechEnded {
			session.current.speechEnded = true
			session.lastEnded = true
			return nil
		}
		return ErrSTTSpeechAlreadyEnded
	}
	session.current.ended = true
	session.current.speechEnded = true
	session.lastEnded = true
	close(session.current.frames)
	return nil
}

// Close synchronously retires PCM queues, streams, timers, and provider
// workers. It does not wait for a user handler already admitted before Close;
// that handler observes the canceled session context and must return promptly.
func (session *STTSession) Close() error {
	if session == nil {
		return nil
	}
	session.closeOnce.Do(func() {
		session.cancel()

		session.mu.Lock()
		session.closed = true
		session.epoch++
		session.warmEpoch++
		session.pendingEnd = false
		session.clearPreRollLocked()

		warmStream := session.warmStream
		session.warmStream = nil
		warmLease := session.warmLease
		session.warmLease = nil

		var activeStream sarvam.STTStream
		var activeTurn *sttTurn
		if session.current != nil {
			activeTurn = session.current
			activeTurn.cancel()
			if !activeTurn.ended {
				activeTurn.ended = true
				close(activeTurn.frames)
			}
			activeStream = activeTurn.stream
		}
		session.mu.Unlock()

		warmLease.stop()
		closeSTTStream(warmStream)
		if !sameSTTStream(activeStream, warmStream) {
			closeSTTStream(activeStream)
		}
		session.workers.Wait()
		session.mu.Lock()
		if activeTurn != nil {
			session.scrubTurnLocked(activeTurn)
			if session.current == activeTurn {
				session.current = nil
			}
		}
		session.mu.Unlock()
	})
	return session.closeResult
}

func (session *STTSession) runSTTTurn(turnState *sttTurn) {
	stream, final, err := session.executeSTTTurn(turnState)
	if err != nil {
		shouldRepeat := session.finishSTTFailure(turnState, stream)
		session.workers.Done()
		if shouldRepeat {
			invokeRepeatHandler(session.ctx, session.onRepeat)
		}
		return
	}

	accepted, callback := session.finishSTTSuccess(turnState, stream, final)
	session.workers.Done()
	if accepted {
		callback()
	}
}

func (session *STTSession) executeSTTTurn(turnState *sttTurn) (sarvam.STTStream, STTFinalTranscript, error) {
	stream := turnState.stream
	retryUsed := false
	audioAccepted := 0

	if streamDone(stream) {
		closeSTTStream(stream)
		stream = nil
		session.setTurnStream(turnState, nil)
	}

	ensureStream := func() error {
		for stream == nil {
			opened, err := openSTTStream(turnState.ctx, session.opener)
			if err != nil {
				if retryUsed {
					return err
				}
				retryUsed = true
				continue
			}
			if !session.setTurnStream(turnState, opened) {
				closeSTTStream(opened)
				return ErrSTTSessionClosed
			}
			stream = opened
			if streamDone(stream) {
				closeSTTStream(stream)
				stream = nil
				session.setTurnStream(turnState, nil)
				if retryUsed {
					return errSTTProviderFailure
				}
				retryUsed = true
			}
		}
		return nil
	}

	if err := ensureStream(); err != nil {
		return stream, STTFinalTranscript{}, err
	}

	writeFrame := func(frame *pcmSTTFrame) error {
		for {
			if err := ensureStream(); err != nil {
				return err
			}

			err := writeSTTFrame(turnState.ctx, stream, frame[:])
			if err == nil {
				audioAccepted++
				return nil
			}
			closeSTTStream(stream)
			stream = nil
			session.setTurnStream(turnState, nil)
			if audioAccepted > 0 || retryUsed {
				return err
			}
			retryUsed = true
		}
	}

	for index := 0; index < turnState.preRollCount; index++ {
		if err := writeFrame(&turnState.preRoll[index]); err != nil {
			return stream, STTFinalTranscript{}, err
		}
		clear(turnState.preRoll[index][:])
	}
	turnState.preRollCount = 0

	for frame := range turnState.frames {
		if err := writeFrame(&frame); err != nil {
			clear(frame[:])
			return stream, STTFinalTranscript{}, err
		}
		clear(frame[:])
	}
	if turnState.ctx.Err() != nil {
		return stream, STTFinalTranscript{}, turnState.ctx.Err()
	}
	if audioAccepted == 0 {
		return stream, STTFinalTranscript{}, errSTTProviderFailure
	}
	if err := flushSTT(turnState.ctx, stream); err != nil {
		return stream, STTFinalTranscript{}, err
	}

	providerFinal, err := session.awaitSTTFinal(turnState, stream)
	if err != nil {
		return stream, STTFinalTranscript{}, err
	}
	final, err := session.selectSTTFinal(providerFinal)
	if err != nil {
		return stream, STTFinalTranscript{}, err
	}
	return stream, final, nil
}

func (session *STTSession) awaitSTTFinal(turnState *sttTurn, stream sarvam.STTStream) (sarvam.FinalTranscript, error) {
	timer := session.clock.NewTimer(session.finalTimeout)
	if nilInterface(timer) {
		return sarvam.FinalTranscript{}, errSTTProviderFailure
	}
	timerChannel := timer.C()
	if timerChannel == nil {
		return sarvam.FinalTranscript{}, errSTTProviderFailure
	}
	finalContext, cancelFinal := context.WithCancel(turnState.ctx)
	watchDone := make(chan struct{})
	watcherExited := make(chan struct{})
	var timedOut atomic.Bool
	go func() {
		defer close(watcherExited)
		select {
		case <-timerChannel:
			timedOut.Store(true)
			cancelFinal()
		case <-watchDone:
		case <-turnState.ctx.Done():
			cancelFinal()
		}
	}()

	final, err := awaitSTTFinal(finalContext, stream)
	close(watchDone)
	timer.Stop()
	<-watcherExited
	cancelFinal()
	if timedOut.Load() {
		return sarvam.FinalTranscript{}, errSTTFinalTimeout
	}
	if err != nil {
		return sarvam.FinalTranscript{}, err
	}
	return final, nil
}

func (session *STTSession) selectSTTFinal(providerFinal sarvam.FinalTranscript) (STTFinalTranscript, error) {
	if strings.TrimSpace(providerFinal.Text) == "" {
		return STTFinalTranscript{}, errSTTEmptyFinal
	}
	if providerFinal.LanguageProbability != nil {
		probability := *providerFinal.LanguageProbability
		if math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
			return STTFinalTranscript{}, errSTTProviderFailure
		}
	}
	selection := turn.SelectResponseLanguage(providerFinal.DetectedLanguage, session.fallbackLanguage)
	final := STTFinalTranscript{
		Text:             providerFinal.Text,
		ProviderLanguage: selection.ProviderLanguage,
		DetectedLanguage: selection.DetectedLanguage,
		ResponseLanguage: selection.ResponseLanguage,
		UsedFallback:     selection.UsedFallback,
	}
	if providerFinal.LanguageProbability != nil {
		probability := *providerFinal.LanguageProbability
		final.LanguageProbability = &probability
	}
	return final, nil
}

func (session *STTSession) finishSTTFailure(turnState *sttTurn, stream sarvam.STTStream) bool {
	session.mu.Lock()
	shouldRepeat := false
	if session.current == turnState {
		session.current = nil
		if !turnState.speechEnded && !session.closed && session.ctx.Err() == nil {
			session.pendingEnd = true
		}
		turnState.cancel()
		if !turnState.ended {
			turnState.ended = true
			close(turnState.frames)
		}
		session.scrubTurnLocked(turnState)
		shouldRepeat = !session.closed && session.ctx.Err() == nil && session.epoch == turnState.epoch
	}
	session.mu.Unlock()
	closeSTTStream(stream)
	return shouldRepeat
}

func (session *STTSession) finishSTTSuccess(turnState *sttTurn, stream sarvam.STTStream, final STTFinalTranscript) (bool, func()) {
	session.mu.Lock()
	if session.closed || session.ctx.Err() != nil || session.current != turnState || session.epoch != turnState.epoch || turnState.ctx.Err() != nil {
		session.mu.Unlock()
		closeSTTStream(stream)
		return false, func() {}
	}

	session.current = nil
	turnState.cancel()
	session.scrubTurnLocked(turnState)
	session.warmEpoch++
	warmEpoch := session.warmEpoch
	session.warmStream = stream
	session.mu.Unlock()

	session.startWarmSTTLease(stream, warmEpoch)
	return true, func() {
		invokeFinalHandler(session.ctx, session.onFinal, final)
	}
}

func (session *STTSession) startWarmSTTLease(stream sarvam.STTStream, epoch uint64) {
	timer := session.clock.NewTimer(session.warmTimeout)
	if nilInterface(timer) {
		closeSTTStream(stream)
		return
	}
	timerChannel := timer.C()
	if timerChannel == nil {
		closeSTTStream(stream)
		return
	}
	lease := &warmSTTLease{timer: timer, cancel: make(chan struct{})}

	session.mu.Lock()
	if session.closed || session.warmEpoch != epoch || !sameSTTStream(session.warmStream, stream) {
		session.mu.Unlock()
		lease.stop()
		return
	}
	session.warmLease = lease
	session.workers.Add(1)
	session.mu.Unlock()

	go func() {
		defer session.workers.Done()
		select {
		case <-timerChannel:
			session.expireWarmSTTStream(stream, epoch, lease)
		case <-stream.Done():
			session.expireWarmSTTStream(stream, epoch, lease)
		case <-lease.cancel:
		case <-session.ctx.Done():
			session.expireWarmSTTStream(stream, epoch, lease)
		}
	}()
}

func (session *STTSession) expireWarmSTTStream(stream sarvam.STTStream, epoch uint64, lease *warmSTTLease) {
	session.mu.Lock()
	if session.warmEpoch != epoch || session.warmLease != lease || !sameSTTStream(session.warmStream, stream) {
		session.mu.Unlock()
		return
	}
	session.warmStream = nil
	session.warmLease = nil
	session.warmEpoch++
	session.mu.Unlock()

	lease.stop()
	closeSTTStream(stream)
}

func (session *STTSession) setTurnStream(turnState *sttTurn, stream sarvam.STTStream) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.current != turnState || session.epoch != turnState.epoch {
		return false
	}
	turnState.stream = stream
	return true
}

func (session *STTSession) pushPreRollLocked(frame pcmSTTFrame) {
	if session.preRollCount < len(session.preRoll) {
		index := (session.preRollHead + session.preRollCount) % len(session.preRoll)
		session.preRoll[index] = frame
		session.preRollCount++
		return
	}
	session.preRoll[session.preRollHead] = frame
	session.preRollHead = (session.preRollHead + 1) % len(session.preRoll)
}

func (session *STTSession) snapshotPreRollLocked(destination []pcmSTTFrame) int {
	count := session.preRollCount
	for index := 0; index < count; index++ {
		destination[index] = session.preRoll[(session.preRollHead+index)%len(session.preRoll)]
	}
	return count
}

func (session *STTSession) clearPreRollLocked() {
	for index := range session.preRoll {
		clear(session.preRoll[index][:])
	}
	session.preRollHead = 0
	session.preRollCount = 0
}

func (session *STTSession) scrubTurnLocked(turnState *sttTurn) {
	for index := range turnState.preRoll {
		clear(turnState.preRoll[index][:])
	}
	turnState.preRollCount = 0
	for {
		select {
		case frame, open := <-turnState.frames:
			if !open {
				return
			}
			clear(frame[:])
		default:
			return
		}
	}
}

func boundedSTTTimeout(value, defaultValue, maximum time.Duration) (time.Duration, error) {
	if value == 0 {
		return defaultValue, nil
	}
	if value < 0 || value > maximum {
		return 0, ErrSTTInvalidTimeout
	}
	return value, nil
}

type realSTTClock struct{}

func (realSTTClock) NewTimer(duration time.Duration) STTTimer {
	return &realSTTTimer{timer: time.NewTimer(duration)}
}

type realSTTTimer struct {
	timer *time.Timer
}

func (timer *realSTTTimer) C() <-chan time.Time { return timer.timer.C }
func (timer *realSTTTimer) Stop() bool          { return timer.timer.Stop() }

func openSTTStream(ctx context.Context, opener sarvam.STTOpener) (stream sarvam.STTStream, err error) {
	defer func() {
		if recover() != nil {
			stream = nil
			err = errSTTProviderFailure
		}
	}()
	stream, err = opener.OpenSTT(ctx)
	if err != nil || nilInterface(stream) {
		if err == nil {
			err = errSTTProviderFailure
		}
		return nil, err
	}
	return stream, nil
}

func writeSTTFrame(ctx context.Context, stream sarvam.STTStream, pcm []byte) (err error) {
	defer func() {
		if recover() != nil {
			err = errSTTProviderFailure
		}
	}()
	return stream.WritePCM16(ctx, pcm)
}

func flushSTT(ctx context.Context, stream sarvam.STTStream) (err error) {
	defer func() {
		if recover() != nil {
			err = errSTTProviderFailure
		}
	}()
	return stream.Flush(ctx)
}

func awaitSTTFinal(ctx context.Context, stream sarvam.STTStream) (final sarvam.FinalTranscript, err error) {
	defer func() {
		if recover() != nil {
			final = sarvam.FinalTranscript{}
			err = errSTTProviderFailure
		}
	}()
	return stream.AwaitFinal(ctx)
}

func closeSTTStream(stream sarvam.STTStream) {
	if nilInterface(stream) {
		return
	}
	defer func() { _ = recover() }()
	_ = stream.Close()
}

func streamDone(stream sarvam.STTStream) (done bool) {
	if nilInterface(stream) {
		return false
	}
	defer func() {
		if recover() != nil {
			done = true
		}
	}()
	doneChannel := stream.Done()
	if doneChannel == nil {
		return true
	}
	select {
	case <-doneChannel:
		return true
	default:
		return false
	}
}

func sameSTTStream(left, right sarvam.STTStream) bool {
	if nilInterface(left) || nilInterface(right) {
		return nilInterface(left) && nilInterface(right)
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	return leftValue.Type() == rightValue.Type() && leftValue.Kind() == reflect.Pointer && leftValue.Pointer() == rightValue.Pointer()
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

func invokeFinalHandler(ctx context.Context, handler FinalTranscriptHandler, final STTFinalTranscript) {
	defer func() { _ = recover() }()
	handler.HandleFinalTranscript(ctx, final)
}

func invokeRepeatHandler(ctx context.Context, handler RepeatRequestHandler) {
	defer func() { _ = recover() }()
	handler.RequestRepeat(ctx)
}

var (
	_ STTClock = realSTTClock{}
	_ STTTimer = (*realSTTTimer)(nil)
)
