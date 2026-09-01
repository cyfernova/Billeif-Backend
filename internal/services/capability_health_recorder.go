package services

import (
	"context"
	"errors"
	"net"
	"time"
)

type CapabilityProviderOutcome struct {
	HTTPStatus int
	Err        error
	FreshFor   time.Duration
}

type providerHTTPError struct {
	status int
}

func (e *providerHTTPError) Error() string       { return "provider request failed" }
func (e *providerHTTPError) HTTPStatusCode() int { return e.status }

type CapabilityOutcomeRecorder interface {
	RecordOutcome(businessID string, capability CapabilityKey, outcome CapabilityProviderOutcome) error
}

type CapabilityHealthRecorder struct {
	cache *CapabilityHealthCache
	now   func() time.Time
}

func NewCapabilityHealthRecorder(cache *CapabilityHealthCache, now func() time.Time) *CapabilityHealthRecorder {
	if now == nil {
		now = time.Now
	}
	return &CapabilityHealthRecorder{cache: cache, now: now}
}

func (r *CapabilityHealthRecorder) EnsureActiveCapacity(activeTargets int) {
	if r != nil && r.cache != nil {
		r.cache.EnsureActiveCapacity(activeTargets)
	}
}

func (r *CapabilityHealthRecorder) RecordOutcome(businessID string, capability CapabilityKey, outcome CapabilityProviderOutcome) error {
	outcome.HTTPStatus = providerOutcomeHTTPStatus(outcome)
	now := r.now().UTC()
	observation := CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now, FreshFor: outcome.FreshFor,
	}
	switch {
	case outcome.HTTPStatus == 429:
		observation.Status = CapabilityProviderDegraded
		observation.CustomerCode = "provider_rate_limited"
		observation.OperatorDetail = "provider_rate_limited"
		retryAt := now.Add(time.Minute)
		observation.RetryAt = &retryAt
	case providerOutcomeTimedOut(outcome.Err):
		observation.Status = CapabilityProviderUnavailable
		observation.CustomerCode = "provider_unavailable"
		observation.OperatorDetail = "provider_timeout"
		retryAt := now.Add(30 * time.Second)
		observation.RetryAt = &retryAt
	case outcome.Err != nil || outcome.HTTPStatus >= 400:
		observation.Status = CapabilityProviderUnavailable
		observation.CustomerCode = "provider_unavailable"
		observation.OperatorDetail = "provider_unavailable"
		retryAt := now.Add(30 * time.Second)
		observation.RetryAt = &retryAt
	}
	return r.cache.Record(businessID, capability, observation)
}

func providerOutcomeHTTPStatus(outcome CapabilityProviderOutcome) int {
	if outcome.HTTPStatus != 0 {
		return outcome.HTTPStatus
	}
	var statusError interface{ HTTPStatusCode() int }
	if errors.As(outcome.Err, &statusError) {
		return statusError.HTTPStatusCode()
	}
	return 0
}

func providerOutcomeTimedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}
