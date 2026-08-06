package turn

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
)

var (
	ErrTurnInterrupted         = errors.New("voice turn interrupted")
	ErrVoiceGenerationClosed   = errors.New("voice generation controller is closed")
	ErrNoActiveVoiceGeneration = errors.New("voice generation is not active")
	ErrStaleVoiceGeneration    = errors.New("voice generation is stale")
	ErrFutureVoiceGeneration   = errors.New("voice generation is not active yet")
	ErrVoiceGenerationOverflow = errors.New("voice generation exhausted")
	ErrVoiceGenerationCleanup  = errors.New("voice generation cleanup failed")
)

type generationAudioCancel func(uint64, error) error

// BargeIn is the session-scoped generation fence shared by reasoning and
// playback. WithGeneration linearizes accepted callbacks against Interrupt:
// after Interrupt returns, no callback from the old generation can run.
type BargeIn struct {
	mu sync.Mutex

	generation      uint64
	active          bool
	reserved        bool
	closed          bool
	cancelLLM       context.CancelCauseFunc
	tts             io.Closer
	cancelAudio     generationAudioCancel
	staleGeneration uint64
	staleCause      error

	closeOnce sync.Once
	closeErr  error
}

func NewBargeIn() *BargeIn { return NewBargeInAfter(0) }

// NewBargeInAfter starts the next admitted turn strictly after a generation
// already retained in session state. A maximum value fails closed from Begin
// with ErrVoiceGenerationOverflow.
func NewBargeInAfter(lastGeneration uint64) *BargeIn {
	return &BargeIn{generation: lastGeneration}
}

