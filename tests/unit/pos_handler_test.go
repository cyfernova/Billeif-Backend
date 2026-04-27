package unit

// go test -v ./tests/unit/... -run "TestPOS"
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
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock POSService
// =============================================================================

type MockPOSService struct {
	mock.Mock
}

func (m *MockPOSService) CreateSession(ctx context.Context, businessID, userID string, input services.CreatePOSSessionInput) (*models.POSSession, error) {
	args := m.Called(ctx, businessID, userID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.POSSession), args.Error(1)
}

func (m *MockPOSService) ListSessions(ctx context.Context, businessID string, page, limit int, status string) ([]models.POSSession, int64, error) {
	args := m.Called(ctx, businessID, page, limit, status)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]models.POSSession), args.Get(1).(int64), args.Error(2)
}

func (m *MockPOSService) SearchCatalog(ctx context.Context, businessID, query, warehouseID string, limit int) ([]services.POSCatalogSearchResult, error) {
	args := m.Called(ctx, businessID, query, warehouseID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]services.POSCatalogSearchResult), args.Error(1)
}

func (m *MockPOSService) ScanItem(ctx context.Context, businessID, sessionID string, input services.ScanPOSItemInput) (*models.POSSession, services.POSSessionCart, error) {
	args := m.Called(ctx, businessID, sessionID, input)
	if args.Get(0) == nil {
		return nil, services.POSSessionCart{}, args.Error(2)
	}
	return args.Get(0).(*models.POSSession), args.Get(1).(services.POSSessionCart), args.Error(2)
}

