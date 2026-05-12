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

// MockSubscriptionRepository mocks the SubscriptionRepository interface
type MockSubscriptionRepository struct {
	mock.Mock
}

func (m *MockSubscriptionRepository) Create(ctx context.Context, subscription *models.Subscription) error {
	args := m.Called(ctx, subscription)
	return args.Error(0)
}

func (m *MockSubscriptionRepository) GetByID(ctx context.Context, id, businessID string) (*models.Subscription, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Subscription), args.Error(1)
}

func (m *MockSubscriptionRepository) GetByBusinessID(ctx context.Context, businessID string) (*models.Subscription, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Subscription), args.Error(1)
}

func (m *MockSubscriptionRepository) Update(ctx context.Context, subscription *models.Subscription) error {
	args := m.Called(ctx, subscription)
	return args.Error(0)
}

// TestCreateSubscription_Success tests successful subscription creation
func TestSubscriptionService_Create_Success(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateSubscriptionInput{
		BusinessID: "business-123",
		Plan:       "free",
	}

	mockRepo.On("GetByBusinessID", ctx, "business-123").Return(nil, nil)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(s *models.Subscription) bool {
		return s.BusinessID == input.BusinessID && s.Plan == input.Plan && s.Status == "active"
	})).Return(nil)

	subscription, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, subscription)
	assert.Equal(t, input.BusinessID, subscription.BusinessID)
	assert.Equal(t, input.Plan, subscription.Plan)
	assert.Equal(t, "active", subscription.Status)
	mockRepo.AssertExpectations(t)
}

// TestCreateSubscription_AlreadyExists tests subscription creation when one already exists
func TestSubscriptionService_Create_AlreadyExists(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateSubscriptionInput{
		BusinessID: "business-123",
		Plan:       "free",
	}

	existing := &models.Subscription{
		ID:         "existing-sub-id",
		BusinessID: "business-123",
		Plan:       "starter",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, "business-123").Return(existing, nil)

	subscription, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, subscription)
	assert.Contains(t, err.Error(), "subscription already exists")
	mockRepo.AssertExpectations(t)
}

// TestCreateSubscription_RejectsPaidPlans tests that direct subscription creation cannot activate paid plans.
func TestSubscriptionService_Create_RejectsPaidPlans(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	plans := []string{"starter", "professional", "enterprise"}

	for _, plan := range plans {
		input := services.CreateSubscriptionInput{
			BusinessID: "business-123-" + plan,
			Plan:       plan,
		}

		subscription, err := svc.Create(ctx, input)

		assert.ErrorIs(t, err, services.ErrSubscriptionPlanChangeRequiresPayment)
		assert.Nil(t, subscription)
	}

	mockRepo.AssertExpectations(t)
}

// TestCreateSubscription_RepositoryError tests subscription creation with repository error
func TestSubscriptionService_Create_RepositoryError(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateSubscriptionInput{
		BusinessID: "business-123",
		Plan:       "free",
	}

	mockRepo.On("GetByBusinessID", ctx, "business-123").Return(nil, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Subscription")).Return(errors.New("database error"))

	subscription, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, subscription)
	assert.Contains(t, err.Error(), "failed to create subscription")
	mockRepo.AssertExpectations(t)
}

// TestGetByBusinessID_Success tests successful subscription retrieval
func TestSubscriptionService_GetByBusinessID_Success(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	expected := &models.Subscription{
		ID:         "sub-456",
		BusinessID: businessID,
		Plan:       "professional",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(expected, nil)

	subscription, err := svc.GetByBusinessID(ctx, businessID)

	assert.NoError(t, err)
	assert.NotNil(t, subscription)
	assert.Equal(t, expected.ID, subscription.ID)
	assert.Equal(t, businessID, subscription.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusinessID_NotFound tests subscription not found
func TestSubscriptionService_GetByBusinessID_NotFound(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(nil, errors.New("subscription not found"))

	subscription, err := svc.GetByBusinessID(ctx, businessID)

	assert.Error(t, err)
	assert.Nil(t, subscription)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusinessID_RepositoryError tests repository error during retrieval
func TestSubscriptionService_GetByBusinessID_RepositoryError(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(nil, errors.New("database error"))

	subscription, err := svc.GetByBusinessID(ctx, businessID)

	assert.Error(t, err)
	assert.Nil(t, subscription)
	mockRepo.AssertExpectations(t)
}

// TestUpdate_Success tests successful subscription status update
func TestSubscriptionService_Update_Success(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.Subscription{
		ID:         "sub-456",
		BusinessID: businessID,
		Plan:       "starter",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(s *models.Subscription) bool {
		return s.Plan == "starter" && s.Status == "expired"
	})).Return(nil)

	input := services.UpdateSubscriptionInput{
		Status: "expired",
	}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, subscription)
	assert.Equal(t, "starter", subscription.Plan)
	assert.Equal(t, "expired", subscription.Status)
	mockRepo.AssertExpectations(t)
}

// TestUpdate_StatusOnly tests updating only the status
func TestSubscriptionService_Update_StatusOnly(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.Subscription{
		ID:         "sub-456",
		BusinessID: businessID,
		Plan:       "starter",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(s *models.Subscription) bool {
		return s.Status == "canceled"
	})).Return(nil)

	input := services.UpdateSubscriptionInput{
		Status: "canceled",
	}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, subscription)
	assert.Equal(t, "canceled", subscription.Status)
	assert.Equal(t, "starter", subscription.Plan) // unchanged
	mockRepo.AssertExpectations(t)
}

