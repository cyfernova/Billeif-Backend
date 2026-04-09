package unit

// go test -v ./tests/unit/... -run "TestTax"
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
// Mock TaxComplianceService
// =============================================================================

type MockTaxComplianceServiceTax struct {
	mock.Mock
}

func (m *MockTaxComplianceServiceTax) FetchGSTIN(ctx context.Context, gstin string) (*services.GSTINLookupResult, error) {
	args := m.Called(ctx, gstin)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.GSTINLookupResult), args.Error(1)
}

func (m *MockTaxComplianceServiceTax) ImportGSTR2B(ctx context.Context, businessID string, input services.ImportGSTR2BInput) (*models.GSTR2BImport, []models.GSTR2BMatchResult, error) {
	args := m.Called(ctx, businessID, input)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(*models.GSTR2BImport), args.Get(1).([]models.GSTR2BMatchResult), args.Error(2)
}

func (m *MockTaxComplianceServiceTax) GetReport(ctx context.Context, businessID, reportType string, opts services.GSTReportOptions) (map[string]interface{}, error) {
	args := m.Called(ctx, businessID, reportType, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockTaxComplianceServiceTax) ExportReport(ctx context.Context, businessID, reportType string, opts services.GSTReportOptions) (*models.GSTReportRun, error) {
	args := m.Called(ctx, businessID, reportType, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTReportRun), args.Error(1)
}

func (m *MockTaxComplianceServiceTax) GetReportRun(ctx context.Context, businessID, id string) (*models.GSTReportRun, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTReportRun), args.Error(1)
}

func (m *MockTaxComplianceServiceTax) ListIntegrationAccounts(ctx context.Context, businessID string) ([]*models.GSTIntegrationAccount, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.GSTIntegrationAccount), args.Error(1)
}

func (m *MockTaxComplianceServiceTax) UpsertIntegrationAccount(ctx context.Context, businessID, id string, input services.UpsertGSTIntegrationAccountInput) (*models.GSTIntegrationAccount, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTIntegrationAccount), args.Error(1)
}

func (m *MockTaxComplianceServiceTax) ValidateIntegrationAccount(ctx context.Context, businessID, id string) (*models.GSTIntegrationAccount, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTIntegrationAccount), args.Error(1)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type TaxHandlerTestable struct {
	svc *MockTaxComplianceServiceTax
	log *logger.Logger
}

func NewTaxHandlerTestable(svc *MockTaxComplianceServiceTax, log *logger.Logger) *TaxHandlerTestable {
	return &TaxHandlerTestable{svc: svc, log: log}
}

