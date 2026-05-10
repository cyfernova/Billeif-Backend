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

// MockBusinessRepository mocks the BusinessRepository interface
type MockBusinessRepository struct {
	mock.Mock
}

func (m *MockBusinessRepository) Create(ctx context.Context, business *models.BusinessProfile) error {
	args := m.Called(ctx, business)
	return args.Error(0)
}

func (m *MockBusinessRepository) GetByID(ctx context.Context, id string) (*models.BusinessProfile, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.BusinessProfile), args.Error(1)
}

func (m *MockBusinessRepository) Update(ctx context.Context, business *models.BusinessProfile) error {
	args := m.Called(ctx, business)
	return args.Error(0)
}

func (m *MockBusinessRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockBusinessRepository) List(ctx context.Context, userID string, page, limit int) ([]*models.BusinessProfile, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	var businesses []*models.BusinessProfile
	if args.Get(0) != nil {
		businesses = args.Get(0).([]*models.BusinessProfile)
	}
	return businesses, args.Get(1).(int64), args.Error(2)
}

// MockBusinessS3Service mocks the S3 service for business tests
type MockBusinessS3Service struct {
	mock.Mock
}

func (m *MockBusinessS3Service) GeneratePresignedUploadURL(ctx context.Context, bucket, key, contentType string, expiresIn int64) (string, error) {
	args := m.Called(ctx, bucket, key, contentType, expiresIn)
	return args.String(0), args.Error(1)
}

// TestBusinessService_Create_Success tests successful business creation
func TestBusinessService_Create_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	input := services.CreateBusinessInput{
		Name:    "Acme Corp",
		Email:   "acme@example.com",
		Phone:   "+1234567890",
		Address: "123 Main St",
		City:    "New York",
		State:   "NY",
		Country: "USA",
		ZipCode: "10001",
		TaxID:   "12-3456789",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(b *models.BusinessProfile) bool {
		return b.Name == input.Name &&
			b.Email == input.Email &&
			b.OwnerID == userID
	})).Return(nil)

	business, err := svc.Create(ctx, userID, input)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, input.Name, business.Name)
	assert.Equal(t, input.Email, business.Email)
	assert.Equal(t, userID, business.OwnerID)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Create_AcceptsPostalCodeAlias verifies API clients can send postal_code.
func TestBusinessService_Create_AcceptsPostalCodeAlias(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	input := services.CreateBusinessInput{
		Name:       "Cyfernova",
		Email:      "accounts@cyfernova.com",
		PostalCode: "743127",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(b *models.BusinessProfile) bool {
		return b.OwnerID == userID && b.PostalCode == input.PostalCode
	})).Return(nil)

	business, err := svc.Create(ctx, userID, input)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, input.PostalCode, business.PostalCode)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Create_DefaultCurrency tests business creation with default currency
func TestBusinessService_Create_DefaultCurrency(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	input := services.CreateBusinessInput{
		Name:     "Test Business",
		Email:    "test@example.com",
		Currency: "", // Empty currency should default to USD
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(b *models.BusinessProfile) bool {
		return b.Currency == "USD"
	})).Return(nil)

	business, err := svc.Create(ctx, userID, input)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, "USD", business.Currency)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Create_WithCurrency tests business creation with specified currency
func TestBusinessService_Create_WithCurrency(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	input := services.CreateBusinessInput{
		Name:     "Euro Business",
		Email:    "euro@example.com",
		Currency: "EUR",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(b *models.BusinessProfile) bool {
		return b.Currency == "EUR"
	})).Return(nil)

	business, err := svc.Create(ctx, userID, input)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, "EUR", business.Currency)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Create_RepositoryError tests business creation with repository error
func TestBusinessService_Create_RepositoryError(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	input := services.CreateBusinessInput{
		Name:  "Test Business",
		Email: "test@example.com",
	}

	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.BusinessProfile")).Return(errors.New("database error"))

	business, err := svc.Create(ctx, userID, input)

	assert.Error(t, err)
	assert.Nil(t, business)
	assert.Contains(t, err.Error(), "failed to create business")
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Get_Success tests successful business retrieval
func TestBusinessService_Get_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"

	expected := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: "user-123",
		Name:    "Acme Corp",
		Email:   "acme@example.com",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(expected, nil)

	business, err := svc.Get(ctx, businessID)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, businessID, business.ID)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Get_NotFound tests business not found
func TestBusinessService_Get_NotFound(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "nonexistent"

	mockRepo.On("GetByID", ctx, businessID).Return(nil, errors.New("business not found"))

	business, err := svc.Get(ctx, businessID)

	assert.Error(t, err)
	assert.Nil(t, business)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_GetByOwner_Success tests successful retrieval by owner
func TestBusinessService_GetByOwner_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	businessID := "business-456"

	expected := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: userID,
		Name:    "My Business",
		Email:   "my@example.com",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(expected, nil)

	business, err := svc.GetByOwner(ctx, userID, businessID)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, businessID, business.ID)
	assert.Equal(t, userID, business.OwnerID)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_GetByOwner_WrongOwner tests business belongs to different owner
