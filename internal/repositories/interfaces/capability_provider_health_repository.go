package interfaces

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
)

var (
	ErrCapabilityProviderHealthNotFound                = errors.New("capability provider health snapshot not found")
	ErrCapabilityProviderHealthScope                   = errors.New("capability provider health scope is invalid")
	ErrCapabilityProviderHealthCredentialRevisionStale = errors.New("GST credential revision is stale")
	ErrCapabilityProviderHealthObservationNotApplied   = errors.New("capability provider health observation was not applied")
	ErrGSTIntegrationAccountNotFound                   = errors.New("GST integration account not found")
)

type GSTIntegrationValidationState struct {
	Status          string
	LastValidatedAt *time.Time
	LastError       string
}

type CapabilityProviderHealthRepository interface {
	Get(ctx context.Context, businessID, providerKey string) (*models.CapabilityProviderHealthSnapshot, error)
	RecordRevisionBound(ctx context.Context, snapshot *models.CapabilityProviderHealthSnapshot) (int64, error)
	RecordValidationRevisionBound(ctx context.Context, snapshot *models.CapabilityProviderHealthSnapshot, state GSTIntegrationValidationState) (int64, error)
	SaveGSTIntegrationAccountAndInvalidate(ctx context.Context, account *models.GSTIntegrationAccount, expectedCredentialRevision int64) error
}