// TestUpdate_BothPlanAndStatus tests updating both plan and status
func TestSubscriptionService_Update_BothPlanAndStatus(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.Subscription{
		ID:         "sub-456",
		BusinessID: businessID,
		Plan:       "free",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(existing, nil)

	input := services.UpdateSubscriptionInput{
		Plan:   "enterprise",
		Status: "active",
	}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.ErrorIs(t, err, services.ErrSubscriptionPlanChangeRequiresPayment)
	assert.Nil(t, subscription)
	mockRepo.AssertExpectations(t)
}

// TestUpdate_NotFound tests update when subscription doesn't exist
func TestSubscriptionService_Update_NotFound(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(nil, errors.New("subscription not found"))

	input := services.UpdateSubscriptionInput{
		Plan: "professional",
	}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, subscription)
	mockRepo.AssertExpectations(t)
}

// TestUpdate_GetRepositoryError tests update when GetByBusinessID fails
func TestSubscriptionService_Update_GetRepositoryError(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(nil, errors.New("database error"))

	input := services.UpdateSubscriptionInput{
		Plan: "professional",
	}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, subscription)
	mockRepo.AssertExpectations(t)
}

// TestUpdate_UpdateRepositoryError tests update when Update fails
func TestSubscriptionService_Update_UpdateRepositoryError(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.Subscription{
		ID:         "sub-456",
		BusinessID: businessID,
		Plan:       "starter",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.Subscription")).Return(errors.New("database error"))

	input := services.UpdateSubscriptionInput{
		Status: "canceled",
	}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, subscription)
	mockRepo.AssertExpectations(t)
}

// TestUpdate_EmptyInput tests update with empty input (no changes)
func TestSubscriptionService_Update_EmptyInput(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.Subscription{
		ID:         "sub-456",
		BusinessID: businessID,
		Plan:       "starter",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(s *models.Subscription) bool {
		return s.Plan == "starter" && s.Status == "active" // unchanged
	})).Return(nil)

	input := services.UpdateSubscriptionInput{}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, subscription)
	mockRepo.AssertExpectations(t)
}

// TestUpdate_PlanChange tests plan change from starter to professional
func TestSubscriptionService_Update_PlanChange(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.Subscription{
		ID:         "sub-456",
		BusinessID: businessID,
		Plan:       "starter",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(existing, nil)

	input := services.UpdateSubscriptionInput{
		Plan: "professional",
	}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.ErrorIs(t, err, services.ErrSubscriptionPlanChangeRequiresPayment)
	assert.Nil(t, subscription)
	mockRepo.AssertExpectations(t)
}

// TestUpdate_CancelSubscription tests canceling a subscription
func TestSubscriptionService_Update_CancelSubscription(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.Subscription{
		ID:         "sub-456",
		BusinessID: businessID,
		Plan:       "enterprise",
		Status:     "active",
	}

	mockRepo.On("GetByBusinessID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(s *models.Subscription) bool {
		return s.Status == "canceled"
	})).Return(nil)

	input := services.UpdateSubscriptionInput{
		Status: "canceled",
	}

	subscription, err := svc.Update(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, subscription)
	assert.Equal(t, "canceled", subscription.Status)
	mockRepo.AssertExpectations(t)
}

// TestCreateSubscription_GetByBusinessIDError tests behavior when checking existing subscription fails
// Note: The service ignores the error from GetByBusinessID and proceeds to Create
func TestSubscriptionService_Create_GetByBusinessIDError(t *testing.T) {
	mockRepo := new(MockSubscriptionRepository)
	log := logger.New()

	svc := services.NewSubscriptionService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateSubscriptionInput{
		BusinessID: "business-123",
		Plan:       "free",
	}

	// Service ignores error from GetByBusinessID, so it proceeds to Create
	mockRepo.On("GetByBusinessID", ctx, "business-123").Return(nil, errors.New("database error"))
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Subscription")).Return(nil)

	subscription, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, subscription)
	mockRepo.AssertExpectations(t)
}
