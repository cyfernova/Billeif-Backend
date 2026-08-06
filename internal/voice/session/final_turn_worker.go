package session

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"
)

const (
	defaultFinalTurnQueueCapacity = 4
	maxFinalTurnQueueCapacity     = 16
	defaultFinalTurnWriteTimeout  = 2 * time.Second
	maxFinalTurnWriteTimeout      = 10 * time.Second
	finalTurnWriteAttempts        = 2
)

var (
	ErrFinalTurnQueueFull    = errors.New("final voice turn queue is full")
	ErrFinalTurnWorkerClosed = errors.New("final voice turn worker is closed")
	ErrFinalTurnWorkerFailed = errors.New("final voice turn worker failed")
	ErrFinalTurnWrite        = errors.New("final voice turn persistence failed")
)

// FinalTurnSink is the narrow post-playback boundary used by a voice session.
// EnqueueAfterPlayback is nonblocking: durable I/O is owned by the worker.
type FinalTurnSink interface {
	EnqueueAfterPlayback(FinalTurn) error
}

type FinalTurnWorkerConfig struct {
	SessionID           string
	QueueCapacity       int
	WriteTimeout        time.Duration
	InitialSequence     int64
	InitialGenerationID int64
}

type FinalTurnWorker struct {
	writer       FinalTurnWriter
	writeTimeout time.Duration
	sessionID    string

	ctx    context.Context
	cancel context.CancelFunc
	jobs   chan FinalTurn
	errors chan error
	done   chan struct{}

	mu               sync.Mutex
	closed           bool
	failed           bool
	terminalErr      error
	lastSequence     int64
	lastGenerationID int64
}

func NewFinalTurnWorker(ctx context.Context, writer FinalTurnWriter, config FinalTurnWorkerConfig) (*FinalTurnWorker, error) {
	if ctx == nil || ctx.Err() != nil || nilFinalTurnWriter(writer) || !validSessionID(config.SessionID) ||
		config.InitialSequence < 0 || config.InitialGenerationID < 0 {
		return nil, ErrInvalidFinalTurn
	}
	capacity := config.QueueCapacity
	if capacity == 0 {
		capacity = defaultFinalTurnQueueCapacity
	}
	if capacity < 1 || capacity > maxFinalTurnQueueCapacity {
		return nil, ErrInvalidFinalTurn
	}
	writeTimeout := config.WriteTimeout
	if writeTimeout == 0 {
		writeTimeout = defaultFinalTurnWriteTimeout
	}
	if writeTimeout < time.Millisecond || writeTimeout > maxFinalTurnWriteTimeout {
		return nil, ErrInvalidFinalTurn
	}
	workerContext, cancel := context.WithCancel(ctx)
	worker := &FinalTurnWorker{
		writer: writer, writeTimeout: writeTimeout, sessionID: config.SessionID,
		ctx: workerContext, cancel: cancel, jobs: make(chan FinalTurn, capacity),
		errors: make(chan error, 1), done: make(chan struct{}),
		lastSequence: config.InitialSequence, lastGenerationID: config.InitialGenerationID,
	}
	go worker.run()
	return worker, nil
}

func (worker *FinalTurnWorker) EnqueueAfterPlayback(turn FinalTurn) error {
	if worker == nil {
		return ErrFinalTurnWorkerClosed
	}
	if err := validateFinalTurn(turn); err != nil || turn.SessionID != worker.sessionID {
		return ErrInvalidFinalTurn
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.failed {
		return ErrFinalTurnWorkerFailed
	}
	if worker.closed || worker.ctx.Err() != nil {
		return ErrFinalTurnWorkerClosed
	}
	if turn.Sequence != worker.lastSequence+1 || turn.GenerationID <= worker.lastGenerationID {
		return ErrFinalTurnSequence
	}
	turn = cloneFinalTurn(turn)
	select {
	case worker.jobs <- turn:
		worker.lastSequence = turn.Sequence
		worker.lastGenerationID = turn.GenerationID
		return nil
	default:
		return ErrFinalTurnQueueFull
	}
}

func cloneFinalTurn(turn FinalTurn) FinalTurn {
	turn.Tools = append([]FinalTurnToolOutcome(nil), turn.Tools...)
	turn.Usage.InputTokens = cloneInt64(turn.Usage.InputTokens)
	turn.Usage.OutputTokens = cloneInt64(turn.Usage.OutputTokens)
	turn.Usage.TotalTokens = cloneInt64(turn.Usage.TotalTokens)
	turn.Usage.TTSCharacters = cloneInt64(turn.Usage.TTSCharacters)
	return turn
}

func (worker *FinalTurnWorker) Errors() <-chan error {
	if worker == nil {
		closed := make(chan error)
		close(closed)
		return closed
	}
	return worker.errors
}

func (worker *FinalTurnWorker) Close(ctx context.Context) error {
	if worker == nil {
		return nil
	}
	if ctx == nil {
		return ErrFinalTurnWorkerClosed
	}
	worker.mu.Lock()
	if !worker.closed {
		worker.closed = true
		close(worker.jobs)
	}
	worker.mu.Unlock()

	select {
	case <-worker.done:
		worker.mu.Lock()
		defer worker.mu.Unlock()
		return worker.terminalErr
	case <-ctx.Done():
		worker.cancel()
		return ctx.Err()
	}
}

func (worker *FinalTurnWorker) run() {
	defer func() {
		worker.cancel()
		worker.mu.Lock()
		worker.closed = true
		worker.mu.Unlock()
		close(worker.errors)
		close(worker.done)
	}()
	for {
		select {
		case <-worker.ctx.Done():
			worker.mu.Lock()
			worker.terminalErr = worker.ctx.Err()
			worker.mu.Unlock()
			return
		case turn, ok := <-worker.jobs:
			if !ok {
				return
			}
			if err := worker.persist(turn); err != nil {
				worker.mu.Lock()
				worker.failed = true
				worker.terminalErr = ErrFinalTurnWrite
				worker.mu.Unlock()
				select {
				case worker.errors <- ErrFinalTurnWrite:
				default:
				}
				return
			}
		}
	}
}

func (worker *FinalTurnWorker) persist(turn FinalTurn) error {
	ctx, cancel := context.WithTimeout(worker.ctx, worker.writeTimeout)
	defer cancel()
	for attempt := 0; attempt < finalTurnWriteAttempts; attempt++ {
		if err := safePersistFinalTurn(ctx, worker.writer, turn); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return ErrFinalTurnWrite
}

func safePersistFinalTurn(ctx context.Context, writer FinalTurnWriter, turn FinalTurn) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrFinalTurnWrite
		}
	}()
	return writer.PersistFinalTurn(ctx, turn)
}

func nilFinalTurnWriter(writer FinalTurnWriter) bool {
	if writer == nil {
		return true
	}
	value := reflect.ValueOf(writer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ FinalTurnSink = (*FinalTurnWorker)(nil)
