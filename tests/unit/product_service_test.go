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

// MockProductRepositoryProd mocks the ProductRepository interface
type MockProductRepositoryProd struct {
	mock.Mock
}

func (m *MockProductRepositoryProd) Create(ctx context.Context, product *models.Product) error {
	args := m.Called(ctx, product)
	return args.Error(0)
}

func (m *MockProductRepositoryProd) GetByID(ctx context.Context, id, businessID string) (*models.Product, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Product), args.Error(1)
}

func (m *MockProductRepositoryProd) GetBySKU(ctx context.Context, businessID, sku string) (*models.Product, error) {
	args := m.Called(ctx, businessID, sku)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Product), args.Error(1)
}

func (m *MockProductRepositoryProd) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Product), args.Get(1).(int64), args.Error(2)
}

func (m *MockProductRepositoryProd) Update(ctx context.Context, product *models.Product) error {
	args := m.Called(ctx, product)
	return args.Error(0)
}

func (m *MockProductRepositoryProd) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockProductRepositoryProd) AdjustStock(ctx context.Context, productID string, quantity int64) error {
	args := m.Called(ctx, productID, quantity)
	return args.Error(0)
}

// MockS3Service mocks the S3 service
type MockS3Service struct {
	mock.Mock
}

func (m *MockS3Service) GeneratePresignedUploadURL(ctx context.Context, bucket, key, contentType string, expiresIn int64) (string, error) {
	args := m.Called(ctx, bucket, key, contentType, expiresIn)
	return args.String(0), args.Error(1)
}

// TestCreateProduct_Success tests successful product creation
func TestProductService_Create_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	input := services.CreateProductInput{
		BusinessID:  "business-123",
		Name:        "Widget A",
		SKU:         "WGT-001",
		Description: "A great widget",
		Price:       29.99,
		Currency:    "USD",
		Unit:        "PCS",
		StockLevel:  100,
		MinStock:    10,
	}

	mockRepo.On("GetBySKU", ctx, "business-123", "WGT-001").Return(nil, nil)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(p *models.Product) bool {
		return p.Name == input.Name && p.SKU == input.SKU && p.Price == input.Price && p.IsActive == true
	})).Return(nil)

	product, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, input.Name, product.Name)
	assert.Equal(t, input.SKU, product.SKU)
	assert.Equal(t, input.Price, product.Price)
	assert.Equal(t, "USD", product.Currency)
	assert.Equal(t, "PCS", product.Unit)
	assert.True(t, product.IsActive)
	mockRepo.AssertExpectations(t)
}

// TestCreateProduct_DefaultCurrency tests product creation with default currency
func TestProductService_Create_DefaultCurrency(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	input := services.CreateProductInput{
		BusinessID: "business-123",
		Name:       "Widget B",
		SKU:        "WGT-002",
		Price:      19.99,
		// Currency left empty
	}

	mockRepo.On("GetBySKU", ctx, "business-123", "WGT-002").Return(nil, nil)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(p *models.Product) bool {
		return p.Currency == "USD" && p.Unit == "PCS"
	})).Return(nil)

	product, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, "USD", product.Currency)
	assert.Equal(t, "PCS", product.Unit)
	mockRepo.AssertExpectations(t)
}

// TestCreateProduct_DuplicateSKU tests product creation with duplicate SKU
func TestProductService_Create_DuplicateSKU(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	input := services.CreateProductInput{
		BusinessID: "business-123",
		Name:       "Widget C",
		SKU:        "WGT-003",
		Price:      39.99,
	}

	existing := &models.Product{
		ID:         "existing-id",
		BusinessID: "business-123",
		Name:       "Existing Widget",
		SKU:        "WGT-003",
		Price:      29.99,
	}

	mockRepo.On("GetBySKU", ctx, "business-123", "WGT-003").Return(existing, nil)

	product, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, product)
	assert.Contains(t, err.Error(), "already exists")
	mockRepo.AssertExpectations(t)
}

