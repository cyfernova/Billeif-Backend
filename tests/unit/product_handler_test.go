package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock ProductService
// =============================================================================

type MockProductService struct {
	mock.Mock
}

func (m *MockProductService) Create(ctx context.Context, input services.CreateProductInput) (*models.Product, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Product), args.Error(1)
}

func (m *MockProductService) GetByBusiness(ctx context.Context, businessID, productID string) (*models.Product, error) {
	args := m.Called(ctx, businessID, productID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Product), args.Error(1)
}

func (m *MockProductService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Product), args.Get(1).(int64), args.Error(2)
}

func (m *MockProductService) UpdateByBusiness(ctx context.Context, businessID, productID string, input services.UpdateProductInput) (*models.Product, error) {
	args := m.Called(ctx, businessID, productID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Product), args.Error(1)
}

func (m *MockProductService) DeleteByBusiness(ctx context.Context, businessID, productID string) error {
	args := m.Called(ctx, businessID, productID)
	return args.Error(0)
}

// =============================================================================
// Handler Testable
// =============================================================================

type ProductHandlerTestable struct {
	svc *MockProductService
}

func NewProductHandlerTestable(svc *MockProductService) *ProductHandlerTestable {
	return &ProductHandlerTestable{svc: svc}
}

func (h *ProductHandlerTestable) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	var input services.CreateProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID

	var product *models.Product
	product, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, product)
}

func (h *ProductHandlerTestable) Get(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")

	product, err := h.svc.GetByBusiness(c.Request.Context(), businessID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}

// =============================================================================
// Helper
// =============================================================================

func createProductTestContext(c *gin.Context, businessID string) {
	c.Set("business_id", businessID)
}

// =============================================================================
// Tests
// =============================================================================

func TestProductHandler_Create_Success(t *testing.T) {
	mockSvc := new(MockProductService)
	handler := NewProductHandlerTestable(mockSvc)

	input := services.CreateProductInput{
		BusinessID: "business-123",
		Name:       "Widget A",
		SKU:        "WGT-001",
		Price:      29.99,
		Currency:   "USD",
	}

	expectedProduct := &models.Product{
		ID:         "product-456",
		BusinessID: "business-123",
		Name:       "Widget A",
		SKU:        "WGT-001",
		Price:      29.99,
		Currency:   "USD",
		IsActive:   true,
	}

	mockSvc.On("Create", mock.Anything, mock.MatchedBy(func(in services.CreateProductInput) bool {
		return in.BusinessID == "business-123" && in.Name == input.Name && in.SKU == input.SKU && in.Price == input.Price
	})).Return(expectedProduct, nil)

	router := gin.New()
	router.POST("/api/v1/products", func(c *gin.Context) {
		createProductTestContext(c, "business-123")
		handler.Create(c)
	})

	body, _ := json.Marshal(input)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(t, http.StatusCreated, res.Code)

	var response models.Product
	err := json.Unmarshal(res.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, expectedProduct.ID, response.ID)
	assert.Equal(t, expectedProduct.Name, response.Name)
	assert.Equal(t, expectedProduct.SKU, response.SKU)
	assert.Equal(t, expectedProduct.Price, response.Price)

	mockSvc.AssertExpectations(t)
}

func TestProductHandler_Create_InvalidJSON(t *testing.T) {
	mockSvc := new(MockProductService)
	handler := NewProductHandlerTestable(mockSvc)

	router := gin.New()
	router.POST("/api/v1/products", func(c *gin.Context) {
		createProductTestContext(c, "business-123")
		handler.Create(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(t, http.StatusBadRequest, res.Code)

	mockSvc.AssertNotCalled(t, "Create")
}

func TestProductHandler_Create_ServiceError(t *testing.T) {
	mockSvc := new(MockProductService)
	handler := NewProductHandlerTestable(mockSvc)

	input := services.CreateProductInput{
		BusinessID: "business-123",
		Name:       "Widget B",
		SKU:        "WGT-002",
		Price:      39.99,
	}

	mockSvc.On("Create", mock.Anything, mock.AnythingOfType("services.CreateProductInput")).Return(nil, errors.New("database error"))

	router := gin.New()
	router.POST("/api/v1/products", func(c *gin.Context) {
		createProductTestContext(c, "business-123")
		handler.Create(c)
	})

	body, _ := json.Marshal(input)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(t, http.StatusInternalServerError, res.Code)

	mockSvc.AssertExpectations(t)
}

func TestProductHandler_Get_Success(t *testing.T) {
	mockSvc := new(MockProductService)
	handler := NewProductHandlerTestable(mockSvc)

	productID := "product-456"
	expected := &models.Product{
		ID:         productID,
		BusinessID: "business-123",
		Name:       "Widget C",
		SKU:        "WGT-003",
		Price:      49.99,
	}

	mockSvc.On("GetByBusiness", mock.Anything, "business-123", productID).Return(expected, nil)

	router := gin.New()
	router.GET("/api/v1/products/:id", func(c *gin.Context) {
		createProductTestContext(c, "business-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/product-456", nil)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(t, http.StatusOK, res.Code)

	var response models.Product
	err := json.Unmarshal(res.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, productID, response.ID)

	mockSvc.AssertExpectations(t)
}

func TestProductHandler_Get_NotFound(t *testing.T) {
	mockSvc := new(MockProductService)
	handler := NewProductHandlerTestable(mockSvc)

	mockSvc.On("GetByBusiness", mock.Anything, "business-123", "nonexistent").Return(nil, errors.New("product not found"))

	router := gin.New()
	router.GET("/api/v1/products/:id", func(c *gin.Context) {
		createProductTestContext(c, "business-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/nonexistent", nil)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(t, http.StatusNotFound, res.Code)

	mockSvc.AssertExpectations(t)
}
