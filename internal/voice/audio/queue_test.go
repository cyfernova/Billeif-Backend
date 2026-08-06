package audio

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerationQueuesPCMOverflowCancelsOnceAndClearsBothQueues(t *testing.T) {
	t.Parallel()

	type cancellation struct {
		generation uint64
		cause      error
		state      QueueState
	}
	cancelled := make(chan cancellation, 2)
	var queues *GenerationQueues
	queues = NewGenerationQueues(func(generation uint64, cause error) {
		cancelled <- cancellation{generation: generation, cause: cause, state: queues.State()}
	})
	if err := queues.Activate(7); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if err := queues.PushOpus(7, []byte{0xaa}); err != nil {
		t.Fatalf("PushOpus() error = %v", err)
	}
	if err := queues.PushPCM(7, make([]int16, 12_000)); err != nil {
		t.Fatalf("first PushPCM() error = %v", err)
	}

	if err := queues.PushPCM(7, []int16{1}); !errors.Is(err, ErrQueueOverflow) {
		t.Fatalf("overflow PushPCM() error = %v, want ErrQueueOverflow", err)
	}
	select {
	case event := <-cancelled:
		if event.generation != 7 || !errors.Is(event.cause, ErrQueueOverflow) {
			t.Fatalf("cancellation = (%d, %v), want (7, ErrQueueOverflow)", event.generation, event.cause)
		}
		if event.state.PCMSamples != 0 || event.state.OpusFrames != 0 || !event.state.Cancelled {
			t.Fatalf("callback state = %+v, want both queues empty and cancelled", event.state)
		}
	case <-time.After(time.Second):
		t.Fatal("overflow cancellation callback blocked")
	}

	if err := queues.PushPCM(7, []int16{2}); !errors.Is(err, ErrGenerationCancelled) {
		t.Fatalf("post-cancel PushPCM() error = %v, want ErrGenerationCancelled", err)
	}
	select {
	case event := <-cancelled:
		t.Fatalf("duplicate cancellation callback = %+v", event)
	default:
	}
}

func TestGenerationQueuesOpusLimitRepresentsAtMostTwoHundredFortyMilliseconds(t *testing.T) {
	t.Parallel()

	var cancellations atomic.Int32
	queues := NewGenerationQueues(func(generation uint64, cause error) {
		if generation != 1 || !errors.Is(cause, ErrQueueOverflow) {
			t.Errorf("cancellation = (%d, %v), want (1, ErrQueueOverflow)", generation, cause)
		}
		cancellations.Add(1)
	})
	if err := queues.Activate(1); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	for frame := 0; frame < 12; frame++ {
		if err := queues.PushOpus(1, []byte{byte(frame)}); err != nil {
			t.Fatalf("PushOpus(frame %d) error = %v", frame, err)
		}
	}
	if state := queues.State(); state.OpusFrames != 12 {
		t.Fatalf("OpusFrames = %d, want 12", state.OpusFrames)
	}

	if err := queues.PushOpus(1, []byte{12}); !errors.Is(err, ErrQueueOverflow) {
		t.Fatalf("overflow PushOpus() error = %v, want ErrQueueOverflow", err)
	}
	if got := cancellations.Load(); got != 1 {
		t.Fatalf("cancellation count = %d, want 1", got)
	}
	state := queues.State()
	if !state.Cancelled || state.OpusFrames != 0 || state.PCMSamples != 0 {
		t.Fatalf("state after overflow = %+v, want cancelled and empty", state)
	}
}