func (controller *BargeIn) Begin(parent context.Context) (context.Context, uint64, error) {
	if controller == nil || parent == nil {
		return nil, 0, ErrVoiceGenerationClosed
	}
	if err := parent.Err(); err != nil {
		return nil, 0, err
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.closed {
		return nil, 0, ErrVoiceGenerationClosed
	}
	if controller.active {
		return nil, 0, ErrTurnInProgress
	}
	if controller.generation == 0 {
		controller.generation = 1
	} else if controller.reserved {
		controller.reserved = false
	} else {
		if controller.generation == math.MaxUint64 {
			return nil, 0, ErrVoiceGenerationOverflow
		}
		controller.generation++
	}
	turnContext, cancel := context.WithCancelCause(parent)
	controller.active = true
	controller.cancelLLM = cancel
	controller.tts = nil
	controller.cancelAudio = nil
	return turnContext, controller.generation, nil
}

func (controller *BargeIn) BindTTS(generation uint64, closer io.Closer) error {
	if controller == nil || nilTurnInterface(closer) {
		return ErrVoiceGenerationCleanup
	}
	controller.mu.Lock()
	err := controller.validateActiveLocked(generation)
	if err == nil && controller.tts != nil {
		err = ErrVoiceGenerationCleanup
	}
	if err == nil {
		controller.tts = closer
		controller.mu.Unlock()
		return nil
	}
	controller.mu.Unlock()
	_ = safeCloseGenerationResource(closer)
	return err
}

func (controller *BargeIn) BindAudio(generation uint64, cancel func(uint64, error) error) error {
	if controller == nil || cancel == nil {
		return ErrVoiceGenerationCleanup
	}
	controller.mu.Lock()
	err := controller.validateActiveLocked(generation)
	if err == nil && controller.cancelAudio != nil {
		err = ErrVoiceGenerationCleanup
	}
	if err == nil {
		controller.cancelAudio = cancel
		controller.mu.Unlock()
		return nil
	}
	cause := err
	if generation == controller.staleGeneration && controller.staleCause != nil {
		cause = controller.staleCause
	}
	controller.mu.Unlock()
	_ = safeCancelGenerationAudio(cancel, generation, cause)
	return err
}

// WithGeneration holds the generation fence while callback performs one
// short, bounded state transition or send. callback must not re-enter BargeIn.
func (controller *BargeIn) WithGeneration(generation uint64, callback func() error) (resultErr error) {
	if controller == nil || callback == nil {
		return ErrVoiceGenerationClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := controller.validateActiveLocked(generation); err != nil {
		return err
	}
	defer func() {
		if recover() != nil {
			resultErr = ErrVoiceGenerationCleanup
		}
	}()
	return callback()
}

func (controller *BargeIn) Interrupt(generation uint64) (uint64, error) {
	if controller == nil {
		return 0, ErrVoiceGenerationClosed
	}
	controller.mu.Lock()
	if err := controller.validateActiveLocked(generation); err != nil {
		current := controller.generation
		controller.mu.Unlock()
		return current, err
	}
	if controller.generation == math.MaxUint64 {
		controller.mu.Unlock()
		return generation, ErrVoiceGenerationOverflow
	}
	oldGeneration := controller.generation
	controller.generation++
	controller.active = false
	controller.reserved = true
	controller.staleGeneration = oldGeneration
	controller.staleCause = ErrTurnInterrupted
	cancel, closer, cancelAudio := controller.detachLocked()
	nextGeneration := controller.generation
	controller.mu.Unlock()

	return nextGeneration, runGenerationCleanup(oldGeneration, ErrTurnInterrupted, cancel, closer, cancelAudio)
}

// Complete retires one successfully played generation without reserving an
// extra ID. The next ordinary turn receives the next monotonically increasing
// generation.
func (controller *BargeIn) Complete(generation uint64) error {
	return controller.CompleteWith(generation, nil)
}

// CompleteWith commits the caller's short in-memory completion transition
// under the same fence as Interrupt. Exactly one of completion or interruption
// wins; a canceled turn can therefore never enter completed-turn memory.
func (controller *BargeIn) CompleteWith(generation uint64, commit func() error) error {
	if controller == nil {
		return ErrVoiceGenerationClosed
	}
	controller.mu.Lock()
	if err := controller.validateActiveLocked(generation); err != nil {
		controller.mu.Unlock()
		return err
	}
	if commit != nil {
		commitErr := func() (resultErr error) {
			defer func() {
				if recover() != nil {
					resultErr = ErrVoiceGenerationCleanup
				}
			}()
			return commit()
		}()
		if commitErr != nil {
			controller.mu.Unlock()
			return commitErr
		}
	}
	controller.active = false
	controller.reserved = false
	controller.staleGeneration = generation
	controller.staleCause = context.Canceled
	cancel, closer, _ := controller.detachLocked()
	controller.mu.Unlock()
	if cancel != nil {
		cancel(nil)
	}
	return safeCloseGenerationResource(closer)
}

// Abort fences a failed generation and clears all attached resources without
// treating the failure as a user barge-in.
func (controller *BargeIn) Abort(generation uint64, cause error) error {
	if controller == nil {
		return ErrVoiceGenerationClosed
	}
	if cause == nil {
		cause = ErrVoiceGenerationCleanup
	}
	controller.mu.Lock()
	if err := controller.validateActiveLocked(generation); err != nil {
		controller.mu.Unlock()
		return err
	}
	controller.active = false
	controller.reserved = false
	controller.staleGeneration = generation
	controller.staleCause = cause
	cancel, closer, cancelAudio := controller.detachLocked()
	controller.mu.Unlock()
	return runGenerationCleanup(generation, cause, cancel, closer, cancelAudio)
}

func (controller *BargeIn) Current() uint64 {
	if controller == nil {
		return 0
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.generation
}

func (controller *BargeIn) Close() error {
	if controller == nil {
		return nil
	}
	controller.closeOnce.Do(func() {
		controller.mu.Lock()
		controller.closed = true
		controller.active = false
		controller.reserved = false
		generation := controller.generation
		controller.staleGeneration = generation
		controller.staleCause = ErrVoiceGenerationClosed
		cancel, closer, cancelAudio := controller.detachLocked()
		controller.mu.Unlock()
		controller.closeErr = runGenerationCleanup(generation, ErrVoiceGenerationClosed, cancel, closer, cancelAudio)
	})
	return controller.closeErr
}

func (controller *BargeIn) validateActiveLocked(generation uint64) error {
	if controller.closed {
		return ErrVoiceGenerationClosed
	}
	if generation < controller.generation {
		return ErrStaleVoiceGeneration
	}
	if generation > controller.generation {
		return ErrFutureVoiceGeneration
	}
	if generation == 0 || !controller.active {
		return ErrNoActiveVoiceGeneration
	}
	return nil
}

func (controller *BargeIn) detachLocked() (context.CancelCauseFunc, io.Closer, generationAudioCancel) {
	cancel := controller.cancelLLM
	closer := controller.tts
	cancelAudio := controller.cancelAudio
	controller.cancelLLM = nil
	controller.tts = nil
	controller.cancelAudio = nil
	return cancel, closer, cancelAudio
}

func runGenerationCleanup(
	generation uint64,
	cause error,
	cancel context.CancelCauseFunc,
	closer io.Closer,
	cancelAudio generationAudioCancel,
) error {
	failed := false
	if cancel != nil {
		cancel(cause)
	}
	if safeCloseGenerationResource(closer) != nil {
		failed = true
	}
	if cancelAudio != nil {
		if err := safeCancelGenerationAudio(cancelAudio, generation, cause); err != nil {
			failed = true
		}
	}
	if failed {
		return ErrVoiceGenerationCleanup
	}
	return nil
}

func safeCancelGenerationAudio(cancel generationAudioCancel, generation uint64, cause error) (resultErr error) {
	if cancel == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			resultErr = ErrVoiceGenerationCleanup
		}
	}()
	if err := cancel(generation, cause); err != nil {
		return ErrVoiceGenerationCleanup
	}
	return nil
}

func safeCloseGenerationResource(closer io.Closer) (resultErr error) {
	if nilTurnInterface(closer) {
		return nil
	}
	defer func() {
		if recover() != nil {
			resultErr = ErrVoiceGenerationCleanup
		}
	}()
	if err := closer.Close(); err != nil {
		return ErrVoiceGenerationCleanup
	}
	return nil
}
