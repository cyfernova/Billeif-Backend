package interfaces

import (
	"context"
	"errors"

	"invoice-backend/internal/models"
)

var (
	ErrCapabilityProviderHealthNotFound = errors.New("capability provider health snapshot not found")
	ErrCapabilityProviderHealthScope    = errors.New("capability provider health scope is invalid")
)

type CapabilityProviderHealthRepository interface {
	Get(ctx context.Context, businessID, providerKey string) (*models.CapabilityProviderHealthSnapshot, error)
	UpsertMonotonic(ctx context.Context, snapshot *models.CapabilityProviderHealthSnapshot) error
	Clear(ctx context.Context, businessID, providerKey string) error
}
