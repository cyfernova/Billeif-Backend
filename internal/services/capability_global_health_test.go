package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/config"

	"github.com/stretchr/testify/require"
)

func TestCapabilityGlobalHealthCacheAcceptsOnlyStaticGlobalProviders(t *testing.T) {
	now := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC)
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{
		MaxAge: time.Minute,
		Now:    func() time.Time { return now },
	})
	retryAt := now.Add(30 * time.Second)
	require.NoError(t, cache.Record(CapabilityRazorpay, CapabilityHealthObservation{
		Status: CapabilityProviderDegraded, ObservedAt: now, RetryAt: &retryAt,
		CustomerCode: "provider_degraded",
	}))
	require.NoError(t, cache.Record(CapabilityAI, CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now,
	}))

	razorpay, found := cache.CustomerFact(CapabilityRazorpay)
	require.True(t, found)
	require.Equal(t, CapabilityProviderDegraded, razorpay.Status)
	require.Equal(t, retryAt, *razorpay.RetryAt)
	require.Equal(t, "provider_degraded", razorpay.Degradation.Code)

	err := cache.Record(CapabilityGSTProvider, CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now,
	})
	require.True(t, errors.Is(err, ErrCapabilityGlobalHealthScope))
	_, found = cache.CustomerFact(CapabilityGSTProvider)
	require.False(t, found)
}

func TestCapabilityGlobalHealthCacheDoesNotReplaceNewerObservation(t *testing.T) {
	now := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC)
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})
	require.NoError(t, cache.Record(CapabilityAI, CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now,
	}))
	require.NoError(t, cache.Record(CapabilityAI, CapabilityHealthObservation{
		Status: CapabilityProviderUnavailable, ObservedAt: now.Add(-time.Minute),
		CustomerCode: "provider_unavailable",
	}))

	fact, found := cache.CustomerFact(CapabilityAI)
	require.True(t, found)
	require.Equal(t, CapabilityProviderHealthy, fact.Status)
	require.Equal(t, now, *fact.ObservedAt)
}

func TestCapabilityGlobalHealthCacheRejectsFreeformProviderDetail(t *testing.T) {
	now := time.Date(2026, 9, 1, 18, 15, 0, 0, time.UTC)
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})

	err := cache.Record(CapabilityAI, CapabilityHealthObservation{
		Status:       CapabilityProviderUnavailable,
		ObservedAt:   now,
		CustomerCode: "raw account acct_123 credential detail",
	})

	require.Error(t, err)
	_, found := cache.OperatorObservation(CapabilityAI)
	require.False(t, found)
}

func TestCapabilityGlobalHealthCacheRetryAtIsMutationIsolated(t *testing.T) {
	now := time.Date(2026, 9, 1, 18, 20, 0, 0, time.UTC)
	originalRetry := now.Add(time.Minute)
	retryInput := originalRetry
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})
	require.NoError(t, cache.Record(CapabilityRazorpay, CapabilityHealthObservation{
		Status: CapabilityProviderUnavailable, ObservedAt: now, RetryAt: &retryInput,
		CustomerCode: "provider_unavailable",
	}))

	retryInput = now.Add(24 * time.Hour)
	fact, found := cache.CustomerFact(CapabilityRazorpay)
	require.True(t, found)
	require.Equal(t, originalRetry, *fact.RetryAt)
	*fact.RetryAt = now.Add(48 * time.Hour)
	observation, found := cache.OperatorObservation(CapabilityRazorpay)
	require.True(t, found)
	require.Equal(t, originalRetry, *observation.RetryAt)
	*observation.RetryAt = now.Add(72 * time.Hour)

	again, found := cache.CustomerFact(CapabilityRazorpay)
	require.True(t, found)
	require.Equal(t, originalRetry, *again.RetryAt)
}

