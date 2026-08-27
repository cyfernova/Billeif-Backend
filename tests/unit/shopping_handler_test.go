package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock ShoppingAgentService
// =============================================================================

type MockShoppingService struct {
	mock.Mock
}

func (m *MockShoppingService) SearchProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	args := m.Called(ctx, query, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.MarketplaceProduct), args.Get(1).(int64), args.Error(2)
}

func (m *MockShoppingService) ProcessShoppingIntent(ctx context.Context, req *ShoppingIntentServiceRequest) (*models.CartMandate, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CartMandate), args.Error(1)
}

func (m *MockShoppingService) AddToCart(ctx context.Context, userID, shoppingAgentID, productID string) (*models.CartMandate, error) {
	args := m.Called(ctx, userID, shoppingAgentID, productID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CartMandate), args.Error(1)
}

func (m *MockShoppingService) CompleteCheckout(ctx context.Context, req *CheckoutServiceRequest) (*models.PaymentMandate, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PaymentMandate), args.Error(1)
}

func (m *MockShoppingService) GetCartMandate(ctx context.Context, cartMandateID, userID string) (*models.CartMandate, error) {
	args := m.Called(ctx, cartMandateID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CartMandate), args.Error(1)
}

func (m *MockShoppingService) GetUserCarts(ctx context.Context, userID string, page, limit int) ([]*models.CartMandate, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.CartMandate), args.Get(1).(int64), args.Error(2)
}

func (m *MockShoppingService) GetUserOrders(ctx context.Context, userID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.MarketplaceOrder), args.Get(1).(int64), args.Error(2)
}

func (m *MockShoppingService) TrackOrder(ctx context.Context, orderID, userID string) (*models.MarketplaceOrder, error) {
	args := m.Called(ctx, orderID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.MarketplaceOrder), args.Error(1)
}

func (m *MockShoppingService) GetAvailableProducts(ctx context.Context, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	args := m.Called(ctx, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.MarketplaceProduct), args.Get(1).(int64), args.Error(2)
}

func (m *MockShoppingService) GetProductDetails(ctx context.Context, productID string) (*models.MarketplaceProduct, error) {
	args := m.Called(ctx, productID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.MarketplaceProduct), args.Error(1)
}

func (m *MockShoppingService) GetShoppingAgentCapabilities(ctx context.Context, agentID string) ([]*models.AgentCapability, error) {
	args := m.Called(ctx, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.AgentCapability), args.Error(1)
}

func (m *MockShoppingService) GenerateIdeas(ctx context.Context, userInput string) (string, error) {
	args := m.Called(ctx, userInput)
	return args.String(0), args.Error(1)
}

// Service-level request types
type ShoppingIntentServiceRequest struct {
	UserID          string
	ShoppingAgentID string
	Query           string
	ProductIDs      []string
	MaxAmount       *float64
	Expiration      int
}

type CheckoutServiceRequest struct {
	UserID          string
	CartMandateID   string
	PaymentMethodID *string
}

// =============================================================================
// Testable wrapper
// =============================================================================

type ShoppingHandlerTestable struct {
	svc *MockShoppingService
	log *logger.Logger
}

func NewShoppingHandlerTestable(svc *MockShoppingService, log *logger.Logger) *ShoppingHandlerTestable {
	return &ShoppingHandlerTestable{
		svc: svc,
		log: log,
	}
}

func parsePagination(c *gin.Context) (page, limit int) {
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "10")
	page, _ = strconv.Atoi(pageStr)
	limit, _ = strconv.Atoi(limitStr)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	return
}