func TestBusinessService_GetByOwner_WrongOwner(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	businessID := "business-456"

	expected := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: "different-user", // Business belongs to different user
		Name:    "My Business",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(expected, nil)

	business, err := svc.GetByOwner(ctx, userID, businessID)

	assert.Error(t, err)
	assert.Nil(t, business)
	assert.Contains(t, err.Error(), "business not found")
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_List_Success tests successful business listing
func TestBusinessService_List_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"

	expected := []*models.BusinessProfile{
		{ID: "b1", OwnerID: userID, Name: "Business 1"},
		{ID: "b2", OwnerID: userID, Name: "Business 2"},
	}

	mockRepo.On("List", ctx, userID, 1, 10).Return(expected, int64(2), nil)

	businesses, total, err := svc.List(ctx, userID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, businesses, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_List_EmptyResult tests listing with no businesses
func TestBusinessService_List_EmptyResult(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"

	mockRepo.On("List", ctx, userID, 1, 10).Return([]*models.BusinessProfile{}, int64(0), nil)

	businesses, total, err := svc.List(ctx, userID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, businesses, 0)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_List_RepositoryError tests listing with repository error
func TestBusinessService_List_RepositoryError(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"

	mockRepo.On("List", ctx, userID, 1, 10).Return(nil, int64(0), errors.New("database error"))

	businesses, total, err := svc.List(ctx, userID, 1, 10)

	assert.Error(t, err)
	assert.Nil(t, businesses)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_List_Pagination tests listing with different pagination values
func TestBusinessService_List_Pagination(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"

	testCases := []struct {
		page       int
		limit      int
		businesses []*models.BusinessProfile
		total      int64
	}{
		{1, 10, []*models.BusinessProfile{{ID: "b1"}}, 1},
		{2, 10, []*models.BusinessProfile{}, 15},
		{1, 5, []*models.BusinessProfile{{ID: "b1"}, {ID: "b2"}}, 2},
	}

	for _, tc := range testCases {
		mockRepo.On("List", ctx, userID, tc.page, tc.limit).Return(tc.businesses, tc.total, nil).Once()

		businesses, total, err := svc.List(ctx, userID, tc.page, tc.limit)

		assert.NoError(t, err)
		assert.Equal(t, tc.businesses, businesses)
		assert.Equal(t, tc.total, total)
	}

	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Update_Success tests successful business update
func TestBusinessService_Update_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.BusinessProfile{
		ID:    businessID,
		Name:  "Old Name",
		Email: "old@example.com",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(b *models.BusinessProfile) bool {
		return b.Name == "New Name" && b.Email == "new@example.com"
	})).Return(nil)

	input := services.UpdateBusinessInput{
		Name:  "New Name",
		Email: "new@example.com",
	}

	business, err := svc.Update(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, "New Name", business.Name)
	assert.Equal(t, "new@example.com", business.Email)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Update_PartialUpdate tests partial update (only name)
func TestBusinessService_Update_PartialUpdate(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.BusinessProfile{
		ID:    businessID,
		Name:  "Old Name",
		Email: "old@example.com",
		Phone: "+1234567890",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(b *models.BusinessProfile) bool {
		return b.Name == "New Name" && b.Email == "old@example.com"
	})).Return(nil)

	input := services.UpdateBusinessInput{
		Name: "New Name",
	}

	business, err := svc.Update(ctx, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, "New Name", business.Name)
	assert.Equal(t, "old@example.com", business.Email)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Update_NotFound tests update on non-existent business
func TestBusinessService_Update_NotFound(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "nonexistent"

	mockRepo.On("GetByID", ctx, businessID).Return(nil, errors.New("business not found"))

	input := services.UpdateBusinessInput{
		Name: "New Name",
	}

	business, err := svc.Update(ctx, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, business)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Update_RepositoryError tests update with repository error
func TestBusinessService_Update_RepositoryError(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"

	existing := &models.BusinessProfile{
		ID:   businessID,
		Name: "Old Name",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.BusinessProfile")).Return(errors.New("database error"))

	input := services.UpdateBusinessInput{
		Name: "New Name",
	}

	business, err := svc.Update(ctx, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, business)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_UpdateByOwner_Success tests successful scoped business update
func TestBusinessService_UpdateByOwner_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	businessID := "business-456"

	existing := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: userID,
		Name:    "Old Name",
		Email:   "old@example.com",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(b *models.BusinessProfile) bool {
		return b.Name == "New Name"
	})).Return(nil)

	input := services.UpdateBusinessInput{
		Name: "New Name",
	}

	business, err := svc.UpdateByOwner(ctx, userID, businessID, input)

	assert.NoError(t, err)
	assert.NotNil(t, business)
	assert.Equal(t, "New Name", business.Name)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_UpdateByOwner_WrongOwner tests update when business belongs to different owner
func TestBusinessService_UpdateByOwner_WrongOwner(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	businessID := "business-456"

	existing := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: "different-user",
		Name:    "Old Name",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(existing, nil)

	input := services.UpdateBusinessInput{
		Name: "New Name",
	}

	business, err := svc.UpdateByOwner(ctx, userID, businessID, input)

	assert.Error(t, err)
	assert.Nil(t, business)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Delete_Success tests successful business deletion
func TestBusinessService_Delete_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("Delete", ctx, businessID).Return(nil)

	err := svc.Delete(ctx, businessID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_Delete_RepositoryError tests deletion with repository error
func TestBusinessService_Delete_RepositoryError(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("Delete", ctx, businessID).Return(errors.New("database error"))

	err := svc.Delete(ctx, businessID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_DeleteByOwner_Success tests successful scoped business deletion
func TestBusinessService_DeleteByOwner_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	businessID := "business-456"

	existing := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: userID,
		Name:    "My Business",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(existing, nil)
	mockRepo.On("Delete", ctx, businessID).Return(nil)

	err := svc.DeleteByOwner(ctx, userID, businessID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_DeleteByOwner_WrongOwner tests deletion when business belongs to different owner
func TestBusinessService_DeleteByOwner_WrongOwner(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	businessID := "business-456"

	existing := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: "different-user",
		Name:    "My Business",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(existing, nil)

	err := svc.DeleteByOwner(ctx, userID, businessID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_GetLogoUploadURL_Success tests successful logo URL generation
func TestBusinessService_GetLogoUploadURL_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	contentType := "image/png"
	expectedURL := "https://s3.example.com/presigned-url"

	mockS3.On("GeneratePresignedUploadURL", ctx, "business-logos", "logos/business-123/logo", contentType, int64(3600)).Return(expectedURL, nil)

	url, err := svc.GetLogoUploadURL(ctx, businessID, contentType)

	assert.NoError(t, err)
	assert.Equal(t, expectedURL, url)
	mockS3.AssertExpectations(t)
}

// TestBusinessService_GetLogoUploadURL_S3Error tests logo URL generation with S3 error
func TestBusinessService_GetLogoUploadURL_S3Error(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	contentType := "image/png"

	mockS3.On("GeneratePresignedUploadURL", ctx, "business-logos", "logos/business-123/logo", contentType, int64(3600)).Return("", errors.New("S3 error"))

	url, err := svc.GetLogoUploadURL(ctx, businessID, contentType)

	assert.Error(t, err)
	assert.Empty(t, url)
	mockS3.AssertExpectations(t)
}

// TestBusinessService_GetLogoUploadURLByOwner_Success tests successful scoped logo URL generation
func TestBusinessService_GetLogoUploadURLByOwner_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	businessID := "business-456"
	contentType := "image/png"
	expectedURL := "https://s3.example.com/presigned-url"

	business := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: userID,
		Name:    "My Business",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(business, nil)
	mockS3.On("GeneratePresignedUploadURL", ctx, "business-logos", "logos/business-456/logo", contentType, int64(3600)).Return(expectedURL, nil)

	url, err := svc.GetLogoUploadURLByOwner(ctx, userID, businessID, contentType)

	assert.NoError(t, err)
	assert.Equal(t, expectedURL, url)
	mockRepo.AssertExpectations(t)
	mockS3.AssertExpectations(t)
}

// TestBusinessService_GetLogoUploadURLByOwner_NotFound tests scoped logo URL when business not found
func TestBusinessService_GetLogoUploadURLByOwner_NotFound(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	userID := "user-123"
	businessID := "nonexistent"
	contentType := "image/png"

	mockRepo.On("GetByID", ctx, businessID).Return(nil, errors.New("business not found"))

	url, err := svc.GetLogoUploadURLByOwner(ctx, userID, businessID, contentType)

	assert.Error(t, err)
	assert.Empty(t, url)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_UpdateLogoURL_Success tests successful logo URL update
func TestBusinessService_UpdateLogoURL_Success(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	logoURL := "https://s3.example.com/logos/business-123/logo.png"

	business := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: "user-123",
		Name:    "My Business",
		LogoURL: "",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(business, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(b *models.BusinessProfile) bool {
		return b.LogoURL == logoURL
	})).Return(nil)

	err := svc.UpdateLogoURL(ctx, businessID, logoURL)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_UpdateLogoURL_NotFound tests logo URL update when business not found
func TestBusinessService_UpdateLogoURL_NotFound(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "nonexistent"
	logoURL := "https://s3.example.com/logo.png"

	mockRepo.On("GetByID", ctx, businessID).Return(nil, errors.New("business not found"))

	err := svc.UpdateLogoURL(ctx, businessID, logoURL)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestBusinessService_UpdateLogoURL_RepositoryError tests logo URL update with repository error
func TestBusinessService_UpdateLogoURL_RepositoryError(t *testing.T) {
	mockRepo := new(MockBusinessRepository)
	mockS3 := new(MockBusinessS3Service)
	log := logger.New()

	svc := services.NewBusinessServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	logoURL := "https://s3.example.com/logo.png"

	business := &models.BusinessProfile{
		ID:      businessID,
		OwnerID: "user-123",
		Name:    "My Business",
	}

	mockRepo.On("GetByID", ctx, businessID).Return(business, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.BusinessProfile")).Return(errors.New("database error"))

	err := svc.UpdateLogoURL(ctx, businessID, logoURL)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}
