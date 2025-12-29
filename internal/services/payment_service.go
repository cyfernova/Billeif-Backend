package services

import (
	"context"
	"fmt"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type PaymentService struct {
	repo        interfaces.PaymentRepository
	invoiceRepo interfaces.InvoiceRepository
	log         *logger.Logger
}

func NewPaymentService(repo interfaces.PaymentRepository, invoiceRepo interfaces.InvoiceRepository, log *logger.Logger) *PaymentService {
	return &PaymentService{repo: repo, invoiceRepo: invoiceRepo, log: log}
}

type CreatePaymentInput struct {
	InvoiceID     string  `json:"invoice_id" binding:"required,uuid"`
	Amount        float64 `json:"amount" binding:"required,gt=0"`
	PaymentMethod string  `json:"payment_method" binding:"required"`
	PaymentDate   string  `json:"payment_date"`
	Reference     string  `json:"reference"`
	Notes         string  `json:"notes"`
}

func (s *PaymentService) Create(ctx context.Context, input CreatePaymentInput) (*models.Payment, error) {
	invoice, err := s.invoiceRepo.GetByID(ctx, input.InvoiceID)
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
		s.log.Error("failed to update invoice after payment", "invoice_id", input.InvoiceID, "error", err)
	}

	return payment, nil
}

func (s *PaymentService) Get(ctx context.Context, id string) (*models.Payment, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *PaymentService) ListByInvoice(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

type UpdatePaymentInput struct {
	Amount        float64 `json:"amount"`
	PaymentMethod string  `json:"payment_method"`
	Reference     string  `json:"reference"`
	Notes         string  `json:"notes"`
}

func (s *PaymentService) Update(ctx context.Context, id string, input UpdatePaymentInput) (*models.Payment, error) {
	payment, err := s.repo.GetByID(ctx, id)
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
