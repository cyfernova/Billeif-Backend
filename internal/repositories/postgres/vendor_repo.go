package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

type vendorRepository struct {
	db *gorm.DB
}

func NewVendorRepository(db *gorm.DB) VendorRepository {
	return &vendorRepository{db: db}
}

func (r *vendorRepository) Create(ctx context.Context, vendor *models.Vendor) error {
	return r.db.WithContext(ctx).Create(vendor).Error
}

func (r *vendorRepository) GetByID(ctx context.Context, id string) (*models.Vendor, error) {
	var vendor models.Vendor
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&vendor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("vendor not found")
	}
	return &vendor, err
}

func (r *vendorRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Vendor, int64, error) {
	var vendors []models.Vendor
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.Vendor{}).Where("business_id = ? AND deleted_at IS NULL", businessID)

	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&vendors).Error; err != nil {
		return nil, err
	}

	return vendors, total, nil
}

func (r *vendorRepository) Update(ctx context.Context, vendor *models.Vendor) error {
	return r.db.WithContext(ctx).Save(vendor).Error
}

func (r *vendorRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.Vendor{ID: id}).Error
}
