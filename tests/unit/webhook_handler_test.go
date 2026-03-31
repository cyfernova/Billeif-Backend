package unit

// go test -v ./tests/unit/... -run "TestWebhook"
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock WebhookService
// =============================================================================

type MockWebhookService struct {
	mock.Mock
}

func (m *MockWebhookService) CreateByBusiness(ctx context.Context, businessID string, input services.CreateWebhookInput) (*models.Webhook, error) {
	args := m.Called(ctx, businessID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Webhook), args.Error(1)
}

func (m *MockWebhookService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Webhook, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Webhook), args.Error(1)
}

func (m *MockWebhookService) List(ctx context.Context, businessID string) ([]*models.Webhook, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Webhook), args.Error(1)
}

func (m *MockWebhookService) UpdateByBusiness(ctx context.Context, businessID, id string, input services.UpdateWebhookInput) (*models.Webhook, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Webhook), args.Error(1)
}

func (m *MockWebhookService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	args := m.Called(ctx, businessID, id)
	return args.Error(0)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type WebhookHandlerTestable struct {
	svc *MockWebhookService
	log *logger.Logger
}

func NewWebhookHandlerTestable(svc *MockWebhookService, log *logger.Logger) *WebhookHandlerTestable {
	return &WebhookHandlerTestable{svc: svc, log: log}
}

func (h *WebhookHandlerTestable) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateWebhookInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID

	webhook, err := h.svc.CreateByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, webhook)
}

func (h *WebhookHandlerTestable) Get(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	webhook, err := h.svc.GetByBusiness(c.Request.Context(), businessID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}
	c.JSON(http.StatusOK, webhook)
}

func (h *WebhookHandlerTestable) List(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	webhooks, err := h.svc.List(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": webhooks})
}

func (h *WebhookHandlerTestable) Update(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var input services.UpdateWebhookInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	webhook, err := h.svc.UpdateByBusiness(c.Request.Context(), businessID, id, input)
	if err != nil {
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, webhook)
}

func (h *WebhookHandlerTestable) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	if err := h.svc.DeleteByBusiness(c.Request.Context(), businessID, id); err != nil {
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// =============================================================================
// Create Tests
// =============================================================================

func TestWebhookCreate_Success(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	now := time.Now()
	webhook := &models.Webhook{
		ID:         "webhook-123",
		BusinessID: "biz-123",
		Name:       "Test Webhook",
		URL:        "https://example.com/webhook",
		Events:     "document.created,document.updated",
		IsActive:   true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	mockSvc.On("CreateByBusiness", mock.Anything, "biz-123", mock.Anything).Return(webhook, nil)

	router := gin.New()
	router.POST("/webhooks", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"name":   "Test Webhook",
		"url":    "https://example.com/webhook",
		"events": []string{"document.created", "document.updated"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWebhookCreate_InvalidInput(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/webhooks", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestWebhookCreate_ServiceError(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	mockSvc.On("CreateByBusiness", mock.Anything, "biz-123", mock.Anything).Return(nil, errors.New("service error"))

	router := gin.New()
	router.POST("/webhooks", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"name":   "Test Webhook",
		"url":    "https://example.com/webhook",
		"events": []string{"document.created"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Get Tests
// =============================================================================

func TestWebhookGet_Success(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	now := time.Now()
	webhook := &models.Webhook{
		ID:         "webhook-123",
		BusinessID: "biz-123",
		Name:       "Test Webhook",
		URL:        "https://example.com/webhook",
		Events:     "document.created",
		IsActive:   true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	mockSvc.On("GetByBusiness", mock.Anything, "biz-123", "webhook-123").Return(webhook, nil)

	router := gin.New()
	router.GET("/webhooks/:id", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/webhooks/webhook-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWebhookGet_NotFound(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	mockSvc.On("GetByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/webhooks/:id", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/webhooks/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// List Tests
// =============================================================================

func TestWebhookList_Success(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	now := time.Now()
	webhooks := []*models.Webhook{
		{ID: "webhook-1", BusinessID: "biz-123", Name: "Webhook 1", URL: "https://example.com/1", Events: "doc.created", IsActive: true, CreatedAt: now, UpdatedAt: now},
		{ID: "webhook-2", BusinessID: "biz-123", Name: "Webhook 2", URL: "https://example.com/2", Events: "doc.updated", IsActive: false, CreatedAt: now, UpdatedAt: now},
	}

	mockSvc.On("List", mock.Anything, "biz-123").Return(webhooks, nil)

	router := gin.New()
	router.GET("/webhooks", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/webhooks", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWebhookList_Empty(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	mockSvc.On("List", mock.Anything, "biz-123").Return([]*models.Webhook{}, nil)

	router := gin.New()
	router.GET("/webhooks", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/webhooks", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWebhookList_ServiceError(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	mockSvc.On("List", mock.Anything, "biz-123").Return(nil, errors.New("database error"))

	router := gin.New()
	router.GET("/webhooks", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/webhooks", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Update Tests
// =============================================================================

func TestWebhookUpdate_Success(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	now := time.Now()
	webhook := &models.Webhook{
		ID:         "webhook-123",
		BusinessID: "biz-123",
		Name:       "Updated Webhook",
		URL:        "https://example.com/updated",
		Events:     "document.created,document.deleted",
		IsActive:   true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	input := services.UpdateWebhookInput{
		Name: "Updated Webhook",
		URL:  "https://example.com/updated",
	}

	mockSvc.On("UpdateByBusiness", mock.Anything, "biz-123", "webhook-123", input).Return(webhook, nil)

	router := gin.New()
	router.PUT("/webhooks/:id", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Webhook",
		"url":  "https://example.com/updated",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/webhooks/webhook-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWebhookUpdate_NotFound(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	input := services.UpdateWebhookInput{
		Name: "Updated Webhook",
	}

	mockSvc.On("UpdateByBusiness", mock.Anything, "biz-123", "nonexistent", input).Return(nil, ErrNotFound)

	router := gin.New()
	router.PUT("/webhooks/:id", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Webhook",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/webhooks/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWebhookUpdate_InvalidInput(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	router := gin.New()
	router.PUT("/webhooks/:id", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/webhooks/webhook-123", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// Delete Tests
// =============================================================================

func TestWebhookDelete_Success(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	mockSvc.On("DeleteByBusiness", mock.Anything, "biz-123", "webhook-123").Return(nil)

	router := gin.New()
	router.DELETE("/webhooks/:id", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/webhooks/webhook-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWebhookDelete_NotFound(t *testing.T) {
	mockSvc := new(MockWebhookService)
	log := logger.New()
	handler := NewWebhookHandlerTestable(mockSvc, log)

	mockSvc.On("DeleteByBusiness", mock.Anything, "biz-123", "nonexistent").Return(ErrNotFound)

	router := gin.New()
	router.DELETE("/webhooks/:id", func(c *gin.Context) {
		createWebhookTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/webhooks/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func createWebhookTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}
