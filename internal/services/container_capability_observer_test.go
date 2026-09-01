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

type joinableCapabilityObserverRunner struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

type generationCapabilityObserverRunner struct {
	mu        sync.Mutex
	active    int
	maxActive int
	starts    int
	entered   chan int
}

type cancelAwareCapabilityProber struct {
	started chan struct{}
	exited  chan struct{}
}

func (p *cancelAwareCapabilityProber) ProbeGlobalCapability(ctx context.Context) CapabilityProviderOutcome {
	close(p.started)
	<-ctx.Done()
	close(p.exited)
	return CapabilityProviderOutcome{Err: ctx.Err()}
}

func (r *generationCapabilityObserverRunner) Run(ctx context.Context, _ time.Duration) error {
	r.mu.Lock()
	r.active++
	r.starts++
	generation := r.starts
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.mu.Unlock()
	r.entered <- generation
	<-ctx.Done()
	r.mu.Lock()
	r.active--
	r.mu.Unlock()
	return ctx.Err()
}

func (r *joinableCapabilityObserverRunner) Run(ctx context.Context, _ time.Duration) error {
	close(r.started)
	<-ctx.Done()
	close(r.canceled)
	<-r.release
	return ctx.Err()
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

func TestContainerStopCancelsAndJoinsObserverBeforeReturning(t *testing.T) {
	runner := &joinableCapabilityObserverRunner{started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	container := &Container{capabilityObserver: &capabilityObserverState{runner: runner}}
	container.StartCapabilityHealthObservation()
	<-runner.started
	returned := make(chan struct{})
	go func() {
		container.StopCapabilityHealthObservation()
		close(returned)
	}()
	<-runner.canceled
	select {
	case <-returned:
		t.Fatal("Stop returned before the in-flight observer exited")
	default:
	}
	close(runner.release)
	require.Eventually(t, func() bool {
		select {
		case <-returned:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestContainerRepeatedStartStopNeverOverlapsObserverGenerations(t *testing.T) {
	runner := &generationCapabilityObserverRunner{entered: make(chan int, 4)}
	container := &Container{capabilityObserver: &capabilityObserverState{runner: runner}}
	for generation := 1; generation <= 3; generation++ {
		container.StartCapabilityHealthObservation()
		require.Equal(t, generation, <-runner.entered)
		container.StartCapabilityHealthObservation()
		container.StopCapabilityHealthObservation()
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	require.Equal(t, 3, runner.starts)
	require.Equal(t, 1, runner.maxActive)
	require.Zero(t, runner.active)
}

func TestContainerStopJoinsConcreteGlobalObserverProbe(t *testing.T) {
	prober := &cancelAwareCapabilityProber{started: make(chan struct{}), exited: make(chan struct{})}
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{})
	observer := NewCapabilityGlobalHealthObserver(
		map[CapabilityKey]CapabilityGlobalProviderProber{CapabilityRazorpay: prober},
		NewCapabilityGlobalHealthRecorder(cache, nil), CapabilityGlobalHealthObserverOptions{MaxAttempts: 1},
	)
	container := &Container{capabilityObserver: &capabilityObserverState{runner: observer}}
	container.StartCapabilityHealthObservation()
	<-prober.started
	container.StopCapabilityHealthObservation()
	select {
	case <-prober.exited:
	default:
		t.Fatal("Stop returned before probe exited")
	}
	if _, found := cache.CustomerFact(CapabilityRazorpay); found {
		t.Fatal("shutdown cancellation must not become a provider-health observation")
	}
}
