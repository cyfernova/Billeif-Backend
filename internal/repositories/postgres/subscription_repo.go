package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

type subscriptionRepository struct {
	db *gorm.DB
}

func NewSubscriptionRepository(db *gorm.DB) SubscriptionRepository {
	return &subscriptionRepository{db: db}
}

func (r *subscriptionRepository) Create(ctx context.Context, subscription *models.Subscription) error {
	return r.db.WithContext(ctx).Create(subscription).Error
}

func (r *subscriptionRepository) GetByID(ctx context.Context, id string) (*models.Subscription, error) {
	var subscription models.Subscription
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&subscription).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("subscription not found")
	}
	return &subscription, err
}

func (r *subscriptionRepository) GetByBusinessID(ctx context.Context, businessID string) (*models.Subscription, error) {
	var subscription models.Subscription
	err := r.db.WithContext(ctx).Where("business_id = ? AND deleted_at IS NULL", businessID).First(&subscription).Error
	if err != nil {
		return nil, err
	}
	return &subscription, err
}

func (r *subscriptionRepository) Update(ctx context.Context, subscription *models.Subscription) error {
	return r.db.WithContext(ctx).Save(subscription).Error
}
