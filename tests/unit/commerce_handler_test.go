package unit

import (
	
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock Commerce Service
// =============================================================================

type MockCommerceService struct {
	mock.Mock
}

func (m *MockCommerceService) ListStorefronts(ctx context.Context, businessID string) ([]*models.Storefront, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Storefront), args.Error(1)
}

func (m *MockCommerceService) CreateStorefront(ctx context.Context, input services.UpsertStorefrontInput) (*models.Storefront, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Storefront), args.Error(1)
}

func (m *MockCommerceService) GetStorefront(ctx context.Context, businessID, id string) (*models.Storefront, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Storefront), args.Error(1)
}

func (m *MockCommerceService) UpdateStorefrontSettings(ctx context.Context, businessID, id string, input services.UpsertStorefrontInput) (*models.Storefront, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Storefront), args.Error(1)
}

func (m *MockCommerceService) ListStorefrontProducts(ctx context.Context, businessID, storefrontID string) ([]*models.StorefrontProduct, error) {
	args := m.Called(ctx, businessID, storefrontID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.StorefrontProduct), args.Error(1)
}

func (m *MockCommerceService) ReplaceStorefrontProducts(ctx context.Context, businessID, storefrontID string, input []services.UpsertStorefrontProductInput) ([]*models.StorefrontProduct, error) {
	args := m.Called(ctx, businessID, storefrontID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.StorefrontProduct), args.Error(1)
}

func (m *MockCommerceService) ListStorefrontCoupons(ctx context.Context, storefrontID string) ([]*models.StorefrontCoupon, error) {
	args := m.Called(ctx, storefrontID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.StorefrontCoupon), args.Error(1)
}

func (m *MockCommerceService) CreateStorefrontCoupon(ctx context.Context, storefrontID string, input services.UpsertStorefrontCouponInput) (*models.StorefrontCoupon, error) {
	args := m.Called(ctx, storefrontID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StorefrontCoupon), args.Error(1)
}

func (m *MockCommerceService) UpdateStorefrontCoupon(ctx context.Context, storefrontID, couponID string, input services.UpsertStorefrontCouponInput) (*models.StorefrontCoupon, error) {
	args := m.Called(ctx, storefrontID, couponID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StorefrontCoupon), args.Error(1)
}

func (m *MockCommerceService) ListStorefrontOrders(ctx context.Context, businessID, storefrontID, status string, page, limit int) ([]*models.StoreOrder, int64, error) {
	args := m.Called(ctx, businessID, storefrontID, status, page, limit)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]*models.StoreOrder), args.Get(1).(int64), args.Error(2)
}

func (m *MockCommerceService) ApproveStoreOrder(ctx context.Context, businessID, storefrontID, orderID string) (*models.StoreOrder, error) {
	args := m.Called(ctx, businessID, storefrontID, orderID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StoreOrder), args.Error(1)
}

func (m *MockCommerceService) CancelStoreOrder(ctx context.Context, businessID, storefrontID, orderID, reason string) (*models.StoreOrder, error) {
	args := m.Called(ctx, businessID, storefrontID, orderID, reason)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StoreOrder), args.Error(1)
}

func (m *MockCommerceService) GetCatalog(ctx context.Context, slug string) (*services.StorefrontCatalogResponse, error) {
	args := m.Called(ctx, slug)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.StorefrontCatalogResponse), args.Error(1)
}

func (m *MockCommerceService) ValidateCoupon(ctx context.Context, slug string, input services.ValidateCouponInput) (*services.CouponValidationResult, error) {
	args := m.Called(ctx, slug, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.CouponValidationResult), args.Error(1)
}

func (m *MockCommerceService) Checkout(ctx context.Context, slug, idempotencyKey string, input services.StorefrontCheckoutInput) (*services.CheckoutResult, error) {
	args := m.Called(ctx, slug, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.CheckoutResult), args.Error(1)
}

func (m *MockCommerceService) GetPublicOrder(ctx context.Context, slug, token string) (*models.StoreOrder, error) {
	args := m.Called(ctx, slug, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StoreOrder), args.Error(1)
}

// =============================================================================
// Test Commerce Handler Wrapper
// =============================================================================

type TestableCommerceHandler struct {
	svc *MockCommerceService
	log *logger.Logger
}

func NewTestableCommerceHandler(svc *MockCommerceService, log *logger.Logger) *TestableCommerceHandler {
	return &TestableCommerceHandler{svc: svc, log: log}
}

func (h *TestableCommerceHandler) ListStorefronts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	storefronts, err := h.svc.ListStorefronts(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, storefronts)
}

func (h *TestableCommerceHandler) CreateStorefront(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertStorefrontInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	storefront, err := h.svc.CreateStorefront(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, storefront)
}

