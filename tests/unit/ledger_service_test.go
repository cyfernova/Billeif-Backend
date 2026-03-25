package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockLedgerRepository mocks the LedgerRepository interface
type MockLedgerRepository struct {
	mock.Mock
}

func (m *MockLedgerRepository) Create(ctx context.Context, entry *models.LedgerEntry) error {
	args := m.Called(ctx, entry)
	return args.Error(0)
}

func (m *MockLedgerRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.LedgerEntry, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	var entries []*models.LedgerEntry
	if args.Get(0) != nil {
		entries = args.Get(0).([]*models.LedgerEntry)
	}
	return entries, args.Get(1).(int64), args.Error(2)
}

func (m *MockLedgerRepository) GetBalance(ctx context.Context, businessID string) (float64, error) {
	args := m.Called(ctx, businessID)
	return args.Get(0).(float64), args.Error(1)
}

// TestLedgerService_List_Success tests successful ledger entries listing
func TestLedgerService_List_Success(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	page := 1
	limit := 10

	entryDate := time.Now()
	expectedEntries := []*models.LedgerEntry{
		{ID: "entry-1", BusinessID: businessID, TransactionID: "txn-1", EntryType: "credit", Amount: 100.00, Description: "Payment received", EntryDate: entryDate},
		{ID: "entry-2", BusinessID: businessID, TransactionID: "txn-2", EntryType: "debit", Amount: 50.00, Description: "Invoice sent", EntryDate: entryDate},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, page, limit).Return(expectedEntries, int64(2), nil)

	entries, total, err := svc.List(ctx, businessID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, entries, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_List_EmptyResult tests listing with no ledger entries
func TestLedgerService_List_EmptyResult(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return([]*models.LedgerEntry{}, int64(0), nil)

	entries, total, err := svc.List(ctx, businessID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, entries, 0)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_List_RepositoryError tests listing with repository error
func TestLedgerService_List_RepositoryError(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return(nil, int64(0), errors.New("database error"))

	entries, total, err := svc.List(ctx, businessID, 1, 10)

	assert.Error(t, err)
	assert.Nil(t, entries)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_List_Pagination tests listing with different pagination values
func TestLedgerService_List_Pagination(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	testCases := []struct {
		page    int
		limit   int
		entries []*models.LedgerEntry
		total   int64
	}{
		{1, 10, []*models.LedgerEntry{{ID: "e1"}}, 1},
		{2, 10, []*models.LedgerEntry{}, 15},
		{1, 5, []*models.LedgerEntry{{ID: "e1"}, {ID: "e2"}}, 2},
	}

	for _, tc := range testCases {
		mockRepo.On("GetByBusinessID", ctx, businessID, tc.page, tc.limit).Return(tc.entries, tc.total, nil).Once()

		entries, total, err := svc.List(ctx, businessID, tc.page, tc.limit)

		assert.NoError(t, err)
		assert.Equal(t, tc.entries, entries)
		assert.Equal(t, tc.total, total)
	}

	mockRepo.AssertExpectations(t)
}

// TestLedgerService_GetBalance_Success tests successful balance retrieval
func TestLedgerService_GetBalance_Success(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	expectedBalance := 1500.50

	mockRepo.On("GetBalance", ctx, businessID).Return(expectedBalance, nil)

	balance, err := svc.GetBalance(ctx, businessID)

	assert.NoError(t, err)
	assert.Equal(t, expectedBalance, balance)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_GetBalance_ZeroBalance tests balance of zero
func TestLedgerService_GetBalance_ZeroBalance(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetBalance", ctx, businessID).Return(float64(0), nil)

	balance, err := svc.GetBalance(ctx, businessID)

	assert.NoError(t, err)
	assert.Equal(t, float64(0), balance)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_GetBalance_NegativeBalance tests negative balance (debit > credit)
func TestLedgerService_GetBalance_NegativeBalance(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	expectedBalance := -500.25

	mockRepo.On("GetBalance", ctx, businessID).Return(expectedBalance, nil)

	balance, err := svc.GetBalance(ctx, businessID)

	assert.NoError(t, err)
	assert.Equal(t, expectedBalance, balance)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_GetBalance_RepositoryError tests balance retrieval with repository error
func TestLedgerService_GetBalance_RepositoryError(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetBalance", ctx, businessID).Return(float64(0), errors.New("database error"))

	balance, err := svc.GetBalance(ctx, businessID)

	assert.Error(t, err)
	assert.Equal(t, float64(0), balance)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_CreateEntry_Success tests successful ledger entry creation
func TestLedgerService_CreateEntry_Success(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	entry := &models.LedgerEntry{
		BusinessID:    "business-123",
		TransactionID: "txn-456",
		EntryType:     "credit",
		Description:   "Payment received for invoice #123",
		Amount:        250.00,
		Currency:      "USD",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(e *models.LedgerEntry) bool {
		return e.BusinessID == entry.BusinessID &&
			e.TransactionID == entry.TransactionID &&
			e.EntryType == entry.EntryType &&
			e.Description == entry.Description &&
			e.Amount == entry.Amount
	})).Return(nil)

	err := svc.CreateEntry(ctx, entry)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_CreateEntry_CreditEntry tests creating a credit entry
func TestLedgerService_CreateEntry_CreditEntry(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	entry := &models.LedgerEntry{
		BusinessID:    "business-123",
		TransactionID: "txn-credit",
		EntryType:     "credit",
		Description:   "Invoice payment received",
		Amount:        1000.00,
		Currency:      "USD",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(e *models.LedgerEntry) bool {
		return e.EntryType == "credit" && e.Amount > 0
	})).Return(nil)

	err := svc.CreateEntry(ctx, entry)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_CreateEntry_DebitEntry tests creating a debit entry
func TestLedgerService_CreateEntry_DebitEntry(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	entry := &models.LedgerEntry{
		BusinessID:    "business-123",
		TransactionID: "txn-debit",
		EntryType:     "debit",
		Description:   "Invoice generated",
		Amount:        500.00,
		Currency:      "USD",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(e *models.LedgerEntry) bool {
		return e.EntryType == "debit" && e.Amount > 0
	})).Return(nil)

	err := svc.CreateEntry(ctx, entry)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_CreateEntry_RepositoryError tests entry creation with repository error
func TestLedgerService_CreateEntry_RepositoryError(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	entry := &models.LedgerEntry{
		BusinessID:    "business-123",
		TransactionID: "txn-789",
		EntryType:     "credit",
		Description:   "Test entry",
		Amount:        100.00,
		Currency:      "USD",
	}

	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.LedgerEntry")).Return(errors.New("database error"))

	err := svc.CreateEntry(ctx, entry)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_List_FiltersCorrectEntries tests that correct business ID is passed
func TestLedgerService_List_FiltersCorrectBusiness(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-specific-123"
	page := 1
	limit := 10

	expectedEntries := []*models.LedgerEntry{
		{ID: "entry-1", BusinessID: businessID, TransactionID: "txn-1"},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, page, limit).Return(expectedEntries, int64(1), nil)

	entries, total, err := svc.List(ctx, businessID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, businessID, entries[0].BusinessID)
	assert.Equal(t, int64(1), total)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_GetBalance_MultipleCalls tests multiple balance calls
func TestLedgerService_GetBalance_MultipleCalls(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()

	mockRepo.On("GetBalance", ctx, "business-1").Return(1000.00, nil).Once()
	mockRepo.On("GetBalance", ctx, "business-2").Return(2500.50, nil).Once()
	mockRepo.On("GetBalance", ctx, "business-3").Return(0.00, nil).Once()

	balance1, err1 := svc.GetBalance(ctx, "business-1")
	assert.NoError(t, err1)
	assert.Equal(t, 1000.00, balance1)

	balance2, err2 := svc.GetBalance(ctx, "business-2")
	assert.NoError(t, err2)
	assert.Equal(t, 2500.50, balance2)

	balance3, err3 := svc.GetBalance(ctx, "business-3")
	assert.NoError(t, err3)
	assert.Equal(t, 0.00, balance3)

	mockRepo.AssertExpectations(t)
}

// TestLedgerService_CreateEntry_WithInvoiceID tests creating entry linked to an invoice
func TestLedgerService_CreateEntry_WithInvoiceID(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	invoiceID := "invoice-123"
	entry := &models.LedgerEntry{
		BusinessID:    "business-123",
		InvoiceID:     &invoiceID,
		TransactionID: "txn-invoice",
		EntryType:     "credit",
		Description:   "Payment for invoice",
		Amount:        500.00,
		Currency:      "USD",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(e *models.LedgerEntry) bool {
		return e.InvoiceID != nil && *e.InvoiceID == invoiceID
	})).Return(nil)

	err := svc.CreateEntry(ctx, entry)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestLedgerService_CreateEntry_WithPaymentID tests creating entry linked to a payment
func TestLedgerService_CreateEntry_WithPaymentID(t *testing.T) {
	mockRepo := new(MockLedgerRepository)
	log := logger.New()

	svc := services.NewLedgerService(mockRepo, log)

	ctx := context.Background()
	paymentID := "payment-456"
	entry := &models.LedgerEntry{
		BusinessID:    "business-123",
		PaymentID:     &paymentID,
		TransactionID: "txn-payment",
		EntryType:     "credit",
		Description:   "Payment processed",
		Amount:        250.00,
		Currency:      "USD",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(e *models.LedgerEntry) bool {
		return e.PaymentID != nil && *e.PaymentID == paymentID
	})).Return(nil)

	err := svc.CreateEntry(ctx, entry)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}