// TestCreateProduct_RepositoryError tests product creation with repository error
func TestProductService_Create_RepositoryError(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	input := services.CreateProductInput{
		BusinessID: "business-123",
		Name:       "Widget D",
		SKU:        "WGT-004",
		Price:      49.99,
	}

	mockRepo.On("GetBySKU", ctx, "business-123", "WGT-004").Return(nil, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.Product")).Return(errors.New("database error"))

	product, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, product)
	assert.Contains(t, err.Error(), "failed to create product")
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_Success tests successful product retrieval
func TestProductService_GetByBusiness_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	expected := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget E",
		SKU:        "WGT-005",
		Price:      59.99,
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(expected, nil)

	product, err := svc.GetByBusiness(ctx, businessID, productID)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, productID, product.ID)
	assert.Equal(t, businessID, product.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_NotFound tests product not found
func TestProductService_GetByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "nonexistent"

	mockRepo.On("GetByID", ctx, productID, businessID).Return(nil, errors.New("product not found"))

	product, err := svc.GetByBusiness(ctx, businessID, productID)

	assert.Error(t, err)
	assert.Nil(t, product)
	mockRepo.AssertExpectations(t)
}

// TestGetBySKU_Success tests successful product retrieval by SKU
func TestProductService_GetBySKU_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	sku := "WGT-006"

	expected := &models.Product{
		ID:         "product-456",
		BusinessID: businessID,
		Name:       "Widget F",
		SKU:        sku,
		Price:      69.99,
	}

	mockRepo.On("GetBySKU", ctx, businessID, sku).Return(expected, nil)

	product, err := svc.GetBySKU(ctx, businessID, sku)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, sku, product.SKU)
	mockRepo.AssertExpectations(t)
}

// TestGetBySKU_NotFound tests product SKU not found
func TestProductService_GetBySKU_NotFound(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	sku := "NONEXISTENT"

	mockRepo.On("GetBySKU", ctx, businessID, sku).Return(nil, errors.New("product not found"))

	product, err := svc.GetBySKU(ctx, businessID, sku)

	assert.Error(t, err)
	assert.Nil(t, product)
	mockRepo.AssertExpectations(t)
}

// TestList_Success tests listing products with pagination
func TestProductService_List_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	page := 1
	limit := 10

	expectedProducts := []*models.Product{
		{ID: "p1", BusinessID: businessID, Name: "Widget 1", SKU: "W1", Price: 10.00},
		{ID: "p2", BusinessID: businessID, Name: "Widget 2", SKU: "W2", Price: 20.00},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, page, limit).Return(expectedProducts, int64(2), nil)

	products, total, err := svc.List(ctx, businessID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, products, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestList_EmptyResult tests listing with no products
func TestProductService_List_EmptyResult(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return([]*models.Product{}, int64(0), nil)

	products, total, err := svc.List(ctx, businessID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, products, 0)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestList_RepositoryError tests listing with repository error
func TestProductService_List_RepositoryError(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return([]*models.Product{}, int64(0), errors.New("database error"))

	products, _, err := svc.List(ctx, businessID, 1, 10)

	assert.Error(t, err)
	assert.Empty(t, products)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_Success tests successful product update
func TestProductService_UpdateByBusiness_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Old Name",
		SKU:        "OLD-SKU",
		Price:      10.00,
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(p *models.Product) bool {
		return p.Name == "New Name" && p.Price == 25.00
	})).Return(nil)

	input := services.UpdateProductInput{
		Name:  "New Name",
		Price: 25.00,
	}

	product, err := svc.UpdateByBusiness(ctx, businessID, productID, input)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, "New Name", product.Name)
	assert.Equal(t, 25.00, product.Price)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_PartialUpdate tests partial update (only price)
func TestProductService_UpdateByBusiness_PartialUpdate(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Original Name",
		SKU:        "ORIG-SKU",
		Price:      15.00,
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(p *models.Product) bool {
		return p.Price == 35.00 && p.Name == "Original Name"
	})).Return(nil)

	input := services.UpdateProductInput{
		Price: 35.00,
	}

	product, err := svc.UpdateByBusiness(ctx, businessID, productID, input)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, 35.00, product.Price)
	assert.Equal(t, "Original Name", product.Name)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_NotFound tests update on non-existent product
