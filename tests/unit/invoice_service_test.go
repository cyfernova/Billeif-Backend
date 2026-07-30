package unit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/invoiceresolution"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockInvoiceRepository mocks the InvoiceRepository interface
type MockInvoiceRepository struct {
	mock.Mock
}

func (m *MockInvoiceRepository) Create(ctx context.Context, invoice *models.Invoice) error {
	args := m.Called(ctx, invoice)
	return args.Error(0)
}

func (m *MockInvoiceRepository) GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Invoice), args.Error(1)
}

func (m *MockInvoiceRepository) GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Invoice), args.Error(1)
}

func (m *MockInvoiceRepository) GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error) {
	args := m.Called(ctx, businessID, invoiceNo)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Invoice), args.Error(1)
}

func (m *MockInvoiceRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Invoice), args.Get(1).(int64), args.Error(2)
}

func (m *MockInvoiceRepository) GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error) {
	args := m.Called(ctx, invoiceID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.InvoiceItem), args.Error(1)
}

func (m *MockInvoiceRepository) Update(ctx context.Context, invoice *models.Invoice) error {
	args := m.Called(ctx, invoice)
	return args.Error(0)
}

func (m *MockInvoiceRepository) UpdateStatus(ctx context.Context, invoiceID string, status string) error {
	args := m.Called(ctx, invoiceID, status)
	return args.Error(0)
}

func (m *MockInvoiceRepository) UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error {
	args := m.Called(ctx, invoiceID, pdfURL)
	return args.Error(0)
}

func (m *MockInvoiceRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockInvoiceRepository) CreateDraftAtomic(ctx context.Context, command interfaces.AtomicInvoiceDraft) (*interfaces.AtomicInvoiceDraftResult, error) {
	if err := m.Create(ctx, command.Invoice); err != nil {
		return nil, err
	}
	return &interfaces.AtomicInvoiceDraftResult{Invoice: command.Invoice}, nil
}

func (m *MockInvoiceRepository) ReplayCompletedDraft(
	context.Context,
	string,
	string,
	string,
	string,
) (*interfaces.AtomicInvoiceDraftResult, error) {
	return nil, nil
}

func (m *MockInvoiceRepository) ResolveInvoiceLines(
	_ context.Context,
	request invoiceresolution.Request,
) ([]invoiceresolution.LineSnapshot, error) {
	snapshots := make([]invoiceresolution.LineSnapshot, len(request.Lines))
	for index, line := range request.Lines {
		snapshots[index] = invoiceresolution.LineSnapshot{
			ProductID: line.ProductID, VariantID: line.VariantID, WarehouseID: line.WarehouseID,
		}
	}
	return snapshots, nil
}

type invoiceBusinessRepository struct{}

func (invoiceBusinessRepository) Create(context.Context, *models.BusinessProfile) error { return nil }
func (invoiceBusinessRepository) GetByID(_ context.Context, id string) (*models.BusinessProfile, error) {
	return &models.BusinessProfile{
		ID:       id,
		Name:     "Test Business",
		Email:    "business@example.com",
		Currency: "USD",
	}, nil
}
func (invoiceBusinessRepository) Update(context.Context, *models.BusinessProfile) error { return nil }
func (invoiceBusinessRepository) Delete(context.Context, string) error                  { return nil }
func (invoiceBusinessRepository) List(context.Context, string, int, int) ([]*models.BusinessProfile, int64, error) {
	return nil, 0, nil
}

// MockCustomerRepository mocks the CustomerRepository interface
type MockCustomerRepository struct {
	mock.Mock
}

func (m *MockCustomerRepository) Create(ctx context.Context, customer *models.Customer) error {
	args := m.Called(ctx, customer)
	return args.Error(0)
}

func (m *MockCustomerRepository) GetByID(ctx context.Context, id, businessID string) (*models.Customer, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Customer), args.Error(1)
}

func (m *MockCustomerRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Customer), args.Get(1).(int64), args.Error(2)
}

func (m *MockCustomerRepository) Update(ctx context.Context, customer *models.Customer) error {
	args := m.Called(ctx, customer)
	return args.Error(0)
}

