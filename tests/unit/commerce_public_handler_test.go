package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock Commerce Service for Public Storefront
// =============================================================================

type MockCommercePublicService struct {
	mock.Mock
}

func (m *MockCommercePublicService) GetCatalog(ctx context.Context, slug string) (*services.StorefrontCatalogResponse, error) {
	args := m.Called(ctx, slug)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.StorefrontCatalogResponse), args.Error(1)
}

func (m *MockCommercePublicService) ValidateCoupon(ctx context.Context, slug string, input services.ValidateCouponInput) (*services.CouponValidationResult, error) {
	args := m.Called(ctx, slug, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.CouponValidationResult), args.Error(1)
}

func (m *MockCommercePublicService) Checkout(ctx context.Context, slug, idempotencyKey string, input services.StorefrontCheckoutInput) (*services.CheckoutResult, error) {
	args := m.Called(ctx, slug, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.CheckoutResult), args.Error(1)
}

func (m *MockCommercePublicService) GetPublicOrder(ctx context.Context, slug, token string) (*models.StoreOrder, error) {
	args := m.Called(ctx, slug, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StoreOrder), args.Error(1)
}

func (m *MockCommercePublicService) HandleRazorpayWebhook(ctx context.Context, signature string, body []byte) error {
	args := m.Called(ctx, signature, body)
	return args.Error(0)
}

// =============================================================================
// Testable Handler for Public Storefront
// =============================================================================

type TestableCommercePublicHandler struct {
	svc *MockCommercePublicService
	log *logger.Logger
}

func NewTestableCommercePublicHandler(svc *MockCommercePublicService, log *logger.Logger) *TestableCommercePublicHandler {
	return &TestableCommercePublicHandler{svc: svc, log: log}
}

func (h *TestableCommercePublicHandler) PublicCatalog(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

func (h *TestableCommercePublicHandler) PublicCategories(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog.Categories)
}

