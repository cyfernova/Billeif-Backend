package unit

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockPaymentRepository mocks the PaymentRepository interface
type MockPaymentRepository struct {
	mock.Mock
}

func (m *MockPaymentRepository) Create(ctx context.Context, payment *models.Payment) error {
	args := m.Called(ctx, payment)
	return args.Error(0)
}

func (m *MockPaymentRepository) GetByID(ctx context.Context, id, businessID string) (*models.Payment, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Payment), args.Error(1)
}

func (m *MockPaymentRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Payment, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Payment), args.Get(1).(int64), args.Error(2)
}

func (m *MockPaymentRepository) GetByInvoiceID(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	args := m.Called(ctx, invoiceID, page, limit)
	return args.Get(0).([]*models.Payment), args.Get(1).(int64), args.Error(2)
}

func (m *MockPaymentRepository) Update(ctx context.Context, payment *models.Payment) error {
	args := m.Called(ctx, payment)
	return args.Error(0)
}

func (m *MockPaymentRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// MockInvoiceRepositoryPayment mocks the InvoiceRepository interface for payment tests
type MockInvoiceRepositoryPayment struct {
	mock.Mock
}

func (m *MockInvoiceRepositoryPayment) Create(ctx context.Context, invoice *models.Invoice) error {
	args := m.Called(ctx, invoice)
	return args.Error(0)
}

func (m *MockInvoiceRepositoryPayment) GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Invoice), args.Error(1)
}

func (m *MockInvoiceRepositoryPayment) GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Invoice), args.Error(1)
}

func (m *MockInvoiceRepositoryPayment) GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error) {
	args := m.Called(ctx, businessID, invoiceNo)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Invoice), args.Error(1)
}

func (m *MockInvoiceRepositoryPayment) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Invoice), args.Get(1).(int64), args.Error(2)
}

func (m *MockInvoiceRepositoryPayment) GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error) {
	args := m.Called(ctx, invoiceID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.InvoiceItem), args.Error(1)
}

func (m *MockInvoiceRepositoryPayment) Update(ctx context.Context, invoice *models.Invoice) error {
	args := m.Called(ctx, invoice)
	return args.Error(0)
}

func (m *MockInvoiceRepositoryPayment) UpdateStatus(ctx context.Context, invoiceID string, status string) error {
	args := m.Called(ctx, invoiceID, status)
	return args.Error(0)
}

func (m *MockInvoiceRepositoryPayment) UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error {
	args := m.Called(ctx, invoiceID, pdfURL)
	return args.Error(0)
}

func (m *MockInvoiceRepositoryPayment) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// TestCreatePayment_Success tests successful payment creation
func TestPaymentService_Create_Success(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "invoice-456"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Total:      1000.00,
		PaidAmount: 0,
		BalanceDue: 1000.00,
		Currency:   "USD",
		Status:     "sent",
	}

	input := services.CreatePaymentInput{
		InvoiceID:     invoiceID,
		Amount:        500.00,
		PaymentMethod: "credit_card",
		Reference:     "REF-001",
		Notes:         "Partial payment",
	}

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(p *models.Payment) bool {
		return p.Amount == 500.00 && p.PaymentMethod == "credit_card" && p.InvoiceID == invoiceID
	})).Return(nil)
	mockInvoiceRepo.On("Update", ctx, mock.AnythingOfType("*models.Invoice")).Return(nil)

	payment, err := svc.Create(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, payment)
	assert.Equal(t, 500.00, payment.Amount)
	assert.Equal(t, "credit_card", payment.PaymentMethod)
	assert.Equal(t, invoiceID, payment.InvoiceID)
	mockRepo.AssertExpectations(t)
	mockInvoiceRepo.AssertExpectations(t)
}

