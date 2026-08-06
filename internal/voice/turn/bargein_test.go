package turn

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/voice/audio"
)

func TestBargeInInterruptIncrementsCancelsAndPurgesAtomically(t *testing.T) {
	controller := NewBargeIn()
	t.Cleanup(func() { _ = controller.Close() })

	turnContext, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if generation != 1 {
		t.Fatalf("generation = %d, want 1", generation)
	}

	queues := audio.NewGenerationQueues(nil)
	if err := queues.Activate(generation); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if err := queues.PushPCM(generation, make([]int16, audio.SamplesPerFrame)); err != nil {
		t.Fatalf("PushPCM() error = %v", err)
	}
	if err := queues.PushOpus(generation, []byte{0x11, 0x22}); err != nil {
		t.Fatalf("PushOpus() error = %v", err)
	}
	if err := controller.BindAudio(generation, queues.Cancel); err != nil {
		t.Fatalf("BindAudio() error = %v", err)
	}
	closer := &countingGenerationCloser{}
	if err := controller.BindTTS(generation, closer); err != nil {
		t.Fatalf("BindTTS() error = %v", err)
	}

	next, err := controller.Interrupt(generation)
	if err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if next != generation+1 || controller.Current() != generation+1 {
		t.Fatalf("next/current generation = %d/%d, want %d", next, controller.Current(), generation+1)
	}
	if !errors.Is(context.Cause(turnContext), ErrTurnInterrupted) {
		t.Fatalf("turn context cause = %v, want ErrTurnInterrupted", context.Cause(turnContext))
	}
	if got := closer.calls.Load(); got != 1 {
		t.Fatalf("TTS closes = %d, want 1", got)
	}
	state := queues.State()
	if state.PCMSamples != 0 || state.OpusFrames != 0 || !state.Cancelled {
		t.Fatalf("queue state after interrupt = %#v", state)
	}
	called := false
	if err := controller.WithGeneration(generation, func() error { called = true; return nil }); !errors.Is(err, ErrStaleVoiceGeneration) {
		t.Fatalf("WithGeneration(stale) error = %v", err)
	}
	if called {
		t.Fatal("stale generation callback ran")
	}

	nextContext, nextGeneration, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin(next) error = %v", err)
	}
	if nextGeneration != next || nextContext.Err() != nil {
		t.Fatalf("next Begin() = (generation %d, error %v)", nextGeneration, nextContext.Err())
	}
}

func TestBargeInCanResumeAfterLastPersistedGeneration(t *testing.T) {
	controller := NewBargeInAfter(41)
	t.Cleanup(func() { _ = controller.Close() })

	_, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if generation != 42 {
		t.Fatalf("generation = %d, want 42", generation)
	}
}

func TestBargeInSerializesLateCallbackAgainstInterrupt(t *testing.T) {
	controller := NewBargeIn()
	t.Cleanup(func() { _ = controller.Close() })
	_, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	callbackDone := make(chan error, 1)
	go func() {
		callbackDone <- controller.WithGeneration(generation, func() error {
			close(callbackEntered)
			<-releaseCallback
			return nil
		})
	}()
	<-callbackEntered

	interruptDone := make(chan error, 1)
	go func() {
		_, interruptErr := controller.Interrupt(generation)
		interruptDone <- interruptErr
	}()
	select {
	case err := <-interruptDone:
		t.Fatalf("Interrupt returned while accepted callback was active: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseCallback)
	if err := <-callbackDone; err != nil {
		t.Fatalf("WithGeneration() error = %v", err)
	}
	if err := <-interruptDone; err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}

	var lateCalls atomic.Int32
	for range 16 {
		if err := controller.WithGeneration(generation, func() error { lateCalls.Add(1); return nil }); !errors.Is(err, ErrStaleVoiceGeneration) {
			t.Fatalf("late WithGeneration() error = %v", err)
		}
	}
	if got := lateCalls.Load(); got != 0 {
		t.Fatalf("late callbacks executed = %d", got)
	}
}

func TestBargeInConcurrentDuplicateInterruptRunsCleanupOnce(t *testing.T) {
	controller := NewBargeIn()
	t.Cleanup(func() { _ = controller.Close() })
	_, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	closer := &countingGenerationCloser{}
	if err := controller.BindTTS(generation, closer); err != nil {
		t.Fatalf("BindTTS() error = %v", err)
	}
	var clears atomic.Int32
	if err := controller.BindAudio(generation, func(uint64, error) error {
		clears.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("BindAudio() error = %v", err)
	}

	const callers = 32
	var successes atomic.Int32
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			if _, interruptErr := controller.Interrupt(generation); interruptErr == nil {
				successes.Add(1)
			} else if !errors.Is(interruptErr, ErrStaleVoiceGeneration) && !errors.Is(interruptErr, ErrNoActiveVoiceGeneration) {
				t.Errorf("Interrupt() error = %v", interruptErr)
			}
		}()
	}
	wait.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful interrupts = %d, want 1", got)
	}
	if got := closer.calls.Load(); got != 1 {
		t.Fatalf("TTS closes = %d, want 1", got)
	}
	if got := clears.Load(); got != 1 {
		t.Fatalf("audio clears = %d, want 1", got)
	}
	if got := controller.Current(); got != generation+1 {
		t.Fatalf("current generation = %d, want %d", got, generation+1)
	}
}

