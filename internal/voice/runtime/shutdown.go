package runtime

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// ActivityLease keeps /ping in HealthyBusy while owned. Close is idempotent.
type ActivityLease struct {
	once    sync.Once
	release func()
}

func (lease *ActivityLease) Close() error {
	if lease == nil {
		return nil
	}
	lease.once.Do(lease.release)
	return nil
}

type activityTracker struct {
	mutex   sync.Mutex
	count   int
	closing bool
	idle    chan struct{}
}

func newActivityTracker() *activityTracker {
	idle := make(chan struct{})
	close(idle)
	return &activityTracker{idle: idle}
}

func (tracker *activityTracker) acquire() (*ActivityLease, error) {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	if tracker.closing {
		return nil, ErrShuttingDown
	}
	if tracker.count == 0 {
		tracker.idle = make(chan struct{})
	}
	tracker.count++
	return &ActivityLease{release: tracker.release}, nil
}

func (tracker *activityTracker) release() {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	if tracker.count == 0 {
		return
	}
	tracker.count--
	if tracker.count == 0 {
		close(tracker.idle)
	}
}

func (tracker *activityTracker) active() int {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	return tracker.count
}

func (tracker *activityTracker) stop() <-chan struct{} {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	tracker.closing = true
	return tracker.idle
}

type shutdownState struct {
	started atomic.Bool
	once    sync.Once

	closers     []io.Closer
	closersDone chan struct{}
	closerError error
	idle        <-chan struct{}
}

func newShutdownState(closers []io.Closer) shutdownState {
	return shutdownState{
		closers:     append([]io.Closer(nil), closers...),
		closersDone: make(chan struct{}),
	}
}

// AcquireActivity obtains a lease for a live peer, provider call, background
// operation, or invocation. New leases are rejected after shutdown begins.
func (s *Server) AcquireActivity() (*ActivityLease, error) {
	return s.activities.acquire()
}

// Shutdown rejects new invocations, cancels request contexts, calls every
// injected closer once, and waits for activity to drain until ctx expires.
func (s *Server) Shutdown(ctx context.Context) error {
	s.shutdown.once.Do(func() {
		s.shutdown.started.Store(true)
		s.cancelRoot()
		s.shutdown.idle = s.activities.stop()
		go s.closeDependencies()
	})

	if err := waitFor(ctx, s.shutdown.closersDone); err != nil {
		return err
	}
	if err := waitFor(ctx, s.shutdown.idle); err != nil {
		return errors.Join(s.shutdown.closerError, err)
	}
	return s.shutdown.closerError
}

func (s *Server) closeDependencies() {
	defer close(s.shutdown.closersDone)
	closerErrors := make(chan error, len(s.shutdown.closers))
	var group sync.WaitGroup
	for _, closer := range s.shutdown.closers {
		if closer == nil {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			if err := closer.Close(); err != nil {
				closerErrors <- err
			}
		}()
	}
	group.Wait()
	close(closerErrors)
	var collected []error
	for err := range closerErrors {
		collected = append(collected, err)
	}
	s.shutdown.closerError = errors.Join(collected...)
}

func waitFor(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
