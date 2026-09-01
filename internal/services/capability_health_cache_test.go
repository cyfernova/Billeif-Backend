package services

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCapabilityHealthCacheIsolatesBusinessObservations(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{
		MaxAge: time.Minute,
		Now:    func() time.Time { return now },
	})
	retryAt := now.Add(30 * time.Second)
	require.NoError(t, cache.Record("business-a", CapabilityRazorpay, CapabilityHealthObservation{
		Status: CapabilityProviderDegraded, ObservedAt: now, RetryAt: &retryAt,
		CustomerCode:   "provider_degraded",
		OperatorDetail: "secret-id=payments-prod account=acct_123 raw timeout",
	}))

	a, ok := cache.CustomerFact("business-a", CapabilityRazorpay)
	require.True(t, ok)
	require.Equal(t, CapabilityProviderDegraded, a.Status)
	require.Equal(t, now, *a.ObservedAt)
	require.Equal(t, retryAt, *a.RetryAt)
	require.False(t, a.Stale)
	require.Equal(t, "provider_degraded", a.Degradation.Code)
	require.NotContains(t, a.Degradation.Message, "acct_123")

	_, ok = cache.CustomerFact("business-b", CapabilityRazorpay)
	require.False(t, ok)

	operator, ok := cache.OperatorObservation("business-a", CapabilityRazorpay)
	require.True(t, ok)
	require.Contains(t, operator.OperatorDetail, "acct_123")
}

func TestCapabilityHealthCacheMarksOldObservationsStale(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{
		MaxAge: 5 * time.Minute,
		Now:    func() time.Time { return now },
	})
	require.NoError(t, cache.Record("business-a", CapabilityEmail, CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now.Add(-6 * time.Minute),
	}))

	fact, ok := cache.CustomerFact("business-a", CapabilityEmail)
	require.True(t, ok)
	require.True(t, fact.Stale)
	require.Equal(t, now.Add(-6*time.Minute), *fact.ObservedAt)
}

func TestCapabilityHealthCacheDoesNotReplaceNewerObservation(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	require.NoError(t, cache.Record("business-a", CapabilityRazorpay, CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now,
	}))
	require.NoError(t, cache.Record("business-a", CapabilityRazorpay, CapabilityHealthObservation{
		Status: CapabilityProviderUnavailable, ObservedAt: now.Add(-time.Minute),
	}))

	fact, ok := cache.CustomerFact("business-a", CapabilityRazorpay)
	require.True(t, ok)
	require.Equal(t, CapabilityProviderHealthy, fact.Status)
	require.Equal(t, now, *fact.ObservedAt)
}

func TestCapabilityHealthCacheEvictsOldestObservationAtCapacity(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{MaxEntries: 2, Now: func() time.Time { return now }})
	for index, businessID := range []string{"business-a", "business-b", "business-c"} {
		require.NoError(t, cache.Record(businessID, CapabilityEmail, CapabilityHealthObservation{
			Status: CapabilityProviderHealthy, ObservedAt: now.Add(time.Duration(index) * time.Second),
		}))
	}

	_, oldestPresent := cache.CustomerFact("business-a", CapabilityEmail)
	_, middlePresent := cache.CustomerFact("business-b", CapabilityEmail)
	_, newestPresent := cache.CustomerFact("business-c", CapabilityEmail)
	require.False(t, oldestPresent)
	require.True(t, middlePresent)
	require.True(t, newestPresent)
}

func TestCapabilityHealthCacheRetryAtIsMutationIsolatedOnRecordAndRead(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	originalRetry := now.Add(time.Minute)
	retryInput := originalRetry
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	require.NoError(t, cache.Record("business-a", CapabilityRazorpay, CapabilityHealthObservation{
		Status: CapabilityProviderUnavailable, ObservedAt: now, RetryAt: &retryInput,
	}))

	retryInput = now.Add(24 * time.Hour)
	fact, ok := cache.CustomerFact("business-a", CapabilityRazorpay)
	require.True(t, ok)
	require.Equal(t, originalRetry, *fact.RetryAt)
	*fact.RetryAt = now.Add(48 * time.Hour)
	operator, ok := cache.OperatorObservation("business-a", CapabilityRazorpay)
	require.True(t, ok)
	require.Equal(t, originalRetry, *operator.RetryAt)
	*operator.RetryAt = now.Add(72 * time.Hour)

	again, ok := cache.CustomerFact("business-a", CapabilityRazorpay)
	require.True(t, ok)
	require.Equal(t, originalRetry, *again.RetryAt)
}

func TestCapabilityHealthCacheConcurrentRecordAndReadOwnRetryAtValues(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	retryAt := now.Add(time.Minute)
	require.NoError(t, cache.Record("business-a", CapabilityRazorpay, CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now, RetryAt: &retryAt,
	}))

	var wait sync.WaitGroup
	errors := make(chan error, 32)
	for index := 0; index < 32; index++ {
		wait.Add(2)
		go func(index int) {
			defer wait.Done()
			observedAt := now.Add(time.Duration(index+1) * time.Second)
			retry := observedAt.Add(time.Minute)
			if err := cache.Record("business-a", CapabilityRazorpay, CapabilityHealthObservation{
				Status: CapabilityProviderHealthy, ObservedAt: observedAt, RetryAt: &retry,
				CustomerCode: fmt.Sprintf("healthy_%d", index),
			}); err != nil {
				errors <- err
			}
			retry = now.Add(24 * time.Hour)
		}(index)
		go func() {
			defer wait.Done()
			if fact, ok := cache.CustomerFact("business-a", CapabilityRazorpay); ok && fact.RetryAt != nil {
				*fact.RetryAt = now.Add(48 * time.Hour)
			}
			if observation, ok := cache.OperatorObservation("business-a", CapabilityRazorpay); ok && observation.RetryAt != nil {
				*observation.RetryAt = now.Add(72 * time.Hour)
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	fact, ok := cache.CustomerFact("business-a", CapabilityRazorpay)
	require.True(t, ok)
	require.NotNil(t, fact.RetryAt)
	require.NotEqual(t, now.Add(24*time.Hour), *fact.RetryAt)
	require.NotEqual(t, now.Add(48*time.Hour), *fact.RetryAt)
	require.NotEqual(t, now.Add(72*time.Hour), *fact.RetryAt)
}
