package unit

// go test -v ./tests/unit/... -run "TestRenderProfile"
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
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock RenderProfileService
// =============================================================================

type MockRenderProfileService struct {
	mock.Mock
}

func (m *MockRenderProfileService) ListRenderProfilesByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.RenderProfile, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.RenderProfile), args.Get(1).(int64), args.Error(2)
}

func (m *MockRenderProfileService) GetRenderProfileByBusiness(ctx context.Context, businessID, profileID string) (*models.RenderProfile, error) {
	args := m.Called(ctx, businessID, profileID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.RenderProfile), args.Error(1)
}

func (m *MockRenderProfileService) GetDefaultRenderProfileByBusiness(ctx context.Context, businessID string) (*models.RenderProfile, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.RenderProfile), args.Error(1)
}

func (m *MockRenderProfileService) CreateRenderProfileByBusiness(ctx context.Context, businessID string, input services.CreateRenderProfileInput) (*models.RenderProfile, error) {
	args := m.Called(ctx, businessID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.RenderProfile), args.Error(1)
}

func (m *MockRenderProfileService) UpdateRenderProfileByBusiness(ctx context.Context, businessID, profileID string, input services.UpdateRenderProfileInput) (*models.RenderProfile, error) {
	args := m.Called(ctx, businessID, profileID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.RenderProfile), args.Error(1)
}

func (m *MockRenderProfileService) DeleteRenderProfileByBusiness(ctx context.Context, businessID, profileID string) error {
	args := m.Called(ctx, businessID, profileID)
	return args.Error(0)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type RenderProfileHandlerTestable struct {
	svc *MockRenderProfileService
	log *logger.Logger
}

func NewRenderProfileHandlerTestable(svc *MockRenderProfileService, log *logger.Logger) *RenderProfileHandlerTestable {
	return &RenderProfileHandlerTestable{svc: svc, log: log}
}

func (h *RenderProfileHandlerTestable) List(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page := 1
	limit := 20
	profiles, total, err := h.svc.ListRenderProfilesByBusiness(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": profiles, "total": total, "page": page, "limit": limit})
}

func (h *RenderProfileHandlerTestable) Get(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	profile, err := h.svc.GetRenderProfileByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "render profile not found"})
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *RenderProfileHandlerTestable) GetDefault(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	profile, err := h.svc.GetDefaultRenderProfileByBusiness(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "render profile not found"})
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *RenderProfileHandlerTestable) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateRenderProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	profile, err := h.svc.CreateRenderProfileByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, profile)
}

func (h *RenderProfileHandlerTestable) Update(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpdateRenderProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	profile, err := h.svc.UpdateRenderProfileByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *RenderProfileHandlerTestable) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteRenderProfileByBusiness(c.Request.Context(), businessID, c.Param("id")); err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// =============================================================================
// List Tests
// =============================================================================

