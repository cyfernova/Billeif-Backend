package interfaces

import (
	"context"
	"errors"
	"invoice-backend/internal/models"
)

var ErrAgentConfigNotFound = errors.New("agent configuration not found")

type AgentConfigRepository interface {
	Save(context.Context, string, *models.WellKnownAgentConfig) error
	Get(context.Context, string) (*models.WellKnownAgentConfig, error)
	List(context.Context) ([]*models.WellKnownAgentConfig, error)
	Delete(context.Context, string) error
}