func TestProductService_UpdateByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "nonexistent"

	mockRepo.On("GetByID", ctx, productID, businessID).Return(nil, errors.New("product not found"))

	input := services.UpdateProductInput{
		Name: "New Name",
	}

	product, err := svc.UpdateByBusiness(ctx, businessID, productID, input)

	assert.Error(t, err)
	assert.Nil(t, product)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_RepositoryError tests update with repository error
func TestProductService_UpdateByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Original Name",
		Price:      10.00,
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.Product")).Return(errors.New("database error"))

	input := services.UpdateProductInput{
		Name: "New Name",
	}

	product, err := svc.UpdateByBusiness(ctx, businessID, productID, input)

	assert.Error(t, err)
	assert.Nil(t, product)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_AllFields tests updating all fields at once
func TestProductService_UpdateByBusiness_AllFields(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Old Name",
		SKU:        "OLD-SKU",
		Description: "Old desc",
		Price:      10.00,
		Currency:   "USD",
		Unit:       "PCS",
		MinStock:   5,
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(p *models.Product) bool {
		return p.Name == "New Name" &&
			p.SKU == "NEW-SKU" &&
			p.Description == "New desc" &&
			p.Price == 50.00 &&
			p.Currency == "EUR" &&
			p.Unit == "BOX" &&
			p.MinStock == 10
	})).Return(nil)

	input := services.UpdateProductInput{
		Name:        "New Name",
		SKU:         "NEW-SKU",
		Description: "New desc",
		Price:       50.00,
		Currency:    "EUR",
		Unit:        "BOX",
		MinStock:    10,
	}

	product, err := svc.UpdateByBusiness(ctx, businessID, productID, input)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_Success tests successful product deletion
func TestProductService_DeleteByBusiness_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget to Delete",
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("Delete", ctx, productID).Return(nil)

	err := svc.DeleteByBusiness(ctx, businessID, productID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_NotFound tests deletion when product not found
func TestProductService_DeleteByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "nonexistent"

	mockRepo.On("GetByID", ctx, productID, businessID).Return(nil, errors.New("product not found"))

	err := svc.DeleteByBusiness(ctx, businessID, productID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_RepositoryError tests deletion with repository error
func TestProductService_DeleteByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget",
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("Delete", ctx, productID).Return(errors.New("database error"))

	err := svc.DeleteByBusiness(ctx, businessID, productID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAdjustStockByBusiness_Success tests successful stock adjustment
func TestProductService_AdjustStockByBusiness_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget",
		StockLevel: 100,
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("AdjustStock", ctx, productID, int64(50)).Return(nil)

	input := services.StockAdjustmentInput{
		Quantity: 50,
		Reason:   "Restocking",
	}

	product, err := svc.AdjustStockByBusiness(ctx, businessID, productID, input)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, int64(150), product.StockLevel)
	mockRepo.AssertExpectations(t)
}

// TestAdjustStockByBusiness_NotFound tests stock adjustment when product not found
func TestProductService_AdjustStockByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "nonexistent"

	mockRepo.On("GetByID", ctx, productID, businessID).Return(nil, errors.New("product not found"))

	input := services.StockAdjustmentInput{
		Quantity: 10,
	}

	product, err := svc.AdjustStockByBusiness(ctx, businessID, productID, input)

	assert.Error(t, err)
	assert.Nil(t, product)
	mockRepo.AssertExpectations(t)
}

// TestAdjustStockByBusiness_NegativeAdjustment tests negative stock adjustment
func TestProductService_AdjustStockByBusiness_NegativeAdjustment(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget",
		StockLevel: 100,
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("AdjustStock", ctx, productID, int64(-30)).Return(nil)

	input := services.StockAdjustmentInput{
		Quantity: -30,
		Reason:   "Sold",
	}

	product, err := svc.AdjustStockByBusiness(ctx, businessID, productID, input)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, int64(70), product.StockLevel)
	mockRepo.AssertExpectations(t)
}