func TestCapabilityGlobalHealthCacheConcurrentRecordAndReadOwnRetryAtValues(t *testing.T) {
	now := time.Date(2026, 9, 1, 18, 25, 0, 0, time.UTC)
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})
	retryAt := now.Add(time.Minute)
	require.NoError(t, cache.Record(CapabilityAI, CapabilityHealthObservation{
		Status: CapabilityProviderUnavailable, ObservedAt: now, RetryAt: &retryAt,
		CustomerCode: "provider_unavailable",
	}))

	var wait sync.WaitGroup
	errorsOut := make(chan error, 32)
	for index := 0; index < 32; index++ {
		wait.Add(2)
		go func(index int) {
			defer wait.Done()
			observedAt := now.Add(time.Duration(index+1) * time.Second)
			retry := observedAt.Add(time.Minute)
			if err := cache.Record(CapabilityAI, CapabilityHealthObservation{
				Status: CapabilityProviderUnavailable, ObservedAt: observedAt, RetryAt: &retry,
				CustomerCode: "provider_unavailable",
			}); err != nil {
				errorsOut <- err
			}
			retry = now.Add(24 * time.Hour)
		}(index)
		go func() {
			defer wait.Done()
			if fact, ok := cache.CustomerFact(CapabilityAI); ok && fact.RetryAt != nil {
				*fact.RetryAt = now.Add(48 * time.Hour)
			}
			if observation, ok := cache.OperatorObservation(CapabilityAI); ok && observation.RetryAt != nil {
				*observation.RetryAt = now.Add(72 * time.Hour)
			}
		}()
	}
	wait.Wait()
	close(errorsOut)
	for err := range errorsOut {
		require.NoError(t, err)
	}

	fact, found := cache.CustomerFact(CapabilityAI)
	require.True(t, found)
	require.NotNil(t, fact.RetryAt)
	require.NotEqual(t, now.Add(24*time.Hour), *fact.RetryAt)
	require.NotEqual(t, now.Add(48*time.Hour), *fact.RetryAt)
	require.NotEqual(t, now.Add(72*time.Hour), *fact.RetryAt)
}

type recordingGlobalCapabilityProber struct {
	mu      sync.Mutex
	calls   int
	outcome CapabilityProviderOutcome
}

type boundedGlobalCapabilityProber struct {
	mu        sync.Mutex
	active    int
	maxActive int
	calls     int
	delay     time.Duration
}

func (p *boundedGlobalCapabilityProber) ProbeGlobalCapability(ctx context.Context) CapabilityProviderOutcome {
	p.mu.Lock()
	p.active++
	p.calls++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(p.delay):
	}
	p.mu.Lock()
	p.active--
	p.mu.Unlock()
	return CapabilityProviderOutcome{}
}

type sequenceGlobalCapabilityProber struct {
	mu       sync.Mutex
	outcomes []CapabilityProviderOutcome
	calls    int
}

func (p *sequenceGlobalCapabilityProber) ProbeGlobalCapability(context.Context) CapabilityProviderOutcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	index := p.calls
	p.calls++
	if index >= len(p.outcomes) {
		return p.outcomes[len(p.outcomes)-1]
	}
	return p.outcomes[index]
}

type failingGlobalOutcomeRecorder struct{ err error }

func (r failingGlobalOutcomeRecorder) RecordGlobalOutcome(CapabilityKey, CapabilityProviderOutcome) error {
	return r.err
}

var _ CapabilityGlobalProviderProber = (*LLMService)(nil)
var _ CapabilityGlobalProviderProber = (*RazorpayPaymentService)(nil)

func (p *recordingGlobalCapabilityProber) ProbeGlobalCapability(context.Context) CapabilityProviderOutcome {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.outcome
}

func (p *recordingGlobalCapabilityProber) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestCapabilityGlobalHealthObserverProbesEachStaticGlobalProviderOncePerCycle(t *testing.T) {
	now := time.Date(2026, 9, 1, 18, 30, 0, 0, time.UTC)
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})
	razorpay := &recordingGlobalCapabilityProber{}
	ai := &recordingGlobalCapabilityProber{outcome: CapabilityProviderOutcome{Err: ErrCapabilityProbeUnsupported}}
	gst := &recordingGlobalCapabilityProber{}
	observer := NewCapabilityGlobalHealthObserver(
		map[CapabilityKey]CapabilityGlobalProviderProber{
			CapabilityRazorpay:    razorpay,
			CapabilityAI:          ai,
			CapabilityGSTProvider: gst,
		},
		NewCapabilityGlobalHealthRecorder(cache, func() time.Time { return now }),
		CapabilityGlobalHealthObserverOptions{MaxAttempts: 1},
	)

	require.NoError(t, observer.ObserveOnce(context.Background()))
	require.Equal(t, 1, razorpay.callCount())
	require.Equal(t, 1, ai.callCount())
	require.Zero(t, gst.callCount(), "tenant-specific GST must never enter the global observer")
	razorpayFact, found := cache.CustomerFact(CapabilityRazorpay)
	require.True(t, found)
	require.Equal(t, CapabilityProviderHealthy, razorpayFact.Status)
	_, found = cache.CustomerFact(CapabilityAI)
	require.False(t, found, "unsupported global probes must leave health unknown")
}

