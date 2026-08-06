package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShutdownRejectsNewWorkCancelsActiveInvocationAndClosesDependenciesOnce(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	closer := &countingCloser{}
	server := NewServer(Config{}, InvocationHandlerFunc(func(ctx context.Context, _ InvocationRequest) (InvocationResponse, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return InvocationResponse{}, ctx.Err()
	}), closer)

	invocationDone := make(chan int, 1)
	go func() {
		invocationDone <- invoke(t, server, `{}`, testSessionID, testAuthorization, "application/json").Code
	}()
	<-started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(shutdownCtx))
	assert.Equal(t, http.StatusInternalServerError, <-invocationDone)

	rejected := invoke(t, server, `{}`, testSessionID, testAuthorization, "application/json")
	assert.Equal(t, http.StatusServiceUnavailable, rejected.Code)
	assert.JSONEq(t, `{"error":"shutting down"}`, rejected.Body.String())
	assert.EqualValues(t, 1, calls.Load())

	require.NoError(t, server.Shutdown(shutdownCtx))
	assert.EqualValues(t, 1, closer.calls.Load())
	_, err := server.AcquireActivity()
	assert.ErrorIs(t, err, ErrShuttingDown)
}

func TestShutdownWaitsForActivityLeaseReleasedByInjectedCloser(t *testing.T) {
	var lease *ActivityLease
	server := NewServer(Config{}, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}), closeFunc(func() error { return lease.Close() }))
	var err error
	lease, err = server.AcquireActivity()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(ctx))
	assertPingStatus(t, server, StatusHealthy)
}

func TestShutdownHonorsDeadlineWhenActiveWorkDoesNotClose(t *testing.T) {
	server := NewServer(Config{}, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))
	lease, err := server.AcquireActivity()
	require.NoError(t, err)
	defer lease.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = server.Shutdown(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestShutdownInvokesEveryCloserEvenWhenAnotherCloserBlocks(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	second := &countingCloser{}
	server := NewServer(Config{}, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}), closeFunc(func() error {
		<-blocked
		return nil
	}), second)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := server.Shutdown(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Eventually(t, func() bool { return second.calls.Load() == 1 }, time.Second, time.Millisecond)
}

func TestConcurrentShutdownCallsCloseDependenciesOnce(t *testing.T) {
	closer := &countingCloser{}
	server := NewServer(Config{}, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}), closer, closer)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var group sync.WaitGroup
	errorsSeen := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsSeen <- server.Shutdown(ctx)
		}()
	}
	group.Wait()
	close(errorsSeen)

	for err := range errorsSeen {
		require.NoError(t, err)
	}
	assert.EqualValues(t, 2, closer.calls.Load(), "each registered closer must run exactly once")
}

func TestShutdownReturnsInjectedCloserErrorsWithoutLeakingRequestData(t *testing.T) {
	sentinel := errors.New("close failed")
	server := NewServer(Config{}, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}), closeFunc(func() error { return sentinel }))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := server.Shutdown(ctx)

	assert.ErrorIs(t, err, sentinel)
}

type countingCloser struct {
	calls atomic.Int32
}

func (c *countingCloser) Close() error {
	c.calls.Add(1)
	return nil
}

type closeFunc func() error

func (fn closeFunc) Close() error { return fn() }

var _ io.Closer = closeFunc(nil)
