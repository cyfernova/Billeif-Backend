package unit

import (
	"context"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockCustomerRepo struct {
	mock.Mock
}

func (m *MockCustomerRepo) Create(ctx context.Context, customer *models.Customer) error {
	args := m.Called(ctx, customer)
	return args.Error(0)
}

func (m *MockCustomerRepo) GetByID(ctx context.Context, id string) (*models.Customer, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Customer), args.Error(1)
}

func (m *MockCustomerRepo) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Customer), args.Get(1).(int64), args.Error(2)
}

func (m *MockCustomerRepo) Update(ctx context.Context, customer *models.Customer) error {
	args := m.Called(ctx, customer)
	return args.Error(0)
}

func (m *MockCustomerRepo) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func TestCustomerService_Create(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := services.NewCustomerService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateCustomerInput{
		BusinessID: "biz-123",
		Name:       "Test Customer",
		Email:      "customer@test.com",
		Phone:      "+1234567890",
	}

	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Customer")).Return(nil)

	customer, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, customer)
	assert.Equal(t, input.Name, customer.Name)
	assert.Equal(t, input.Email, customer.Email)
	mockRepo.AssertExpectations(t)
}

func TestCustomerService_Get(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := services.NewCustomerService(mockRepo, log)

	ctx := context.Background()
	expected := &models.Customer{
		ID:         "cust-123",
		BusinessID: "biz-123",
		Name:       "Test Customer",
		Email:      "customer@test.com",
	}

	mockRepo.On("GetByID", ctx, "cust-123").Return(expected, nil)

	customer, err := svc.Get(ctx, "cust-123")

	assert.NoError(t, err)
	assert.Equal(t, expected.ID, customer.ID)
	assert.Equal(t, expected.Name, customer.Name)
	mockRepo.AssertExpectations(t)
}

func TestCustomerService_List(t *testing.T) {
	mockRepo := new(MockCustomerRepo)
	log := logger.New()
	svc := services.NewCustomerService(mockRepo, log)

	ctx := context.Background()
	expected := []*models.Customer{
		{ID: "cust-1", Name: "Customer 1"},
		{ID: "cust-2", Name: "Customer 2"},
	}

	mockRepo.On("GetByBusinessID", ctx, "biz-123", 1, 10).Return(expected, int64(2), nil)

	customers, total, err := svc.List(ctx, "biz-123", 1, 10)

	assert.NoError(t, err)
	assert.Len(t, customers, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}
