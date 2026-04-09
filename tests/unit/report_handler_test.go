package unit

// go test -v ./tests/unit/... -run "TestReport"
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/reporting"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock ReportService
// =============================================================================

type MockReportService struct {
	mock.Mock
}

func (m *MockReportService) Catalog() []reporting.Definition {
	args := m.Called()
	return args.Get(0).([]reporting.Definition)
}

func (m *MockReportService) Query(ctx context.Context, businessID, userID, reportKey string, input services.ReportQueryInput) (*reporting.Result, error) {
	args := m.Called(ctx, businessID, userID, reportKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*reporting.Result), args.Error(1)
}

func (m *MockReportService) Export(ctx context.Context, businessID, userID, reportKey string, input services.ReportExportInput) (*services.ReportExportResponse, error) {
	args := m.Called(ctx, businessID, userID, reportKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.ReportExportResponse), args.Error(1)
}

func (m *MockReportService) Dashboard(ctx context.Context, businessID string, input services.ReportQueryInput) (map[string]interface{}, error) {
	args := m.Called(ctx, businessID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockReportService) GetPreference(ctx context.Context, businessID, userID, reportKey string) (*services.ReportPreferenceResponse, error) {
	args := m.Called(ctx, businessID, userID, reportKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.ReportPreferenceResponse), args.Error(1)
}

func (m *MockReportService) SavePreference(ctx context.Context, businessID, userID, reportKey string, input services.ReportPreferenceInput) (*services.ReportPreferenceResponse, error) {
	args := m.Called(ctx, businessID, userID, reportKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.ReportPreferenceResponse), args.Error(1)
}

func (m *MockReportService) CreateShare(ctx context.Context, businessID, userID, reportKey string, input services.ReportShareInput) (*services.ReportShareCreateResponse, error) {
	args := m.Called(ctx, businessID, userID, reportKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.ReportShareCreateResponse), args.Error(1)
}

func (m *MockReportService) ListShares(ctx context.Context, businessID string, page, limit int) ([]services.ReportShareHistoryItem, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]services.ReportShareHistoryItem), args.Get(1).(int64), args.Error(2)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type ReportHandlerTestable struct {
	svc *MockReportService
	log *logger.Logger
}

func NewReportHandlerTestable(svc *MockReportService, log *logger.Logger) *ReportHandlerTestable {
	return &ReportHandlerTestable{svc: svc, log: log}
}

func (h *ReportHandlerTestable) Catalog(c *gin.Context) {
	if _, ok := requireBusinessScope(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.svc.Catalog()})
}