func (m *MockPOSService) Checkout(ctx context.Context, businessID, sessionID, idempotencyKey string, input services.CheckoutPOSCartInput) (*models.Document, error) {
	args := m.Called(ctx, businessID, sessionID, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

func (m *MockPOSService) GetThermalReceipt(ctx context.Context, businessID, documentID, format, width string) (*services.POSReceiptResponse, error) {
	args := m.Called(ctx, businessID, documentID, format, width)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.POSReceiptResponse), args.Error(1)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type POSHandlerTestable struct {
	svc *MockPOSService
	log *logger.Logger
}

func NewPOSHandlerTestable(svc *MockPOSService, log *logger.Logger) *POSHandlerTestable {
	return &POSHandlerTestable{svc: svc, log: log}
}

func (h *POSHandlerTestable) CreateSession(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.CreatePOSSessionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	session, err := h.svc.CreateSession(c.Request.Context(), businessID, userID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, session)
}

func (h *POSHandlerTestable) ListSessions(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	sessions, total, err := h.svc.ListSessions(c.Request.Context(), businessID, page, limit, c.Query("status"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response := utils.NewPaginatedResponse(sessions, total, page, limit)
	c.JSON(http.StatusOK, gin.H{
		"data":        response.Data,
		"items":       sessions,
		"total":       response.Total,
		"page":        response.Page,
		"limit":       response.Limit,
		"total_pages": response.TotalPages,
		"has_next":    response.HasNext,
		"has_prev":    response.HasPrev,
	})
}

func (h *POSHandlerTestable) SearchCatalog(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	limit := 20
	results, err := h.svc.SearchCatalog(c.Request.Context(), businessID, c.Query("q"), c.Query("warehouse_id"), limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": results})
}

func (h *POSHandlerTestable) ScanItem(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.ScanPOSItemInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	session, cart, err := h.svc.ScanItem(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": session, "cart": cart})
}

func (h *POSHandlerTestable) Checkout(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	if idempotencyKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key header is required"})
		return
	}
	var input services.CheckoutPOSCartInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	document, err := h.svc.Checkout(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *POSHandlerTestable) GetReceipt(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	receipt, err := h.svc.GetThermalReceipt(c.Request.Context(), businessID, c.Param("documentID"), "thermal", "58mm")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, receipt)
}

// =============================================================================
// CreateSession Tests
// =============================================================================

func TestPOSCreateSession_Success(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	session := &models.POSSession{
		ID:         "session-123",
		BusinessID: "biz-123",
		UserID:     "user-123",
		Status:     "open",
	}

	input := services.CreatePOSSessionInput{
		POSProfileID: "profile-123",
		SessionName:  "Test Session",
		WarehouseID:  "warehouse-123",
	}

	mockSvc.On("CreateSession", mock.Anything, "biz-123", "user-123", input).Return(session, nil)

	router := gin.New()
	router.POST("/pos/sessions", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.CreateSession(c)
	})

	reqBody := map[string]interface{}{
		"pos_profile_id": "profile-123",
		"session_name":   "Test Session",
		"warehouse_id":   "warehouse-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/pos/sessions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPOSCreateSession_ServiceError(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	input := services.CreatePOSSessionInput{
		SessionName: "Test Session",
	}

	mockSvc.On("CreateSession", mock.Anything, "biz-123", "user-123", input).Return(nil, errors.New("service error"))

	router := gin.New()
	router.POST("/pos/sessions", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.CreateSession(c)
	})

	reqBody := map[string]interface{}{
		"session_name": "Test Session",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/pos/sessions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListSessions Tests
// =============================================================================

func TestPOSListSessions_Success(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	sessions := []models.POSSession{
		{
			ID:         "session-123",
			BusinessID: "biz-123",
			UserID:     "user-123",
			Status:     "open",
		},
	}

	mockSvc.On("ListSessions", mock.Anything, "biz-123", 1, 10, "active").Return(sessions, int64(1), nil)

	router := gin.New()
	router.GET("/pos/sessions", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.ListSessions(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/pos/sessions?status=active", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected JSON response: %v", err)
	}
	if response["total"].(float64) != 1 {
		t.Fatalf("expected total 1, got %v", response["total"])
	}
	if _, ok := response["items"].([]interface{}); !ok {
		t.Fatalf("expected items array, got %T", response["items"])
	}

	mockSvc.AssertExpectations(t)
}

func TestPOSListSessions_ServiceError(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	mockSvc.On("ListSessions", mock.Anything, "biz-123", 1, 10, "").Return(nil, int64(0), errors.New("list error"))

	router := gin.New()
	router.GET("/pos/sessions", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.ListSessions(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/pos/sessions", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// SearchCatalog Tests
// =============================================================================

func TestPOSSearchCatalog_Success(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	results := []services.POSCatalogSearchResult{
		{ID: "prod-1", EntityType: "product", Name: "Product 1", Price: 100},
		{ID: "prod-2", EntityType: "product", Name: "Product 2", Price: 200},
	}

	mockSvc.On("SearchCatalog", mock.Anything, "biz-123", "laptop", "", 20).Return(results, nil)

	router := gin.New()
	router.GET("/pos/catalog/search", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.SearchCatalog(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/pos/catalog/search?q=laptop", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPOSSearchCatalog_ServiceError(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	mockSvc.On("SearchCatalog", mock.Anything, "biz-123", "invalid", "", 20).Return(nil, errors.New("search error"))

	router := gin.New()
	router.GET("/pos/catalog/search", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.SearchCatalog(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/pos/catalog/search?q=invalid", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ScanItem Tests
// =============================================================================

func TestPOSScanItem_Success(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	session := &models.POSSession{
		ID:         "session-123",
		BusinessID: "biz-123",
		Status:     "open",
	}

	cart := services.POSSessionCart{
		Items: []services.POSSessionCartLine{
			{ProductID: "prod-1", Name: "Product 1", Quantity: 1, UnitPrice: 100},
		},
		Subtotal:  100,
		Total:     118,
		ItemCount: 1,
	}

	input := services.ScanPOSItemInput{
		Code:     "123456789",
		Quantity: 1,
	}

	mockSvc.On("ScanItem", mock.Anything, "biz-123", "session-123", input).Return(session, cart, nil)

	router := gin.New()
	router.POST("/pos/carts/:id/items/scan", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.ScanItem(c)
	})

	reqBody := map[string]interface{}{
		"code":     "123456789",
		"quantity": 1,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/pos/carts/session-123/items/scan", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPOSScanItem_InvalidInput(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/pos/carts/:id/items/scan", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.ScanItem(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/pos/carts/session-123/items/scan", bytes.NewBuffer([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestPOSScanItem_ServiceError(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	input := services.ScanPOSItemInput{
		Code:     "123456789",
		Quantity: 1,
	}

	mockSvc.On("ScanItem", mock.Anything, "biz-123", "session-123", input).Return(nil, services.POSSessionCart{}, errors.New("scan error"))

	router := gin.New()
	router.POST("/pos/carts/:id/items/scan", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.ScanItem(c)
	})

	reqBody := map[string]interface{}{
		"code":     "123456789",
		"quantity": 1,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/pos/carts/session-123/items/scan", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Checkout Tests
// =============================================================================

func TestPOSCheckout_Success(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	document := &models.Document{
		ID:         "doc-123",
		BusinessID: "biz-123",
		Status:     "draft",
	}

	input := services.CheckoutPOSCartInput{
		PartyID:   "party-123",
		PartyType: "customer",
	}

	mockSvc.On("Checkout", mock.Anything, "biz-123", "session-123", "idem-key-123", input).Return(document, nil)

	router := gin.New()
	router.POST("/pos/carts/:id/checkout", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.Checkout(c)
	})

	reqBody := map[string]interface{}{
		"party_id":   "party-123",
		"party_type": "customer",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/pos/carts/session-123/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-key-123")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPOSCheckout_MissingIdempotencyKey(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/pos/carts/:id/checkout", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.Checkout(c)
	})

	reqBody := map[string]interface{}{
		"party_id":   "party-123",
		"party_type": "customer",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/pos/carts/session-123/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestPOSCheckout_ServiceError(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	input := services.CheckoutPOSCartInput{
		PartyID:   "party-123",
		PartyType: "customer",
	}

	mockSvc.On("Checkout", mock.Anything, "biz-123", "session-123", "idem-key-456", input).Return(nil, errors.New("checkout error"))

	router := gin.New()
	router.POST("/pos/carts/:id/checkout", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.Checkout(c)
	})

	reqBody := map[string]interface{}{
		"party_id":   "party-123",
		"party_type": "customer",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/pos/carts/session-123/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-key-456")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetReceipt Tests
// =============================================================================

func TestPOSGetReceipt_Success(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	receipt := &services.POSReceiptResponse{
		DocumentID:  "doc-123",
		Format:      "thermal",
		Width:       "58mm",
		ContentType: "text/plain",
		Content:     "RECEIPT CONTENT",
	}

	mockSvc.On("GetThermalReceipt", mock.Anything, "biz-123", "doc-123", "thermal", "58mm").Return(receipt, nil)

	router := gin.New()
	router.GET("/pos/receipts/:documentID", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.GetReceipt(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/pos/receipts/doc-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPOSGetReceipt_ServiceError(t *testing.T) {
	mockSvc := new(MockPOSService)
	log := logger.New()
	handler := NewPOSHandlerTestable(mockSvc, log)

	mockSvc.On("GetThermalReceipt", mock.Anything, "biz-123", "nonexistent", "thermal", "58mm").Return(nil, errors.New("receipt error"))

	router := gin.New()
	router.GET("/pos/receipts/:documentID", func(c *gin.Context) {
		createPOSTestContext(c, "user-123", "biz-123")
		handler.GetReceipt(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/pos/receipts/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func createPOSTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}
