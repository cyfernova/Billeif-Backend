package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCapabilityGlobalHealthRecorderClassifiesSanitizedProviderOutcomes(t *testing.T) {
	now := time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)
	for _, fixture := range []struct {
		name       string
		outcome    CapabilityProviderOutcome
		wantStatus CapabilityProviderStatus
		wantCode   string
		wantRetry  *time.Time
	}{
		{name: "success", wantStatus: CapabilityProviderHealthy},
		{name: "rate limit", outcome: CapabilityProviderOutcome{HTTPStatus: 429, Err: errors.New("raw account acct_123 secret response")}, wantStatus: CapabilityProviderDegraded, wantCode: "provider_rate_limited", wantRetry: timePointer(now.Add(time.Minute))},
		{name: "unavailable", outcome: CapabilityProviderOutcome{HTTPStatus: 503, Err: errors.New("raw provider topology")}, wantStatus: CapabilityProviderUnavailable, wantCode: "provider_unavailable", wantRetry: timePointer(now.Add(30 * time.Second))},
		{name: "timeout", outcome: CapabilityProviderOutcome{Err: context.DeadlineExceeded}, wantStatus: CapabilityProviderUnavailable, wantCode: "provider_unavailable", wantRetry: timePointer(now.Add(30 * time.Second))},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})
			recorder := NewCapabilityGlobalHealthRecorder(cache, func() time.Time { return now })
			require.NoError(t, recorder.RecordGlobalOutcome(CapabilityAI, fixture.outcome))
			fact, found := cache.CustomerFact(CapabilityAI)
			require.True(t, found)
			require.Equal(t, fixture.wantStatus, fact.Status)
			require.Equal(t, fixture.wantRetry, fact.RetryAt)
			if fixture.wantCode == "" {
				require.Nil(t, fact.Degradation)
			} else {
				require.Equal(t, fixture.wantCode, fact.Degradation.Code)
			}
			operator, found := cache.OperatorObservation(CapabilityAI)
			require.True(t, found)
			stored := fmt.Sprintf("%#v", operator)
			require.NotContains(t, stored, "acct_123")
			require.NotContains(t, stored, "topology")
		})
	}
}
