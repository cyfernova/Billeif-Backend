package postgres

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type subscriptionLifecycleRepository struct {
	db *gorm.DB
}

func NewSubscriptionLifecycleRepository(db *gorm.DB) interfaces.SubscriptionLifecycleRepository {
	return &subscriptionLifecycleRepository{db: db}
}

func (r *subscriptionLifecycleRepository) Transaction(ctx context.Context, operation func(interfaces.SubscriptionLifecycleRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return operation(&subscriptionLifecycleRepository{db: tx})
	})
}

func (r *subscriptionLifecycleRepository) GetByBusinessIDForUpdate(ctx context.Context, businessID string) (*models.Subscription, error) {
	var subscription models.Subscription
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("business_id = ? AND deleted_at IS NULL", businessID).First(&subscription).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrSubscriptionLifecycleNotFound
	}
	return &subscription, err
}

func (r *subscriptionLifecycleRepository) GetByProviderIDForUpdate(ctx context.Context, providerMode, providerSubscriptionID string) (*models.Subscription, error) {
	var subscription models.Subscription
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("provider_mode = ? AND provider_subscription_id = ? AND deleted_at IS NULL", providerMode, providerSubscriptionID).
		First(&subscription).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrSubscriptionLifecycleNotFound
	}
	return &subscription, err
}

func (r *subscriptionLifecycleRepository) CreateSubscription(ctx context.Context, subscription *models.Subscription) error {
	return r.db.WithContext(ctx).Create(subscription).Error
}

func (r *subscriptionLifecycleRepository) SaveSubscription(ctx context.Context, subscription *models.Subscription) error {
	subscription.LifecycleVersion++
	return r.db.WithContext(ctx).Save(subscription).Error
}

func (r *subscriptionLifecycleRepository) GetCommandForUpdate(ctx context.Context, businessID, actorUserID, action, idempotencyKey string) (*models.SubscriptionCommand, error) {
	var command models.SubscriptionCommand
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"business_id = ? AND actor_user_id = ? AND action = ? AND idempotency_key = ?",
		businessID, actorUserID, action, idempotencyKey,
	).First(&command).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrSubscriptionCommandNotFound
	}
	return &command, err
}

func (r *subscriptionLifecycleRepository) CreateCommand(ctx context.Context, command *models.SubscriptionCommand) error {
	return r.db.WithContext(ctx).Create(command).Error
}

func (r *subscriptionLifecycleRepository) SaveCommand(ctx context.Context, command *models.SubscriptionCommand) error {
	return r.db.WithContext(ctx).Save(command).Error
}

func (r *subscriptionLifecycleRepository) CreateAudit(ctx context.Context, record *models.SubscriptionAuditRecord) error {
	return r.db.WithContext(ctx).Create(record).Error
}

func (r *subscriptionLifecycleRepository) CreateBillingRecord(ctx context.Context, record *models.SubscriptionBillingRecord) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(record).Error
}

func (r *subscriptionLifecycleRepository) GetEventForUpdate(ctx context.Context, providerMode, providerEventID string) (*models.RazorpayWebhookEvent, error) {
	var event models.RazorpayWebhookEvent
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("provider_mode = ? AND razorpay_event_id = ?", providerMode, providerEventID).First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrRazorpayEventNotFound
	}
	return &event, err
}

func (r *subscriptionLifecycleRepository) CreateEvent(ctx context.Context, event *models.RazorpayWebhookEvent) error {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return interfaces.ErrRazorpayEventAlreadyExists
	}
	return nil
}

func (r *subscriptionLifecycleRepository) SaveEvent(ctx context.Context, event *models.RazorpayWebhookEvent) error {
	return r.db.WithContext(ctx).Save(event).Error
}

func (r *subscriptionLifecycleRepository) ListBillingRecords(ctx context.Context, businessID string, limit int) ([]models.SubscriptionBillingRecord, error) {
	var records []models.SubscriptionBillingRecord
	err := r.db.WithContext(ctx).Where("business_id = ?", businessID).Order("occurred_at DESC, id DESC").Limit(limit).Find(&records).Error
	return records, err
}

func (r *subscriptionLifecycleRepository) ListAuditRecords(ctx context.Context, businessID string, limit int) ([]models.SubscriptionAuditRecord, error) {
	var records []models.SubscriptionAuditRecord
	err := r.db.WithContext(ctx).Where("business_id = ?", businessID).Order("occurred_at DESC, id DESC").Limit(limit).Find(&records).Error
	return records, err
}

func (r *subscriptionLifecycleRepository) ListReconciliationDue(ctx context.Context, providerMode string, limit int) ([]models.Subscription, error) {
	var records []models.Subscription
	err := r.db.WithContext(ctx).Where("provider_mode = ? AND status = ? AND provider_subscription_id <> '' AND deleted_at IS NULL", providerMode, models.SubscriptionStatusReconciliationRequired).
		Order("updated_at ASC").Limit(limit).Find(&records).Error
	return records, err
}

func (r *subscriptionLifecycleRepository) ListGraceDue(ctx context.Context, providerMode string, before time.Time, limit int) ([]models.Subscription, error) {
	var records []models.Subscription
	err := r.db.WithContext(ctx).Where("provider_mode = ? AND status IN ? AND grace_deadline IS NOT NULL AND grace_deadline <= ? AND deleted_at IS NULL",
		providerMode, []string{models.SubscriptionStatusPastDue, models.SubscriptionStatusGracePeriod}, before.UTC()).
		Order("grace_deadline ASC").Limit(limit).Find(&records).Error
	return records, err
}

var _ interfaces.SubscriptionLifecycleRepository = (*subscriptionLifecycleRepository)(nil)