func TestGenerationQueuesGenerationAdvanceDiscardsPriorAudio(t *testing.T) {
	t.Parallel()

	queues := NewGenerationQueues(nil)
	if err := queues.Activate(41); err != nil {
		t.Fatalf("Activate(41) error = %v", err)
	}
	if err := queues.PushPCM(41, []int16{111, 222}); err != nil {
		t.Fatalf("PushPCM(41) error = %v", err)
	}
	if err := queues.PushOpus(41, []byte{0xaa, 0xbb}); err != nil {
		t.Fatalf("PushOpus(41) error = %v", err)
	}

	if err := queues.Activate(42); err != nil {
		t.Fatalf("Activate(42) error = %v", err)
	}
	if err := queues.PushPCM(41, []int16{333}); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale PushPCM() error = %v, want ErrStaleGeneration", err)
	}
	if err := queues.PushOpus(43, []byte{0xcc}); !errors.Is(err, ErrFutureGeneration) {
		t.Fatalf("future PushOpus() error = %v, want ErrFutureGeneration", err)
	}
	if err := queues.PopPCMFrame(42, make([]int16, 320)); !errors.Is(err, ErrQueueEmpty) {
		t.Fatalf("PopPCMFrame(42) error = %v, want ErrQueueEmpty", err)
	}
	if _, err := queues.PopOpus(42, make([]byte, 2)); !errors.Is(err, ErrQueueEmpty) {
		t.Fatalf("PopOpus(42) error = %v, want ErrQueueEmpty", err)
	}
	if state := queues.State(); state.Generation != 42 || state.PCMSamples != 0 || state.OpusFrames != 0 {
		t.Fatalf("state = %+v, want empty generation 42", state)
	}
}

func TestGenerationQueuesPCMQueuePreservesFIFOAcrossWrap(t *testing.T) {
	t.Parallel()

	queues := NewGenerationQueues(nil)
	if err := queues.Activate(1); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	first := make([]int16, 11_840)
	for index := range first {
		first[index] = int16(index)
	}
	if err := queues.PushPCM(1, first); err != nil {
		t.Fatalf("first PushPCM() error = %v", err)
	}
	discard := make([]int16, 320)
	for frame := 0; frame < 36; frame++ {
		if err := queues.PopPCMFrame(1, discard); err != nil {
			t.Fatalf("PopPCMFrame(discard %d) error = %v", frame, err)
		}
	}
	wrapped := make([]int16, 640)
	for index := range wrapped {
		wrapped[index] = -int16(index + 1)
	}
	if err := queues.PushPCM(1, wrapped); err != nil {
		t.Fatalf("wrapped PushPCM() error = %v", err)
	}

	destination := make([]int16, 320)
	if err := queues.PopPCMFrame(1, destination); err != nil {
		t.Fatalf("first wrapped PopPCMFrame() error = %v", err)
	}
	if destination[0] != 11_520 || destination[319] != 11_839 {
		t.Fatalf("first wrapped PCM boundaries = (%d, %d), want (11520, 11839)", destination[0], destination[319])
	}
	if err := queues.PopPCMFrame(1, destination); err != nil {
		t.Fatalf("second wrapped PopPCMFrame() error = %v", err)
	}
	if destination[0] != -1 || destination[319] != -320 {
		t.Fatalf("second wrapped PCM boundaries = (%d, %d), want (-1, -320)", destination[0], destination[319])
	}
	if err := queues.PopPCMFrame(1, destination); err != nil {
		t.Fatalf("third wrapped PopPCMFrame() error = %v", err)
	}
	if destination[0] != -321 || destination[319] != -640 {
		t.Fatalf("third wrapped PCM boundaries = (%d, %d), want (-321, -640)", destination[0], destination[319])
	}
}

func TestGenerationQueuesPCMConsumptionRequiresExactlyOneFrame(t *testing.T) {
	t.Parallel()

	queues := NewGenerationQueues(nil)
	if err := queues.Activate(3); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if err := queues.PushPCM(3, make([]int16, 320)); err != nil {
		t.Fatalf("PushPCM() error = %v", err)
	}
	for _, size := range []int{319, 321} {
		if err := queues.PopPCMFrame(3, make([]int16, size)); !errors.Is(err, ErrInvalidPCMFrame) {
			t.Fatalf("PopPCMFrame(%d samples) error = %v, want ErrInvalidPCMFrame", size, err)
		}
	}
	if err := queues.PopPCMFrame(3, make([]int16, 320)); err != nil {
		t.Fatalf("PopPCMFrame(320 samples) error = %v", err)
	}
}