func TestBargeInClosesTTSBoundAfterGenerationWentStale(t *testing.T) {
	controller := NewBargeIn()
	t.Cleanup(func() { _ = controller.Close() })
	_, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if _, err := controller.Interrupt(generation); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	late := &countingGenerationCloser{}
	if err := controller.BindTTS(generation, late); !errors.Is(err, ErrStaleVoiceGeneration) {
		t.Fatalf("BindTTS(stale) error = %v", err)
	}
	if got := late.calls.Load(); got != 1 {
		t.Fatalf("late TTS closes = %d, want 1", got)
	}
}

func TestBargeInPurgesAudioBoundAfterGenerationWentStale(t *testing.T) {
	controller := NewBargeIn()
	t.Cleanup(func() { _ = controller.Close() })
	_, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if _, err := controller.Interrupt(generation); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	var calls atomic.Int32
	var gotGeneration uint64
	var gotCause error
	err = controller.BindAudio(generation, func(boundGeneration uint64, cause error) error {
		calls.Add(1)
		gotGeneration = boundGeneration
		gotCause = cause
		return nil
	})
	if !errors.Is(err, ErrStaleVoiceGeneration) {
		t.Fatalf("BindAudio(stale) error = %v", err)
	}
	if got := calls.Load(); got != 1 || gotGeneration != generation || !errors.Is(gotCause, ErrTurnInterrupted) {
		t.Fatalf("stale audio purge = (calls %d, generation %d, cause %v)", got, gotGeneration, gotCause)
	}
}

func TestBargeInCloseCancelsActiveGenerationAndRejectsReuse(t *testing.T) {
	controller := NewBargeIn()
	turnContext, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	closer := &countingGenerationCloser{}
	if err := controller.BindTTS(generation, closer); err != nil {
		t.Fatalf("BindTTS() error = %v", err)
	}
	if err := controller.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := controller.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if !errors.Is(context.Cause(turnContext), ErrVoiceGenerationClosed) {
		t.Fatalf("turn context cause = %v", context.Cause(turnContext))
	}
	if got := closer.calls.Load(); got != 1 {
		t.Fatalf("TTS closes = %d, want 1", got)
	}
	if _, _, err := controller.Begin(context.Background()); !errors.Is(err, ErrVoiceGenerationClosed) {
		t.Fatalf("Begin(after close) error = %v", err)
	}
}

func TestBargeInCloseReturnsCachedCleanupFailure(t *testing.T) {
	controller := NewBargeIn()
	_, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := controller.BindTTS(generation, errorGenerationCloser{}); err != nil {
		t.Fatalf("BindTTS() error = %v", err)
	}
	if err := controller.Close(); !errors.Is(err, ErrVoiceGenerationCleanup) {
		t.Fatalf("Close() error = %v, want cleanup failure", err)
	}
	if err := controller.Close(); !errors.Is(err, ErrVoiceGenerationCleanup) {
		t.Fatalf("second Close() error = %v, want cached cleanup failure", err)
	}
}

func TestBargeInCompleteWithCommitsUnderInterruptFence(t *testing.T) {
	controller := NewBargeIn()
	t.Cleanup(func() { _ = controller.Close() })
	_, generation, err := controller.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	commitEntered := make(chan struct{})
	releaseCommit := make(chan struct{})
	completeDone := make(chan error, 1)
	go func() {
		completeDone <- controller.CompleteWith(generation, func() error {
			close(commitEntered)
			<-releaseCommit
			return nil
		})
	}()
	<-commitEntered
	interruptDone := make(chan error, 1)
	go func() {
		_, interruptErr := controller.Interrupt(generation)
		interruptDone <- interruptErr
	}()
	select {
	case err := <-interruptDone:
		t.Fatalf("Interrupt crossed in-progress completion: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseCommit)
	if err := <-completeDone; err != nil {
		t.Fatalf("CompleteWith() error = %v", err)
	}
	if err := <-interruptDone; !errors.Is(err, ErrNoActiveVoiceGeneration) {
		t.Fatalf("Interrupt(after completion) error = %v", err)
	}
}

type countingGenerationCloser struct{ calls atomic.Int32 }

func (closer *countingGenerationCloser) Close() error {
	closer.calls.Add(1)
	return nil
}

type errorGenerationCloser struct{}

func (errorGenerationCloser) Close() error { return errors.New("sensitive cleanup failure") }
