package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFinalTurnWorkerEnqueueAfterPlaybackNeverWaitsForDurableIO(t *testing.T) {
	writer := &blockingFinalTurnWriter{
		started: make(chan FinalTurn, 1),
		release: make(chan struct{}),
	}
	worker, err := NewFinalTurnWorker(context.Background(), writer, FinalTurnWorkerConfig{
		SessionID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", QueueCapacity: 1,
		WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	first := testFinalTurn()
	returned := make(chan error, 1)
	go func() { returned <- worker.EnqueueAfterPlayback(first) }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("durable writer delayed the post-playback enqueue path")
	}

	select {
	case got := <-writer.started:
		if got.Sequence != first.Sequence {
			t.Fatalf("wrong first turn: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not start durable write")
	}

	second := nextFinalTurn(first)
	wantSecondResponse := second.Response
	wantSecondTool := second.Tools[0].Name
	wantSecondInputTokens := *second.Usage.InputTokens
	if err := worker.EnqueueAfterPlayback(second); err != nil {
		t.Fatalf("fill bounded queue: %v", err)
	}
	third := nextFinalTurn(second)
	if err := worker.EnqueueAfterPlayback(third); !errors.Is(err, ErrFinalTurnQueueFull) {
		t.Fatalf("expected nonblocking queue saturation, got %v", err)
	}
	second.Response = "caller mutation"
	second.Tools[0].Name = "caller_mutation"
	*second.Usage.InputTokens = 999

	close(writer.release)
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Close(closeCtx); err != nil {
		t.Fatalf("close and drain: %v", err)
	}
	if got := writer.sequences(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("worker did not drain in order: %v", got)
	}
	retained := writer.turnsCopy()[1]
	if retained.Response != wantSecondResponse || retained.Tools[0].Name != wantSecondTool || *retained.Usage.InputTokens != wantSecondInputTokens {
		t.Fatalf("worker retained caller-owned mutable state: %#v", retained)
	}
}

func TestFinalTurnWorkerRetriesOneAmbiguousFailureThenContinuesInOrder(t *testing.T) {
	writer := &recordingFinalTurnWriter{errors: []error{errors.New("ambiguous write"), nil, nil}}
	worker, err := NewFinalTurnWorker(context.Background(), writer, FinalTurnWorkerConfig{
		SessionID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", QueueCapacity: 2,
		WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	first := testFinalTurn()
	second := nextFinalTurn(first)
	if err := worker.EnqueueAfterPlayback(first); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	if err := worker.EnqueueAfterPlayback(second); err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Close(closeCtx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := writer.sequences(); len(got) != 3 || got[0] != 1 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("unexpected bounded retry/order: %v", got)
	}
}

func TestFinalTurnWorkerFailsClosedAfterBoundedWriteFailures(t *testing.T) {
	writer := &recordingFinalTurnWriter{errors: []error{errors.New("first"), errors.New("second")}}
	worker, err := NewFinalTurnWorker(context.Background(), writer, FinalTurnWorkerConfig{
		SessionID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", QueueCapacity: 1,
		WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	if err := worker.EnqueueAfterPlayback(testFinalTurn()); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case got := <-worker.Errors():
		if !errors.Is(got, ErrFinalTurnWrite) || got.Error() != ErrFinalTurnWrite.Error() {
			t.Fatalf("worker exposed unsafe or wrong error: %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("worker failure was not surfaced")
	}

	deadline := time.Now().Add(time.Second)
	for {
		err := worker.EnqueueAfterPlayback(nextFinalTurn(testFinalTurn()))
		if errors.Is(err, ErrFinalTurnWorkerFailed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker did not fail closed: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Close(closeCtx); !errors.Is(err, ErrFinalTurnWrite) {
		t.Fatalf("close must report terminal persistence failure, got %v", err)
	}
}

func TestFinalTurnWorkerRejectsWrongSessionSequenceGenerationAndClosedState(t *testing.T) {
	worker, err := NewFinalTurnWorker(context.Background(), &recordingFinalTurnWriter{}, FinalTurnWorkerConfig{
		SessionID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", QueueCapacity: 2,
		InitialSequence: 4, InitialGenerationID: 8, WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	base := testFinalTurn()
	base.Sequence = 5
	base.GenerationID = 9

	wrongSession := base
	wrongSession.SessionID = "voice_other"
	if err := worker.EnqueueAfterPlayback(wrongSession); !errors.Is(err, ErrInvalidFinalTurn) {
		t.Fatalf("wrong session accepted: %v", err)
	}
	wrongSequence := base
	wrongSequence.Sequence = 6
	if err := worker.EnqueueAfterPlayback(wrongSequence); !errors.Is(err, ErrFinalTurnSequence) {
		t.Fatalf("out-of-order sequence accepted: %v", err)
	}
	wrongGeneration := base
	wrongGeneration.GenerationID = 8
	if err := worker.EnqueueAfterPlayback(wrongGeneration); !errors.Is(err, ErrFinalTurnSequence) {
		t.Fatalf("non-monotonic generation accepted: %v", err)
	}
	if err := worker.EnqueueAfterPlayback(base); err != nil {
		t.Fatalf("valid turn: %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Close(closeCtx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := worker.EnqueueAfterPlayback(nextFinalTurn(base)); !errors.Is(err, ErrFinalTurnWorkerClosed) {
		t.Fatalf("closed worker accepted turn: %v", err)
	}
}

func TestFinalTurnWorkerReportsParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	worker, err := NewFinalTurnWorker(parent, &recordingFinalTurnWriter{}, FinalTurnWorkerConfig{
		SessionID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	cancelParent()
	select {
	case <-worker.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not observe parent cancellation")
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	if err := worker.Close(closeCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation was reported as success: %v", err)
	}
}

func TestFinalTurnWorkerRejectsTypedNilWriter(t *testing.T) {
	var writer finalTurnWriterFunc
	worker, err := NewFinalTurnWorker(context.Background(), writer, FinalTurnWorkerConfig{
		SessionID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST",
	})
	if worker != nil || !errors.Is(err, ErrInvalidFinalTurn) {
		t.Fatalf("NewFinalTurnWorker(typed nil) = (%v, %v), want ErrInvalidFinalTurn", worker, err)
	}
}

func nextFinalTurn(previous FinalTurn) FinalTurn {
	next := previous
	next.Sequence++
	next.GenerationID++
	next.Transcript = "next final transcript"
	next.Response = "next final response"
	next.CompletedAt = previous.CompletedAt.Add(time.Second)
	return next
}

type blockingFinalTurnWriter struct {
	started chan FinalTurn
	release chan struct{}

	mu    sync.Mutex
	turns []FinalTurn
}

func (writer *blockingFinalTurnWriter) PersistFinalTurn(ctx context.Context, turn FinalTurn) error {
	select {
	case writer.started <- turn:
	default:
	}
	select {
	case <-writer.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	writer.mu.Lock()
	writer.turns = append(writer.turns, turn)
	writer.mu.Unlock()
	return nil
}

func (writer *blockingFinalTurnWriter) sequences() []int64 {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	result := make([]int64, len(writer.turns))
	for index, turn := range writer.turns {
		result[index] = turn.Sequence
	}
	return result
}

func (writer *blockingFinalTurnWriter) turnsCopy() []FinalTurn {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return append([]FinalTurn(nil), writer.turns...)
}

type recordingFinalTurnWriter struct {
	mu     sync.Mutex
	turns  []FinalTurn
	errors []error
}

type finalTurnWriterFunc func(context.Context, FinalTurn) error

func (writer finalTurnWriterFunc) PersistFinalTurn(ctx context.Context, turn FinalTurn) error {
	return writer(ctx, turn)
}

func (writer *recordingFinalTurnWriter) PersistFinalTurn(_ context.Context, turn FinalTurn) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.turns = append(writer.turns, turn)
	index := len(writer.turns) - 1
	if index < len(writer.errors) {
		return writer.errors[index]
	}
	return nil
}

func (writer *recordingFinalTurnWriter) sequences() []int64 {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	result := make([]int64, len(writer.turns))
	for index, turn := range writer.turns {
		result[index] = turn.Sequence
	}
	return result
}
