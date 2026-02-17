package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type invoiceRepository struct {
	db *gorm.DB
}

func NewInvoiceRepository(db *gorm.DB) interfaces.InvoiceRepository {
	return &invoiceRepository{db: db}
}

func (r *invoiceRepository) Create(ctx context.Context, invoice *models.Invoice) error {
	return r.db.WithContext(ctx).Create(invoice).Error
}

func (r *invoiceRepository) GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.db.WithContext(ctx).Preload("Items").Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("invoice not found")
	}
	return &invoice, err
}

func (r *invoiceRepository) GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.db.WithContext(ctx).Preload("Items").Where("id = ? AND deleted_at IS NULL", id).First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("invoice not found")
	}
	return &invoice, err
}

func (r *invoiceRepository) GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.db.WithContext(ctx).Where("business_id = ? AND invoice_no = ? AND deleted_at IS NULL", businessID, invoiceNo).First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("invoice not found")
	}
	return &invoice, err
}

func (r *invoiceRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	var invoices []models.Invoice
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.Invoice{}).Where("business_id = ? AND deleted_at IS NULL", businessID).Order("created_at DESC")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&invoices).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Invoice, len(invoices))
	for i := range invoices {
		result[i] = &invoices[i]
	}

	return result, total, nil
}

func (r *invoiceRepository) GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error) {
	var items []models.InvoiceItem
	err := r.db.WithContext(ctx).Where("invoice_id = ?", invoiceID).Find(&items).Error
	if err != nil {
		return nil, err
	}

	result := make([]*models.InvoiceItem, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result, nil
}

func (r *invoiceRepository) Update(ctx context.Context, invoice *models.Invoice) error {
	return r.db.WithContext(ctx).Save(invoice).Error
}

func (r *invoiceRepository) UpdateStatus(ctx context.Context, invoiceID string, status string) error {
	return r.db.WithContext(ctx).Model(&models.Invoice{}).Where("id = ?", invoiceID).Update("status", status).Error
}

func (r *invoiceRepository) UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error {
	return r.db.WithContext(ctx).Model(&models.Invoice{}).Where("id = ?", invoiceID).Update("pdf_url", pdfURL).Error
}

func (r *invoiceRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Invoice{}).Error
}
