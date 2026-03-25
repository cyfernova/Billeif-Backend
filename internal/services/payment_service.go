package services

import (
	"context"
	"fmt"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

type PaymentService struct {
	db          *gorm.DB
	repo        interfaces.PaymentRepository
	invoiceRepo interfaces.InvoiceRepository
	log         *logger.Logger
}

func NewPaymentService(db *gorm.DB, repo interfaces.PaymentRepository, invoiceRepo interfaces.InvoiceRepository, log *logger.Logger) *PaymentService {
	return &PaymentService{db: db, repo: repo, invoiceRepo: invoiceRepo, log: log}
}

type CreatePaymentInput struct {
	InvoiceID     string  `json:"invoice_id" binding:"required,uuid"`
	Amount        float64 `json:"amount" binding:"required,gt=0"`
	PaymentMethod string  `json:"payment_method" binding:"required"`
	PaymentDate   string  `json:"payment_date"`
	Reference     string  `json:"reference"`
	Notes         string  `json:"notes"`
}

func (s *PaymentService) Create(ctx context.Context, businessID string, input CreatePaymentInput) (*models.Payment, error) {
	invoice, err := s.invoiceRepo.GetByID(ctx, input.InvoiceID, businessID)
	if err != nil {
		return nil, fmt.Errorf("invoice not found: %w", err)
	}

	paymentDate := time.Now()
	if input.PaymentDate != "" {
		if parsed, err := time.Parse(time.RFC3339, input.PaymentDate); err == nil {
			paymentDate = parsed
		}
	}

	payment := &models.Payment{
		InvoiceID:     input.InvoiceID,
		BusinessID:    invoice.BusinessID,
		Amount:        input.Amount,
		Currency:      invoice.Currency,
		PaymentDate:   paymentDate,
		PaymentMethod: input.PaymentMethod,
		Reference:     input.Reference,
		Notes:         input.Notes,
	}

	// Use transaction to ensure atomicity of payment creation and invoice update
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(payment).Error; err != nil {
			return fmt.Errorf("failed to create payment: %w", err)
		}

		invoice.PaidAmount += input.Amount
		invoice.BalanceDue = invoice.Total - invoice.PaidAmount
		if invoice.PaidAmount >= invoice.Total {
			invoice.Status = "paid"
			now := time.Now()
			invoice.PaidAt = &now
		} else {
			invoice.Status = "partial"
		}

		if err := tx.Save(invoice).Error; err != nil {
			return fmt.Errorf("failed to update invoice: %w", err)
		}

		return nil
	})

	if err != nil {
		s.log.Error("payment transaction failed", "invoice_id", input.InvoiceID, "error", err)
		return nil, err
	}

	return payment, nil
}

func (s *PaymentService) CreateByBusiness(ctx context.Context, businessID string, input CreatePaymentInput) (*models.Payment, error) {
	return s.Create(ctx, businessID, input)
}

func (s *PaymentService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Payment, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

func (s *PaymentService) ListByInvoice(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

func (s *PaymentService) ListByInvoiceAndBusiness(ctx context.Context, businessID, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	_, err := s.invoiceRepo.GetByID(ctx, invoiceID, businessID)
	if err != nil {
		return nil, 0, fmt.Errorf("invoice not found: %w", err)
	}
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

type UpdatePaymentInput struct {
	Amount        float64 `json:"amount"`
	PaymentMethod string  `json:"payment_method"`
	Reference     string  `json:"reference"`
	Notes         string  `json:"notes"`
}

func (s *PaymentService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdatePaymentInput) (*models.Payment, error) {
	payment, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if input.Amount > 0 {
		payment.Amount = input.Amount
	}
	if input.PaymentMethod != "" {
		payment.PaymentMethod = input.PaymentMethod
	}
	if input.Reference != "" {
		payment.Reference = input.Reference
	}
	if input.Notes != "" {
		payment.Notes = input.Notes
	}

	if err := s.repo.Update(ctx, payment); err != nil {
		return nil, err
	}
	return payment, nil
}

func (s *PaymentService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *PaymentService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	payment, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, payment.ID)
}

// PaymentServiceTestable is a test-friendly version of PaymentService
type PaymentServiceTestable struct {
	repo        PaymentRepositoryTestable
	invoiceRepo PaymentInvoiceRepositoryTestable
	log         *logger.Logger
}

// PaymentInvoiceRepositoryTestable is the testable interface for InvoiceRepository
type PaymentInvoiceRepositoryTestable interface {
	Create(ctx context.Context, invoice *models.Invoice) error
	GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error)
	GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error)
	GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error)
	GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error)
	Update(ctx context.Context, invoice *models.Invoice) error
	UpdateStatus(ctx context.Context, invoiceID string, status string) error
	UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error
	Delete(ctx context.Context, id string) error
}