func (h *TestableCommercePublicHandler) PublicValidateCoupon(c *gin.Context) {
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

func (h *TestableCommercePublicHandler) PublicCheckout(c *gin.Context) {
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

func (h *TestableCommercePublicHandler) PublicOrder(c *gin.Context) {
	order, err := h.svc.GetPublicOrder(c.Request.Context(), c.Param("slug"), c.Param("token"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, services.NewPublicStoreOrderResponse(order))
}

func (h *TestableCommercePublicHandler) PublicRazorpayWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}
	if err := h.svc.HandleRazorpayWebhook(c.Request.Context(), c.GetHeader("X-Razorpay-Signature"), body); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// =============================================================================
// Helper Functions
// =============================================================================

// =============================================================================
// PublicCatalog Tests
// =============================================================================

func TestPublicCatalog_Success(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

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

func TestPublicCatalog_StoreNotFound(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	mockSvc.On("GetCatalog", mock.Anything, "non-existent").Return((*services.StorefrontCatalogResponse)(nil), errors.New("not found"))

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

func TestPublicCatalog_WithProducts(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	catalog := &services.StorefrontCatalogResponse{
		Storefront: &models.Storefront{ID: "sf-123", Name: "Tech Store", Slug: "tech-store"},
		Categories: []*models.StorefrontCategory{
			{ID: "cat-1", Name: "Laptops", Slug: "laptops"},
		},
		Products: []*services.StorefrontCatalogItem{
			{
				StorefrontProduct: &models.StorefrontProduct{ID: "sp-1", DisplayPrice: 50000},
				Product:           &models.Product{ID: "prod-1", Name: "Gaming Laptop"},
			},
		},
	}
	mockSvc.On("GetCatalog", mock.Anything, "tech-store").Return(catalog, nil)

	router := gin.New()
	router.GET("/public/store/catalog/:slug", handler.PublicCatalog)

	req := httptest.NewRequest(http.MethodGet, "/public/store/catalog/tech-store", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response services.StorefrontCatalogResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(response.Products))
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// PublicCategories Tests
// =============================================================================

func TestPublicCategories_Success(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	catalog := &services.StorefrontCatalogResponse{
		Storefront: &models.Storefront{ID: "sf-123", Name: "My Store", Slug: "my-store"},
		Categories: []*models.StorefrontCategory{
			{ID: "cat-1", Name: "Electronics", Slug: "electronics"},
			{ID: "cat-2", Name: "Clothing", Slug: "clothing"},
		},
		Products: []*services.StorefrontCatalogItem{},
	}
	mockSvc.On("GetCatalog", mock.Anything, "my-store").Return(catalog, nil)

	router := gin.New()
	router.GET("/public/store/categories/:slug", handler.PublicCategories)

	req := httptest.NewRequest(http.MethodGet, "/public/store/categories/my-store", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var categories []*models.StorefrontCategory
	if err := json.Unmarshal(res.Body.Bytes(), &categories); err != nil {
		t.Fatalf("decode categories: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(categories))
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicCategories_StoreNotFound(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	mockSvc.On("GetCatalog", mock.Anything, "non-existent").Return((*services.StorefrontCatalogResponse)(nil), errors.New("not found"))

	router := gin.New()
	router.GET("/public/store/categories/:slug", handler.PublicCategories)

	req := httptest.NewRequest(http.MethodGet, "/public/store/categories/non-existent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicCategories_EmptyCategories(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	catalog := &services.StorefrontCatalogResponse{
		Storefront: &models.Storefront{ID: "sf-123", Name: "Empty Store", Slug: "empty-store"},
		Categories: []*models.StorefrontCategory{},
		Products:   []*services.StorefrontCatalogItem{},
	}
	mockSvc.On("GetCatalog", mock.Anything, "empty-store").Return(catalog, nil)

	router := gin.New()
	router.GET("/public/store/categories/:slug", handler.PublicCategories)

	req := httptest.NewRequest(http.MethodGet, "/public/store/categories/empty-store", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var categories []*models.StorefrontCategory
	if err := json.Unmarshal(res.Body.Bytes(), &categories); err != nil {
		t.Fatalf("decode categories: %v", err)
	}
	if len(categories) != 0 {
		t.Fatalf("expected 0 categories, got %d", len(categories))
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// PublicValidateCoupon Tests
// =============================================================================

func TestPublicValidateCoupon_Success(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

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

func TestPublicValidateCoupon_BadRequest_MissingCode(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	router := gin.New()
	router.POST("/public/store/coupons/validate/:slug", handler.PublicValidateCoupon)

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/store/coupons/validate/my-store", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestPublicValidateCoupon_StoreNotFound(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	mockSvc.On("ValidateCoupon", mock.Anything, "non-existent-store", mock.Anything).Return((*services.CouponValidationResult)(nil), errors.New("not found"))

	router := gin.New()
	router.POST("/public/store/coupons/validate/:slug", handler.PublicValidateCoupon)

	reqBody := map[string]interface{}{
		"code": "SAVE10",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/store/coupons/validate/non-existent-store", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicValidateCoupon_InvalidCoupon(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	result := &services.CouponValidationResult{
		Valid:         false,
		DiscountTotal: 0,
		Message:       "Coupon has expired",
	}
	mockSvc.On("ValidateCoupon", mock.Anything, "my-store", mock.Anything).Return(result, nil)

	router := gin.New()
	router.POST("/public/store/coupons/validate/:slug", handler.PublicValidateCoupon)

	reqBody := map[string]interface{}{
		"code": "EXPIRED10",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/store/coupons/validate/my-store", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response services.CouponValidationResult
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Valid != false {
		t.Fatalf("expected Valid=false, got %v", response.Valid)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// PublicCheckout Tests
// =============================================================================

func TestPublicCheckout_Success(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	result := &services.CheckoutResult{
		Order:          &models.StoreOrder{ID: "ord-123", Status: "pending", Total: 500.00},
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

func TestPublicCheckout_BadRequest_MissingCustomer(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	router := gin.New()
	router.POST("/public/store/checkout/:slug", handler.PublicCheckout)

	reqBody := map[string]interface{}{
		"items": []map[string]interface{}{
			{"product_id": "11111111-1111-1111-1111-111111111111", "quantity": 1},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/store/checkout/my-store", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestPublicCheckout_BadRequest_EmptyItems(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	router := gin.New()
	router.POST("/public/store/checkout/:slug", handler.PublicCheckout)

	reqBody := map[string]interface{}{
		"items":    []map[string]interface{}{},
		"customer": map[string]interface{}{"name": "Test"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/store/checkout/my-store", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestPublicCheckout_StoreNotFound(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	mockSvc.On("Checkout", mock.Anything, "non-existent-store", "test-idempotency-key", mock.Anything).Return((*services.CheckoutResult)(nil), errors.New("store not found"))

	router := gin.New()
	router.POST("/public/store/checkout/:slug", handler.PublicCheckout)

	reqBody := map[string]interface{}{
		"items": []map[string]interface{}{
			{"product_id": "11111111-1111-1111-1111-111111111111", "quantity": 1},
		},
		"customer": map[string]interface{}{
			"name": "Test Customer",
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/store/checkout/non-existent-store", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicCheckout_WithCoupon_Success(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	result := &services.CheckoutResult{
		Order:          &models.StoreOrder{ID: "ord-456", Status: "pending", Total: 450.00},
		GatewayOrderID: "gw-456",
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
		"coupon_code": "SAVE20",
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
// PublicOrder Tests
// =============================================================================

func TestPublicOrder_Success(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

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

func TestPublicOrder_ResponseContainsOnlyAllowlistedFields(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)
	branchID := "branch-internal"
	customerID := "customer-internal"
	couponID := "coupon-internal"
	productID := "product-internal"
	variantID := "variant-internal"
	warehouseID := "warehouse-internal"
	paidAt := time.Date(2026, time.March, 2, 10, 30, 0, 0, time.UTC)
	cancelledAt := time.Date(2026, time.March, 3, 10, 30, 0, 0, time.UTC)
	orderedAt := time.Date(2026, time.March, 1, 10, 30, 0, 0, time.UTC)

	order := &models.StoreOrder{
		ID:                 "order-internal",
		BusinessID:         "business-internal",
		StorefrontID:       "storefront-internal",
		BranchID:           &branchID,
		CustomerID:         &customerID,
		CouponID:           &couponID,
		SalesOrderID:       publicOrderStringPtr("sales-order-internal"),
		SalesInvoiceID:     publicOrderStringPtr("sales-invoice-internal"),
		PublicToken:        "public-token-secret",
		OrderNumber:        "ORDER-2026-001",
		Status:             models.StoreOrderStatusPaid,
		PaymentStatus:      models.StoreOrderPaymentStatusPaid,
		PaymentMethod:      "online",
		Currency:           "INR",
		ExchangeRate:       82.35,
		FXProvider:         "internal-fx-provider",
		FXBaseCurrency:     "USD",
		FXQuoteCurrency:    "INR",
		FXRateTimestamp:    &orderedAt,
		Subtotal:           100,
		DiscountTotal:      10,
		TaxTotal:           16.2,
		ShippingTotal:      20,
		Total:              126.2,
		Snapshot:           `{"cost_price": 42}`,
		BillingAddress:     `{"street":"private billing address"}`,
		ShippingAddress:    `{"street":"private shipping address"}`,
		Notes:              "private order note",
		IdempotencyKey:     "idempotency-secret",
		ExternalOrderID:    "external-order-internal",
		ExternalPaymentID:  "external-payment-internal",
		GatewayOrderID:     "gateway-order-secret",
		GatewayPaymentID:   "gateway-payment-secret",
		WebhookReference:   "webhook-reference-secret",
		OrderedAt:          orderedAt,
		PaidAt:             &paidAt,
		CancelledAt:        &cancelledAt,
		CancellationReason: "internal cancellation reason",
		CreatedAt:          orderedAt,
		UpdatedAt:          cancelledAt,
		Lines: []*models.StoreOrderLine{{
			ID:             "line-internal",
			StoreOrderID:   "order-internal",
			ProductID:      &productID,
			VariantID:      &variantID,
			WarehouseID:    &warehouseID,
			Title:          "Public product title",
			SKU:            "PUBLIC-SKU",
			Quantity:       2,
			UnitPrice:      50,
			DiscountAmount: 10,
			TaxRate:        18,
			TaxAmount:      16.2,
			LineTotal:      106.2,
			Snapshot:       `{"supplier_cost": 25}`,
			CreatedAt:      orderedAt,
			UpdatedAt:      cancelledAt,
		}},
		Events: []*models.StoreOrderEvent{{
			ID:           "event-internal",
			StoreOrderID: "order-internal",
			EventType:    "payment.webhook.received",
			Status:       "paid",
			Payload:      `{"signature":"webhook-secret"}`,
			CreatedAt:    orderedAt,
			UpdatedAt:    cancelledAt,
		}},
	}
	mockSvc.On("GetPublicOrder", mock.Anything, "my-store", "public-token-secret").Return(order, nil)

	router := gin.New()
	router.GET("/public/store/orders/:slug/:token", handler.PublicOrder)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/public/store/orders/my-store/public-token-secret", nil))

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertExactJSONKeys(t, response, []string{
		"order_number", "status", "payment_status", "payment_method", "currency",
		"subtotal", "discount_total", "tax_total", "shipping_total", "total",
		"ordered_at", "paid_at", "cancelled_at", "lines",
	})
	var lines []map[string]json.RawMessage
	if err := json.Unmarshal(response["lines"], &lines); err != nil {
		t.Fatalf("decode lines: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	assertExactJSONKeys(t, lines[0], []string{
		"title", "sku", "quantity", "unit_price", "discount_amount", "tax_rate", "tax_amount", "line_total",
	})
	mockSvc.AssertExpectations(t)
}

func assertExactJSONKeys(t *testing.T, value map[string]json.RawMessage, expected []string) {
	t.Helper()
	actual := make([]string, 0, len(value))
	for key := range value {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if len(actual) != len(expected) {
		t.Fatalf("expected keys %v, got %v", expected, actual)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("expected keys %v, got %v", expected, actual)
		}
	}
}

func publicOrderStringPtr(value string) *string {
	return &value
}

func TestPublicOrder_NotFound(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

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

func TestPublicOrder_DifferentStorefronts(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	order := &models.StoreOrder{
		ID:           "ord-789",
		StorefrontID: "sf-other",
		Status:       "completed",
		Total:        1000.00,
		PublicToken:  "token-789",
	}
	mockSvc.On("GetPublicOrder", mock.Anything, "other-store", "token-789").Return(order, nil)

	router := gin.New()
	router.GET("/public/store/orders/:slug/:token", handler.PublicOrder)

	req := httptest.NewRequest(http.MethodGet, "/public/store/orders/other-store/token-789", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicOrder_CompletedStatus(t *testing.T) {
	mockSvc := new(MockCommercePublicService)
	log := logger.New()
	handler := NewTestableCommercePublicHandler(mockSvc, log)

	order := &models.StoreOrder{
		ID:           "ord-completed",
		StorefrontID: "sf-123",
		Status:       "completed",
		Total:        250.00,
		PublicToken:  "token-completed",
	}
	mockSvc.On("GetPublicOrder", mock.Anything, "my-store", "token-completed").Return(order, nil)

	router := gin.New()
	router.GET("/public/store/orders/:slug/:token", handler.PublicOrder)

	req := httptest.NewRequest(http.MethodGet, "/public/store/orders/my-store/token-completed", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response models.StoreOrder
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "completed" {
		t.Fatalf("expected status=completed, got %s", response.Status)
	}

	mockSvc.AssertExpectations(t)
}