func TestGenerationQueuesOpusPopPreservesPayloadAndDoesNotConsumeOnShortDestination(t *testing.T) {
	t.Parallel()

	queues := NewGenerationQueues(nil)
	if err := queues.Activate(5); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if err := queues.PushOpus(5, []byte{0x10, 0x20, 0x30}); err != nil {
		t.Fatalf("PushOpus() error = %v", err)
	}
	if _, err := queues.PopOpus(5, make([]byte, 2)); !errors.Is(err, ErrDestinationTooSmall) {
		t.Fatalf("short PopOpus() error = %v, want ErrDestinationTooSmall", err)
	}

	destination := make([]byte, 3)
	bytesRead, err := queues.PopOpus(5, destination)
	if err != nil {
		t.Fatalf("PopOpus() error = %v", err)
	}
	if bytesRead != 3 {
		t.Fatalf("PopOpus() bytes = %d, want 3", bytesRead)
	}
	if destination[0] != 0x10 || destination[1] != 0x20 || destination[2] != 0x30 {
		t.Fatalf("PopOpus() payload = %x, want 102030", destination)
	}
}

func TestGenerationQueuesManualCancellationIsIdempotentAndReentrant(t *testing.T) {
	t.Parallel()

	wantCause := errors.New("barge in")
	var calls atomic.Int32
	var queues *GenerationQueues
	queues = NewGenerationQueues(func(generation uint64, cause error) {
		calls.Add(1)
		if generation != 9 || !errors.Is(cause, wantCause) {
			t.Errorf("cancellation = (%d, %v), want (9, barge in)", generation, cause)
		}
		_ = queues.State()
	})
	if err := queues.Activate(9); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if err := queues.Cancel(9, wantCause); err != nil {
		t.Fatalf("first Cancel() error = %v", err)
	}
	if err := queues.Cancel(9, wantCause); !errors.Is(err, ErrGenerationCancelled) {
		t.Fatalf("second Cancel() error = %v, want ErrGenerationCancelled", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("callback calls = %d, want 1", got)
	}
}

func TestGenerationQueuesCloseIsIdempotentAndRejectsFurtherAudio(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	queues := NewGenerationQueues(func(uint64, error) { calls.Add(1) })
	if err := queues.Activate(2); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if err := queues.PushPCM(2, []int16{123}); err != nil {
		t.Fatalf("PushPCM() error = %v", err)
	}
	if err := queues.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := queues.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := queues.PushPCM(2, []int16{456}); !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("post-close PushPCM() error = %v, want ErrQueueClosed", err)
	}
	state := queues.State()
	if !state.Closed || state.PCMSamples != 0 || state.OpusFrames != 0 {
		t.Fatalf("closed state = %+v, want closed and empty", state)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("Close callback calls = %d, want 0", got)
	}
}

func TestGenerationQueuesCloseRace(t *testing.T) {
	queues := NewGenerationQueues(nil)
	if err := queues.Activate(1); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	var workers sync.WaitGroup
	workers.Add(4)
	for worker := 0; worker < 4; worker++ {
		go func() {
			defer workers.Done()
			pcm := make([]int16, 320)
			opus := make([]byte, MaxOpusPacketBytes)
			for iteration := 0; iteration < 500; iteration++ {
				_ = queues.PushPCM(1, pcm)
				_ = queues.PopPCMFrame(1, pcm)
				_ = queues.PushOpus(1, []byte{0x01})
				_, _ = queues.PopOpus(1, opus)
				_ = queues.State()
				runtime.Gosched()
			}
		}()
	}
	if err := queues.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	workers.Wait()
}
