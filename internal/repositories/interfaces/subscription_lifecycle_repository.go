package interfaces

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
)

var (
	ErrSubscriptionLifecycleNotFound = errors.New("subscription lifecycle not found")
	ErrSubscriptionCommandNotFound   = errors.New("subscription command not found")
	ErrRazorpayEventNotFound         = errors.New("Razorpay event not found")
	ErrRazorpayEventAlreadyExists    = errors.New("Razorpay event already exists")
)

type SubscriptionLifecycleRepository interface {
	Transaction(ctx context.Context, operation func(SubscriptionLifecycleRepository) error) error
	GetByBusinessIDForUpdate(ctx context.Context, businessID string) (*models.Subscription, error)
	GetByProviderIDForUpdate(ctx context.Context, providerMode, providerSubscriptionID string) (*models.Subscription, error)
	CreateSubscription(ctx context.Context, subscription *models.Subscription) error
	SaveSubscription(ctx context.Context, subscription *models.Subscription) error
	GetCommandForUpdate(ctx context.Context, businessID, actorUserID, action, idempotencyKey string) (*models.SubscriptionCommand, error)
	CreateCommand(ctx context.Context, command *models.SubscriptionCommand) error
	SaveCommand(ctx context.Context, command *models.SubscriptionCommand) error
	CreateAudit(ctx context.Context, record *models.SubscriptionAuditRecord) error
	CreateBillingRecord(ctx context.Context, record *models.SubscriptionBillingRecord) error
	GetEventForUpdate(ctx context.Context, providerMode, providerEventID string) (*models.RazorpayWebhookEvent, error)
	CreateEvent(ctx context.Context, event *models.RazorpayWebhookEvent) error
	SaveEvent(ctx context.Context, event *models.RazorpayWebhookEvent) error
	ListBillingRecords(ctx context.Context, businessID string, limit int) ([]models.SubscriptionBillingRecord, error)
	ListAuditRecords(ctx context.Context, businessID string, limit int) ([]models.SubscriptionAuditRecord, error)
	ListReconciliationDue(ctx context.Context, providerMode string, limit int) ([]models.Subscription, error)
	ListGraceDue(ctx context.Context, providerMode string, before time.Time, limit int) ([]models.Subscription, error)
}
