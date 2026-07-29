package services

import (
	"context"

	"invoice-backend/internal/config"
)

type ProviderConfigResolver interface {
	ResolveProvider(context.Context, *config.Config, config.SecretKind) (*config.Config, error)
}
