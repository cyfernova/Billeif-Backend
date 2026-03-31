package unit

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
// Mock JournalService
// =============================================================================

type MockJournalService struct {
	mock.Mock
}

func (m *MockJournalService) CreateByBusiness(ctx context.Context, businessID string, input services.CreateJournalInput) (*models.Journal, error) {
	args := m.Called(ctx, businessID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Journal), args.Error(1)
}

func (m *MockJournalService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Journal, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Journal), args.Error(1)
}

func (m *MockJournalService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Journal, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.Journal), args.Get(1).(int64), args.Error(2)
}

func (m *MockJournalService) UpdateByBusiness(ctx context.Context, businessID, id string, input services.CreateJournalInput) (*models.Journal, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Journal), args.Error(1)
}

func (m *MockJournalService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	args := m.Called(ctx, businessID, id)
	return args.Error(0)
}

func (m *MockJournalService) PostByBusiness(ctx context.Context, businessID, id string) (*models.Journal, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Journal), args.Error(1)
}

func (m *MockJournalService) ReverseByBusiness(ctx context.Context, businessID, id string) (*models.Journal, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Journal), args.Error(1)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type JournalHandlerTestable struct {
	svc *MockJournalService
	log *logger.Logger
}

func NewJournalHandlerTestable(svc *MockJournalService, log *logger.Logger) *JournalHandlerTestable {
	return &JournalHandlerTestable{svc: svc, log: log}
}

func (h *JournalHandlerTestable) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateJournalInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	journal, err := h.svc.CreateByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, journal)
}

func (h *JournalHandlerTestable) Get(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	journal, err := h.svc.GetByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "journal not found"})
		return
	}
	c.JSON(http.StatusOK, journal)
}

func (h *JournalHandlerTestable) List(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page := 1
	limit := 20
	journals, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": journals, "total": total, "page": page, "limit": limit})
}

func (h *JournalHandlerTestable) Update(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateJournalInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	journal, err := h.svc.UpdateByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only draft journals can be updated" || err.Error() == "journal is not balanced" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, journal)
}

func (h *JournalHandlerTestable) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteByBusiness(c.Request.Context(), businessID, c.Param("id")); err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only draft journals can be deleted" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *JournalHandlerTestable) Post(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	journal, err := h.svc.PostByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "journal already posted" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, journal)
}

func (h *JournalHandlerTestable) Reverse(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	journal, err := h.svc.ReverseByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only posted journals can be reversed" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, journal)
}

// =============================================================================
// Helper
// =============================================================================

func createJournalTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}

// =============================================================================
// Create Tests
// =============================================================================