// TestAdjustStockByBusiness_RepositoryError tests stock adjustment with repository error
func TestProductService_AdjustStockByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget",
		StockLevel: 100,
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("AdjustStock", ctx, productID, int64(10)).Return(errors.New("database error"))

	input := services.StockAdjustmentInput{
		Quantity: 10,
	}

	product, err := svc.AdjustStockByBusiness(ctx, businessID, productID, input)

	assert.Error(t, err)
	assert.Nil(t, product)
	mockRepo.AssertExpectations(t)
}

// TestGetImageUploadURLByBusiness_Success tests successful presigned URL generation
func TestProductService_GetImageUploadURLByBusiness_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"
	contentType := "image/jpeg"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget",
	}

	expectedURL := "https://s3.example.com/products/product-456/image?signature=abc"

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockS3.On("GeneratePresignedUploadURL", ctx, "product-images", "products/product-456/image", contentType, int64(3600)).Return(expectedURL, nil)

	url, err := svc.GetImageUploadURLByBusiness(ctx, businessID, productID, contentType)

	assert.NoError(t, err)
	assert.Equal(t, expectedURL, url)
	mockRepo.AssertExpectations(t)
	mockS3.AssertExpectations(t)
}

// TestGetImageUploadURLByBusiness_ProductNotFound tests URL generation when product not found
func TestProductService_GetImageUploadURLByBusiness_ProductNotFound(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "nonexistent"

	mockRepo.On("GetByID", ctx, productID, businessID).Return(nil, errors.New("product not found"))

	url, err := svc.GetImageUploadURLByBusiness(ctx, businessID, productID, "image/jpeg")

	assert.Error(t, err)
	assert.Empty(t, url)
	mockRepo.AssertExpectations(t)
}

// TestGetImageUploadURLByBusiness_S3Error tests URL generation when S3 fails
func TestProductService_GetImageUploadURLByBusiness_S3Error(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget",
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockS3.On("GeneratePresignedUploadURL", ctx, "product-images", "products/product-456/image", "image/jpeg", int64(3600)).Return("", errors.New("S3 error"))

	url, err := svc.GetImageUploadURLByBusiness(ctx, businessID, productID, "image/jpeg")

	assert.Error(t, err)
	assert.Empty(t, url)
	mockRepo.AssertExpectations(t)
	mockS3.AssertExpectations(t)
}

// TestUpdateImageURL_Success tests successful image URL update
func TestProductService_UpdateImageURL_Success(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"
	imageURL := "https://s3.example.com/products/product-456/image.jpg"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget",
		ImageURL:   "",
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(p *models.Product) bool {
		return p.ImageURL == imageURL
	})).Return(nil)

	err := svc.UpdateImageURL(ctx, businessID, productID, imageURL)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestUpdateImageURL_NotFound tests image URL update when product not found
func TestProductService_UpdateImageURL_NotFound(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "nonexistent"

	mockRepo.On("GetByID", ctx, productID, businessID).Return(nil, errors.New("product not found"))

	err := svc.UpdateImageURL(ctx, businessID, productID, "https://example.com/image.jpg")

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestUpdateImageURL_RepositoryError tests image URL update with repository error
func TestProductService_UpdateImageURL_RepositoryError(t *testing.T) {
	mockRepo := new(MockProductRepositoryProd)
	mockS3 := new(MockS3Service)
	log := logger.New()

	svc := services.NewProductServiceForTesting(mockRepo, mockS3, log)

	ctx := context.Background()
	businessID := "business-123"
	productID := "product-456"

	existing := &models.Product{
		ID:         productID,
		BusinessID: businessID,
		Name:       "Widget",
		ImageURL:   "",
	}

	mockRepo.On("GetByID", ctx, productID, businessID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.Product")).Return(errors.New("database error"))

	err := svc.UpdateImageURL(ctx, businessID, productID, "https://example.com/image.jpg")

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}
