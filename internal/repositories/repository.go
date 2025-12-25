package repositories

import (
	"context"

	"github.com/cyfernova/invoice-backend/internal/database"
	"github.com/cyfernova/invoice-backend/pkg/awsclients"
)

// Registry holds all repository instances
type Registry struct {
	User    UserRepository
	Session SessionRepository
}

// NewRegistry creates a new repository registry
func NewRegistry(db *database.Database, dynamoClient *awsclients.DynamoDBClient, sessionsTable string) *Registry {
	return &Registry{
		User:    NewUserRepository(db),
		Session: NewSessionRepository(dynamoClient, sessionsTable),
	}
}

// HealthChecker is an interface for health checking repositories
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}