// PaymentRepositoryTestable is the testable interface for PaymentRepository
type PaymentRepositoryTestable interface {
	Create(ctx context.Context, payment *models.Payment) error
	GetByID(ctx context.Context, id, businessID string) (*models.Payment, error)
	GetByInvoiceID(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error)
	Update(ctx context.Context, payment *models.Payment) error
	Delete(ctx context.Context, id string) error
}

// NewPaymentServiceForTesting creates a PaymentServiceTestable for unit testing
func NewPaymentServiceForTesting(
	repo PaymentRepositoryTestable,
	invoiceRepo PaymentInvoiceRepositoryTestable,
	log *logger.Logger,
) *PaymentServiceTestable {
	return &PaymentServiceTestable{
		repo:        repo,
		invoiceRepo: invoiceRepo,
		log:         log,
	}
}

// Create creates a payment (testable version - simplified without transaction)
func (s *PaymentServiceTestable) Create(ctx context.Context, businessID string, input CreatePaymentInput) (*models.Payment, error) {
	invoice, err := s.invoiceRepo.GetByID(ctx, input.InvoiceID, businessID)
	if err != nil {
		return nil, fmt.Errorf("invoice not found: %w", err)
	}

	paymentDate := time.Now()
	if input.PaymentDate != "" {
		if parsed, err := time.Parse(time.RFC3339, input.PaymentDate); err == nil {
			paymentDate = parsed
		}
	}

	payment := &models.Payment{
		InvoiceID:     input.InvoiceID,
		BusinessID:    invoice.BusinessID,
		Amount:        input.Amount,
		Currency:      invoice.Currency,
		PaymentDate:   paymentDate,
		PaymentMethod: input.PaymentMethod,
		Reference:     input.Reference,
		Notes:         input.Notes,
	}

	if err := s.repo.Create(ctx, payment); err != nil {
		return nil, fmt.Errorf("failed to create payment: %w", err)
	}

	// Update invoice paid amount
	invoice.PaidAmount += input.Amount
	invoice.BalanceDue = invoice.Total - invoice.PaidAmount
	if invoice.PaidAmount >= invoice.Total {
		invoice.Status = "paid"
		now := time.Now()
		invoice.PaidAt = &now
	} else {
		invoice.Status = "partial"
	}

	if err := s.invoiceRepo.Update(ctx, invoice); err != nil {
		return nil, fmt.Errorf("failed to update invoice: %w", err)
	}

	return payment, nil
}

// GetByBusiness retrieves a payment by business ID and payment ID
func (s *PaymentServiceTestable) GetByBusiness(ctx context.Context, businessID, id string) (*models.Payment, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

// ListByInvoice retrieves payments for an invoice with pagination
func (s *PaymentServiceTestable) ListByInvoice(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

// ListByInvoiceAndBusiness retrieves payments for an invoice scoped to business
func (s *PaymentServiceTestable) ListByInvoiceAndBusiness(ctx context.Context, businessID, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	_, err := s.invoiceRepo.GetByID(ctx, invoiceID, businessID)
	if err != nil {
		return nil, 0, fmt.Errorf("invoice not found: %w", err)
	}
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

// UpdateByBusiness updates a payment (testable version)
func (s *PaymentServiceTestable) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdatePaymentInput) (*models.Payment, error) {
	payment, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if input.Amount > 0 {
		payment.Amount = input.Amount
	}
	if input.PaymentMethod != "" {
		payment.PaymentMethod = input.PaymentMethod
	}
	if input.Reference != "" {
		payment.Reference = input.Reference
	}
	if input.Notes != "" {
		payment.Notes = input.Notes
	}

	if err := s.repo.Update(ctx, payment); err != nil {
		return nil, err
	}
	return payment, nil
}

// DeleteByBusiness deletes a payment (testable version)
func (s *PaymentServiceTestable) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	payment, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, payment.ID)
}
