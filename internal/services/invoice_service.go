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
	BusinessID string                   `json:"business_id" binding:"required,uuid"`
	CustomerID string                   `json:"customer_id" binding:"required,uuid"`
	DueDate    time.Time                `json:"due_date" binding:"required"`
	Notes      string                   `json:"notes"`
	Items      []CreateInvoiceItemInput `json:"items" binding:"required,min=1,dive"`
}

func (s *InvoiceService) Create(ctx context.Context, input CreateInvoiceInput) (*models.Invoice, error) {
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

func (s *InvoiceService) Get(ctx context.Context, id string) (*models.Invoice, error) {
	// GetByID already preloads Items, no need to call GetItems separately
	return s.repo.GetByID(ctx, id)
}

func (s *InvoiceService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

type UpdateInvoiceInput struct {
	DueDate time.Time `json:"due_date"`
	Notes   string    `json:"notes"`
}

func (s *InvoiceService) Update(ctx context.Context, id string, input UpdateInvoiceInput) (*models.Invoice, error) {
	invoice, err := s.repo.GetByID(ctx, id)
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

func (s *InvoiceService) Delete(ctx context.Context, id string) error {
	invoice, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if invoice.Status != "draft" {
		return fmt.Errorf("cannot delete invoice with status: %s", invoice.Status)
	}

	return s.repo.Delete(ctx, id)
}

func (s *InvoiceService) UpdateStatus(ctx context.Context, id, status string) error {
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *InvoiceService) Send(ctx context.Context, id string) error {
	invoice, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	customer, err := s.customerRepo.GetByID(ctx, invoice.CustomerID)
	if err != nil {
		return err
	}

	if err := s.repo.UpdateStatus(ctx, id, "sent"); err != nil {
		return err
	}

	subject := fmt.Sprintf("Invoice %s", invoice.InvoiceNo)
	body := fmt.Sprintf("Please find attached invoice %s for amount %s%.2f",
		invoice.InvoiceNo, invoice.Currency, invoice.Total)

	return s.email.SendEmail(ctx, customer.Email, subject, body)
}

func (s *InvoiceService) GetPDFURL(ctx context.Context, invoiceID string) (string, error) {
	invoice, err := s.repo.GetByID(ctx, invoiceID)
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

func (s *InvoiceService) UpdatePDFUrl(ctx context.Context, invoiceID, pdfURL string) error {
	invoice, err := s.repo.GetByID(ctx, invoiceID)
	if err != nil {
		return err
	}
	invoice.PDFURL = pdfURL
	return s.repo.Update(ctx, invoice)
}

type Invoice = models.Invoice
