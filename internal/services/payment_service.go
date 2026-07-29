package services

import (
	"context"
	"fmt"
	"strings"
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
	documents   *DocumentService
	journals    *JournalService
	log         *logger.Logger
}

func NewPaymentService(db *gorm.DB, repo interfaces.PaymentRepository, invoiceRepo interfaces.InvoiceRepository, documents *DocumentService, journals *JournalService, log *logger.Logger) *PaymentService {
	return &PaymentService{db: db, repo: repo, invoiceRepo: invoiceRepo, documents: documents, journals: journals, log: log}
}

type CreatePaymentInput struct {
	InvoiceID     string            `json:"invoice_id" binding:"required,uuid"`
	ProjectID     string            `json:"project_id,omitempty" binding:"omitempty,uuid"`
	Amount        float64           `json:"amount" binding:"required,gt=0"`
	PaymentType   string            `json:"payment_type"`
	PaymentMethod string            `json:"payment_method" binding:"required"`
	PaymentDate   string            `json:"payment_date"`
	Reference     string            `json:"reference"`
	Notes         string            `json:"notes"`
	Withholding   *WithholdingInput `json:"withholding,omitempty"`
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
	projectID := normalizeProjectID(input.ProjectID)
	if projectID == "" && invoice.ProjectID != nil {
		projectID = *invoice.ProjectID
	}
	if projectID == "" {
		projectID = extractProjectIDFromTags(nestedMap(unmarshalJSONMap(invoice.TaxProfile), "report_tags"))
	}

	payment := &models.Payment{
		InvoiceID:       input.InvoiceID,
		BusinessID:      invoice.BusinessID,
		ProjectID:       projectIDPointer(projectID),
		Amount:          input.Amount,
		Currency:        invoice.Currency,
		PaymentDate:     paymentDate,
		PaymentType:     coalesceString(input.PaymentType, "normal"),
		PaymentMethod:   input.PaymentMethod,
		Reference:       input.Reference,
		Notes:           input.Notes,
		WithholdingData: mustMarshalMap(withholdingToMap(input.Withholding)),
	}
	// Use transaction to ensure atomicity of payment creation and invoice update
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(payment).Error; err != nil {
			return fmt.Errorf("failed to create payment: %w", err)
		}

		if err := s.syncPaymentWithholdingTx(ctx, tx, payment, input.Withholding); err != nil {
			return err
		}

		invoice.PaidAmount += settlementAmount(payment.Amount, input.Withholding)
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
	if s.documents != nil {
		if err := s.documents.SyncLegacyInvoicePayment(ctx, invoice.ID, invoice.PaidAmount, invoice.BalanceDue, invoice.Status); err != nil {
			s.log.Error("failed to sync mirrored document payment state", "invoice_id", invoice.ID, "payment_id", payment.ID, "error", err)
		}
	}
	if s.journals != nil {
		if err := s.createPaymentJournal(ctx, payment, invoice); err != nil {
			s.log.Error("failed to create payment journal", "invoice_id", invoice.ID, "payment_id", payment.ID, "error", err)
		}
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

func (s *PaymentService) ListByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

func (s *PaymentService) ListByInvoiceAndBusiness(ctx context.Context, businessID, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	_, err := s.invoiceRepo.GetByID(ctx, invoiceID, businessID)
	if err != nil {
		return nil, 0, fmt.Errorf("invoice not found: %w", err)
	}
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

type UpdatePaymentInput struct {
	ProjectID     *string           `json:"project_id,omitempty"`
	Amount        float64           `json:"amount"`
	PaymentType   string            `json:"payment_type"`
	PaymentMethod string            `json:"payment_method"`
	Reference     string            `json:"reference"`
	Notes         string            `json:"notes"`
	Withholding   *WithholdingInput `json:"withholding,omitempty"`
}

func (s *PaymentService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdatePaymentInput) (*models.Payment, error) {
	payment, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	invoice, err := s.invoiceRepo.GetByID(ctx, payment.InvoiceID, businessID)
	if err != nil {
		return nil, fmt.Errorf("invoice not found: %w", err)
	}

	oldWithholding := unmarshalJSONMap(payment.WithholdingData)
	oldSettlement := settlementAmount(payment.Amount, mapToWithholdingInput(oldWithholding))

	if input.Amount > 0 {
		payment.Amount = input.Amount
	}
	if input.PaymentType != "" {
		payment.PaymentType = input.PaymentType
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
	if input.ProjectID != nil {
		payment.ProjectID = projectIDPointer(*input.ProjectID)
	}
	if input.Withholding != nil {
		payment.WithholdingData = mustMarshalMap(withholdingToMap(input.Withholding))
	}

	newWithholding := mapToWithholdingInput(unmarshalJSONMap(payment.WithholdingData))
	newSettlement := settlementAmount(payment.Amount, newWithholding)

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(payment).Error; err != nil {
			return err
		}
		if err := s.syncPaymentWithholdingTx(ctx, tx, payment, newWithholding); err != nil {
			return err
		}
		invoice.PaidAmount += (newSettlement - oldSettlement)
		invoice.BalanceDue = invoice.Total - invoice.PaidAmount
		switch {
		case invoice.PaidAmount >= invoice.Total:
			invoice.Status = "paid"
		case invoice.PaidAmount > 0:
			invoice.Status = "partial"
		default:
			invoice.Status = "sent"
		}
		return tx.Save(invoice).Error
	}); err != nil {
		return nil, err
	}
	if s.documents != nil {
		if err := s.documents.SyncLegacyInvoicePayment(ctx, invoice.ID, invoice.PaidAmount, invoice.BalanceDue, invoice.Status); err != nil {
			s.log.Error("failed to sync mirrored document payment state after payment update", "invoice_id", invoice.ID, "payment_id", payment.ID, "error", err)
		}
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

func (s *PaymentService) syncPaymentWithholdingTx(ctx context.Context, tx *gorm.DB, payment *models.Payment, withholding *WithholdingInput) error {
	if payment == nil {
		return nil
	}
	if err := tx.WithContext(ctx).Where("payment_id = ?", payment.ID).Delete(&models.PaymentWithholding{}).Error; err != nil {
		return err
	}
	if withholding == nil || withholding.SectionCode == "" || withholding.Amount == 0 {
		return nil
	}
	record := &models.PaymentWithholding{
		BusinessID:      payment.BusinessID,
		PaymentID:       payment.ID,
		InvoiceID:       &payment.InvoiceID,
		SectionCode:     withholding.SectionCode,
		WithholdingType: coalesceString(withholding.WithholdingType, models.WithholdingTypeTDS),
		Rate:            withholding.Rate,
		TaxableAmount:   withholding.TaxableAmount,
		Amount:          withholding.Amount,
		Metadata:        mustMarshalMap(withholding.Metadata),
	}
	return tx.WithContext(ctx).Create(record).Error
}

func settlementAmount(amount float64, withholding *WithholdingInput) float64 {
	if withholding == nil {
		return amount
	}
	return amount + withholding.Amount
}

func withholdingToMap(input *WithholdingInput) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"section_code":     input.SectionCode,
		"withholding_type": input.WithholdingType,
		"rate":             input.Rate,
		"taxable_amount":   input.TaxableAmount,
		"amount":           input.Amount,
		"metadata":         input.Metadata,
	}
}

func mapToWithholdingInput(data map[string]interface{}) *WithholdingInput {
	if len(data) == 0 {
		return nil
	}
	input := &WithholdingInput{
		SectionCode:     readStringCandidate(data, "section_code"),
		WithholdingType: readStringCandidate(data, "withholding_type"),
		Rate:            readFloatCandidate(data, "rate"),
		TaxableAmount:   readFloatCandidate(data, "taxable_amount"),
		Amount:          readFloatCandidate(data, "amount"),
		Metadata:        nestedMap(data, "metadata"),
	}
	if input.SectionCode == "" && input.Amount == 0 {
		return nil
	}
	return input
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
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Payment, int64, error)
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
		ProjectID:     invoice.ProjectID,
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

// ListByBusiness retrieves payments for a business with pagination.
func (s *PaymentServiceTestable) ListByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
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
	if input.ProjectID != nil {
		payment.ProjectID = projectIDPointer(*input.ProjectID)
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

func (s *PaymentService) createPaymentJournal(ctx context.Context, payment *models.Payment, invoice *models.Invoice) error {
	assetCode := "BANK"
	assetName := "Bank Account"
	if strings.Contains(strings.ToLower(payment.PaymentMethod), "cash") {
		assetCode = "CASH"
		assetName = "Cash"
	}

	documentID := invoice.ID
	metadata := map[string]interface{}{
		"payment_id":     payment.ID,
		"payment_method": payment.PaymentMethod,
	}

	_, err := s.journals.CreateByBusiness(ctx, invoice.BusinessID, CreateJournalInput{
		Name:        fmt.Sprintf("Payment %s", models.StringValue(invoice.InvoiceNo)),
		Reference:   coalesceString(payment.Reference, payment.ID),
		ProjectID:   normalizeProjectID(derefString(payment.ProjectID)),
		PostingDate: payment.PaymentDate,
		Status:      models.JournalStatusPosted,
		Notes:       payment.Notes,
		Lines: []CreateJournalLineInput{
			{
				AccountCode: assetCode,
				AccountName: assetName,
				EntryType:   "debit",
				Amount:      payment.Amount,
				Currency:    payment.Currency,
				Description: fmt.Sprintf("Receipt for invoice %s", models.StringValue(invoice.InvoiceNo)),
				DocumentID:  &documentID,
				Metadata:    metadata,
			},
			{
				AccountCode: "AR",
				AccountName: "Accounts Receivable",
				EntryType:   "credit",
				Amount:      payment.Amount,
				Currency:    payment.Currency,
				Description: fmt.Sprintf("Settlement for invoice %s", models.StringValue(invoice.InvoiceNo)),
				DocumentID:  &documentID,
				Metadata:    metadata,
			},
		},
	})
	return err
}
