package services

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrCapabilityProbeUnsupported = errors.New("capability provider probe is unsupported")

type CapabilityHealthCycleIssue struct {
	Code string
}

type capabilityHealthCycleError struct {
	code  string
	cause error
}

func (e *capabilityHealthCycleError) Error() string { return e.code }
func (e *capabilityHealthCycleError) Unwrap() error { return e.cause }

func newCapabilityHealthCycleError(code string, cause error) error {
	return &capabilityHealthCycleError{code: code, cause: cause}
}

type CapabilityGlobalProviderProber interface {
	ProbeGlobalCapability(ctx context.Context) CapabilityProviderOutcome
}

type CapabilityGlobalHealthObserverOptions struct {
	Concurrency      int
	ProbeTimeout     time.Duration
	MaxAttempts      int
	RetryDelay       time.Duration
	RefreshInterval  time.Duration
	MaxCycleDuration time.Duration
	OnCycleIssue     func(CapabilityHealthCycleIssue)
}

type CapabilityGlobalHealthObserver struct {
	probers  map[CapabilityKey]CapabilityGlobalProviderProber
	keys     []CapabilityKey
	recorder CapabilityGlobalOutcomeRecorder
	options  CapabilityGlobalHealthObserverOptions
}

func NewCapabilityGlobalHealthObserver(
	probers map[CapabilityKey]CapabilityGlobalProviderProber,
	recorder CapabilityGlobalOutcomeRecorder,
	options CapabilityGlobalHealthObserverOptions,
) *CapabilityGlobalHealthObserver {
	if options.Concurrency <= 0 || options.Concurrency > 2 {
		options.Concurrency = 2
	}
	if options.ProbeTimeout <= 0 || options.ProbeTimeout > 30*time.Second {
		options.ProbeTimeout = 5 * time.Second
	}
	if options.MaxAttempts <= 0 || options.MaxAttempts > 3 {
		options.MaxAttempts = 2
	}
	if options.RetryDelay < 0 || options.RetryDelay > time.Second {
		options.RetryDelay = 100 * time.Millisecond
	}
	if options.RefreshInterval <= 0 {
		options.RefreshInterval = 2 * time.Minute
	}
	if options.MaxCycleDuration <= 0 || options.MaxCycleDuration > time.Minute {
		options.MaxCycleDuration = 30 * time.Second
	}
	accepted := make(map[CapabilityKey]CapabilityGlobalProviderProber, 2)
	keys := make([]CapabilityKey, 0, 2)
	for _, key := range []CapabilityKey{CapabilityRazorpay, CapabilityAI} {
		if prober := probers[key]; prober != nil {
			accepted[key] = prober
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return &CapabilityGlobalHealthObserver{probers: accepted, keys: keys, recorder: recorder, options: options}
}

func (o *CapabilityGlobalHealthObserver) ObserveOnce(ctx context.Context) error {
	if o == nil || o.recorder == nil || len(o.keys) == 0 {
		return nil
	}
	cycleCtx, cancel := context.WithTimeout(ctx, o.options.MaxCycleDuration)
	defer cancel()
	jobs := make(chan CapabilityKey)
	errorsOut := make(chan error, len(o.keys))
	workerCount := min(o.options.Concurrency, len(o.keys))
	var wait sync.WaitGroup
	for index := 0; index < workerCount; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for key := range jobs {
				if err := o.observeProvider(cycleCtx, key); err != nil {
					errorsOut <- err
				}
			}
		}()
	}
	for _, key := range o.keys {
		select {
		case <-cycleCtx.Done():
			close(jobs)
			wait.Wait()
			return newCapabilityHealthCycleError("cycle_deadline_exceeded", cycleCtx.Err())
		case jobs <- key:
		}
	}
	close(jobs)
	wait.Wait()
	close(errorsOut)
	for observeErr := range errorsOut {
		return observeErr
	}
	return nil
}

func (o *CapabilityGlobalHealthObserver) observeProvider(ctx context.Context, key CapabilityKey) error {
	prober := o.probers[key]
	if prober == nil {
		return nil
	}
	var outcome CapabilityProviderOutcome
	for attempt := 1; attempt <= o.options.MaxAttempts; attempt++ {
		probeCtx, cancel := context.WithTimeout(ctx, o.options.ProbeTimeout)
		outcome = prober.ProbeGlobalCapability(probeCtx)
		cancel()
		if outcome.Err == nil || providerOutcomeHTTPStatus(outcome) != 0 || errors.Is(outcome.Err, ErrCapabilityProbeUnsupported) {
			break
		}
		if attempt < o.options.MaxAttempts && o.options.RetryDelay > 0 {
			timer := time.NewTimer(o.options.RetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if ctx.Err() != nil {
		return newCapabilityHealthCycleError("cycle_deadline_exceeded", ctx.Err())
	}
	if errors.Is(outcome.Err, ErrCapabilityProbeUnsupported) {
		return nil
	}
	if err := o.recorder.RecordGlobalOutcome(key, outcome); err != nil {
		return newCapabilityHealthCycleError("observation_record_failed", err)
	}
	return nil
}

func (o *CapabilityGlobalHealthObserver) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = o.options.RefreshInterval
	}
	if interval > o.options.RefreshInterval {
		interval = o.options.RefreshInterval
	}
	for {
		if err := o.ObserveOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if o.options.OnCycleIssue != nil {
				code := "observation_cycle_failed"
				var cycleError *capabilityHealthCycleError
				if errors.As(err, &cycleError) {
					code = cycleError.code
				}
				o.options.OnCycleIssue(CapabilityHealthCycleIssue{Code: code})
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