func (h *TestableCommerceHandler) GetStorefront(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	storefront, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, storefront)
}

func (h *TestableCommerceHandler) UpdateStorefrontSettings(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertStorefrontInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	storefront, err := h.svc.UpdateStorefrontSettings(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, storefront)
}

func (h *TestableCommerceHandler) ListStorefrontProducts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	items, err := h.svc.ListStorefrontProducts(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *TestableCommerceHandler) ReplaceStorefrontProducts(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var input []services.UpsertStorefrontProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, err := h.svc.ReplaceStorefrontProducts(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *TestableCommerceHandler) ListStorefrontCoupons(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	coupons, err := h.svc.ListStorefrontCoupons(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, coupons)
}

func (h *TestableCommerceHandler) CreateStorefrontCoupon(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var input services.UpsertStorefrontCouponInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	coupon, err := h.svc.CreateStorefrontCoupon(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, coupon)
}

func (h *TestableCommerceHandler) UpdateStorefrontCoupon(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var input services.UpsertStorefrontCouponInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	coupon, err := h.svc.UpdateStorefrontCoupon(c.Request.Context(), c.Param("id"), c.Param("coupon_id"), input)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, coupon)
}

func (h *TestableCommerceHandler) ListStorefrontOrders(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page := 1
	limit := 20
	orders, total, err := h.svc.ListStorefrontOrders(c.Request.Context(), businessID, c.Param("id"), c.Query("status"), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": orders, "total": total})
}

func (h *TestableCommerceHandler) ApproveStorefrontOrder(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	order, err := h.svc.ApproveStoreOrder(c.Request.Context(), businessID, c.Param("id"), c.Param("order_id"))
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, order)
}

func (h *TestableCommerceHandler) CancelStorefrontOrder(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&payload)
	order, err := h.svc.CancelStoreOrder(c.Request.Context(), businessID, c.Param("id"), c.Param("order_id"), payload.Reason)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, order)
}

