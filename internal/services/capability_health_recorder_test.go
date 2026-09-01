package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCapabilityHealthRecorderClassifiesSanitizedProviderOutcomesPerTenant(t *testing.T) {
	now := time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	recorder := NewCapabilityHealthRecorder(cache, func() time.Time { return now })

	require.NoError(t, recorder.RecordOutcome("biz-success", CapabilityAI, CapabilityProviderOutcome{}))
	require.NoError(t, recorder.RecordOutcome("biz-rate", CapabilityAI, CapabilityProviderOutcome{HTTPStatus: 429, Err: errors.New("raw account acct_123 secret rate response")}))
	require.NoError(t, recorder.RecordOutcome("biz-unavailable", CapabilityAI, CapabilityProviderOutcome{HTTPStatus: 503, Err: errors.New("raw provider topology")}))
	require.NoError(t, recorder.RecordOutcome("biz-timeout", CapabilityAI, CapabilityProviderOutcome{Err: context.DeadlineExceeded}))

	success, ok := cache.CustomerFact("biz-success", CapabilityAI)
	require.True(t, ok)
	require.Equal(t, CapabilityProviderHealthy, success.Status)
	require.Nil(t, success.Degradation)

	rateLimited, ok := cache.CustomerFact("biz-rate", CapabilityAI)
	require.True(t, ok)
	require.Equal(t, CapabilityProviderDegraded, rateLimited.Status)
	require.Equal(t, "provider_rate_limited", rateLimited.Degradation.Code)
	require.Equal(t, now.Add(time.Minute), *rateLimited.RetryAt)

	for _, businessID := range []string{"biz-unavailable", "biz-timeout"} {
		fact, found := cache.CustomerFact(businessID, CapabilityAI)
		require.True(t, found)
		require.Equal(t, CapabilityProviderUnavailable, fact.Status)
		require.Equal(t, "provider_unavailable", fact.Degradation.Code)
		require.Equal(t, now.Add(30*time.Second), *fact.RetryAt)
	}

	_, ok = cache.CustomerFact("other-business", CapabilityAI)
	require.False(t, ok)
	for _, businessID := range []string{"biz-rate", "biz-unavailable"} {
		operator, found := cache.OperatorObservation(businessID, CapabilityAI)
		require.True(t, found)
		require.NotContains(t, operator.OperatorDetail, "acct_123")
		require.NotContains(t, operator.OperatorDetail, "topology")
	}
}
