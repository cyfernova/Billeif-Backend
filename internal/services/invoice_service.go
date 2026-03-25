package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type InvoiceService struct {
	cfg          *config.Config
	repo         interfaces.InvoiceRepository
	productRepo  interfaces.ProductRepository
	customerRepo interfaces.CustomerRepository
	sqs          *sqs.Client
	s3           *S3Service
	email        *EmailService
	log          *logger.Logger
}

func NewInvoiceService(
	cfg *config.Config,
	repo interfaces.InvoiceRepository,
	productRepo interfaces.ProductRepository,
	customerRepo interfaces.CustomerRepository,
	aws *awsclients.Config,
	s3 *S3Service,
	email *EmailService,
	log *logger.Logger,
) *InvoiceService {
	return &InvoiceService{
		cfg:          cfg,
		repo:         repo,
		productRepo:  productRepo,
		customerRepo: customerRepo,
		sqs:          aws.SQS,
		s3:           s3,
		email:        email,
		log:          log,
	}
}

type CreateInvoiceItemInput struct {
	ProductID   string  `json:"product_id"`
	Description string  `json:"description" binding:"required"`
	Quantity    float64 `json:"quantity" binding:"required,gt=0"`
	UnitPrice   float64 `json:"unit_price" binding:"required,gt=0"`
	TaxRate     float64 `json:"tax_rate"`
}

type CreateInvoiceInput struct {
	BusinessID string                   `json:"business_id,omitempty"`
	CustomerID string                   `json:"customer_id" binding:"required,uuid"`
	DueDate    time.Time                `json:"due_date" binding:"required"`
	Notes      string                   `json:"notes"`
	Items      []CreateInvoiceItemInput `json:"items" binding:"required,min=1,dive"`
}

func (s *InvoiceService) Create(ctx context.Context, input CreateInvoiceInput) (*models.Invoice, error) {
	_, err := s.customerRepo.GetByID(ctx, input.CustomerID, input.BusinessID)
	if err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
	}

	invoiceNo, err := s.generateInvoiceNumber(ctx, input.BusinessID)
	if err != nil {
		return nil, err
	}

	var subtotal, taxTotal float64
	items := make([]*models.InvoiceItem, len(input.Items))

	for i, item := range input.Items {
		itemSubtotal := item.Quantity * item.UnitPrice
		itemTax := itemSubtotal * (item.TaxRate / 100)
		subtotal += itemSubtotal
		taxTotal += itemTax

		var productID *string
		if item.ProductID != "" {
			productID = &item.ProductID
		}

		items[i] = &models.InvoiceItem{
			ProductID:   productID,
			Description: item.Description,
			Quantity:    item.Quantity,
			UnitPrice:   item.UnitPrice,
			TaxRate:     item.TaxRate,
			Total:       itemSubtotal + itemTax,
		}
	}

	invoice := &models.Invoice{
		BusinessID:  input.BusinessID,
		CustomerID:  input.CustomerID,
		InvoiceNo:   invoiceNo,
		Status:      "draft",
		InvoiceDate: time.Now(),
		DueDate:     input.DueDate,
		Subtotal:    subtotal,
		Tax:         taxTotal,
		Total:       subtotal + taxTotal,
		BalanceDue:  subtotal + taxTotal,
		Notes:       input.Notes,
		Currency:    "USD",
		Items:       items,
	}

	if err := s.repo.Create(ctx, invoice); err != nil {
		return nil, fmt.Errorf("failed to create invoice: %w", err)
	}

	go s.queuePDFGeneration(invoice.ID)

	return invoice, nil
}

func (s *InvoiceService) CreateByBusiness(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error) {
	input.BusinessID = businessID
	return s.Create(ctx, input)
}

func (s *InvoiceService) generateInvoiceNumber(ctx context.Context, businessID string) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("INV-%d-", year)
	return prefix + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000), nil
}

func (s *InvoiceService) queuePDFGeneration(invoiceID string) {
	ctx := context.Background()
	message := map[string]string{
		"type":       "generate_pdf",
		"invoice_id": invoiceID,
	}
	body, err := json.Marshal(message)
	if err != nil {
		s.log.Error("failed to marshal PDF generation message", "invoice_id", invoiceID, "error", err)
		return
	}

	_, err = s.sqs.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(s.cfg.SQS.InvoiceQueue),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		s.log.Error("failed to queue PDF generation", "invoice_id", invoiceID, "error", err)
	}
}