func (h *TaxHandlerTestable) FetchGSTIN(c *gin.Context) {
	if _, ok := requireBusinessScope(c); !ok {
		return
	}
	result, err := h.svc.FetchGSTIN(c.Request.Context(), c.Param("gstin"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TaxHandlerTestable) ImportGSTR2B(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.ImportGSTR2BInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	gstrImport, results, err := h.svc.ImportGSTR2B(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"import": gstrImport, "results": results})
}

func (h *TaxHandlerTestable) GetReport(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	start, err := time.Parse("2006-01-02", c.Query("period_start"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	end, err := time.Parse("2006-01-02", c.Query("period_end"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	opts := services.GSTReportOptions{
		PeriodStart:     start,
		PeriodEnd:       end,
		FilingFrequency: c.Query("filing_frequency"),
		ExportFormat:    c.Query("export_format"),
	}
	report, err := h.svc.GetReport(c.Request.Context(), businessID, c.Param("type"), opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

func (h *TaxHandlerTestable) ExportReport(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.GSTReportOptions
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if userID, exists := c.Get("user_id"); exists {
		if typed, ok := userID.(string); ok {
			input.CreatedBy = typed
		}
	}
	run, err := h.svc.ExportReport(c.Request.Context(), businessID, c.Param("type"), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, run)
}

func (h *TaxHandlerTestable) GetReportRun(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	run, err := h.svc.GetReportRun(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "report run not found"})
		return
	}
	c.JSON(http.StatusOK, run)
}

func (h *TaxHandlerTestable) ListIntegrationAccounts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	accounts, err := h.svc.ListIntegrationAccounts(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": accounts})
}

func (h *TaxHandlerTestable) UpsertIntegrationAccount(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertGSTIntegrationAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	account, err := h.svc.UpsertIntegrationAccount(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, account)
}

func (h *TaxHandlerTestable) ValidateIntegrationAccount(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	account, err := h.svc.ValidateIntegrationAccount(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "account": account})
		return
	}
	c.JSON(http.StatusOK, account)
}

// =============================================================================
// FetchGSTIN Tests
// =============================================================================

func TestTaxFetchGSTIN_Success(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	result := &services.GSTINLookupResult{
		GSTIN:     "27AABCU9603R1ZM",
		PAN:       "AABCU9603R",
		LegalName: "Test Company Pvt Ltd",
		StateCode: "27",
		Source:    "govregistry",
		IsValid:   true,
	}

	mockSvc.On("FetchGSTIN", mock.Anything, "27AABCU9603R1ZM").Return(result, nil)

	router := gin.New()
	router.POST("/utils/gstin/:gstin/fetch", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.FetchGSTIN(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/utils/gstin/27AABCU9603R1ZM/fetch", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTaxFetchGSTIN_ServiceError(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	mockSvc.On("FetchGSTIN", mock.Anything, "invalid").Return(nil, errors.New("GSTIN lookup failed"))

	router := gin.New()
	router.POST("/utils/gstin/:gstin/fetch", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.FetchGSTIN(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/utils/gstin/invalid/fetch", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ImportGSTR2B Tests
// =============================================================================

func TestTaxImportGSTR2B_Success(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	now := time.Now()
	gstrImport := &models.GSTR2BImport{
		ID:          "import-123",
		BusinessID:  "biz-123",
		PeriodStart: now,
		PeriodEnd:   now,
		Source:      "api",
		Status:      "processed",
	}
	results := []models.GSTR2BMatchResult{
		{ID: "result-1", Status: "matched"},
	}

	mockSvc.On("ImportGSTR2B", mock.Anything, "biz-123", mock.Anything).Return(gstrImport, results, nil)

	router := gin.New()
	router.POST("/tax/gstr-2b/import", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.ImportGSTR2B(c)
	})

	reqBody := map[string]interface{}{
		"period_start": now.Format(time.RFC3339),
		"period_end":   now.Format(time.RFC3339),
		"source":       "api",
		"lines": []map[string]interface{}{
			{"supplier_gstin": "27AABCU9603R1ZM", "supplier_name": "Supplier 1"},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/tax/gstr-2b/import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTaxImportGSTR2B_InvalidInput(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/tax/gstr-2b/import", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.ImportGSTR2B(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tax/gstr-2b/import", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// GetReport Tests
// =============================================================================

func TestTaxGetReport_Success(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	report := map[string]interface{}{
		"total_taxable_value": 100000.0,
		"total_igst":          18000.0,
		"total_cgst":          9000.0,
		"total_sgst":          9000.0,
	}

	start, _ := time.Parse("2006-01-02", "2024-01-01")
	end, _ := time.Parse("2006-01-02", "2024-01-31")
	opts := services.GSTReportOptions{
		PeriodStart: start,
		PeriodEnd:   end,
	}

	mockSvc.On("GetReport", mock.Anything, "biz-123", "gstr1", opts).Return(report, nil)

	router := gin.New()
	router.GET("/tax/reports/:type", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.GetReport(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tax/reports/gstr1?period_start=2024-01-01&period_end=2024-01-31", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTaxGetReport_ServiceError(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	start, _ := time.Parse("2006-01-02", "2024-01-01")
	end, _ := time.Parse("2006-01-02", "2024-01-31")
	opts := services.GSTReportOptions{
		PeriodStart: start,
		PeriodEnd:   end,
	}

	mockSvc.On("GetReport", mock.Anything, "biz-123", "gstr1", opts).Return(nil, errors.New("report generation failed"))

	router := gin.New()
	router.GET("/tax/reports/:type", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.GetReport(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tax/reports/gstr1?period_start=2024-01-01&period_end=2024-01-31", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ExportReport Tests
// =============================================================================

func TestTaxExportReport_Success(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	now := time.Now()
	run := &models.GSTReportRun{
		ID:              "run-123",
		BusinessID:      "biz-123",
		ReportType:      "gstr1",
		PeriodStart:     now,
		PeriodEnd:       now,
		FilingFrequency: "monthly",
		ExportFormat:    "json",
		Status:          "completed",
	}

	mockSvc.On("ExportReport", mock.Anything, "biz-123", "gstr1", mock.Anything).Return(run, nil)

	router := gin.New()
	router.POST("/tax/reports/:type/export", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.ExportReport(c)
	})

	reqBody := map[string]interface{}{
		"period_start":  now.Format(time.RFC3339),
		"period_end":    now.Format(time.RFC3339),
		"export_format": "json",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/tax/reports/gstr1/export", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTaxExportReport_InvalidInput(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/tax/reports/:type/export", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.ExportReport(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tax/reports/gstr1/export", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// GetReportRun Tests
// =============================================================================

func TestTaxGetReportRun_Success(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	now := time.Now()
	run := &models.GSTReportRun{
		ID:              "run-123",
		BusinessID:      "biz-123",
		ReportType:      "gstr1",
		PeriodStart:     now,
		PeriodEnd:       now,
		FilingFrequency: "monthly",
		Status:          "completed",
	}

	mockSvc.On("GetReportRun", mock.Anything, "biz-123", "run-123").Return(run, nil)

	router := gin.New()
	router.GET("/tax/report-runs/:id", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.GetReportRun(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tax/report-runs/run-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTaxGetReportRun_NotFound(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	mockSvc.On("GetReportRun", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/tax/report-runs/:id", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.GetReportRun(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tax/report-runs/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListIntegrationAccounts Tests
// =============================================================================

func TestTaxListIntegrationAccounts_Success(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	accounts := []*models.GSTIntegrationAccount{
		{ID: "acc-1", BusinessID: "biz-123", Provider: "nic", ServiceType: "gst", Status: "active"},
		{ID: "acc-2", BusinessID: "biz-123", Provider: "gsp1", ServiceType: "einvoice", Status: "pending"},
	}

	mockSvc.On("ListIntegrationAccounts", mock.Anything, "biz-123").Return(accounts, nil)

	router := gin.New()
	router.GET("/tax/integrations", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.ListIntegrationAccounts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tax/integrations", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTaxListIntegrationAccounts_ServiceError(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	mockSvc.On("ListIntegrationAccounts", mock.Anything, "biz-123").Return(nil, errors.New("database error"))

	router := gin.New()
	router.GET("/tax/integrations", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.ListIntegrationAccounts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/tax/integrations", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpsertIntegrationAccount Tests
// =============================================================================

func TestTaxUpsertIntegrationAccount_Success(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	account := &models.GSTIntegrationAccount{
		ID:          "acc-new",
		BusinessID:  "biz-123",
		Provider:    "nic",
		ServiceType: "gst",
		Status:      "pending",
	}

	input := services.UpsertGSTIntegrationAccountInput{
		Provider:    "nic",
		ServiceType: "gst",
		Credentials: services.GSTIntegrationAccountCredentials{},
	}

	mockSvc.On("UpsertIntegrationAccount", mock.Anything, "biz-123", "acc-new", input).Return(account, nil)

	router := gin.New()
	router.POST("/tax/integrations/:id", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.UpsertIntegrationAccount(c)
	})

	reqBody := map[string]interface{}{
		"provider":     "nic",
		"service_type": "gst",
		"credentials":  map[string]interface{}{},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/tax/integrations/acc-new", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTaxUpsertIntegrationAccount_InvalidInput(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/tax/integrations/:id", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.UpsertIntegrationAccount(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tax/integrations/acc-1", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// ValidateIntegrationAccount Tests
// =============================================================================

func TestTaxValidateIntegrationAccount_Success(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	account := &models.GSTIntegrationAccount{
		ID:          "acc-1",
		BusinessID:  "biz-123",
		Provider:    "nic",
		ServiceType: "gst",
		Status:      "active",
	}

	mockSvc.On("ValidateIntegrationAccount", mock.Anything, "biz-123", "acc-1").Return(account, nil)

	router := gin.New()
	router.POST("/tax/integrations/:id/validate", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.ValidateIntegrationAccount(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tax/integrations/acc-1/validate", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestTaxValidateIntegrationAccount_ServiceError(t *testing.T) {
	mockSvc := new(MockTaxComplianceServiceTax)
	log := logger.New()
	handler := NewTaxHandlerTestable(mockSvc, log)

	mockSvc.On("ValidateIntegrationAccount", mock.Anything, "biz-123", "acc-1").Return(nil, errors.New("validation failed"))

	router := gin.New()
	router.POST("/tax/integrations/:id/validate", func(c *gin.Context) {
		createTaxTestContext(c, "user-123", "biz-123")
		handler.ValidateIntegrationAccount(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tax/integrations/acc-1/validate", nil)
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

func createTaxTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}
