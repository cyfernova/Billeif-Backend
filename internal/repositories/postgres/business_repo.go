package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type businessRepository struct {
	db *gorm.DB
}

func NewBusinessRepository(db *gorm.DB) interfaces.BusinessRepository {
	return &businessRepository{db: db}
}

func (r *businessRepository) Create(ctx context.Context, business *models.BusinessProfile) error {
	return r.db.WithContext(ctx).Create(business).Error
}

func (r *businessRepository) GetByID(ctx context.Context, id string) (*models.BusinessProfile, error) {
	var business models.BusinessProfile
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&business).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("business not found")
	}
	return &business, err
}

func (r *businessRepository) Update(ctx context.Context, business *models.BusinessProfile) error {
	return r.db.WithContext(ctx).Save(business).Error
}

func (r *businessRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.BusinessProfile{ID: id}).Error
}

func (r *businessRepository) List(ctx context.Context, userID string, page, limit int) ([]*models.BusinessProfile, int64, error) {
	var businesses []models.BusinessProfile
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.BusinessProfile{}).Where("owner_id = ? AND deleted_at IS NULL", userID).Order("created_at DESC")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&businesses).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.BusinessProfile, len(businesses))
	for i := range businesses {
		result[i] = &businesses[i]
	}

	return result, total, nil
}
