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

// MockVendorRepository mocks the VendorRepository interface
type MockVendorRepository struct {
	mock.Mock
}

func (m *MockVendorRepository) Create(ctx context.Context, vendor *models.Vendor) error {
	args := m.Called(ctx, vendor)
	return args.Error(0)
}

func (m *MockVendorRepository) GetByID(ctx context.Context, id, businessID string) (*models.Vendor, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Vendor), args.Error(1)
}

func (m *MockVendorRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Vendor, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Vendor), args.Get(1).(int64), args.Error(2)
}

func (m *MockVendorRepository) Update(ctx context.Context, vendor *models.Vendor) error {
	args := m.Called(ctx, vendor)
	return args.Error(0)
}

func (m *MockVendorRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// TestCreateVendor_Success tests successful vendor creation
func TestVendorService_Create_Success(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateVendorInput{
		BusinessID:   "business-123",
		Name:         "Acme Corp",
		Email:        "acme@example.com",
		Phone:        "+1234567890",
		Address:      "123 Main St",
		City:         "New York",
		State:        "NY",
		Country:      "USA",
		PostalCode:   "10001",
		TaxID:        "12-3456789",
		PaymentTerms: "Net 30",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(v *models.Vendor) bool {
		return v.Name == input.Name &&
			v.Email == input.Email &&
			v.BusinessID == input.BusinessID
	})).Return(nil)

	vendor, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, vendor)
	assert.Equal(t, input.Name, vendor.Name)
	assert.Equal(t, input.Email, vendor.Email)
	assert.Equal(t, input.BusinessID, vendor.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestCreateVendor_RepositoryError tests vendor creation with repository error
func TestVendorService_Create_RepositoryError(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateVendorInput{
		BusinessID: "business-123",
		Name:       "Acme Corp",
		Email:      "acme@example.com",
	}

	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Vendor")).Return(errors.New("database error"))

	vendor, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, vendor)
	assert.Contains(t, err.Error(), "failed to create vendor")
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_Success tests successful vendor retrieval
func TestVendorService_GetByBusiness_Success(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "vendor-456"

	expectedVendor := &models.Vendor{
		ID:         vendorID,
		BusinessID: businessID,
		Name:       "Acme Corp",
		Email:      "acme@example.com",
	}

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(expectedVendor, nil)

	vendor, err := svc.GetByBusiness(ctx, businessID, vendorID)

	assert.NoError(t, err)
	assert.NotNil(t, vendor)
	assert.Equal(t, vendorID, vendor.ID)
	assert.Equal(t, businessID, vendor.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_NotFound tests vendor not found scenario
func TestVendorService_GetByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "nonexistent"

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(nil, errors.New("vendor not found"))

	vendor, err := svc.GetByBusiness(ctx, businessID, vendorID)

	assert.Error(t, err)
	assert.Nil(t, vendor)
	mockRepo.AssertExpectations(t)
}

// TestList_Success tests listing vendors with pagination
func TestVendorService_List_Success(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	page := 1
	limit := 10

	expectedVendors := []*models.Vendor{
		{ID: "vendor-1", BusinessID: businessID, Name: "Acme Corp"},
		{ID: "vendor-2", BusinessID: businessID, Name: "Globex Inc"},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, page, limit).Return(expectedVendors, int64(2), nil)

	vendors, total, err := svc.List(ctx, businessID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, vendors, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestList_EmptyResult tests listing with no vendors
func TestVendorService_List_EmptyResult(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return([]*models.Vendor{}, int64(0), nil)

	vendors, total, err := svc.List(ctx, businessID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, vendors, 0)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestList_RepositoryError tests listing with repository error
func TestVendorService_List_RepositoryError(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return(nil, int64(0), errors.New("database error"))

	vendors, total, err := svc.List(ctx, businessID, 1, 10)

	assert.Error(t, err)
	assert.Nil(t, vendors)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_Success tests successful vendor update
func TestVendorService_UpdateByBusiness_Success(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "vendor-456"

	existingVendor := &models.Vendor{
		ID:         vendorID,
		BusinessID: businessID,
		Name:       "Old Name",
		Email:      "old@example.com",
	}

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(existingVendor, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(v *models.Vendor) bool {
		return v.Name == "New Name" && v.Email == "new@example.com"
	})).Return(nil)

	input := services.UpdateVendorInput{
		Name:  "New Name",
		Email: "new@example.com",
	}

	vendor, err := svc.UpdateByBusiness(ctx, businessID, vendorID, input)

	assert.NoError(t, err)
	assert.NotNil(t, vendor)
	assert.Equal(t, "New Name", vendor.Name)
	assert.Equal(t, "new@example.com", vendor.Email)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_PartialUpdate tests partial update (only name)
func TestVendorService_UpdateByBusiness_PartialUpdate(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "vendor-456"

	existingVendor := &models.Vendor{
		ID:         vendorID,
		BusinessID: businessID,
		Name:       "Old Name",
		Email:      "old@example.com",
		Phone:      "+1234567890",
	}

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(existingVendor, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(v *models.Vendor) bool {
		return v.Name == "New Name" && v.Email == "old@example.com" && v.Phone == "+1234567890"
	})).Return(nil)

	input := services.UpdateVendorInput{
		Name: "New Name",
	}

	vendor, err := svc.UpdateByBusiness(ctx, businessID, vendorID, input)

	assert.NoError(t, err)
	assert.NotNil(t, vendor)
	assert.Equal(t, "New Name", vendor.Name)
	assert.Equal(t, "old@example.com", vendor.Email)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_NotFound tests update on non-existent vendor
func TestVendorService_UpdateByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "nonexistent"

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(nil, errors.New("vendor not found"))

	input := services.UpdateVendorInput{
		Name: "New Name",
	}

	vendor, err := svc.UpdateByBusiness(ctx, businessID, vendorID, input)

	assert.Error(t, err)
	assert.Nil(t, vendor)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_RepositoryError tests update with repository error
func TestVendorService_UpdateByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "vendor-456"

	existingVendor := &models.Vendor{
		ID:         vendorID,
		BusinessID: businessID,
		Name:       "Old Name",
	}

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(existingVendor, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.Vendor")).Return(errors.New("database error"))

	input := services.UpdateVendorInput{
		Name: "New Name",
	}

	vendor, err := svc.UpdateByBusiness(ctx, businessID, vendorID, input)

	assert.Error(t, err)
	assert.Nil(t, vendor)
	mockRepo.AssertExpectations(t)
}

// TestDelete_Success tests successful vendor deletion
func TestVendorService_Delete_Success(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	vendorID := "vendor-456"

	mockRepo.On("Delete", ctx, vendorID).Return(nil)

	err := svc.Delete(ctx, vendorID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDelete_RepositoryError tests deletion with repository error
func TestVendorService_Delete_RepositoryError(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	vendorID := "vendor-456"

	mockRepo.On("Delete", ctx, vendorID).Return(errors.New("database error"))

	err := svc.Delete(ctx, vendorID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_Success tests successful scoped vendor deletion
func TestVendorService_DeleteByBusiness_Success(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "vendor-456"

	existingVendor := &models.Vendor{
		ID:         vendorID,
		BusinessID: businessID,
		Name:       "Acme Corp",
	}

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(existingVendor, nil)
	mockRepo.On("Delete", ctx, vendorID).Return(nil)

	err := svc.DeleteByBusiness(ctx, businessID, vendorID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_NotFound tests scoped deletion when vendor not found
func TestVendorService_DeleteByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "nonexistent"

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(nil, errors.New("vendor not found"))

	err := svc.DeleteByBusiness(ctx, businessID, vendorID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_WrongBusiness tests deletion when vendor belongs to different business
func TestVendorService_DeleteByBusiness_WrongBusiness(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "vendor-456"

	// Vendor not found for this business (belongs to different business)
	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(nil, errors.New("vendor not found"))

	err := svc.DeleteByBusiness(ctx, businessID, vendorID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_AllFields tests updating all fields at once
func TestVendorService_UpdateByBusiness_AllFields(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	vendorID := "vendor-456"

	existingVendor := &models.Vendor{
		ID:         vendorID,
		BusinessID: businessID,
		Name:       "Old Name",
		Email:      "old@example.com",
	}

	mockRepo.On("GetByID", ctx, vendorID, businessID).Return(existingVendor, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(v *models.Vendor) bool {
		return v.Name == "New Name" &&
			v.Email == "new@example.com" &&
			v.Phone == "+9876543210" &&
			v.Address == "456 New St" &&
			v.City == "Los Angeles" &&
			v.State == "CA" &&
			v.Country == "USA" &&
			v.PostalCode == "90001" &&
			v.TaxID == "98-7654321" &&
			v.PaymentTerms == "Net 60"
	})).Return(nil)

	input := services.UpdateVendorInput{
		Name:         "New Name",
		Email:        "new@example.com",
		Phone:        "+9876543210",
		Address:      "456 New St",
		City:         "Los Angeles",
		State:        "CA",
		Country:      "USA",
		PostalCode:   "90001",
		TaxID:        "98-7654321",
		PaymentTerms: "Net 60",
	}

	vendor, err := svc.UpdateByBusiness(ctx, businessID, vendorID, input)

	assert.NoError(t, err)
	assert.NotNil(t, vendor)
	mockRepo.AssertExpectations(t)
}

// TestCreateVendor_AllFields tests vendor creation with all fields
func TestVendorService_Create_AllFields(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateVendorInput{
		BusinessID:   "business-123",
		Name:        "Acme Corp",
		Email:       "acme@example.com",
		Phone:       "+1234567890",
		Address:     "123 Main St",
		City:        "New York",
		State:       "NY",
		Country:     "USA",
		PostalCode:  "10001",
		TaxID:       "12-3456789",
		PaymentTerms: "Net 30",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(v *models.Vendor) bool {
		return v.Name == input.Name &&
			v.Email == input.Email &&
			v.Phone == input.Phone &&
			v.Address == input.Address &&
			v.City == input.City &&
			v.State == input.State &&
			v.Country == input.Country &&
			v.PostalCode == input.PostalCode &&
			v.TaxID == input.TaxID &&
			v.PaymentTerms == input.PaymentTerms
	})).Return(nil)

	vendor, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, vendor)
	assert.Equal(t, input.Name, vendor.Name)
	assert.Equal(t, input.Email, vendor.Email)
	assert.Equal(t, input.Phone, vendor.Phone)
	assert.Equal(t, input.Address, vendor.Address)
	assert.Equal(t, input.City, vendor.City)
	assert.Equal(t, input.State, vendor.State)
	assert.Equal(t, input.Country, vendor.Country)
	assert.Equal(t, input.PostalCode, vendor.PostalCode)
	assert.Equal(t, input.TaxID, vendor.TaxID)
	assert.Equal(t, input.PaymentTerms, vendor.PaymentTerms)
	mockRepo.AssertExpectations(t)
}

// TestList_Pagination tests listing with different page and limit values
func TestVendorService_List_Pagination(t *testing.T) {
	mockRepo := new(MockVendorRepository)
	log := logger.New()

	svc := services.NewVendorService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	testCases := []struct {
		page   int
		limit  int
		vendor []*models.Vendor
		total  int64
	}{
		{1, 10, []*models.Vendor{{ID: "v1"}}, 1},
		{2, 10, []*models.Vendor{}, 15},
		{1, 5, []*models.Vendor{{ID: "v1"}, {ID: "v2"}}, 2},
	}

	for _, tc := range testCases {
		mockRepo.On("GetByBusinessID", ctx, businessID, tc.page, tc.limit).Return(tc.vendor, tc.total, nil).Once()

		vendors, total, err := svc.List(ctx, businessID, tc.page, tc.limit)

		assert.NoError(t, err)
		assert.Equal(t, tc.vendor, vendors)
		assert.Equal(t, tc.total, total)
	}

	mockRepo.AssertExpectations(t)
}
