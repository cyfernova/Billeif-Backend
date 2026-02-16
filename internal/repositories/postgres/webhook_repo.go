package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type webhookRepository struct {
	db *gorm.DB
}

func NewWebhookRepository(db *gorm.DB) interfaces.WebhookRepository {
	return &webhookRepository{db: db}
}

func (r *webhookRepository) Create(ctx context.Context, webhook *models.Webhook) error {
	return r.db.WithContext(ctx).Create(webhook).Error
}

func (r *webhookRepository) GetByID(ctx context.Context, id string) (*models.Webhook, error) {
	var webhook models.Webhook
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&webhook).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("webhook not found")
	}
	return &webhook, err
}

func (r *webhookRepository) GetByBusinessID(ctx context.Context, businessID string) ([]*models.Webhook, error) {
	var webhooks []models.Webhook
	err := r.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("created_at DESC").
		Limit(defaultUnpaginatedQueryLimit).
		Find(&webhooks).Error
	if err != nil {
		return nil, err
	}

	result := make([]*models.Webhook, len(webhooks))
	for i := range webhooks {
		result[i] = &webhooks[i]
	}

	return result, nil
}

func (r *webhookRepository) Update(ctx context.Context, webhook *models.Webhook) error {
	return r.db.WithContext(ctx).Save(webhook).Error
}

func (r *webhookRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Webhook{}).Error
}
