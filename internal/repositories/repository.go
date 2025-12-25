package repositories

import (
	"context"
	"time"

	"github.com/cyfernova/invoice-backend/internal/database"
	"github.com/cyfernova/invoice-backend/pkg/awsclients"
)

// Registry holds all repository instances
type Registry struct {
	User            UserRepository
	Session         SessionRepository
	Customer        CustomerRepository
	Vendor          VendorRepository
	BusinessProfile BusinessProfileRepository
}

// NewRegistry creates a new repository registry
func NewRegistry(
	db *database.Database,
	dynamoClient *awsclients.DynamoDBClient,
	sessionsTable,
	customersCacheTable,
	vendorsCacheTable,
	businessProfilesCacheTable string,
	cacheTTL time.Duration,
) *Registry {
	return &Registry{
		User:            NewUserRepository(db),
		Session:         NewSessionRepository(dynamoClient, sessionsTable),
		Customer:        NewCustomerRepository(db, NewCustomerCacheRepository(dynamoClient, customersCacheTable, cacheTTL)),
		Vendor:          NewVendorRepository(db, NewVendorCacheRepository(dynamoClient, vendorsCacheTable, cacheTTL)),
		BusinessProfile: NewBusinessProfileRepository(db, NewBusinessProfileCacheRepository(dynamoClient, businessProfilesCacheTable, cacheTTL)),
	}
}

// HealthChecker is an interface for health checking repositories
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}