func TestCapabilityGlobalHealthObserverBoundsConcurrencyAndRetries(t *testing.T) {
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{})
	shared := &boundedGlobalCapabilityProber{delay: 10 * time.Millisecond}
	observer := NewCapabilityGlobalHealthObserver(
		map[CapabilityKey]CapabilityGlobalProviderProber{
			CapabilityRazorpay: shared,
			CapabilityAI:       shared,
		},
		NewCapabilityGlobalHealthRecorder(cache, nil),
		CapabilityGlobalHealthObserverOptions{Concurrency: 2, MaxAttempts: 1, ProbeTimeout: time.Second},
	)
	require.NoError(t, observer.ObserveOnce(context.Background()))
	shared.mu.Lock()
	require.Equal(t, 2, shared.calls)
	require.LessOrEqual(t, shared.maxActive, 2)
	shared.mu.Unlock()

	transient := &sequenceGlobalCapabilityProber{outcomes: []CapabilityProviderOutcome{
		{Err: errors.New("transient raw network detail")},
		{},
	}}
	retryObserver := NewCapabilityGlobalHealthObserver(
		map[CapabilityKey]CapabilityGlobalProviderProber{CapabilityAI: transient},
		NewCapabilityGlobalHealthRecorder(cache, nil),
		CapabilityGlobalHealthObserverOptions{MaxAttempts: 2, RetryDelay: time.Millisecond, ProbeTimeout: time.Second},
	)
	require.NoError(t, retryObserver.ObserveOnce(context.Background()))
	require.Equal(t, 2, transient.calls)
}

func TestCapabilityGlobalHealthObserverDoesNotRetryTypedRateLimit(t *testing.T) {
	now := time.Date(2026, 9, 1, 18, 45, 0, 0, time.UTC)
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})
	prober := &recordingGlobalCapabilityProber{outcome: CapabilityProviderOutcome{
		Err: &providerHTTPError{status: http.StatusTooManyRequests},
	}}
	observer := NewCapabilityGlobalHealthObserver(
		map[CapabilityKey]CapabilityGlobalProviderProber{CapabilityRazorpay: prober},
		NewCapabilityGlobalHealthRecorder(cache, func() time.Time { return now }),
		CapabilityGlobalHealthObserverOptions{MaxAttempts: 2, RetryDelay: time.Millisecond, ProbeTimeout: time.Second},
	)

	require.NoError(t, observer.ObserveOnce(context.Background()))
	require.Equal(t, 1, prober.callCount())
	fact, found := cache.CustomerFact(CapabilityRazorpay)
	require.True(t, found)
	require.Equal(t, CapabilityProviderDegraded, fact.Status)
	require.Equal(t, "provider_rate_limited", fact.Degradation.Code)
}