func (h *ReportHandlerTestable) Query(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ReportQueryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.Query(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ReportHandlerTestable) Export(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ReportExportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.Export(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "unsupported export format" || err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *ReportHandlerTestable) Dashboard(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page := 1
	limit := 20
	if raw := c.Query("page"); raw != "" {
		if parsed := parseInt(raw); parsed > 0 {
			page = parsed
		}
	}
	if raw := c.Query("limit"); raw != "" {
		if parsed := parseInt(raw); parsed > 0 {
			limit = parsed
		}
	}
	input := services.ReportQueryInput{Page: page, Limit: limit}
	result, err := h.svc.Dashboard(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ReportHandlerTestable) GetPreference(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	result, err := h.svc.GetPreference(c.Request.Context(), businessID, userID, c.Param("key"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ReportHandlerTestable) SavePreference(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ReportPreferenceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.SavePreference(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ReportHandlerTestable) CreateShare(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ReportShareInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.CreateShare(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "unsupported share mode" || err.Error() == "expires_at must be in the future" || err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *ReportHandlerTestable) ShareHistory(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page := 1
	limit := 20
	if raw := c.Query("page"); raw != "" {
		if parsed := parseInt(raw); parsed > 0 {
			page = parsed
		}
	}
	if raw := c.Query("limit"); raw != "" {
		if parsed := parseInt(raw); parsed > 0 {
			limit = parsed
		}
	}
	items, total, err := h.svc.ListShares(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func parseInt(s string) int {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// =============================================================================
// Catalog Tests
// =============================================================================

func TestReportCatalog_Success(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	definitions := []reporting.Definition{
		{Key: "sales", Name: "Sales Report"},
		{Key: "inventory", Name: "Inventory Report"},
	}

	mockSvc.On("Catalog").Return(definitions)

	router := gin.New()
	router.GET("/reports/catalog", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.Catalog(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/reports/catalog", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Query Tests
// =============================================================================

func TestReportQuery_Success(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	result := &reporting.Result{
		Columns: []reporting.Column{{Key: "date", Label: "Date", Type: "string"}},
		Rows:    []map[string]interface{}{{"date": "2024-01-01"}},
		Totals:  map[string]interface{}{},
	}

	input := services.ReportQueryInput{
		Page:    1,
		Limit:   20,
		Columns: []string{"date"},
	}

	mockSvc.On("Query", mock.Anything, "biz-123", "user-123", "sales", input).Return(result, nil)

	router := gin.New()
	router.POST("/reports/:key/query", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.Query(c)
	})

	reqBody := map[string]interface{}{
		"page":    1,
		"limit":   20,
		"columns": []string{"date"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/reports/sales/query", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestReportQuery_NotFound(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	input := services.ReportQueryInput{
		Page:    1,
		Limit:   20,
		Columns: []string{"date"},
	}

	mockSvc.On("Query", mock.Anything, "biz-123", "user-123", "nonexistent", input).Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/reports/:key/query", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.Query(c)
	})

	reqBody := map[string]interface{}{
		"page":    1,
		"limit":   20,
		"columns": []string{"date"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/reports/nonexistent/query", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestReportQuery_InvalidColumns(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	input := services.ReportQueryInput{
		Page:    1,
		Limit:   20,
		Columns: []string{"invalid"},
	}

	mockSvc.On("Query", mock.Anything, "biz-123", "user-123", "sales", input).Return(nil, errors.New("at least one valid column is required"))

	router := gin.New()
	router.POST("/reports/:key/query", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.Query(c)
	})

	reqBody := map[string]interface{}{
		"page":    1,
		"limit":   20,
		"columns": []string{"invalid"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/reports/sales/query", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Export Tests
// =============================================================================

func TestReportExport_Success(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	response := &services.ReportExportResponse{
		Filename:    "report.csv",
		ContentType: "text/csv",
		Data:        "date,amount\n2024-01-01,100",
	}

	input := services.ReportExportInput{
		Page:    1,
		Limit:   20,
		Columns: []string{"date", "amount"},
		Format:  "csv",
	}

	mockSvc.On("Export", mock.Anything, "biz-123", "user-123", "sales", input).Return(response, nil)

	router := gin.New()
	router.POST("/reports/:key/export", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.Export(c)
	})

	reqBody := map[string]interface{}{
		"page":    1,
		"limit":   20,
		"columns": []string{"date", "amount"},
		"format":  "csv",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/reports/sales/export", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestReportExport_UnsupportedFormat(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	input := services.ReportExportInput{
		Format: "xml",
	}

	mockSvc.On("Export", mock.Anything, "biz-123", "user-123", "sales", input).Return(nil, errors.New("unsupported export format"))

	router := gin.New()
	router.POST("/reports/:key/export", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.Export(c)
	})

	reqBody := map[string]interface{}{
		"format": "xml",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/reports/sales/export", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Dashboard Tests
// =============================================================================

func TestReportDashboard_Success(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	data := map[string]interface{}{
		"total_sales":  10000.0,
		"total_orders": float64(50),
	}

	input := services.ReportQueryInput{Page: 1, Limit: 20}

	mockSvc.On("Dashboard", mock.Anything, "biz-123", input).Return(data, nil)

	router := gin.New()
	router.GET("/reports/dashboard", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.Dashboard(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/reports/dashboard", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestReportDashboard_ServiceError(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	input := services.ReportQueryInput{Page: 1, Limit: 20}

	mockSvc.On("Dashboard", mock.Anything, "biz-123", input).Return(nil, errors.New("service error"))

	router := gin.New()
	router.GET("/reports/dashboard", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.Dashboard(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/reports/dashboard", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetPreference Tests
// =============================================================================

func TestReportGetPreference_Success(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	response := &services.ReportPreferenceResponse{
		ReportKey: "sales",
		Columns:   []string{"date", "amount"},
	}

	mockSvc.On("GetPreference", mock.Anything, "biz-123", "user-123", "sales").Return(response, nil)

	router := gin.New()
	router.GET("/reports/preferences/:key", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.GetPreference(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/reports/preferences/sales", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestReportGetPreference_NotFound(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	mockSvc.On("GetPreference", mock.Anything, "biz-123", "user-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/reports/preferences/:key", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.GetPreference(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/reports/preferences/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// SavePreference Tests
// =============================================================================

func TestReportSavePreference_Success(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	response := &services.ReportPreferenceResponse{
		ReportKey: "sales",
		Columns:   []string{"date", "amount", "customer"},
	}

	input := services.ReportPreferenceInput{
		Columns: []string{"date", "amount", "customer"},
	}

	mockSvc.On("SavePreference", mock.Anything, "biz-123", "user-123", "sales", input).Return(response, nil)

	router := gin.New()
	router.PUT("/reports/preferences/:key", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.SavePreference(c)
	})

	reqBody := map[string]interface{}{
		"columns": []string{"date", "amount", "customer"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/reports/preferences/sales", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestReportSavePreference_InvalidColumns(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	router := gin.New()
	router.PUT("/reports/preferences/:key", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.SavePreference(c)
	})

	// Empty columns - binding validation should fail
	reqBody := map[string]interface{}{
		"columns": []string{},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/reports/preferences/sales", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// CreateShare Tests
// =============================================================================

func TestReportCreateShare_Success(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	now := time.Now()
	response := &services.ReportShareCreateResponse{
		Share: services.ReportShareHistoryItem{
			ID:        "share-123",
			ReportKey: "sales",
			Title:     "Monthly Sales",
			ShareMode: "view",
			Status:    "active",
			ExpiresAt: &now,
			CreatedAt: now,
		},
		Token:       "token-abc123",
		MetadataURL: "/public/report-shares/token-abc123/metadata",
		AccessURL:   "/public/report-shares/token-abc123/access",
	}

	input := services.ReportShareInput{
		Title:   "Monthly Sales",
		Mode:    "view",
		Columns: []string{"date", "amount"},
	}

	mockSvc.On("CreateShare", mock.Anything, "biz-123", "user-123", "sales", input).Return(response, nil)

	router := gin.New()
	router.POST("/reports/:key/share", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.CreateShare(c)
	})

	reqBody := map[string]interface{}{
		"title":   "Monthly Sales",
		"mode":    "view",
		"columns": []string{"date", "amount"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/reports/sales/share", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestReportCreateShare_UnsupportedMode(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	input := services.ReportShareInput{
		Title: "Monthly Sales",
		Mode:  "invalid",
	}

	mockSvc.On("CreateShare", mock.Anything, "biz-123", "user-123", "sales", input).Return(nil, errors.New("unsupported share mode"))

	router := gin.New()
	router.POST("/reports/:key/share", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.CreateShare(c)
	})

	reqBody := map[string]interface{}{
		"title": "Monthly Sales",
		"mode":  "invalid",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/reports/sales/share", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ShareHistory Tests
// =============================================================================

func TestReportShareHistory_Success(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	now := time.Now()
	items := []services.ReportShareHistoryItem{
		{ID: "share-1", ReportKey: "sales", Title: "Sales Report", Status: "active", CreatedAt: now},
		{ID: "share-2", ReportKey: "inventory", Title: "Inventory Report", Status: "revoked", CreatedAt: now},
	}

	mockSvc.On("ListShares", mock.Anything, "biz-123", 1, 20).Return(items, int64(2), nil)

	router := gin.New()
	router.GET("/reports/shares/history", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.ShareHistory(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/reports/shares/history", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestReportShareHistory_Empty(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	mockSvc.On("ListShares", mock.Anything, "biz-123", 1, 20).Return([]services.ReportShareHistoryItem{}, int64(0), nil)

	router := gin.New()
	router.GET("/reports/shares/history", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.ShareHistory(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/reports/shares/history", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestReportShareHistory_ServiceError(t *testing.T) {
	mockSvc := new(MockReportService)
	log := logger.New()
	handler := NewReportHandlerTestable(mockSvc, log)

	mockSvc.On("ListShares", mock.Anything, "biz-123", 1, 20).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/reports/shares/history", func(c *gin.Context) {
		createReportTestContext(c, "user-123", "biz-123")
		handler.ShareHistory(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/reports/shares/history", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func createReportTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}
