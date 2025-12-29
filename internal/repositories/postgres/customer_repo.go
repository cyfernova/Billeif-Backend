package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type customerRepository struct {
	db *gorm.DB
}

func NewCustomerRepository(db *gorm.DB) interfaces.CustomerRepository {
	return &customerRepository{db: db}
}

func (r *customerRepository) Create(ctx context.Context, customer *models.Customer) error {
	return r.db.WithContext(ctx).Create(customer).Error
}

func (r *customerRepository) GetByID(ctx context.Context, id string) (*models.Customer, error) {
	var customer models.Customer
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&customer).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("customer not found")
	}
	return &customer, err
}

func (r *customerRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error) {
	var customers []models.Customer
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.Customer{}).Where("business_id = ? AND deleted_at IS NULL", businessID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&customers).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Customer, len(customers))
	for i := range customers {
		result[i] = &customers[i]
	}

	return result, total, nil
}

func (r *customerRepository) Update(ctx context.Context, customer *models.Customer) error {
	return r.db.WithContext(ctx).Save(customer).Error
}

func (r *customerRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.Customer{ID: id}).Error
}