func TestJournalCreate_Success(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	journal := &models.Journal{
		ID:         "journal-123",
		BusinessID: "biz-123",
		Name:       "Test Journal",
		Status:     models.JournalStatusDraft,
	}

	input := services.CreateJournalInput{
		Name: "Test Journal",
		Lines: []services.CreateJournalLineInput{
			{AccountCode: "ACC001", AccountName: "Cash", EntryType: "debit", Amount: 100},
			{AccountCode: "ACC002", AccountName: "Revenue", EntryType: "credit", Amount: 100},
		},
	}

	mockSvc.On("CreateByBusiness", mock.Anything, "biz-123", input).Return(journal, nil)

	router := gin.New()
	router.POST("/journals", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"name": "Test Journal",
		"lines": []map[string]interface{}{
			{"account_code": "ACC001", "account_name": "Cash", "entry_type": "debit", "amount": 100},
			{"account_code": "ACC002", "account_name": "Revenue", "entry_type": "credit", "amount": 100},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/journals", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalCreate_InvalidInput(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/journals", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/journals", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestJournalCreate_ServiceError(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	input := services.CreateJournalInput{
		Name: "Test Journal",
		Lines: []services.CreateJournalLineInput{
			{AccountCode: "ACC001", AccountName: "Cash", EntryType: "debit", Amount: 100},
			{AccountCode: "ACC002", AccountName: "Revenue", EntryType: "credit", Amount: 100},
		},
	}

	mockSvc.On("CreateByBusiness", mock.Anything, "biz-123", input).Return(nil, errors.New("database error"))

	router := gin.New()
	router.POST("/journals", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"name": "Test Journal",
		"lines": []map[string]interface{}{
			{"account_code": "ACC001", "account_name": "Cash", "entry_type": "debit", "amount": 100},
			{"account_code": "ACC002", "account_name": "Revenue", "entry_type": "credit", "amount": 100},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/journals", bytes.NewBuffer(body))
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

func TestJournalGet_Success(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	journal := &models.Journal{
		ID:         "journal-123",
		BusinessID: "biz-123",
		Name:       "Test Journal",
		Status:     models.JournalStatusDraft,
	}

	mockSvc.On("GetByBusiness", mock.Anything, "biz-123", "journal-123").Return(journal, nil)

	router := gin.New()
	router.GET("/journals/:id", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/journals/journal-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalGet_NotFound(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("GetByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/journals/:id", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/journals/nonexistent", nil)
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

func TestJournalList_Success(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	journals := []*models.Journal{
		{ID: "journal-1", BusinessID: "biz-123", Name: "Journal 1"},
		{ID: "journal-2", BusinessID: "biz-123", Name: "Journal 2"},
	}

	mockSvc.On("List", mock.Anything, "biz-123", 1, 20).Return(journals, int64(2), nil)

	router := gin.New()
	router.GET("/journals", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/journals", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalList_InternalError(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("List", mock.Anything, "biz-123", 1, 20).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/journals", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/journals", nil)
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

func TestJournalUpdate_Success(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	journal := &models.Journal{
		ID:         "journal-123",
		BusinessID: "biz-123",
		Name:       "Updated Journal",
		Status:     models.JournalStatusDraft,
	}

	input := services.CreateJournalInput{
		Name: "Updated Journal",
		Lines: []services.CreateJournalLineInput{
			{AccountCode: "ACC001", AccountName: "Cash", EntryType: "debit", Amount: 200},
			{AccountCode: "ACC002", AccountName: "Revenue", EntryType: "credit", Amount: 200},
		},
	}

	mockSvc.On("UpdateByBusiness", mock.Anything, "biz-123", "journal-123", input).Return(journal, nil)

	router := gin.New()
	router.PUT("/journals/:id", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Journal",
		"lines": []map[string]interface{}{
			{"account_code": "ACC001", "account_name": "Cash", "entry_type": "debit", "amount": 200},
			{"account_code": "ACC002", "account_name": "Revenue", "entry_type": "credit", "amount": 200},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/journals/journal-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalUpdate_NotFound(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	input := services.CreateJournalInput{
		Name: "Updated Journal",
		Lines: []services.CreateJournalLineInput{
			{AccountCode: "ACC001", AccountName: "Cash", EntryType: "debit", Amount: 200},
			{AccountCode: "ACC002", AccountName: "Revenue", EntryType: "credit", Amount: 200},
		},
	}

	mockSvc.On("UpdateByBusiness", mock.Anything, "biz-123", "nonexistent", input).Return(nil, ErrNotFound)

	router := gin.New()
	router.PUT("/journals/:id", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Journal",
		"lines": []map[string]interface{}{
			{"account_code": "ACC001", "account_name": "Cash", "entry_type": "debit", "amount": 200},
			{"account_code": "ACC002", "account_name": "Revenue", "entry_type": "credit", "amount": 200},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/journals/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalUpdate_OnlyDraftAllowed(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	input := services.CreateJournalInput{
		Name: "Updated Journal",
		Lines: []services.CreateJournalLineInput{
			{AccountCode: "ACC001", AccountName: "Cash", EntryType: "debit", Amount: 200},
			{AccountCode: "ACC002", AccountName: "Revenue", EntryType: "credit", Amount: 200},
		},
	}

	mockSvc.On("UpdateByBusiness", mock.Anything, "biz-123", "journal-123", input).Return(nil, errors.New("only draft journals can be updated"))

	router := gin.New()
	router.PUT("/journals/:id", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Journal",
		"lines": []map[string]interface{}{
			{"account_code": "ACC001", "account_name": "Cash", "entry_type": "debit", "amount": 200},
			{"account_code": "ACC002", "account_name": "Revenue", "entry_type": "credit", "amount": 200},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/journals/journal-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Delete Tests
// =============================================================================

func TestJournalDelete_Success(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("DeleteByBusiness", mock.Anything, "biz-123", "journal-123").Return(nil)

	router := gin.New()
	router.DELETE("/journals/:id", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/journals/journal-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalDelete_NotFound(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("DeleteByBusiness", mock.Anything, "biz-123", "nonexistent").Return(ErrNotFound)

	router := gin.New()
	router.DELETE("/journals/:id", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/journals/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalDelete_OnlyDraftAllowed(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("DeleteByBusiness", mock.Anything, "biz-123", "journal-123").Return(errors.New("only draft journals can be deleted"))

	router := gin.New()
	router.DELETE("/journals/:id", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/journals/journal-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Post Tests
// =============================================================================

func TestJournalPost_Success(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	now := time.Now()
	journal := &models.Journal{
		ID:         "journal-123",
		BusinessID: "biz-123",
		Name:       "Test Journal",
		Status:     models.JournalStatusPosted,
		PostedAt:   &now,
	}

	mockSvc.On("PostByBusiness", mock.Anything, "biz-123", "journal-123").Return(journal, nil)

	router := gin.New()
	router.POST("/journals/:id/post", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Post(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/journals/journal-123/post", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalPost_NotFound(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("PostByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/journals/:id/post", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Post(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/journals/nonexistent/post", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalPost_AlreadyPosted(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("PostByBusiness", mock.Anything, "biz-123", "journal-123").Return(nil, errors.New("journal already posted"))

	router := gin.New()
	router.POST("/journals/:id/post", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Post(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/journals/journal-123/post", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Reverse Tests
// =============================================================================

func TestJournalReverse_Success(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	now := time.Now()
	reversedJournal := &models.Journal{
		ID:           "journal-reversed-456",
		BusinessID:   "biz-123",
		Name:         "Reversed Journal",
		Status:       models.JournalStatusReversed,
		ReversalOfID: stringPtr("journal-123"),
		ReversedAt:   &now,
	}

	mockSvc.On("ReverseByBusiness", mock.Anything, "biz-123", "journal-123").Return(reversedJournal, nil)

	router := gin.New()
	router.POST("/journals/:id/reverse", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Reverse(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/journals/journal-123/reverse", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalReverse_NotFound(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("ReverseByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/journals/:id/reverse", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Reverse(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/journals/nonexistent/reverse", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestJournalReverse_OnlyPostedAllowed(t *testing.T) {
	mockSvc := new(MockJournalService)
	log := logger.New()
	handler := NewJournalHandlerTestable(mockSvc, log)

	mockSvc.On("ReverseByBusiness", mock.Anything, "biz-123", "journal-123").Return(nil, errors.New("only posted journals can be reversed"))

	router := gin.New()
	router.POST("/journals/:id/reverse", func(c *gin.Context) {
		createJournalTestContext(c, "user-123", "biz-123")
		handler.Reverse(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/journals/journal-123/reverse", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func stringPtr(s string) *string {
	return &s
}