func (m *MockCustomerRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// MockProductRepository mocks the ProductRepository interface
type MockProductRepository struct {
	mock.Mock
}

func (m *MockProductRepository) Create(ctx context.Context, product *models.Product) error {
	args := m.Called(ctx, product)
	return args.Error(0)
}

func (m *MockProductRepository) GetByID(ctx context.Context, id, businessID string) (*models.Product, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Product), args.Error(1)
}

func (m *MockProductRepository) GetByIDWithoutTenant(ctx context.Context, id string) (*models.Product, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Product), args.Error(1)
}

func (m *MockProductRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Product), args.Get(1).(int64), args.Error(2)
}

func (m *MockProductRepository) GetBySKU(ctx context.Context, businessID, sku string) (*models.Product, error) {
	args := m.Called(ctx, businessID, sku)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Product), args.Error(1)
}

func (m *MockProductRepository) Update(ctx context.Context, product *models.Product) error {
	args := m.Called(ctx, product)
	return args.Error(0)
}

func (m *MockProductRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockProductRepository) AdjustStock(ctx context.Context, productID string, quantity int64) error {
	args := m.Called(ctx, productID, quantity)
	return args.Error(0)
}

// MockEmailService mocks the Email service
type MockEmailService struct {
	mock.Mock
}

func (m *MockEmailService) SendEmail(ctx context.Context, to, subject, body string) error {
	args := m.Called(ctx, to, subject, body)
	return args.Error(0)
}

func newInvoiceService(
	repo *MockInvoiceRepository,
	productRepo *MockProductRepository,
	customerRepo *MockCustomerRepository,
	email *MockEmailService,
	log *logger.Logger,
) *services.InvoiceService {
	return services.NewInvoiceService(nil, nil, repo, invoiceBusinessRepository{}, productRepo, customerRepo, nil, nil, nil, email, log)
}

// TestCreateInvoice_Success tests successful invoice creation
func TestInvoiceService_Create_Success(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "customer-456"
	dueDate := time.Now().Add(24 * time.Hour)

	customer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Test Customer",
		Email:      "customer@example.com",
	}

	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(customer, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Invoice")).Return(nil)

	input := services.CreateInvoiceInput{
		BusinessID:     businessID,
		CustomerID:     customerID,
		IdempotencyKey: uuid.NewString(),
		DueDate:        dueDate,
		Notes:          "Test invoice",
		Items: []services.CreateInvoiceItemInput{
			{
				Description: "Product A",
				Quantity:    2,
				UnitPrice:   100.00,
				TaxRate:     10,
			},
		},
	}

	invoice, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	assert.Equal(t, businessID, invoice.BusinessID)
	assert.Equal(t, customerID, models.StringValue(invoice.CustomerID))
	assert.Equal(t, "draft", invoice.Status)
	assert.Equal(t, models.InvoiceOriginManual, invoice.Origin)
	assert.Nil(t, invoice.InvoiceNo, "draft creation must not allocate an invoice number")
	assert.Equal(t, 200.00, invoice.Subtotal) // 2 * 100
	assert.Equal(t, 20.00, invoice.Tax)       // 200 * 10%
	assert.Equal(t, 220.00, invoice.Total)    // 200 + 20
	assert.Equal(t, 220.00, invoice.BalanceDue)
	assert.Equal(t, "USD", invoice.Currency)
	assert.Len(t, invoice.Items, 1)

	mockCustomer.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestInvoiceService_Create_DerivesSubscriptionOriginFromRunMarkers(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()
	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	customer := &models.Customer{ID: "customer-456", BusinessID: "business-123", Name: "Test Customer"}
	mockCustomer.On("GetByID", ctx, customer.ID, customer.BusinessID).Return(customer, nil)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(invoice *models.Invoice) bool {
		return invoice.Origin == models.InvoiceOriginSubscription &&
			models.StringValue(invoice.OriginSubscriptionID) == "subscription-123" &&
			models.StringValue(invoice.OriginRunID) == "run-456"
	})).Return(nil)

	invoice, err := svc.Create(ctx, services.CreateInvoiceInput{
		BusinessID:           customer.BusinessID,
		CustomerID:           customer.ID,
		IdempotencyKey:       uuid.NewString(),
		DueDate:              time.Now().Add(24 * time.Hour),
		OriginSubscriptionID: "subscription-123",
		OriginRunID:          "run-456",
		Items: []services.CreateInvoiceItemInput{{
			Description: "Subscription line",
			Quantity:    1,
			UnitPrice:   100,
		}},
	})

	assert.NoError(t, err)
	if assert.NotNil(t, invoice) {
		assert.Equal(t, models.InvoiceOriginSubscription, invoice.Origin)
	}
	mockCustomer.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestCreateInvoiceInput_IgnoresInternalSubscriptionMarkersFromPublicJSON(t *testing.T) {
	var input services.CreateInvoiceInput
	err := json.Unmarshal([]byte(`{
		"customer_id": "customer-456",
		"origin_subscription_id": "forged-subscription",
		"origin_run_id": "forged-run"
	}`), &input)

	assert.NoError(t, err)
	assert.Empty(t, input.OriginSubscriptionID)
	assert.Empty(t, input.OriginRunID)
}

