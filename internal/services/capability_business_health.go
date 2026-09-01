package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

const defaultGSTProviderHealthFreshFor = 24 * time.Hour

type CapabilityBusinessHealthFactReader interface {
	CustomerFact(ctx context.Context, businessID string, capability CapabilityKey) (CapabilityProviderHealth, bool, error)
}

type CapabilityBusinessHealthReader struct {
	repository interfaces.CapabilityProviderHealthRepository
	now        func() time.Time
}

func NewCapabilityBusinessHealthReader(
	repository interfaces.CapabilityProviderHealthRepository,
	now func() time.Time,
) *CapabilityBusinessHealthReader {
	if now == nil {
		now = time.Now
	}
	return &CapabilityBusinessHealthReader{repository: repository, now: now}
}

func (r *CapabilityBusinessHealthReader) CustomerFact(
	ctx context.Context,
	businessID string,
	capability CapabilityKey,
) (CapabilityProviderHealth, bool, error) {
	if capability != CapabilityGSTProvider || r == nil || r.repository == nil {
		return CapabilityProviderHealth{Status: CapabilityProviderUnknown}, false, nil
	}
	snapshot, err := r.repository.Get(ctx, strings.TrimSpace(businessID), string(CapabilityGSTProvider))
	if errors.Is(err, interfaces.ErrCapabilityProviderHealthNotFound) {
		return CapabilityProviderHealth{Status: CapabilityProviderUnknown}, false, nil
	}
	if err != nil {
		return CapabilityProviderHealth{}, false, fmt.Errorf("read business provider health: %w", err)
	}
	observation := CapabilityHealthObservation{
		Status:       CapabilityProviderStatus(snapshot.Status),
		ObservedAt:   snapshot.ObservedAt,
		RetryAt:      snapshot.RetryAt,
		CustomerCode: snapshot.CustomerCode,
	}
	if err := validateCapabilityHealthObservation(observation); err != nil {
		return CapabilityProviderHealth{}, false, fmt.Errorf("read invalid business provider health: %w", err)
	}
	fact := capabilityCustomerHealth(observation, r.now().UTC(), snapshot.FreshUntil.Sub(snapshot.ObservedAt))
	if snapshot.FreshUntil.Before(snapshot.ObservedAt) {
		return CapabilityProviderHealth{}, false, errors.New("business provider health freshness is invalid")
	}
	fact.Stale = r.now().UTC().After(snapshot.FreshUntil.UTC())
	return fact, true, nil
}

type GSTProviderHealthOutcomeRecorder interface {
	RecordGSTOutcome(ctx context.Context, businessID string, outcome CapabilityProviderOutcome) error
	ClearGSTOutcome(ctx context.Context, businessID string) error
}

type GSTProviderHealthRecorderOptions struct {
	Now      func() time.Time
	FreshFor time.Duration
}

type GSTProviderHealthRecorder struct {
	repository interfaces.CapabilityProviderHealthRepository
	now        func() time.Time
	freshFor   time.Duration
}

func NewGSTProviderHealthRecorder(
	repository interfaces.CapabilityProviderHealthRepository,
	options GSTProviderHealthRecorderOptions,
) *GSTProviderHealthRecorder {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.FreshFor <= 0 {
		options.FreshFor = defaultGSTProviderHealthFreshFor
	}
	return &GSTProviderHealthRecorder{repository: repository, now: options.Now, freshFor: options.FreshFor}
}

func (r *GSTProviderHealthRecorder) RecordGSTOutcome(
	ctx context.Context,
	businessID string,
	outcome CapabilityProviderOutcome,
) error {
	if r == nil || r.repository == nil || strings.TrimSpace(businessID) == "" {
		return interfaces.ErrCapabilityProviderHealthScope
	}
	now := r.now().UTC()
	observation := capabilityHealthObservationFromOutcome(outcome, now)
	return r.repository.UpsertMonotonic(ctx, &models.CapabilityProviderHealthSnapshot{
		BusinessID: strings.TrimSpace(businessID), ProviderKey: string(CapabilityGSTProvider),
		Status: string(observation.Status), ObservedAt: now, FreshUntil: now.Add(r.freshFor),
		RetryAt: cloneCapabilityTime(observation.RetryAt), CustomerCode: observation.CustomerCode,
		CreatedAt: now, UpdatedAt: now,
	})
}

func (r *GSTProviderHealthRecorder) ClearGSTOutcome(ctx context.Context, businessID string) error {
	if r == nil || r.repository == nil || strings.TrimSpace(businessID) == "" {
		return interfaces.ErrCapabilityProviderHealthScope
	}
	return r.repository.Clear(ctx, strings.TrimSpace(businessID), string(CapabilityGSTProvider))
}