func (s *InvoiceService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Invoice, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

// GetForWorker fetches an invoice without tenant scoping. Only for trusted internal callers (workers).
func (s *InvoiceService) GetForWorker(ctx context.Context, id string) (*models.Invoice, error) {
	return s.repo.GetByIDInternal(ctx, id)
}

func (s *InvoiceService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

type UpdateInvoiceInput struct {
	DueDate time.Time `json:"due_date"`
	Notes   string    `json:"notes"`
}

func (s *InvoiceService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateInvoiceInput) (*models.Invoice, error) {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if invoice.Status != "draft" {
		return nil, fmt.Errorf("cannot update invoice with status: %s", invoice.Status)
	}

	if !input.DueDate.IsZero() {
		invoice.DueDate = input.DueDate
	}
	if input.Notes != "" {
		invoice.Notes = input.Notes
	}

	if err := s.repo.Update(ctx, invoice); err != nil {
		return nil, err
	}
	return invoice, nil
}

func (s *InvoiceService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	if invoice.Status != "draft" {
		return fmt.Errorf("cannot delete invoice with status: %s", invoice.Status)
	}
	return s.repo.Delete(ctx, invoice.ID)
}

func (s *InvoiceService) UpdateStatus(ctx context.Context, id, status string) error {
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *InvoiceService) SendByBusiness(ctx context.Context, businessID, id string) error {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}

	customer, err := s.customerRepo.GetByID(ctx, invoice.CustomerID, businessID)
	if err != nil {
		return err
	}

	if err := s.repo.UpdateStatus(ctx, id, "sent"); err != nil {
		return err
	}

	subject := fmt.Sprintf("Invoice %s", invoice.InvoiceNo)
	body := fmt.Sprintf("Please find attached invoice %s for amount %s%.2f", invoice.InvoiceNo, invoice.Currency, invoice.Total)
	return s.email.SendEmail(ctx, customer.Email, subject, body)
}

func (s *InvoiceService) GetPDFURLByBusiness(ctx context.Context, businessID, invoiceID string) (string, error) {
	invoice, err := s.GetByBusiness(ctx, businessID, invoiceID)
	if err != nil {
		return "", err
	}
	if invoice.PDFURL != "" {
		return invoice.PDFURL, nil
	}
	return "", fmt.Errorf("PDF not yet generated")
}

func (s *InvoiceService) GetNextNumber(ctx context.Context, businessID string) (string, error) {
	return s.generateInvoiceNumber(ctx, businessID)
}

// UpdatePDFUrl is called by the internal worker (no tenant context).
// Uses direct column update rather than tenant-scoped GetByID.
func (s *InvoiceService) UpdatePDFUrl(ctx context.Context, invoiceID, pdfURL string) error {
	return s.repo.UpdatePDFURL(ctx, invoiceID, pdfURL)
}

type Invoice = models.Invoice

// InvoiceServiceTestable is a test-friendly version of InvoiceService
type InvoiceServiceTestable struct {
	cfg          *config.Config
	repo         InvoiceRepositoryTestable
	productRepo  interfaces.ProductRepository
	customerRepo CustomerRepositoryTestable
	sqs          SQSServiceTestable
	s3           *S3Service
	email        EmailServiceTestable
	log          *logger.Logger
}

// InvoiceRepositoryTestable is the testable interface for InvoiceRepository
type InvoiceRepositoryTestable interface {
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

// CustomerRepositoryTestable is the testable interface for CustomerRepository
type CustomerRepositoryTestable interface {
	Create(ctx context.Context, customer *models.Customer) error
	GetByID(ctx context.Context, id, businessID string) (*models.Customer, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error)
	Update(ctx context.Context, customer *models.Customer) error
	Delete(ctx context.Context, id string) error
}

// SQSServiceTestable is the testable interface for SQS operations
type SQSServiceTestable interface {
	SendMessage(ctx context.Context, queueUrl string, message interface{}) error
}

// EmailServiceTestable is the testable interface for EmailService
type EmailServiceTestable interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

// NewInvoiceServiceForTesting creates an InvoiceServiceTestable for unit testing
func NewInvoiceServiceForTesting(
	repo InvoiceRepositoryTestable,
	productRepo interfaces.ProductRepository,
	customerRepo CustomerRepositoryTestable,
	sqs SQSServiceTestable,
	s3 *S3Service,
	email EmailServiceTestable,
	log *logger.Logger,
) *InvoiceServiceTestable {
	return &InvoiceServiceTestable{
		cfg:          nil,
		repo:         repo,
		productRepo:  productRepo,
		customerRepo: customerRepo,
		sqs:          sqs,
		s3:           s3,
		email:        email,
		log:          log,
	}
}

// Create creates an invoice (testable version)
func (s *InvoiceServiceTestable) Create(ctx context.Context, input CreateInvoiceInput) (*models.Invoice, error) {
	_, err := s.customerRepo.GetByID(ctx, input.CustomerID, input.BusinessID)
	if err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
	}

	invoiceNo, err := s.generateInvoiceNumber(ctx, input.BusinessID)
	if err != nil {
		return nil, err
	}

	var subtotal, taxTotal float64
	items := make([]*models.InvoiceItem, len(input.Items))

	for i, item := range input.Items {
		itemSubtotal := item.Quantity * item.UnitPrice
		itemTax := itemSubtotal * (item.TaxRate / 100)
		subtotal += itemSubtotal
		taxTotal += itemTax

		var productID *string
		if item.ProductID != "" {
			productID = &item.ProductID
		}

		items[i] = &models.InvoiceItem{
			ProductID:   productID,
			Description: item.Description,
			Quantity:    item.Quantity,
			UnitPrice:   item.UnitPrice,
			TaxRate:     item.TaxRate,
			Total:       itemSubtotal + itemTax,
		}
	}

	invoice := &models.Invoice{
		BusinessID:  input.BusinessID,
		CustomerID:  input.CustomerID,
		InvoiceNo:   invoiceNo,
		Status:      "draft",
		InvoiceDate: time.Now(),
		DueDate:     input.DueDate,
		Subtotal:    subtotal,
		Tax:         taxTotal,
		Total:       subtotal + taxTotal,
		BalanceDue:  subtotal + taxTotal,
		Notes:       input.Notes,
		Currency:    "USD",
		Items:       items,
	}

	if err := s.repo.Create(ctx, invoice); err != nil {
		return nil, fmt.Errorf("failed to create invoice: %w", err)
	}

	if s.sqs != nil {
		_ = s.queuePDFGeneration(ctx, invoice.ID)
	}

	return invoice, nil
}

// GetByBusiness retrieves an invoice by business ID and invoice ID
func (s *InvoiceServiceTestable) GetByBusiness(ctx context.Context, businessID, id string) (*models.Invoice, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

// List retrieves invoices for a business with pagination
func (s *InvoiceServiceTestable) List(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

// UpdateByBusiness updates an invoice (testable version)
func (s *InvoiceServiceTestable) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateInvoiceInput) (*models.Invoice, error) {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if invoice.Status != "draft" {
		return nil, fmt.Errorf("cannot update invoice with status: %s", invoice.Status)
	}

	if !input.DueDate.IsZero() {
		invoice.DueDate = input.DueDate
	}
	if input.Notes != "" {
		invoice.Notes = input.Notes
	}

	if err := s.repo.Update(ctx, invoice); err != nil {
		return nil, err
	}
	return invoice, nil
}

// DeleteByBusiness deletes an invoice (testable version)
func (s *InvoiceServiceTestable) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	if invoice.Status != "draft" {
		return fmt.Errorf("cannot delete invoice with status: %s", invoice.Status)
	}
	return s.repo.Delete(ctx, invoice.ID)
}

// SendByBusiness sends an invoice email (testable version)
func (s *InvoiceServiceTestable) SendByBusiness(ctx context.Context, businessID, id string) error {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}

	customer, err := s.customerRepo.GetByID(ctx, invoice.CustomerID, businessID)
	if err != nil {
		return err
	}

	if err := s.repo.UpdateStatus(ctx, id, "sent"); err != nil {
		return err
	}

	subject := fmt.Sprintf("Invoice %s", invoice.InvoiceNo)
	body := fmt.Sprintf("Please find attached invoice %s for amount %s%.2f", invoice.InvoiceNo, invoice.Currency, invoice.Total)
	return s.email.SendEmail(ctx, customer.Email, subject, body)
}

// GetPDFURLByBusiness gets the PDF URL for an invoice
func (s *InvoiceServiceTestable) GetPDFURLByBusiness(ctx context.Context, businessID, invoiceID string) (string, error) {
	invoice, err := s.GetByBusiness(ctx, businessID, invoiceID)
	if err != nil {
		return "", err
	}
	if invoice.PDFURL != "" {
		return invoice.PDFURL, nil
	}
	return "", fmt.Errorf("PDF not yet generated")
}

// GetNextNumber generates the next invoice number
func (s *InvoiceServiceTestable) GetNextNumber(ctx context.Context, businessID string) (string, error) {
	return s.generateInvoiceNumber(ctx, businessID)
}

// queuePDFGeneration queues PDF generation (testable version)
func (s *InvoiceServiceTestable) queuePDFGeneration(ctx context.Context, invoiceID string) error {
	message := map[string]string{
		"type":       "generate_pdf",
		"invoice_id": invoiceID,
	}
	return s.sqs.SendMessage(ctx, "invoice-queue", message)
}

// generateInvoiceNumber generates a unique invoice number
func (s *InvoiceServiceTestable) generateInvoiceNumber(ctx context.Context, businessID string) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("INV-%d-", year)
	return prefix + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000), nil
}