func (h *ShoppingHandlerTestable) SearchProducts(c *gin.Context) {
	query := c.Query("q")
	page, limit := parsePagination(c)
	category := c.Query("category")

	var products []*models.MarketplaceProduct
	var total int64
	var err error

	if category != "" {
		products, total, err = h.svc.SearchProducts(c.Request.Context(), query+" category:"+category, page, limit)
	} else {
		products, total, err = h.svc.SearchProducts(c.Request.Context(), query, page, limit)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *ShoppingHandlerTestable) CreateCart(c *gin.Context) {
	userID := c.GetString("user_id")
	shoppingAgentID := c.Query("agent_id")

	if shoppingAgentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	var req struct {
		ProductIDs []string `json:"product_ids" binding:"required"`
		MaxAmount  *float64 `json:"max_amount"`
		Query      string   `json:"query"`
		Expiration int      `json:"expiration"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Expiration == 0 {
		req.Expiration = 24
	}

	shoppingReq := &ShoppingIntentServiceRequest{
		UserID:          userID,
		ShoppingAgentID: shoppingAgentID,
		Query:           req.Query,
		ProductIDs:      req.ProductIDs,
		MaxAmount:       req.MaxAmount,
		Expiration:      req.Expiration,
	}

	cartMandate, err := h.svc.ProcessShoppingIntent(c.Request.Context(), shoppingReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, cartMandate)
}

func (h *ShoppingHandlerTestable) AddToCart(c *gin.Context) {
	userID := c.GetString("user_id")
	shoppingAgentID := c.Query("agent_id")

	if shoppingAgentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	var req struct {
		ProductID string `json:"product_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cartMandate, err := h.svc.AddToCart(c.Request.Context(), userID, shoppingAgentID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, cartMandate)
}

func (h *ShoppingHandlerTestable) Checkout(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		CartMandateID   string  `json:"cart_mandate_id" binding:"required"`
		PaymentMethodID *string `json:"payment_method_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	checkoutReq := &CheckoutServiceRequest{
		UserID:          userID,
		CartMandateID:   req.CartMandateID,
		PaymentMethodID: req.PaymentMethodID,
	}

	paymentMandate, err := h.svc.CompleteCheckout(c.Request.Context(), checkoutReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, paymentMandate)
}

func (h *ShoppingHandlerTestable) GetCart(c *gin.Context) {
	cartID := c.Param("id")
	userID := c.GetString("user_id")

	cartMandate, err := h.svc.GetCartMandate(c.Request.Context(), cartID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cart not found"})
		return
	}

	c.JSON(http.StatusOK, cartMandate)
}

func (h *ShoppingHandlerTestable) ListCarts(c *gin.Context) {
	userID := c.GetString("user_id")
	page, limit := parsePagination(c)

	carts, total, err := h.svc.GetUserCarts(c.Request.Context(), userID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  carts,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *ShoppingHandlerTestable) ListOrders(c *gin.Context) {
	userID := c.GetString("user_id")
	page, limit := parsePagination(c)
	status := c.Query("status")

	orders, total, err := h.svc.GetUserOrders(c.Request.Context(), userID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_ = status // suppress unused warning

	c.JSON(http.StatusOK, gin.H{
		"data":  orders,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *ShoppingHandlerTestable) TrackOrder(c *gin.Context) {
	orderID := c.Param("id")
	userID := c.GetString("user_id")

	order, err := h.svc.TrackOrder(c.Request.Context(), orderID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	c.JSON(http.StatusOK, order)
}

func (h *ShoppingHandlerTestable) GetAvailableProducts(c *gin.Context) {
	page, limit := parsePagination(c)

	products, total, err := h.svc.GetAvailableProducts(c.Request.Context(), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *ShoppingHandlerTestable) GetProductDetails(c *gin.Context) {
	productID := c.Param("id")

	product, err := h.svc.GetProductDetails(c.Request.Context(), productID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}

func (h *ShoppingHandlerTestable) GetAgentCapabilities(c *gin.Context) {
	agentID := c.Param("id")

	capabilities, err := h.svc.GetShoppingAgentCapabilities(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, capabilities)
}

func (h *ShoppingHandlerTestable) GenerateIdeas(c *gin.Context) {
	var req struct {
		Input string `json:"input" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.svc.GenerateIdeas(c.Request.Context(), req.Input)
	if err != nil {
		h.log.Error("failed to generate ideas", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate ideas"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"response": response})
}

// =============================================================================
// SearchProducts Tests
// =============================================================================

func TestSearchProducts_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	products := []*models.MarketplaceProduct{
		{ID: "prod-1", Name: "Product 1", Price: 100.00, Currency: "INR", IsAvailable: true},
		{ID: "prod-2", Name: "Product 2", Price: 200.00, Currency: "INR", IsAvailable: true},
	}

	mockSvc.On("SearchProducts", mock.Anything, "laptop", 1, 10).Return(products, int64(2), nil)

	router := gin.New()
	router.GET("/agents/shopping/search", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.SearchProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/search?q=laptop", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestSearchProducts_WithCategory(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	products := []*models.MarketplaceProduct{
		{ID: "prod-1", Name: "Gaming Laptop", Price: 1000.00, Currency: "INR", IsAvailable: true},
	}

	mockSvc.On("SearchProducts", mock.Anything, "laptop category:electronics", 1, 10).Return(products, int64(1), nil)

	router := gin.New()
	router.GET("/agents/shopping/search", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.SearchProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/search?q=laptop&category=electronics", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestSearchProducts_Error(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("SearchProducts", mock.Anything, "laptop", 1, 10).Return(nil, int64(0), errors.New("search failed"))

	router := gin.New()
	router.GET("/agents/shopping/search", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.SearchProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/search?q=laptop", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CreateCart Tests
// =============================================================================

func TestCreateCart_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	now := time.Now()
	cartMandate := &models.CartMandate{
		ID:          "cart-123",
		UserID:      "user-123",
		AgentID:     "agent-123",
		Items:       `[{"product_id":"prod-1","name":"Product 1","quantity":1,"unit_price":100}]`,
		TotalAmount: 100.00,
		Currency:    "INR",
		Status:      "pending",
		ExpiresAt:   now.Add(24 * time.Hour),
		CreatedAt:   now,
	}

	mockSvc.On("ProcessShoppingIntent", mock.Anything, mock.Anything).Return(cartMandate, nil)

	router := gin.New()
	router.POST("/agents/shopping/cart", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateCart(c)
	})

	reqBody := map[string]interface{}{
		"product_ids": []string{"prod-1"},
		"query":       "buy laptop",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/cart?agent_id=agent-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateCart_MissingAgentID(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/shopping/cart", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateCart(c)
	})

	reqBody := map[string]interface{}{
		"product_ids": []string{"prod-1"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/cart", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCreateCart_BadRequest_MissingProductIDs(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/shopping/cart", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateCart(c)
	})

	reqBody := map[string]interface{}{
		"query": "buy laptop",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/cart?agent_id=agent-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCreateCart_InternalError(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("ProcessShoppingIntent", mock.Anything, mock.Anything).Return(nil, errors.New("failed to create cart"))

	router := gin.New()
	router.POST("/agents/shopping/cart", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateCart(c)
	})

	reqBody := map[string]interface{}{
		"product_ids": []string{"prod-1"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/cart?agent_id=agent-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// AddToCart Tests
// =============================================================================

func TestAddToCart_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	now := time.Now()
	cartMandate := &models.CartMandate{
		ID:          "cart-123",
		UserID:      "user-123",
		AgentID:     "agent-123",
		Items:       `[{"product_id":"prod-1","name":"Product 1","quantity":1,"unit_price":100}]`,
		TotalAmount: 100.00,
		Currency:    "INR",
		Status:      "pending",
		ExpiresAt:   now.Add(24 * time.Hour),
		CreatedAt:   now,
	}

	mockSvc.On("AddToCart", mock.Anything, "user-123", "agent-123", "prod-1").Return(cartMandate, nil)

	router := gin.New()
	router.POST("/agents/shopping/cart/add", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddToCart(c)
	})

	reqBody := map[string]interface{}{
		"product_id": "prod-1",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/cart/add?agent_id=agent-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestAddToCart_MissingAgentID(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/shopping/cart/add", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddToCart(c)
	})

	reqBody := map[string]interface{}{
		"product_id": "prod-1",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/cart/add", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestAddToCart_BadRequest_MissingProductID(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/shopping/cart/add", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddToCart(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/cart/add?agent_id=agent-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestAddToCart_ProductNotFound(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("AddToCart", mock.Anything, "user-123", "agent-123", "invalid-prod").Return(nil, errors.New("invalid product"))

	router := gin.New()
	router.POST("/agents/shopping/cart/add", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddToCart(c)
	})

	reqBody := map[string]interface{}{
		"product_id": "invalid-prod",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/cart/add?agent_id=agent-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Checkout Tests
// =============================================================================

func TestCheckout_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	now := time.Now()
	paymentMandate := &models.PaymentMandate{
		ID:            "pay-123",
		CartMandateID: "cart-123",
		UserID:        "user-123",
		Amount:        100.00,
		Currency:      "INR",
		Status:        "pending",
		CreatedAt:     now,
	}

	mockSvc.On("CompleteCheckout", mock.Anything, mock.Anything).Return(paymentMandate, nil)

	router := gin.New()
	router.POST("/agents/shopping/checkout", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.Checkout(c)
	})

	reqBody := map[string]interface{}{
		"cart_mandate_id": "cart-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCheckout_BadRequest_MissingCartMandateID(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/shopping/checkout", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.Checkout(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCheckout_CartNotFound(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("CompleteCheckout", mock.Anything, mock.Anything).Return(nil, errors.New("cart mandate not found"))

	router := gin.New()
	router.POST("/agents/shopping/checkout", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.Checkout(c)
	})

	reqBody := map[string]interface{}{
		"cart_mandate_id": "nonexistent",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/shopping/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetCart Tests
// =============================================================================

func TestGetCart_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	now := time.Now()
	cartMandate := &models.CartMandate{
		ID:          "cart-123",
		UserID:      "user-123",
		AgentID:     "agent-123",
		Items:       `[{"product_id":"prod-1","name":"Product 1","quantity":1,"unit_price":100}]`,
		TotalAmount: 100.00,
		Currency:    "INR",
		Status:      "pending",
		ExpiresAt:   now.Add(24 * time.Hour),
		CreatedAt:   now,
	}

	mockSvc.On("GetCartMandate", mock.Anything, "cart-123", "user-123").Return(cartMandate, nil)

	router := gin.New()
	router.GET("/agents/shopping/cart/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetCart(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/cart/cart-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetCart_NotFound(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GetCartMandate", mock.Anything, "nonexistent", "user-123").Return(nil, errors.New("cart mandate not found"))

	router := gin.New()
	router.GET("/agents/shopping/cart/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetCart(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/cart/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListCarts Tests
// =============================================================================

func TestListCarts_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	now := time.Now()
	carts := []*models.CartMandate{
		{
			ID:          "cart-1",
			UserID:      "user-123",
			AgentID:     "agent-1",
			Items:       `[{"product_id":"prod-1","name":"Product 1","quantity":1,"unit_price":100}]`,
			TotalAmount: 100.00,
			Currency:    "INR",
			Status:      "pending",
			ExpiresAt:   now.Add(24 * time.Hour),
			CreatedAt:   now,
		},
	}

	mockSvc.On("GetUserCarts", mock.Anything, "user-123", 1, 10).Return(carts, int64(1), nil)

	router := gin.New()
	router.GET("/agents/shopping/carts", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListCarts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/carts", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListCarts_Empty(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GetUserCarts", mock.Anything, "user-123", 1, 10).Return([]*models.CartMandate{}, int64(0), nil)

	router := gin.New()
	router.GET("/agents/shopping/carts", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListCarts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/carts", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestListCarts_Error(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GetUserCarts", mock.Anything, "user-123", 1, 10).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/agents/shopping/carts", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListCarts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/carts", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListOrders Tests
// =============================================================================

func TestListOrders_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	orders := []*models.MarketplaceOrder{
		{ID: "order-1", UserID: "user-123", TotalAmount: 100.00, Currency: "INR", Status: "pending"},
	}

	mockSvc.On("GetUserOrders", mock.Anything, "user-123", 1, 10).Return(orders, int64(1), nil)

	router := gin.New()
	router.GET("/agents/shopping/orders", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListOrders(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/orders", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListOrders_Error(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GetUserOrders", mock.Anything, "user-123", 1, 10).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/agents/shopping/orders", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListOrders(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/orders", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// TrackOrder Tests
// =============================================================================

func TestTrackOrder_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	order := &models.MarketplaceOrder{
		ID:          "order-123",
		UserID:      "user-123",
		TotalAmount: 100.00,
		Currency:    "INR",
		Status:      "shipped",
	}

	mockSvc.On("TrackOrder", mock.Anything, "order-123", "user-123").Return(order, nil)

	router := gin.New()
	router.GET("/agents/shopping/orders/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.TrackOrder(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/orders/order-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTrackOrder_NotFound(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("TrackOrder", mock.Anything, "nonexistent", "user-123").Return(nil, errors.New("order not found"))

	router := gin.New()
	router.GET("/agents/shopping/orders/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.TrackOrder(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/orders/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetAvailableProducts Tests
// =============================================================================

func TestGetAvailableProducts_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	products := []*models.MarketplaceProduct{
		{ID: "prod-1", Name: "Product 1", Price: 100.00, Currency: "INR", IsAvailable: true},
		{ID: "prod-2", Name: "Product 2", Price: 200.00, Currency: "INR", IsAvailable: true},
	}

	mockSvc.On("GetAvailableProducts", mock.Anything, 1, 10).Return(products, int64(2), nil)

	router := gin.New()
	router.GET("/agents/shopping/products/available", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAvailableProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/products/available", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetAvailableProducts_Error(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GetAvailableProducts", mock.Anything, 1, 10).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/agents/shopping/products/available", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAvailableProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/products/available", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetProductDetails Tests
// =============================================================================

func TestGetProductDetails_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	product := &models.MarketplaceProduct{
		ID:          "prod-123",
		Name:        "Test Product",
		Description: strPtr("A test product"),
		Price:       99.99,
		Currency:    "INR",
		IsAvailable: true,
	}

	mockSvc.On("GetProductDetails", mock.Anything, "prod-123").Return(product, nil)

	router := gin.New()
	router.GET("/agents/shopping/products/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetProductDetails(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/products/prod-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetProductDetails_NotFound(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GetProductDetails", mock.Anything, "nonexistent").Return(nil, errors.New("product not found"))

	router := gin.New()
	router.GET("/agents/shopping/products/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetProductDetails(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/products/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetShoppingAgentCapabilities Tests
// =============================================================================

func TestGetShoppingAgentCapabilities_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	capabilities := []*models.AgentCapability{
		{ID: "cap-1", AgentID: "agent-123", CapabilityType: "search_products"},
		{ID: "cap-2", AgentID: "agent-123", CapabilityType: "compare_prices"},
	}

	mockSvc.On("GetShoppingAgentCapabilities", mock.Anything, mock.Anything).Return(capabilities, nil)

	router := gin.New()
	router.GET("/agents/shopping/:id/capabilities", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAgentCapabilities(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/agent-123/capabilities", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetShoppingAgentCapabilities_Error(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GetShoppingAgentCapabilities", mock.Anything, "agent-123").Return(nil, errors.New("failed to get capabilities"))

	router := gin.New()
	router.GET("/agents/shopping/:id/capabilities", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAgentCapabilities(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/shopping/agent-123/capabilities", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GenerateIdeas Tests
// =============================================================================

func TestGenerateIdeas_Success(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GenerateIdeas", mock.Anything, "I need a laptop for gaming").Return("Here are some gaming laptop recommendations...", nil)

	router := gin.New()
	router.POST("/agents/ideate", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GenerateIdeas(c)
	})

	reqBody := map[string]interface{}{
		"input": "I need a laptop for gaming",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/ideate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGenerateIdeas_BadRequest_MissingInput(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/ideate", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GenerateIdeas(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/ideate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestGenerateIdeas_InternalError(t *testing.T) {
	mockSvc := new(MockShoppingService)
	log := logger.New()
	handler := NewShoppingHandlerTestable(mockSvc, log)

	mockSvc.On("GenerateIdeas", mock.Anything, "test input").Return("", errors.New("failed to generate ideas"))

	router := gin.New()
	router.POST("/agents/ideate", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GenerateIdeas(c)
	})

	reqBody := map[string]interface{}{
		"input": "test input",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/ideate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}