// TestCreateInvoice_CustomerNotFound tests invoice creation when customer is not found
func TestInvoiceService_Create_CustomerNotFound(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "nonexistent-customer"

	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(nil, errors.New("customer not found"))

	input := services.CreateInvoiceInput{
		BusinessID:     businessID,
		CustomerID:     customerID,
		IdempotencyKey: uuid.NewString(),
		DueDate:        time.Now().Add(24 * time.Hour),
		Items: []services.CreateInvoiceItemInput{
			{
				Description: "Product A",
				Quantity:    1,
				UnitPrice:   100.00,
				TaxRate:     0,
			},
		},
	}

	invoice, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, invoice)
	assert.Contains(t, err.Error(), "customer not found")
	mockCustomer.AssertExpectations(t)
}

// TestCreateInvoice_MultipleItems tests invoice creation with multiple items
func TestInvoiceService_Create_MultipleItems(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "customer-456"

	customer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Test Customer",
	}

	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(customer, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Invoice")).Return(nil)

	input := services.CreateInvoiceInput{
		BusinessID:     businessID,
		CustomerID:     customerID,
		IdempotencyKey: uuid.NewString(),
		DueDate:        time.Now().Add(24 * time.Hour),
		Items: []services.CreateInvoiceItemInput{
			{
				Description: "Product A",
				Quantity:    2,
				UnitPrice:   100.00,
				TaxRate:     10,
			},
			{
				Description: "Product B",
				Quantity:    3,
				UnitPrice:   50.00,
				TaxRate:     5,
			},
		},
	}

	invoice, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	assert.Equal(t, 350.00, invoice.Subtotal) // (2*100) + (3*50)
	assert.Equal(t, 27.50, invoice.Tax)       // (200*10%) + (150*5%)
	assert.Equal(t, 377.50, invoice.Total)    // 350 + 27.50
	assert.Len(t, invoice.Items, 2)

	mockCustomer.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestCreateInvoice_ZeroTaxRate tests invoice creation with zero tax
func TestInvoiceService_Create_ZeroTaxRate(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "customer-456"

	customer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Test Customer",
	}

	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(customer, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Invoice")).Return(nil)

	input := services.CreateInvoiceInput{
		BusinessID:     businessID,
		CustomerID:     customerID,
		IdempotencyKey: uuid.NewString(),
		DueDate:        time.Now().Add(24 * time.Hour),
		Items: []services.CreateInvoiceItemInput{
			{
				Description: "Product A",
				Quantity:    1,
				UnitPrice:   100.00,
				TaxRate:     0,
			},
		},
	}

	invoice, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	assert.Equal(t, 100.00, invoice.Subtotal)
	assert.Equal(t, 0.00, invoice.Tax)
	assert.Equal(t, 100.00, invoice.Total)

	mockCustomer.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_Success tests successful invoice retrieval
func TestInvoiceService_GetByBusiness_Success(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"

	expectedInvoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status:     "draft",
		Total:      100.00,
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(expectedInvoice, nil)

	invoice, err := svc.GetByBusiness(ctx, businessID, invoiceID)

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	assert.Equal(t, invoiceID, invoice.ID)
	assert.Equal(t, businessID, invoice.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_NotFound tests invoice not found scenario
func TestInvoiceService_GetByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "nonexistent"

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(nil, errors.New("invoice not found"))

	invoice, err := svc.GetByBusiness(ctx, businessID, invoiceID)

	assert.Error(t, err)
	assert.Nil(t, invoice)
	mockRepo.AssertExpectations(t)
}

// TestList_Success tests listing invoices with pagination
func TestInvoiceService_List_Success(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	page := 1
	limit := 10

	expectedInvoices := []*models.Invoice{
		{ID: "invoice-1", BusinessID: businessID, Total: 100.00},
		{ID: "invoice-2", BusinessID: businessID, Total: 200.00},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, page, limit).Return(expectedInvoices, int64(2), nil)

	invoices, total, err := svc.List(ctx, businessID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, invoices, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_Success tests successful invoice update
func TestInvoiceService_UpdateByBusiness_Success(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"

	existingInvoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status:     "draft",
		Total:      100.00,
	}

	newDueDate := time.Now().Add(48 * time.Hour)

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(existingInvoice, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.Invoice")).Return(nil)

	input := services.UpdateInvoiceInput{
		DueDate: newDueDate,
		Notes:   "Updated notes",
	}

	invoice, err := svc.UpdateByBusiness(ctx, businessID, invoiceID, input)

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	assert.Equal(t, "Updated notes", invoice.Notes)
	mockRepo.AssertExpectations(t)
}

func TestInvoiceService_Create_WithRenderProfile(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "customer-456"
	renderProfileID := "11111111-1111-1111-1111-111111111111"

	customer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Template Customer",
		Email:      "customer@example.com",
	}

	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(customer, nil)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(invoice *models.Invoice) bool {
		return invoice.RenderProfileID != nil && *invoice.RenderProfileID == renderProfileID
	})).Return(nil)

	input := services.CreateInvoiceInput{
		BusinessID:      businessID,
		CustomerID:      customerID,
		IdempotencyKey:  uuid.NewString(),
		RenderProfileID: renderProfileID,
		DueDate:         time.Now().Add(24 * time.Hour),
		Items: []services.CreateInvoiceItemInput{
			{
				Description: "Product A",
				Quantity:    1,
				UnitPrice:   100.00,
				TaxRate:     0,
			},
		},
	}

	invoice, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	if assert.NotNil(t, invoice.RenderProfileID) {
		assert.Equal(t, renderProfileID, *invoice.RenderProfileID)
	}
	mockCustomer.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestInvoiceService_Create_WithInvalidRenderProfileID(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "customer-456"

	input := services.CreateInvoiceInput{
		BusinessID:      businessID,
		CustomerID:      customerID,
		IdempotencyKey:  uuid.NewString(),
		RenderProfileID: "not-a-uuid",
		DueDate:         time.Now().Add(24 * time.Hour),
		Items: []services.CreateInvoiceItemInput{
			{
				Description: "Product A",
				Quantity:    1,
				UnitPrice:   100.00,
				TaxRate:     0,
			},
		},
	}

	invoice, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, invoice)
	assert.Contains(t, err.Error(), "invalid render profile id")
	mockCustomer.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestInvoiceService_UpdateByBusiness_AllowsSentWithRenderProfile(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"
	renderProfileID := "22222222-2222-2222-2222-222222222222"

	existingInvoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status:     "sent",
		Total:      100.00,
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(existingInvoice, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(invoice *models.Invoice) bool {
		return invoice.RenderProfileID != nil && *invoice.RenderProfileID == renderProfileID
	})).Return(nil)

	input := services.UpdateInvoiceInput{
		Notes:           "Resent with updated template",
		RenderProfileID: &renderProfileID,
	}

	invoice, err := svc.UpdateByBusiness(ctx, businessID, invoiceID, input)

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	if assert.NotNil(t, invoice.RenderProfileID) {
		assert.Equal(t, renderProfileID, *invoice.RenderProfileID)
	}
	mockRepo.AssertExpectations(t)
}

func TestInvoiceService_UpdateByBusiness_RejectsInvalidRenderProfileID(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"
	invalidID := "render-profile"

	existingInvoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status:     "sent",
		Total:      100.00,
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(existingInvoice, nil)

	input := services.UpdateInvoiceInput{
		RenderProfileID: &invalidID,
	}

	invoice, err := svc.UpdateByBusiness(ctx, businessID, invoiceID, input)

	assert.Error(t, err)
	assert.Nil(t, invoice)
	assert.Contains(t, err.Error(), "invalid render profile id")
	mockRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestUpdateByBusiness_InvalidStatus tests update on non-editable invoice
func TestInvoiceService_UpdateByBusiness_InvalidStatus(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"

	existingInvoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status:     "paid",
		Total:      100.00,
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(existingInvoice, nil)

	input := services.UpdateInvoiceInput{
		Notes: "Updated notes",
	}

	invoice, err := svc.UpdateByBusiness(ctx, businessID, invoiceID, input)

	assert.Error(t, err)
	assert.Nil(t, invoice)
	assert.Contains(t, err.Error(), "cannot update invoice with status: paid")
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_Success tests successful invoice deletion
func TestInvoiceService_DeleteByBusiness_Success(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"

	existingInvoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status:     "draft",
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(existingInvoice, nil)
	mockRepo.On("Delete", ctx, invoiceID).Return(nil)

	err := svc.DeleteByBusiness(ctx, businessID, invoiceID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_InvalidStatus tests deletion of non-draft invoice
func TestInvoiceService_DeleteByBusiness_InvalidStatus(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"

	existingInvoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status:     "paid",
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(existingInvoice, nil)

	err := svc.DeleteByBusiness(ctx, businessID, invoiceID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete invoice with status: paid")
	mockRepo.AssertExpectations(t)
}

// TestSendByBusiness_Success tests successful invoice sending
func TestInvoiceService_SendByBusiness_Success(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"
	customerID := "customer-789"
	issuedAt := time.Now()

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		CustomerID: models.StringPointer(customerID),
		InvoiceNo:  models.StringPointer("INV-2026-123456"),
		Status:     models.InvoiceStatusIssued,
		IssuedAt:   &issuedAt,
		Total:      100.00,
		Currency:   "USD",
	}

	customer := &models.Customer{
		ID:    customerID,
		Email: "customer@example.com",
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(customer, nil)
	mockRepo.On("UpdateStatus", ctx, invoiceID, "sent").Return(nil)
	mockEmail.On("SendEmail", ctx, "customer@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	err := svc.SendByBusiness(ctx, businessID, invoiceID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockCustomer.AssertExpectations(t)
	mockEmail.AssertExpectations(t)
}

// TestSendByBusiness_CustomerNotFound tests sending when customer not found
func TestInvoiceService_SendByBusiness_CustomerNotFound(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"
	customerID := "customer-789"
	issuedAt := time.Now()

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		CustomerID: models.StringPointer(customerID),
		InvoiceNo:  models.StringPointer("INV-2026-123457"),
		Status:     models.InvoiceStatusIssued,
		IssuedAt:   &issuedAt,
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)
	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(nil, errors.New("customer not found"))

	err := svc.SendByBusiness(ctx, businessID, invoiceID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "customer not found")
	mockRepo.AssertExpectations(t)
	mockCustomer.AssertExpectations(t)
}

func TestInvoiceService_SendByBusiness_RejectsUnissuedDraft(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"
	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		CustomerID: models.StringPointer("customer-789"),
		Status:     models.InvoiceStatusDraft,
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)

	err := svc.SendByBusiness(ctx, businessID, invoiceID)

	assert.ErrorIs(t, err, models.ErrInvalidInvoiceLifecycle)
	mockCustomer.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything)
	mockEmail.AssertNotCalled(t, "SendEmail", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestGetPDFURLByBusiness_Success tests getting existing PDF URL
func TestInvoiceService_GetPDFURLByBusiness_Success(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"
	pdfURL := "https://s3.example.com/invoices/test.pdf"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		PDFURL:     pdfURL,
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)

	url, err := svc.GetPDFURLByBusiness(ctx, businessID, invoiceID)

	assert.NoError(t, err)
	assert.Equal(t, pdfURL, url)
	mockRepo.AssertExpectations(t)
}

// TestGetPDFURLByBusiness_NotGenerated tests PDF not yet generated
func TestInvoiceService_GetPDFURLByBusiness_NotGenerated(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	invoiceID := "invoice-456"

	invoice := &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		PDFURL:     "",
	}

	mockRepo.On("GetByID", ctx, invoiceID, businessID).Return(invoice, nil)

	url, err := svc.GetPDFURLByBusiness(ctx, businessID, invoiceID)

	assert.Error(t, err)
	assert.Empty(t, url)
	assert.Contains(t, err.Error(), "PDF not yet generated")
	mockRepo.AssertExpectations(t)
}

// TestCreateInvoice_WithProductID tests invoice creation with product ID
func TestInvoiceService_Create_WithProductID(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "customer-456"
	productID := "product-789"

	customer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Test Customer",
	}

	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(customer, nil)
	mockProduct.On("GetByID", ctx, productID, businessID).Return(&models.Product{
		ID:         productID,
		BusinessID: businessID,
		Unit:       "PCS",
	}, nil).Twice()
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Invoice")).Return(nil)

	input := services.CreateInvoiceInput{
		BusinessID:     businessID,
		CustomerID:     customerID,
		IdempotencyKey: uuid.NewString(),
		DueDate:        time.Now().Add(24 * time.Hour),
		Items: []services.CreateInvoiceItemInput{
			{
				ProductID:   productID,
				Description: "Product A",
				Quantity:    1,
				UnitPrice:   100.00,
				TaxRate:     0,
			},
		},
	}

	invoice, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	assert.Len(t, invoice.Items, 1)
	assert.NotNil(t, invoice.Items[0].ProductID)
	assert.Equal(t, productID, *invoice.Items[0].ProductID)

	mockCustomer.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestCreateInvoice_RepositoryError tests handling of repository error
func TestInvoiceService_Create_RepositoryError(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "customer-456"

	customer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Test Customer",
	}

	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(customer, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Invoice")).Return(errors.New("database error"))

	input := services.CreateInvoiceInput{
		BusinessID:     businessID,
		CustomerID:     customerID,
		IdempotencyKey: uuid.NewString(),
		DueDate:        time.Now().Add(24 * time.Hour),
		Items: []services.CreateInvoiceItemInput{
			{
				Description: "Product A",
				Quantity:    1,
				UnitPrice:   100.00,
				TaxRate:     0,
			},
		},
	}

	invoice, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, invoice)
	assert.Contains(t, err.Error(), "failed to create invoice")
	mockCustomer.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestInvoiceService_Create_SnapshotsUnitAndHSN(t *testing.T) {
	mockRepo := new(MockInvoiceRepository)
	mockCustomer := new(MockCustomerRepository)
	mockProduct := new(MockProductRepository)
	mockEmail := new(MockEmailService)
	log := logger.New()

	svc := newInvoiceService(mockRepo, mockProduct, mockCustomer, mockEmail, log)

	ctx := services.ContextWithActor(context.Background(), services.ActorContext{UserID: uuid.NewString()})
	businessID := "business-123"
	customerID := "customer-456"

	customer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Test Customer",
	}

	mockCustomer.On("GetByID", ctx, customerID, businessID).Return(customer, nil)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(invoice *models.Invoice) bool {
		return len(invoice.Items) == 1 &&
			invoice.Items[0].Unit == "CBM" &&
			invoice.Items[0].HSNSACCode == "1234"
	})).Return(nil)

	invoice, err := svc.Create(ctx, services.CreateInvoiceInput{
		BusinessID:     businessID,
		CustomerID:     customerID,
		IdempotencyKey: uuid.NewString(),
		DueDate:        time.Now().Add(24 * time.Hour),
		Items: []services.CreateInvoiceItemInput{
			{
				Description: "Concrete",
				Quantity:    2,
				UnitPrice:   100,
				TaxRate:     18,
				Unit:        "cbm",
				HSNSACCode:  "1234",
			},
		},
	})

	assert.NoError(t, err)
	assert.NotNil(t, invoice)
	assert.Len(t, invoice.Items, 1)
	assert.Equal(t, "CBM", invoice.Items[0].Unit)
	assert.Equal(t, "1234", invoice.Items[0].HSNSACCode)
	mockCustomer.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}
