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

// MockCustomerRepo mocks the CustomerRepository interface
type MockCustomerRepo struct {
	mock.Mock
}

type allowCustomerPermissionChecker struct{}

func (allowCustomerPermissionChecker) UserHasPermission(context.Context, string, string, string) bool {
	return true
}

func newCustomerService(repo *MockCustomerRepo, log *logger.Logger) *services.CustomerService {
	return services.NewCustomerService(repo, allowCustomerPermissionChecker{}, log)
}

func authorizedCustomerContext() context.Context {
	return services.ContextWithActor(context.Background(), services.ActorContext{UserID: "accountant-1", Role: "accountant"})
}

func (m *MockCustomerRepo) Create(ctx context.Context, customer *models.Customer) error {
	args := m.Called(ctx, customer)
	return args.Error(0)
}

func (m *MockCustomerRepo) GetByID(ctx context.Context, id, businessID string) (*models.Customer, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Customer), args.Error(1)
}

func (m *MockCustomerRepo) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	var customers []*models.Customer
	if args.Get(0) != nil {
		customers = args.Get(0).([]*models.Customer)
	}
	return customers, args.Get(1).(int64), args.Error(2)
}

func (m *MockCustomerRepo) Update(ctx context.Context, customer *models.Customer) error {
	args := m.Called(ctx, customer)
	return args.Error(0)
}

func (m *MockCustomerRepo) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// TestCreateCustomer_Success tests successful customer creation
func TestCustomerService_Create_Success(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	input := services.CreateCustomerInput{
		BusinessID:  "business-123",
		Name:        "John Doe",
		Email:       "john@example.com",
		Phone:       "+1234567890",
		Address:     "123 Main St",
		City:        "New York",
		State:       "NY",
		Country:     "USA",
		ZipCode:     "10001",
		TaxID:       "12-3456789",
		CreditLimit: 5000.00,
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == input.Name &&
			c.Email == input.Email &&
			c.Phone == input.Phone &&
			c.Address == input.Address &&
			c.City == input.City &&
			c.BusinessID == input.BusinessID
	})).Return(nil)

	customer, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	assert.Equal(t, input.Name, customer.Name)
	assert.Equal(t, input.Email, customer.Email)
	assert.Equal(t, input.BusinessID, customer.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestCreateCustomer_RepositoryError tests customer creation with repository error
func TestCustomerService_Create_RepositoryError(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	input := services.CreateCustomerInput{
		BusinessID: "business-123",
		Name:       "John Doe",
		Email:      "john@example.com",
	}

	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Customer")).Return(errors.New("database error"))

	customer, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, customer)
	assert.Contains(t, err.Error(), "failed to create customer")
	mockRepo.AssertExpectations(t)
}

// TestCreateCustomer_AllFields tests customer creation with all fields
func TestCustomerService_Create_AllFields(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	input := services.CreateCustomerInput{
		BusinessID:   "business-123",
		Name:         "Acme Corp",
		Email:        "acme@example.com",
		Phone:        "+1234567890",
		Address:      "123 Main St",
		City:         "New York",
		State:        "NY",
		Country:      "USA",
		ZipCode:      "10001",
		TaxID:        "12-3456789",
		CreditLimit:  10000.00,
		PaymentTerms: 30,
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == input.Name &&
			c.Email == input.Email &&
			c.Phone == input.Phone &&
			c.Address == input.Address &&
			c.City == input.City &&
			c.State == input.State &&
			c.Country == input.Country &&
			c.PostalCode == input.ZipCode &&
			c.TaxID == input.TaxID &&
			c.CreditLimit == input.CreditLimit
	})).Return(nil)

	customer, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	assert.Equal(t, input.Name, customer.Name)
	assert.Equal(t, "10001", customer.PostalCode)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_GetByBusiness_Success tests successful customer retrieval