func TestRenderProfileList_Success(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	profiles := []*models.RenderProfile{
		{ID: "profile-1", BusinessID: "biz-123", Name: "Standard Profile"},
		{ID: "profile-2", BusinessID: "biz-123", Name: "Compact Profile"},
	}

	mockSvc.On("ListRenderProfilesByBusiness", mock.Anything, "biz-123", 1, 20).Return(profiles, int64(2), nil)

	router := gin.New()
	router.GET("/render-profiles", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/render-profiles", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderProfileList_Empty(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	mockSvc.On("ListRenderProfilesByBusiness", mock.Anything, "biz-123", 1, 20).Return([]*models.RenderProfile{}, int64(0), nil)

	router := gin.New()
	router.GET("/render-profiles", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/render-profiles", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderProfileList_InternalError(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	mockSvc.On("ListRenderProfilesByBusiness", mock.Anything, "biz-123", 1, 20).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/render-profiles", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/render-profiles", nil)
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

func TestRenderProfileGet_Success(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	profile := &models.RenderProfile{
		ID:         "profile-123",
		BusinessID: "biz-123",
		Name:       "Standard Profile",
		PageSize:   "A4",
	}

	mockSvc.On("GetRenderProfileByBusiness", mock.Anything, "biz-123", "profile-123").Return(profile, nil)

	router := gin.New()
	router.GET("/render-profiles/:id", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/render-profiles/profile-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderProfileGet_NotFound(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	mockSvc.On("GetRenderProfileByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/render-profiles/:id", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/render-profiles/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetDefault Tests
// =============================================================================

func TestRenderProfileGetDefault_Success(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	profile := &models.RenderProfile{
		ID:         "profile-default",
		BusinessID: "biz-123",
		Name:       "Default Profile",
		IsDefault:  true,
	}

	mockSvc.On("GetDefaultRenderProfileByBusiness", mock.Anything, "biz-123").Return(profile, nil)

	router := gin.New()
	router.GET("/render-profiles/default", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.GetDefault(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/render-profiles/default", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderProfileGetDefault_NotFound(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	mockSvc.On("GetDefaultRenderProfileByBusiness", mock.Anything, "biz-123").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/render-profiles/default", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.GetDefault(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/render-profiles/default", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Create Tests
// =============================================================================

func TestRenderProfileCreate_Success(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	profile := &models.RenderProfile{
		ID:         "profile-new",
		BusinessID: "biz-123",
		Name:       "New Profile",
		PageSize:   "A4",
	}

	input := services.CreateRenderProfileInput{
		Name:     "New Profile",
		PageSize: "A4",
	}

	mockSvc.On("CreateRenderProfileByBusiness", mock.Anything, "biz-123", input).Return(profile, nil)

	router := gin.New()
	router.POST("/render-profiles", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"name":      "New Profile",
		"page_size": "A4",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/render-profiles", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderProfileCreate_InvalidInput(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/render-profiles", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/render-profiles", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestRenderProfileCreate_ServiceError(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	input := services.CreateRenderProfileInput{
		Name:     "New Profile",
		PageSize: "A4",
	}

	mockSvc.On("CreateRenderProfileByBusiness", mock.Anything, "biz-123", input).Return(nil, errors.New("service error"))

	router := gin.New()
	router.POST("/render-profiles", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"name":      "New Profile",
		"page_size": "A4",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/render-profiles", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
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

func TestRenderProfileUpdate_Success(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	profile := &models.RenderProfile{
		ID:         "profile-123",
		BusinessID: "biz-123",
		Name:       "Updated Profile",
		PageSize:   "A3",
	}

	input := services.UpdateRenderProfileInput{
		Name: "Updated Profile",
	}

	mockSvc.On("UpdateRenderProfileByBusiness", mock.Anything, "biz-123", "profile-123", input).Return(profile, nil)

	router := gin.New()
	router.PUT("/render-profiles/:id", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Profile",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/render-profiles/profile-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderProfileUpdate_NotFound(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	input := services.UpdateRenderProfileInput{
		Name: "Updated Profile",
	}

	mockSvc.On("UpdateRenderProfileByBusiness", mock.Anything, "biz-123", "nonexistent", input).Return(nil, ErrNotFound)

	router := gin.New()
	router.PUT("/render-profiles/:id", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Profile",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/render-profiles/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderProfileUpdate_InvalidInput(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	router := gin.New()
	router.PUT("/render-profiles/:id", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/render-profiles/profile-123", bytes.NewBuffer([]byte("invalid json")))
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

func TestRenderProfileDelete_Success(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	mockSvc.On("DeleteRenderProfileByBusiness", mock.Anything, "biz-123", "profile-123").Return(nil)

	router := gin.New()
	router.DELETE("/render-profiles/:id", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/render-profiles/profile-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderProfileDelete_NotFound(t *testing.T) {
	mockSvc := new(MockRenderProfileService)
	log := logger.New()
	handler := NewRenderProfileHandlerTestable(mockSvc, log)

	mockSvc.On("DeleteRenderProfileByBusiness", mock.Anything, "biz-123", "nonexistent").Return(ErrNotFound)

	router := gin.New()
	router.DELETE("/render-profiles/:id", func(c *gin.Context) {
		createRenderProfileTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/render-profiles/nonexistent", nil)
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

func createRenderProfileTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}