func TestCapabilityGlobalHealthObserverReportsSanitizedCycleIssuesWithoutBusyLoop(t *testing.T) {
	issues := make(chan CapabilityHealthCycleIssue, 4)
	observer := NewCapabilityGlobalHealthObserver(
		map[CapabilityKey]CapabilityGlobalProviderProber{CapabilityAI: &recordingGlobalCapabilityProber{}},
		failingGlobalOutcomeRecorder{err: errors.New("database credential raw secret")},
		CapabilityGlobalHealthObserverOptions{OnCycleIssue: func(issue CapabilityHealthCycleIssue) { issues <- issue }},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- observer.Run(ctx, 25*time.Millisecond) }()
	first := <-issues
	second := <-issues
	require.Equal(t, "observation_record_failed", first.Code)
	require.Equal(t, first.Code, second.Code)
	require.NotContains(t, fmt.Sprintf("%#v", first), "credential")
	require.NotContains(t, fmt.Sprintf("%#v", first), "raw secret")
	require.GreaterOrEqual(t, time.Since(started), 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestCapabilityGlobalHealthObserverRunCannotExceedDeclaredRefreshInterval(t *testing.T) {
	prober := &recordingGlobalCapabilityProber{}
	observer := NewCapabilityGlobalHealthObserver(
		map[CapabilityKey]CapabilityGlobalProviderProber{CapabilityRazorpay: prober},
		NewCapabilityGlobalHealthRecorder(NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{}), nil),
		CapabilityGlobalHealthObserverOptions{RefreshInterval: 10 * time.Millisecond, MaxAttempts: 1},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- observer.Run(ctx, time.Hour) }()
	require.Eventually(t, func() bool { return prober.callCount() >= 2 }, time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

type businessScopedCapabilityPermissions struct{}

func (businessScopedCapabilityPermissions) UserHasPermission(_ context.Context, _ string, businessID, _ string) bool {
	return businessID == "business-a"
}

func TestCapabilityServiceCombinesGlobalProviderHealthWithBusinessFacts(t *testing.T) {
	now := time.Date(2026, 9, 1, 19, 0, 0, 0, time.UTC)
	global := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})
	require.NoError(t, global.Record(CapabilityRazorpay, CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now,
	}))
	service := NewCapabilityService(CapabilityServiceOptions{
		Configuration: config.CapabilityConfiguration{Razorpay: true},
		Permissions:   businessScopedCapabilityPermissions{},
		Setup:         staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}},
		GlobalHealth:  global,
		Now:           func() time.Time { return now },
	})

	available, err := service.Evaluate(context.Background(), CapabilityRequest{
		BusinessID: "business-a", UserID: "user-a", Capability: CapabilityRazorpay,
	})
	require.NoError(t, err)
	require.True(t, available.Available)
	require.Equal(t, CapabilityProviderHealthy, available.ProviderHealth.Status)

	denied, err := service.Evaluate(context.Background(), CapabilityRequest{
		BusinessID: "business-b", UserID: "user-b", Capability: CapabilityRazorpay,
	})
	require.NoError(t, err)
	require.False(t, denied.Available)
	require.Equal(t, CapabilityStatePermissionDenied, denied.State)
	require.Equal(t, CapabilityProviderHealthy, denied.ProviderHealth.Status)

	razorpayDefinition, found := capabilityDefinitionByKey(CapabilityRazorpay)
	require.True(t, found)
	require.Equal(t, CapabilityHealthScopeGlobal, razorpayDefinition.HealthScope)
	gstDefinition, found := capabilityDefinitionByKey(CapabilityGSTProvider)
	require.True(t, found)
	require.Equal(t, CapabilityHealthScopeBusiness, gstDefinition.HealthScope)
}

func TestEveryCapabilityDefinitionDeclaresItsProviderHealthScope(t *testing.T) {
	want := map[CapabilityKey]CapabilityHealthScope{
		CapabilityRazorpay:           CapabilityHealthScopeGlobal,
		CapabilityGSTProvider:        CapabilityHealthScopeBusiness,
		CapabilityEInvoice:           CapabilityHealthScopeBusiness,
		CapabilityEWayBill:           CapabilityHealthScopeBusiness,
		CapabilityWhatsApp:           CapabilityHealthScopeUnobserved,
		CapabilityEmail:              CapabilityHealthScopeUnobserved,
		CapabilityS3Uploads:          CapabilityHealthScopeUnobserved,
		CapabilityVoice:              CapabilityHealthScopeUnobserved,
		CapabilityAI:                 CapabilityHealthScopeGlobal,
		CapabilityStorefrontPayments: CapabilityHealthScopeGlobal,
		CapabilityReportExports:      CapabilityHealthScopeUnobserved,
		CapabilityBulkImports:        CapabilityHealthScopeUnobserved,
		CapabilitySavedPayments:      CapabilityHealthScopeUnobserved,
	}
	require.Len(t, capabilityDefinitions, len(want))
	for _, definition := range capabilityDefinitions {
		require.Equal(t, want[definition.Key], definition.HealthScope, "capability %s", definition.Key)
	}
}