// TestCreatePayment_FullPayment tests full payment that marks invoice as paid
func TestPaymentService_Create_FullPayment(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "invoice-456"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Total:      1000.00,
		PaidAmount: 0,
		BalanceDue: 1000.00,
		Currency:   "USD",
		Status:     "sent",
	}

	input := services.CreatePaymentInput{
		InvoiceID:     invoiceID,
		Amount:        1000.00,
		PaymentMethod: "bank_transfer",
	}

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Payment")).Return(nil)
	mockInvoiceRepo.On("Update", ctx, mock.MatchedBy(func(i *models.Invoice) bool {
		return i.Status == "paid" && i.PaidAmount == 1000.00 && i.PaidAt != nil
	})).Return(nil)

	payment, err := svc.Create(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, payment)
	assert.Equal(t, 1000.00, payment.Amount)
	mockInvoiceRepo.AssertExpectations(t)
}

// TestCreatePayment_PartialPayment tests partial payment
func TestPaymentService_Create_PartialPayment(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "invoice-456"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Total:      1000.00,
		PaidAmount: 300.00,
		BalanceDue: 700.00,
		Currency:   "USD",
		Status:     models.InvoiceStatusPartiallyPaid,
	}

	input := services.CreatePaymentInput{
		InvoiceID:     invoiceID,
		Amount:        200.00,
		PaymentMethod: "cash",
	}

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Payment")).Return(nil)
	mockInvoiceRepo.On("Update", ctx, mock.MatchedBy(func(i *models.Invoice) bool {
		return i.Status == models.InvoiceStatusPartiallyPaid && i.PaidAmount == 500.00 && i.BalanceDue == 500.00
	})).Return(nil)

	payment, err := svc.Create(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, payment)
	mockInvoiceRepo.AssertExpectations(t)
}

// TestCreatePayment_InvoiceNotFound tests payment creation with invalid invoice
func TestPaymentService_Create_InvoiceNotFound(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "nonexistent"

	input := services.CreatePaymentInput{
		InvoiceID:     invoiceID,
		Amount:        100.00,
		PaymentMethod: "credit_card",
	}

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(nil, errors.New("invoice not found"))

	payment, err := svc.Create(ctx, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, payment)
	assert.Contains(t, err.Error(), "invoice not found")
	mockInvoiceRepo.AssertExpectations(t)
}

// TestCreatePayment_RepositoryError tests payment creation with repository error
func TestPaymentService_Create_RepositoryError(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "invoice-456"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Total:      1000.00,
		Currency:   "USD",
		Status:     "sent",
	}

	input := services.CreatePaymentInput{
		InvoiceID:     invoiceID,
		Amount:        100.00,
		PaymentMethod: "credit_card",
	}

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Payment")).Return(errors.New("database error"))

	payment, err := svc.Create(ctx, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, payment)
	assert.Contains(t, err.Error(), "failed to create payment")
	mockRepo.AssertExpectations(t)
}

// TestCreatePayment_InvoiceUpdateError tests payment creation when invoice update fails
func TestPaymentService_Create_InvoiceUpdateError(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "invoice-456"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Total:      1000.00,
		Currency:   "USD",
		Status:     "sent",
	}

	input := services.CreatePaymentInput{
		InvoiceID:     invoiceID,
		Amount:        100.00,
		PaymentMethod: "credit_card",
	}

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Payment")).Return(nil)
	mockInvoiceRepo.On("Update", ctx, mock.AnythingOfType("*models.Invoice")).Return(errors.New("invoice update failed"))

	payment, err := svc.Create(ctx, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, payment)
	assert.Contains(t, err.Error(), "failed to update invoice")
	mockRepo.AssertExpectations(t)
}

// TestCreatePayment_WithPaymentDate tests payment creation with custom payment date
func TestPaymentService_Create_WithPaymentDate(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "invoice-456"
	customDate := "2026-01-15T10:00:00Z"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Total:      1000.00,
		Currency:   "USD",
		Status:     "sent",
	}

	input := services.CreatePaymentInput{
		InvoiceID:     invoiceID,
		Amount:        500.00,
		PaymentMethod: "check",
		PaymentDate:   customDate,
	}

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(p *models.Payment) bool {
		return p.PaymentDate.Year() == 2026 && p.PaymentDate.Month() == 1 && p.PaymentDate.Day() == 15
	})).Return(nil)
	mockInvoiceRepo.On("Update", ctx, mock.AnythingOfType("*models.Invoice")).Return(nil)

	payment, err := svc.Create(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, payment)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_Success tests successful payment retrieval
