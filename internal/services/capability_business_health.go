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

type CapabilityBusinessHealthAccountFactReader interface {
	CustomerFactForAccount(ctx context.Context, businessID string, capability CapabilityKey, integrationAccountID, serviceType string) (CapabilityProviderHealth, bool, error)
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

func (r *CapabilityBusinessHealthReader) CustomerFactForAccount(
	ctx context.Context,
	businessID string,
	capability CapabilityKey,
	integrationAccountID, _ string,
) (CapabilityProviderHealth, bool, error) {
	if strings.TrimSpace(integrationAccountID) == "" {
		return r.CustomerFact(ctx, businessID, capability)
	}
	snapshot, err := r.repository.Get(ctx, strings.TrimSpace(businessID), string(capability))
	if errors.Is(err, interfaces.ErrCapabilityProviderHealthNotFound) {
		return CapabilityProviderHealth{Status: CapabilityProviderUnknown}, false, nil
	}
	if err != nil {
		return CapabilityProviderHealth{}, false, fmt.Errorf("read business provider health: %w", err)
	}
	if snapshot.IntegrationAccountID != strings.TrimSpace(integrationAccountID) {
		return CapabilityProviderHealth{Status: CapabilityProviderUnknown}, false, nil
	}
	return r.CustomerFact(ctx, businessID, capability)
}

type GSTProviderHealthOutcomeRecorder interface {
	RecordGSTOutcome(ctx context.Context, businessID, accountID string, credentialRevision int64, outcome CapabilityProviderOutcome) (int64, error)
	RecordGSTValidationOutcome(ctx context.Context, businessID, accountID string, credentialRevision int64, outcome CapabilityProviderOutcome, state interfaces.GSTIntegrationValidationState) (int64, error)
	SaveGSTIntegrationAccountAndInvalidate(ctx context.Context, account *models.GSTIntegrationAccount, expectedCredentialRevision int64) error
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
	businessID, accountID string,
	credentialRevision int64,
	outcome CapabilityProviderOutcome,
) (int64, error) {
	snapshot, err := r.snapshotFromOutcome(businessID, accountID, credentialRevision, outcome)
	if err != nil {
		return 0, err
	}
	return r.repository.RecordRevisionBound(ctx, snapshot)
}

func (r *GSTProviderHealthRecorder) RecordGSTValidationOutcome(
	ctx context.Context,
	businessID, accountID string,
	credentialRevision int64,
	outcome CapabilityProviderOutcome,
	state interfaces.GSTIntegrationValidationState,
) (int64, error) {
	snapshot, err := r.snapshotFromOutcome(businessID, accountID, credentialRevision, outcome)
	if err != nil {
		return 0, err
	}
	return r.repository.RecordValidationRevisionBound(ctx, snapshot, state)
}

func (r *GSTProviderHealthRecorder) SaveGSTIntegrationAccountAndInvalidate(
	ctx context.Context,
	account *models.GSTIntegrationAccount,
	expectedCredentialRevision int64,
) error {
	if r == nil || r.repository == nil {
		return interfaces.ErrCapabilityProviderHealthScope
	}
	return r.repository.SaveGSTIntegrationAccountAndInvalidate(ctx, account, expectedCredentialRevision)
}

func (r *GSTProviderHealthRecorder) snapshotFromOutcome(
	businessID, accountID string,
	credentialRevision int64,
	outcome CapabilityProviderOutcome,
) (*models.CapabilityProviderHealthSnapshot, error) {
	if r == nil || r.repository == nil || strings.TrimSpace(businessID) == "" ||
		strings.TrimSpace(accountID) == "" || credentialRevision <= 0 {
		return nil, interfaces.ErrCapabilityProviderHealthScope
	}
	now := r.now().UTC()
	observation := capabilityHealthObservationFromOutcome(outcome, now)
	return &models.CapabilityProviderHealthSnapshot{
		BusinessID: strings.TrimSpace(businessID), ProviderKey: string(CapabilityGSTProvider),
		IntegrationAccountID: strings.TrimSpace(accountID), CredentialRevision: credentialRevision,
		Status: string(observation.Status), ObservedAt: now, FreshUntil: now.Add(r.freshFor),
		RetryAt: cloneCapabilityTime(observation.RetryAt), CustomerCode: observation.CustomerCode,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}
