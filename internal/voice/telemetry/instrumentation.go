package telemetry

import (
	"errors"
	"sync/atomic"
	"time"
)

var ErrInvalidEmitter = errors.New("invalid telemetry emitter")

// RuntimeTurnTelemetry is the narrow lifecycle seam used by the voice runtime.
// It contains only coarse turn events and cannot accept audio frames, provider
// bodies, transcripts, or answers.
type RuntimeTurnTelemetry interface {
	SpeechStarted(time.Time) error
	SpeechEnded(time.Time) error
	STTFinalReceived(time.Time) error
	LLMRequestStarted(time.Time) error
	LLMFirstToken(time.Time) error
	TTSRequestStarted(time.Time) error
	TTSFirstAudio(time.Time) error
	ClientFirstAudioPlayed(time.Time) error
	Interrupt(time.Time, time.Time) error
	DurabilityFailure()
	Complete(time.Time, TurnMeasurements) error
	Fail(time.Time, TurnMeasurements) error
	Cancel(time.Time, TurnMeasurements) error
}

// TurnMeasurements are supplied once, at the terminal boundary.
type TurnMeasurements struct {
	ErrorClass string
	Usage      Usage
	Signals    Signals
	Details    map[string]any
}

// Instrumentation joins a strict boundary tracker to the aggregate emitter.
// Runtime code can keep it behind RuntimeTurnTelemetry.
type Instrumentation struct {
	tracker            *Tracker
	emitter            *Emitter
	durabilityFailures atomic.Int64
}

var _ RuntimeTurnTelemetry = (*Instrumentation)(nil)

func NewInstrumentation(correlationID string, emitter *Emitter) (*Instrumentation, error) {
	if emitter == nil {
		return nil, ErrInvalidEmitter
	}
	tracker, err := NewTracker(correlationID)
	if err != nil {
		return nil, err
	}
	return &Instrumentation{tracker: tracker, emitter: emitter}, nil
}

func (instrumentation *Instrumentation) SpeechStarted(at time.Time) error {
	return instrumentation.record(SpeechStarted, at)
}

func (instrumentation *Instrumentation) SpeechEnded(at time.Time) error {
	return instrumentation.record(SpeechEnded, at)
}

func (instrumentation *Instrumentation) STTFinalReceived(at time.Time) error {
	return instrumentation.record(STTFinalReceived, at)
}

func (instrumentation *Instrumentation) LLMRequestStarted(at time.Time) error {
	return instrumentation.record(LLMRequestStarted, at)
}

func (instrumentation *Instrumentation) LLMFirstToken(at time.Time) error {
	return instrumentation.record(LLMFirstToken, at)
}

// TTSRequestStarted marks submission of the first LLM text delta to TTS. A
// TTS WebSocket connection or provider configuration warmup is not this
// boundary and must not call this method.
func (instrumentation *Instrumentation) TTSRequestStarted(at time.Time) error {
	return instrumentation.record(TTSRequestStarted, at)
}

func (instrumentation *Instrumentation) TTSFirstAudio(at time.Time) error {
	return instrumentation.record(TTSFirstAudio, at)
}

func (instrumentation *Instrumentation) ClientFirstAudioPlayed(at time.Time) error {
	return instrumentation.record(ClientFirstAudioPlayed, at)
}

func (instrumentation *Instrumentation) Interrupt(interruptedAt, playbackStoppedAt time.Time) error {
	if instrumentation == nil || instrumentation.tracker == nil {
		return ErrIncompleteTurn
	}
	return instrumentation.tracker.RecordInterrupt(interruptedAt, playbackStoppedAt)
}

// DurabilityFailure records that a post-response persistence operation failed.
// It deliberately returns no error: loss of durable replay metadata degrades
// durability and raises telemetry, but must not poison an otherwise successful
// live voice turn.
func (instrumentation *Instrumentation) DurabilityFailure() {
	if instrumentation == nil {
		return
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	for {
		current := instrumentation.durabilityFailures.Load()
		if current == maxInt64 || instrumentation.durabilityFailures.CompareAndSwap(current, current+1) {
			return
		}
	}
}

func (instrumentation *Instrumentation) Complete(at time.Time, measurements TurnMeasurements) error {
	return instrumentation.finish(TurnCompleted, OutcomeSuccess, at, measurements)
}

func (instrumentation *Instrumentation) Fail(at time.Time, measurements TurnMeasurements) error {
	return instrumentation.finish(TurnCancelled, OutcomeError, at, measurements)
}

func (instrumentation *Instrumentation) Cancel(at time.Time, measurements TurnMeasurements) error {
	return instrumentation.finish(TurnCancelled, OutcomeCancelled, at, measurements)
}

func (instrumentation *Instrumentation) record(boundary Boundary, at time.Time) error {
	if instrumentation == nil || instrumentation.tracker == nil {
		return ErrIncompleteTurn
	}
	return instrumentation.tracker.Record(boundary, at)
}

func (instrumentation *Instrumentation) finish(boundary Boundary, outcome Outcome, at time.Time, measurements TurnMeasurements) error {
	if instrumentation == nil || instrumentation.tracker == nil || instrumentation.emitter == nil {
		return ErrInvalidEmitter
	}
	if err := instrumentation.tracker.Record(boundary, at); err != nil {
		return err
	}
	snapshot, err := instrumentation.tracker.Snapshot()
	if err != nil {
		return err
	}
	measurements.Signals.DurabilityFailures = saturatingAdd(
		measurements.Signals.DurabilityFailures,
		instrumentation.durabilityFailures.Load(),
	)
	return instrumentation.emitter.EmitTurn(TurnEvent{
		Snapshot:   snapshot,
		Outcome:    outcome,
		ErrorClass: measurements.ErrorClass,
		Usage:      measurements.Usage,
		Signals:    measurements.Signals,
		Details:    measurements.Details,
	})
}

func saturatingAdd(left, right int64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	if left < 0 || right < 0 {
		return -1
	}
	if left > maxInt64-right {
		return maxInt64
	}
	return left + right
}
