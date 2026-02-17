package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type productRepository struct {
	db *gorm.DB
}

func NewProductRepository(db *gorm.DB) interfaces.ProductRepository {
	return &productRepository{db: db}
}

func (r *productRepository) Create(ctx context.Context, product *models.Product) error {
	return r.db.WithContext(ctx).Create(product).Error
}

func (r *productRepository) GetByID(ctx context.Context, id, businessID string) (*models.Product, error) {
	var product models.Product
	err := r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).First(&product).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	return &product, err
}

func (r *productRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error) {
	var products []models.Product
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.Product{}).Where("business_id = ? AND deleted_at IS NULL", businessID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&products).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Product, len(products))
	for i := range products {
		result[i] = &products[i]
	}

	return result, total, nil
}

func (r *productRepository) GetBySKU(ctx context.Context, businessID, sku string) (*models.Product, error) {
	var product models.Product
	err := r.db.WithContext(ctx).Where("business_id = ? AND sku = ? AND deleted_at IS NULL", businessID, sku).First(&product).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	return &product, err
}

func (r *productRepository) Update(ctx context.Context, product *models.Product) error {
	return r.db.WithContext(ctx).Save(product).Error
}

func (r *productRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Product{}).Error
}

func (r *productRepository) AdjustStock(ctx context.Context, productID string, quantity int64) error {
	return r.db.WithContext(ctx).Model(&models.Product{}).Where("id = ?", productID).UpdateColumn("stock", gorm.Expr("stock + ?", quantity)).Error
}
