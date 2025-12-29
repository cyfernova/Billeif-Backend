package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type paymentRepository struct {
	db *gorm.DB
}

func NewPaymentRepository(db *gorm.DB) interfaces.PaymentRepository {
	return &paymentRepository{db: db}
}

func (r *paymentRepository) Create(ctx context.Context, payment *models.Payment) error {
	return r.db.WithContext(ctx).Create(payment).Error
}

func (r *paymentRepository) GetByID(ctx context.Context, id string) (*models.Payment, error) {
	var payment models.Payment
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&payment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("payment not found")
	}
	return &payment, err
}

func (r *paymentRepository) GetByInvoiceID(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	var payments []models.Payment
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.Payment{}).Where("invoice_id = ?", invoiceID).Order("payment_date DESC")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&payments).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Payment, len(payments))
	for i := range payments {
		result[i] = &payments[i]
	}

	return result, total, nil
}

func (r *paymentRepository) Update(ctx context.Context, payment *models.Payment) error {
	return r.db.WithContext(ctx).Save(payment).Error
}

func (r *paymentRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.Payment{ID: id}).Error
}
