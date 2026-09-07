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
}

type providerHTTPError struct {
	status int
}

func (e *providerHTTPError) Error() string       { return "provider request failed" }
func (e *providerHTTPError) HTTPStatusCode() int { return e.status }

type CapabilityGlobalOutcomeRecorder interface {
	RecordGlobalOutcome(capability CapabilityKey, outcome CapabilityProviderOutcome) error
}

type CapabilityGlobalHealthRecorder struct {
	cache *CapabilityGlobalHealthCache
	now   func() time.Time
}

func NewCapabilityGlobalHealthRecorder(cache *CapabilityGlobalHealthCache, now func() time.Time) *CapabilityGlobalHealthRecorder {
	if now == nil {
		now = time.Now
	}
	return &CapabilityGlobalHealthRecorder{cache: cache, now: now}
}

func (r *CapabilityGlobalHealthRecorder) RecordGlobalOutcome(capability CapabilityKey, outcome CapabilityProviderOutcome) error {
	if r == nil || r.cache == nil {
		return errors.New("global capability health cache is required")
	}
	return r.cache.Record(capability, capabilityHealthObservationFromOutcome(outcome, r.now().UTC()))
}

func capabilityHealthObservationFromOutcome(outcome CapabilityProviderOutcome, now time.Time) CapabilityHealthObservation {
	outcome.HTTPStatus = providerOutcomeHTTPStatus(outcome)
	observation := CapabilityHealthObservation{Status: CapabilityProviderHealthy, ObservedAt: now}
	switch {
	case outcome.HTTPStatus == 429:
		observation.Status = CapabilityProviderDegraded
		observation.CustomerCode = "provider_rate_limited"
		retryAt := now.Add(time.Minute)
		observation.RetryAt = &retryAt
	case providerOutcomeTimedOut(outcome.Err):
		observation.Status = CapabilityProviderUnavailable
		observation.CustomerCode = "provider_unavailable"
		retryAt := now.Add(30 * time.Second)
		observation.RetryAt = &retryAt
	case outcome.Err != nil || outcome.HTTPStatus >= 400:
		observation.Status = CapabilityProviderUnavailable
		observation.CustomerCode = "provider_unavailable"
		retryAt := now.Add(30 * time.Second)
		observation.RetryAt = &retryAt
	}
	return observation
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