func (h *TestableCommerceHandler) PublicCatalog(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

func (h *TestableCommerceHandler) PublicValidateCoupon(c *gin.Context) {
	var input services.ValidateCouponInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.ValidateCoupon(c.Request.Context(), c.Param("slug"), input)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TestableCommerceHandler) PublicCheckout(c *gin.Context) {
	var input services.StorefrontCheckoutInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.Checkout(c.Request.Context(), c.Param("slug"), "test-idempotency-key", input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *TestableCommerceHandler) PublicOrder(c *gin.Context) {
	order, err := h.svc.GetPublicOrder(c.Request.Context(), c.Param("slug"), c.Param("token"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, order)
}

func requestContextWithActor(c *gin.Context) {
	// No-op for testing
}

func requireBusinessScope(c *gin.Context) (string, bool) {
	businessID, exists := c.Get("business_id")
	if !exists || businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return "", false
	}
	return businessID.(string), true
}

func requireUserScope(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user scope required"})
		return "", false
	}
	return userID.(string), true
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

// =============================================================================
// ListStorefronts Tests
// =============================================================================

func TestListStorefronts_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefronts := []*models.Storefront{
		{ID: "sf-1", Name: "Store 1", Slug: "store-1", Status: "active", BusinessID: "biz-123"},
		{ID: "sf-2", Name: "Store 2", Slug: "store-2", Status: "draft", BusinessID: "biz-123"},
	}
	mockSvc.On("ListStorefronts", mock.Anything, "biz-123").Return(storefronts, nil)

	router := gin.New()
	router.GET("/storefronts", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefronts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListStorefronts_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/storefronts", func(c *gin.Context) {
		// No business_id set
		handler.ListStorefronts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// CreateStorefront Tests
// =============================================================================

func TestCreateStorefront_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{
		ID:         "sf-123",
		Name:       "My Store",
		Slug:       "my-store",
		Status:     "draft",
		BusinessID: "biz-123",
	}
	mockSvc.On("CreateStorefront", mock.Anything, mock.MatchedBy(func(input services.UpsertStorefrontInput) bool {
		return input.Name == "My Store" && input.BusinessID == "biz-123"
	})).Return(storefront, nil)

	router := gin.New()
	router.POST("/storefronts", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateStorefront(c)
	})

	reqBody := map[string]interface{}{
		"name": "My Store",
		"slug": "my-store",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateStorefront_BadRequest(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/storefronts", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateStorefront(c)
	})

	// Missing required "name" field
	reqBody := map[string]interface{}{
		"slug": "my-store",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// GetStorefront Tests
// =============================================================================

func TestGetStorefront_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", Name: "My Store", Slug: "my-store", BusinessID: "biz-123"}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)

	router := gin.New()
	router.GET("/storefronts/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.GetStorefront(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetStorefront_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "non-existent").Return((*models.Storefront)(nil), errors.New("record not found"))

	router := gin.New()
	router.GET("/storefronts/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.GetStorefront(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/non-existent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpdateStorefrontSettings Tests
// =============================================================================

func TestUpdateStorefrontSettings_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", Name: "Updated Store", Status: "active", BusinessID: "biz-123"}
	mockSvc.On("UpdateStorefrontSettings", mock.Anything, "biz-123", "sf-123", mock.Anything).Return(storefront, nil)

	router := gin.New()
	router.PUT("/storefronts/:id/settings", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateStorefrontSettings(c)
	})

	reqBody := map[string]interface{}{
		"name":   "Updated Store",
		"status": "active",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/settings", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListStorefrontProducts Tests
// =============================================================================

func TestListStorefrontProducts_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	products := []*models.StorefrontProduct{
		{ID: "sp-1", StorefrontID: "sf-123", ProductID: "prod-1", DisplayPrice: 100.00},
	}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("ListStorefrontProducts", mock.Anything, "biz-123", "sf-123").Return(products, nil)

	router := gin.New()
	router.GET("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/products", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListStorefrontProducts_StorefrontNotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "non-existent").Return((*models.Storefront)(nil), errors.New("record not found"))

	router := gin.New()
	router.GET("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/non-existent/products", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ReplaceStorefrontProducts Tests
// =============================================================================

func TestReplaceStorefrontProducts_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	products := []*models.StorefrontProduct{
		{ID: "sp-1", StorefrontID: "sf-123", ProductID: "prod-1"},
	}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("ReplaceStorefrontProducts", mock.Anything, "biz-123", "sf-123", mock.Anything).Return(products, nil)

	router := gin.New()
	router.PUT("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ReplaceStorefrontProducts(c)
	})

	reqBody := []map[string]interface{}{
		{"product_id": "11111111-1111-1111-1111-111111111111", "display_price": 100.00},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/products", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListStorefrontCoupons Tests
// =============================================================================

func TestListStorefrontCoupons_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	coupons := []*models.StorefrontCoupon{
		{ID: "cp-1", StorefrontID: "sf-123", Code: "SAVE10", DiscountType: "percentage", DiscountValue: 10},
	}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("ListStorefrontCoupons", mock.Anything, "sf-123").Return(coupons, nil)

	router := gin.New()
	router.GET("/storefronts/:id/coupons", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontCoupons(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/coupons", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CreateStorefrontCoupon Tests
// =============================================================================

func TestCreateStorefrontCoupon_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	coupon := &models.StorefrontCoupon{ID: "cp-1", StorefrontID: "sf-123", Code: "SAVE20", DiscountType: "percentage", DiscountValue: 20}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("CreateStorefrontCoupon", mock.Anything, "sf-123", mock.MatchedBy(func(input services.UpsertStorefrontCouponInput) bool {
		return input.Code == "SAVE20" && input.DiscountType == "percentage"
	})).Return(coupon, nil)

	router := gin.New()
	router.POST("/storefronts/:id/coupons", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{
		"code":            "SAVE20",
		"discount_type":   "percentage",
		"discount_value":   20,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/coupons", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpdateStorefrontCoupon Tests
// =============================================================================

func TestUpdateStorefrontCoupon_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	coupon := &models.StorefrontCoupon{ID: "cp-1", StorefrontID: "sf-123", Code: "SAVE30", DiscountType: "fixed", DiscountValue: 30}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("UpdateStorefrontCoupon", mock.Anything, "sf-123", "cp-1", mock.Anything).Return(coupon, nil)

	router := gin.New()
	router.PUT("/storefronts/:id/coupons/:coupon_id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{
		"code":            "SAVE30",
		"discount_type":   "fixed",
		"discount_value":   30,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/coupons/cp-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListStorefrontOrders Tests
// =============================================================================

func TestListStorefrontOrders_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	orders := []*models.StoreOrder{
		{ID: "ord-1", StorefrontID: "sf-123", Status: "pending", Total: 500.00},
	}
	mockSvc.On("ListStorefrontOrders", mock.Anything, "biz-123", "sf-123", "", 1, 20).Return(orders, int64(1), nil)

	router := gin.New()
	router.GET("/storefronts/:id/orders", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontOrders(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/orders", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListStorefrontOrders_WithStatusFilter(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	orders := []*models.StoreOrder{
		{ID: "ord-1", StorefrontID: "sf-123", Status: "completed", Total: 500.00},
	}
	mockSvc.On("ListStorefrontOrders", mock.Anything, "biz-123", "sf-123", "completed", 1, 20).Return(orders, int64(1), nil)

	router := gin.New()
	router.GET("/storefronts/:id/orders", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontOrders(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/orders?status=completed", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ApproveStorefrontOrder Tests
// =============================================================================

func TestApproveStorefrontOrder_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	order := &models.StoreOrder{ID: "ord-1", StorefrontID: "sf-123", Status: "approved", Total: 500.00}
	mockSvc.On("ApproveStoreOrder", mock.Anything, "biz-123", "sf-123", "ord-1").Return(order, nil)

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/approve", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ApproveStorefrontOrder(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/ord-1/approve", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestApproveStorefrontOrder_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("ApproveStoreOrder", mock.Anything, "biz-123", "sf-123", "non-existent").Return((*models.StoreOrder)(nil), errors.New("order not found"))

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/approve", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ApproveStorefrontOrder(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/non-existent/approve", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CancelStorefrontOrder Tests
// =============================================================================

func TestCancelStorefrontOrder_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	order := &models.StoreOrder{ID: "ord-1", StorefrontID: "sf-123", Status: "cancelled", Total: 500.00}
	mockSvc.On("CancelStoreOrder", mock.Anything, "biz-123", "sf-123", "ord-1", "Customer request").Return(order, nil)

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/cancel", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CancelStorefrontOrder(c)
	})

	reqBody := map[string]interface{}{
		"reason": "Customer request",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/ord-1/cancel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Public Catalog Tests
// =============================================================================

func TestPublicCatalog_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	catalog := &services.StorefrontCatalogResponse{
		Storefront: &models.Storefront{ID: "sf-123", Name: "My Store", Slug: "my-store"},
		Categories: []*models.StorefrontCategory{},
		Products:   []*services.StorefrontCatalogItem{},
	}
	mockSvc.On("GetCatalog", mock.Anything, "my-store").Return(catalog, nil)

	router := gin.New()
	router.GET("/public/store/catalog/:slug", handler.PublicCatalog)

	req := httptest.NewRequest(http.MethodGet, "/public/store/catalog/my-store", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicCatalog_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetCatalog", mock.Anything, "non-existent").Return((*services.StorefrontCatalogResponse)(nil), errors.New("record not found"))

	router := gin.New()
	router.GET("/public/store/catalog/:slug", handler.PublicCatalog)

	req := httptest.NewRequest(http.MethodGet, "/public/store/catalog/non-existent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Public ValidateCoupon Tests
// =============================================================================

func TestPublicValidateCoupon_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	result := &services.CouponValidationResult{
		Valid:         true,
		DiscountTotal: 10,
	}
	mockSvc.On("ValidateCoupon", mock.Anything, "my-store", mock.Anything).Return(result, nil)

	router := gin.New()
	router.POST("/public/store/coupons/validate/:slug", handler.PublicValidateCoupon)

	reqBody := map[string]interface{}{
		"code": "SAVE10",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/store/coupons/validate/my-store", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Public Checkout Tests
// =============================================================================

func TestPublicCheckout_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	result := &services.CheckoutResult{
		Order: &models.StoreOrder{ID: "ord-123", Status: "pending", Total: 500.00},
		GatewayOrderID: "gw-123",
	}
	mockSvc.On("Checkout", mock.Anything, "my-store", "test-idempotency-key", mock.Anything).Return(result, nil)

	router := gin.New()
	router.POST("/public/store/checkout/:slug", handler.PublicCheckout)

	reqBody := map[string]interface{}{
		"items": []map[string]interface{}{
			{"product_id": "11111111-1111-1111-1111-111111111111", "quantity": 2},
		},
		"customer": map[string]interface{}{
			"name":  "Test Customer",
			"email": "test@example.com",
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/store/checkout/my-store", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Public Order Tests
// =============================================================================

func TestPublicOrder_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	order := &models.StoreOrder{
		ID:           "ord-123",
		StorefrontID: "sf-123",
		Status:       "pending",
		Total:        500.00,
		PublicToken:  "public-token-123",
	}
	mockSvc.On("GetPublicOrder", mock.Anything, "my-store", "public-token-123").Return(order, nil)

	router := gin.New()
	router.GET("/public/store/orders/:slug/:token", handler.PublicOrder)

	req := httptest.NewRequest(http.MethodGet, "/public/store/orders/my-store/public-token-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicOrder_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetPublicOrder", mock.Anything, "my-store", "invalid-token").Return((*models.StoreOrder)(nil), errors.New("order not found"))

	router := gin.New()
	router.GET("/public/store/orders/:slug/:token", handler.PublicOrder)

	req := httptest.NewRequest(http.MethodGet, "/public/store/orders/my-store/invalid-token", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}