func TestPaymentService_GetByBusiness_Success(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "payment-789"

	expected := &models.Payment{
		ID:            paymentID,
		BusinessID:    businessID,
		InvoiceID:     "invoice-456",
		Amount:        250.00,
		PaymentMethod: "credit_card",
	}

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(expected, nil)

	payment, err := svc.GetByBusiness(ctx, businessID, paymentID)

	assert.NoError(t, err)
	assert.NotNil(t, payment)
	assert.Equal(t, paymentID, payment.ID)
	assert.Equal(t, businessID, payment.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_NotFound tests payment not found
func TestPaymentService_GetByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "nonexistent"

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(nil, errors.New("payment not found"))

	payment, err := svc.GetByBusiness(ctx, businessID, paymentID)

	assert.Error(t, err)
	assert.Nil(t, payment)
	mockRepo.AssertExpectations(t)
}

// TestListByInvoice_Success tests listing payments by invoice
func TestPaymentService_ListByInvoice_Success(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	invoiceID := "invoice-456"
	page := 1
	limit := 10

	expectedPayments := []*models.Payment{
		{ID: "p1", InvoiceID: invoiceID, Amount: 100.00},
		{ID: "p2", InvoiceID: invoiceID, Amount: 200.00},
	}

	mockRepo.On("GetByInvoiceID", ctx, invoiceID, page, limit).Return(expectedPayments, int64(2), nil)

	payments, total, err := svc.ListByInvoice(ctx, invoiceID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, payments, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestListByInvoice_EmptyResult tests listing with no payments
func TestPaymentService_ListByInvoice_EmptyResult(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	invoiceID := "invoice-456"

	mockRepo.On("GetByInvoiceID", ctx, invoiceID, 1, 10).Return([]*models.Payment{}, int64(0), nil)

	payments, total, err := svc.ListByInvoice(ctx, invoiceID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, payments, 0)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestListByBusiness_Success tests listing all payments for a business.
func TestPaymentService_ListByBusiness_Success(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	expectedPayments := []*models.Payment{
		{ID: "p1", BusinessID: businessID, InvoiceID: "invoice-1", Amount: 100.00},
		{ID: "p2", BusinessID: businessID, InvoiceID: "invoice-2", Amount: 200.00},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 20).Return(expectedPayments, int64(2), nil)

	payments, total, err := svc.ListByBusiness(ctx, businessID, 1, 20)

	assert.NoError(t, err)
	assert.Len(t, payments, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestListByInvoiceAndBusiness_Success tests listing payments scoped to business
func TestPaymentService_ListByInvoiceAndBusiness_Success(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "invoice-456"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
	}

	expectedPayments := []*models.Payment{
		{ID: "p1", InvoiceID: invoiceID, Amount: 500.00},
	}

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockRepo.On("GetByInvoiceID", ctx, invoiceID, 1, 10).Return(expectedPayments, int64(1), nil)

	payments, total, err := svc.ListByInvoiceAndBusiness(ctx, businessID, invoiceID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, payments, 1)
	assert.Equal(t, int64(1), total)
	mockInvoiceRepo.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestListByInvoiceAndBusiness_InvoiceNotFound tests listing when invoice not found
func TestPaymentService_ListByInvoiceAndBusiness_InvoiceNotFound(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	invoiceID := "nonexistent"

	mockInvoiceRepo.On("GetByID", ctx, invoiceID, businessID).Return(nil, errors.New("invoice not found"))

	payments, total, err := svc.ListByInvoiceAndBusiness(ctx, businessID, invoiceID, 1, 10)

	assert.Error(t, err)
	assert.Nil(t, payments)
	assert.Equal(t, int64(0), total)
	assert.Contains(t, err.Error(), "invoice not found")
	mockInvoiceRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_Success tests successful payment update
func TestPaymentService_UpdateByBusiness_Success(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "payment-789"

	existing := &models.Payment{
		ID:            paymentID,
		BusinessID:    businessID,
		InvoiceID:     "invoice-456",
		Amount:        100.00,
		PaymentMethod: "credit_card",
	}

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(p *models.Payment) bool {
		return p.Amount == 150.00 && p.PaymentMethod == "bank_transfer"
	})).Return(nil)

	input := services.UpdatePaymentInput{
		Amount:        150.00,
		PaymentMethod: "bank_transfer",
	}

	payment, err := svc.UpdateByBusiness(ctx, businessID, paymentID, input)

	assert.NoError(t, err)
	assert.NotNil(t, payment)
	assert.Equal(t, 150.00, payment.Amount)
	assert.Equal(t, "bank_transfer", payment.PaymentMethod)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_PartialUpdate tests partial update (only notes)
func TestPaymentService_UpdateByBusiness_PartialUpdate(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "payment-789"

	existing := &models.Payment{
		ID:            paymentID,
		BusinessID:    businessID,
		Amount:        100.00,
		PaymentMethod: "cash",
		Notes:         "",
	}

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(p *models.Payment) bool {
		return p.Notes == "Updated notes" && p.Amount == 100.00
	})).Return(nil)

	input := services.UpdatePaymentInput{
		Notes: "Updated notes",
	}

	payment, err := svc.UpdateByBusiness(ctx, businessID, paymentID, input)

	assert.NoError(t, err)
	assert.NotNil(t, payment)
	assert.Equal(t, "Updated notes", payment.Notes)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_NotFound tests update on non-existent payment
func TestPaymentService_UpdateByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "nonexistent"

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(nil, errors.New("payment not found"))

	input := services.UpdatePaymentInput{
		Amount: 150.00,
	}

	payment, err := svc.UpdateByBusiness(ctx, businessID, paymentID, input)

	assert.Error(t, err)
	assert.Nil(t, payment)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_RepositoryError tests update with repository error
func TestPaymentService_UpdateByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "payment-789"

	existing := &models.Payment{
		ID:            paymentID,
		BusinessID:    businessID,
		Amount:        100.00,
		PaymentMethod: "cash",
	}

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.Payment")).Return(errors.New("database error"))

	input := services.UpdatePaymentInput{
		Amount: 150.00,
	}

	payment, err := svc.UpdateByBusiness(ctx, businessID, paymentID, input)

	assert.Error(t, err)
	assert.Nil(t, payment)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_Success tests successful payment deletion
func TestPaymentService_DeleteByBusiness_Success(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "payment-789"

	existing := &models.Payment{
		ID:         paymentID,
		BusinessID: businessID,
	}

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(existing, nil)
	mockRepo.On("Delete", ctx, paymentID).Return(nil)

	err := svc.DeleteByBusiness(ctx, businessID, paymentID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_NotFound tests deletion when payment not found
func TestPaymentService_DeleteByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "nonexistent"

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(nil, errors.New("payment not found"))

	err := svc.DeleteByBusiness(ctx, businessID, paymentID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_RepositoryError tests deletion with repository error
func TestPaymentService_DeleteByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockPaymentRepository)
	mockInvoiceRepo := new(MockInvoiceRepositoryPayment)
	log := logger.New()

	svc := services.NewPaymentServiceForTesting(mockRepo, mockInvoiceRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	paymentID := "payment-789"

	existing := &models.Payment{
		ID:         paymentID,
		BusinessID: businessID,
	}

	mockRepo.On("GetByID", ctx, paymentID, businessID).Return(existing, nil)
	mockRepo.On("Delete", ctx, paymentID).Return(errors.New("database error"))

	err := svc.DeleteByBusiness(ctx, businessID, paymentID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}