func TestCustomerService_GetByBusiness_Success(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	expected := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "John Doe",
		Email:      "john@example.com",
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(expected, nil)

	customer, err := svc.GetByBusiness(ctx, businessID, customerID)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	assert.Equal(t, customerID, customer.ID)
	assert.Equal(t, businessID, customer.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_GetByBusiness_NotFound tests customer not found
func TestCustomerService_GetByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "nonexistent"

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(nil, errors.New("customer not found"))

	customer, err := svc.GetByBusiness(ctx, businessID, customerID)

	assert.Error(t, err)
	assert.Nil(t, customer)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_List_Success tests listing customers with pagination
func TestCustomerService_List_Success(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"

	expected := []*models.Customer{
		{ID: "customer-1", BusinessID: businessID, Name: "Customer 1", Email: "c1@example.com"},
		{ID: "customer-2", BusinessID: businessID, Name: "Customer 2", Email: "c2@example.com"},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return(expected, int64(2), nil)

	customers, total, err := svc.List(ctx, businessID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, customers, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_List_EmptyResult tests listing with no customers
func TestCustomerService_List_EmptyResult(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return([]*models.Customer{}, int64(0), nil)

	customers, total, err := svc.List(ctx, businessID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, customers, 0)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_List_RepositoryError tests listing with repository error
func TestCustomerService_List_RepositoryError(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return(nil, int64(0), errors.New("database error"))

	customers, total, err := svc.List(ctx, businessID, 1, 10)

	assert.Error(t, err)
	assert.Nil(t, customers)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_List_Pagination tests listing with different pagination values
func TestCustomerService_List_Pagination(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"

	testCases := []struct {
		page      int
		limit     int
		customers []*models.Customer
		total     int64
	}{
		{1, 10, []*models.Customer{{ID: "c1"}}, 1},
		{2, 10, []*models.Customer{}, 15},
		{1, 5, []*models.Customer{{ID: "c1"}, {ID: "c2"}}, 2},
	}

	for _, tc := range testCases {
		mockRepo.On("GetByBusinessID", ctx, businessID, tc.page, tc.limit).Return(tc.customers, tc.total, nil).Once()

		customers, total, err := svc.List(ctx, businessID, tc.page, tc.limit)

		assert.NoError(t, err)
		assert.Equal(t, tc.customers, customers)
		assert.Equal(t, tc.total, total)
	}

	mockRepo.AssertExpectations(t)
}

// TestCustomerService_UpdateByBusiness_Success tests successful customer update
func TestCustomerService_UpdateByBusiness_Success(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	existingCustomer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Old Name",
		Email:      "old@example.com",
		Phone:      "+1234567890",
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(existingCustomer, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == "New Name" && c.Email == "new@example.com"
	})).Return(nil)

	input := services.UpdateCustomerInput{
		Name:  "New Name",
		Email: "new@example.com",
	}

	customer, err := svc.UpdateByBusiness(ctx, businessID, customerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	assert.Equal(t, "New Name", customer.Name)
	assert.Equal(t, "new@example.com", customer.Email)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_UpdateByBusiness_PartialUpdate tests partial update (only name)
func TestCustomerService_UpdateByBusiness_PartialUpdate(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	existingCustomer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Old Name",
		Email:      "old@example.com",
		Phone:      "+1234567890",
		Address:    "123 Main St",
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(existingCustomer, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == "New Name" && c.Email == "old@example.com"
	})).Return(nil)

	input := services.UpdateCustomerInput{
		Name: "New Name",
	}

	customer, err := svc.UpdateByBusiness(ctx, businessID, customerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	assert.Equal(t, "New Name", customer.Name)
	assert.Equal(t, "old@example.com", customer.Email)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_UpdateByBusiness_NotFound tests update on non-existent customer
func TestCustomerService_UpdateByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "nonexistent"

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(nil, errors.New("customer not found"))

	input := services.UpdateCustomerInput{
		Name: "New Name",
	}

	customer, err := svc.UpdateByBusiness(ctx, businessID, customerID, input)

	assert.Error(t, err)
	assert.Nil(t, customer)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_UpdateByBusiness_RepositoryError tests update with repository error
func TestCustomerService_UpdateByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	existingCustomer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Old Name",
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(existingCustomer, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.Customer")).Return(errors.New("database error"))

	input := services.UpdateCustomerInput{
		Name: "New Name",
	}

	customer, err := svc.UpdateByBusiness(ctx, businessID, customerID, input)

	assert.Error(t, err)
	assert.Nil(t, customer)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_UpdateByBusiness_AllFields tests updating all fields at once
func TestCustomerService_UpdateByBusiness_AllFields(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	existingCustomer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Old Name",
		Email:      "old@example.com",
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(existingCustomer, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == "New Name" &&
			c.Email == "new@example.com" &&
			c.Phone == "+9876543210" &&
			c.Address == "456 New St" &&
			c.City == "Los Angeles" &&
			c.State == "CA" &&
			c.Country == "USA" &&
			c.PostalCode == "90001" &&
			c.TaxID == "98-7654321" &&
			c.CreditLimit == 15000.00
	})).Return(nil)

	input := services.UpdateCustomerInput{
		Name:        "New Name",
		Email:       "new@example.com",
		Phone:       "+9876543210",
		Address:     "456 New St",
		City:        "Los Angeles",
		State:       "CA",
		Country:     "USA",
		ZipCode:     "90001",
		TaxID:       "98-7654321",
		CreditLimit: 15000.00,
	}

	customer, err := svc.UpdateByBusiness(ctx, businessID, customerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_DeleteByBusiness_Success tests successful scoped customer deletion
func TestCustomerService_DeleteByBusiness_Success(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	existingCustomer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "John Doe",
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(existingCustomer, nil)
	mockRepo.On("Delete", ctx, customerID).Return(nil)

	err := svc.DeleteByBusiness(ctx, businessID, customerID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_DeleteByBusiness_NotFound tests scoped deletion when customer not found
func TestCustomerService_DeleteByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "nonexistent"

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(nil, errors.New("customer not found"))

	err := svc.DeleteByBusiness(ctx, businessID, customerID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_DeleteByBusiness_WrongBusiness tests deletion when customer belongs to different business
func TestCustomerService_DeleteByBusiness_WrongBusiness(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	// Customer not found for this business (belongs to different business)
	mockRepo.On("GetByID", ctx, customerID, businessID).Return(nil, errors.New("customer not found"))

	err := svc.DeleteByBusiness(ctx, businessID, customerID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_Import_Success tests successful customer import
func TestCustomerService_Import_Success(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customers := []services.CreateCustomerInput{
		{Name: "Customer 1", Email: "c1@example.com"},
		{Name: "Customer 2", Email: "c2@example.com"},
		{Name: "Customer 3", Email: "c3@example.com"},
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.BusinessID == businessID
	})).Return(nil).Times(3)

	count, err := svc.Import(ctx, businessID, customers)

	assert.NoError(t, err)
	assert.Equal(t, 3, count)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_Import_PartialFailure tests import with some failures
func TestCustomerService_Import_PartialFailure(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customers := []services.CreateCustomerInput{
		{Name: "Customer 1", Email: "c1@example.com"},
		{Name: "Customer 2", Email: "c2@example.com"},
		{Name: "Customer 3", Email: "c3@example.com"},
	}

	// First and third succeed, second fails
	mockRepo.On("Create", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == "Customer 1"
	})).Return(nil).Once()

	mockRepo.On("Create", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == "Customer 2"
	})).Return(errors.New("duplicate email")).Once()

	mockRepo.On("Create", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == "Customer 3"
	})).Return(nil).Once()

	count, err := svc.Import(ctx, businessID, customers)

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_Import_AllFailures tests import where all customers fail
func TestCustomerService_Import_AllFailures(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customers := []services.CreateCustomerInput{
		{Name: "Customer 1", Email: "c1@example.com"},
		{Name: "Customer 2", Email: "c2@example.com"},
	}

	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Customer")).Return(errors.New("database error")).Times(2)

	count, err := svc.Import(ctx, businessID, customers)

	assert.NoError(t, err) // Import doesn't error, just returns count
	assert.Equal(t, 0, count)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_Export_Success tests successful customer export
func TestCustomerService_Export_Success(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"

	expected := []*models.Customer{
		{ID: "c1", BusinessID: businessID, Name: "Customer 1"},
		{ID: "c2", BusinessID: businessID, Name: "Customer 2"},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 5000).Return(expected, int64(2), nil)

	customers, err := svc.Export(ctx, businessID)

	assert.NoError(t, err)
	assert.Len(t, customers, 2)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_Export_EmptyResult tests export with no customers
func TestCustomerService_Export_EmptyResult(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 5000).Return([]*models.Customer{}, int64(0), nil)

	customers, err := svc.Export(ctx, businessID)

	assert.NoError(t, err)
	assert.Len(t, customers, 0)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_Export_RepositoryError tests export with repository error
func TestCustomerService_Export_RepositoryError(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 5000).Return(nil, int64(0), errors.New("database error"))

	customers, err := svc.Export(ctx, businessID)

	assert.Error(t, err)
	assert.Nil(t, customers)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_UpdateByBusiness_CreditLimitOnly tests updating only credit limit
func TestCustomerService_UpdateByBusiness_CreditLimitOnly(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	existingCustomer := &models.Customer{
		ID:          customerID,
		BusinessID:  businessID,
		Name:        "Old Name",
		CreditLimit: 1000.00,
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(existingCustomer, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.CreditLimit == 5000.00
	})).Return(nil)

	input := services.UpdateCustomerInput{
		CreditLimit: 5000.00,
	}

	customer, err := svc.UpdateByBusiness(ctx, businessID, customerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	assert.Equal(t, 5000.00, customer.CreditLimit)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_UpdateByBusiness_EmptyInput tests update with empty input (no changes)
func TestCustomerService_UpdateByBusiness_EmptyInput(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	existingCustomer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Existing Name",
		Email:      "existing@example.com",
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(existingCustomer, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(c *models.Customer) bool {
		return c.Name == "Existing Name" && c.Email == "existing@example.com"
	})).Return(nil)

	input := services.UpdateCustomerInput{}

	customer, err := svc.UpdateByBusiness(ctx, businessID, customerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	mockRepo.AssertExpectations(t)
}

// TestCustomerService_DeleteByBusiness_RepositoryError tests scoped deletion with repository error
func TestCustomerService_DeleteByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := newCustomerService(mockRepo, log)

	ctx := authorizedCustomerContext()
	businessID := "business-123"
	customerID := "customer-456"

	existingCustomer := &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "John Doe",
	}

	mockRepo.On("GetByID", ctx, customerID, businessID).Return(existingCustomer, nil)
	mockRepo.On("Delete", ctx, customerID).Return(errors.New("database error"))

	err := svc.DeleteByBusiness(ctx, businessID, customerID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}
