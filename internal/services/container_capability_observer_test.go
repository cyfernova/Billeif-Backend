package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockingCapabilityObserverRunner struct {
	started chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func (r *blockingCapabilityObserverRunner) Run(ctx context.Context, _ time.Duration) error {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	close(r.stopped)
	return ctx.Err()
}

func TestContainerStartsAndStopsCapabilityHealthObserver(t *testing.T) {
	runner := &blockingCapabilityObserverRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	container := &Container{capabilityObserver: &capabilityObserverState{runner: runner}}

	container.StartCapabilityHealthObservation()
	require.Eventually(t, func() bool {
		select {
		case <-runner.started:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	container.StopCapabilityHealthObservation()
	require.Eventually(t, func() bool {
		select {
		case <-runner.stopped:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}
