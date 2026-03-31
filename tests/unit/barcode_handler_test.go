package unit

//go test -v ./tests/unit/... -run "TestGenerate|TestAssign|TestLookup|TestRenderPNG|TestRenderSVG|TestRenderPDF"
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
// Mock BarcodeService
// =============================================================================

type MockBarcodeService struct {
	mock.Mock
}

func (m *MockBarcodeService) GenerateValue(prefix, seed string) string {
	args := m.Called(prefix, seed)
	return args.String(0)
}

func (m *MockBarcodeService) EnsureBarcode(ctx context.Context, businessID, productID, variantID string) (string, error) {
	args := m.Called(ctx, businessID, productID, variantID)
	return args.String(0), args.Error(1)
}

func (m *MockBarcodeService) Lookup(ctx context.Context, businessID, code string) (map[string]interface{}, error) {
	args := m.Called(ctx, businessID, code)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockBarcodeService) RenderPNG(input services.BarcodeRenderInput) ([]byte, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockBarcodeService) RenderSVG(input services.BarcodeRenderInput) ([]byte, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockBarcodeService) RenderPDF(input services.BarcodeRenderInput) ([]byte, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type BarcodeHandlerTestable struct {
	svc *MockBarcodeService
	log *logger.Logger
}

func NewBarcodeHandlerTestable(svc *MockBarcodeService, log *logger.Logger) *BarcodeHandlerTestable {
	return &BarcodeHandlerTestable{
		svc: svc,
		log: log,
	}
}

type GenerateRequest struct {
	Prefix string `json:"prefix"`
	Seed   string `json:"seed"`
}

type AssignRequest struct {
	ProductID string `json:"product_id" binding:"required,uuid"`
	VariantID string `json:"variant_id,omitempty"`
}

func (h *BarcodeHandlerTestable) Generate(c *gin.Context) {
	var body GenerateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"value": h.svc.GenerateValue(body.Prefix, body.Seed)})
}

func (h *BarcodeHandlerTestable) Assign(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var body AssignRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	value, err := h.svc.EnsureBarcode(c.Request.Context(), businessID, body.ProductID, body.VariantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"value": value})
}

func (h *BarcodeHandlerTestable) Lookup(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	value := c.Query("value")
	result, err := h.svc.Lookup(c.Request.Context(), businessID, value)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *BarcodeHandlerTestable) RenderPNG(c *gin.Context) {
	var input services.BarcodeRenderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := h.svc.RenderPNG(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "image/png", data)
}

func (h *BarcodeHandlerTestable) RenderSVG(c *gin.Context) {
	var input services.BarcodeRenderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := h.svc.RenderSVG(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "image/svg+xml", data)
}

func (h *BarcodeHandlerTestable) RenderPDF(c *gin.Context) {
	var input services.BarcodeRenderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := h.svc.RenderPDF(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/pdf", data)
}

var ErrNotFound = errors.New("record not found")

func isNotFoundErr(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, context.DeadlineExceeded)
}

// =============================================================================
// Generate Tests
// =============================================================================

func TestGenerate_Success(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	mockSvc.On("GenerateValue", "SKU", "123456").Return("SKU-123456")

	router := gin.New()
	router.POST("/barcodes/generate", func(c *gin.Context) {
		handler.Generate(c)
	})

	reqBody := map[string]interface{}{
		"prefix": "SKU",
		"seed":   "123456",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/generate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGenerate_EmptySeed(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	mockSvc.On("GenerateValue", "", "").Return("SKU")

	router := gin.New()
	router.POST("/barcodes/generate", func(c *gin.Context) {
		handler.Generate(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/generate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Assign Tests
// =============================================================================

func TestAssign_Success(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	mockSvc.On("EnsureBarcode", mock.Anything, "biz-123", prodID, "").Return("PRD-123456", nil)

	router := gin.New()
	router.POST("/barcodes/assign", func(c *gin.Context) {
		createBarcodeTestContext(c, "user-123", "biz-123")
		handler.Assign(c)
	})

	reqBody := map[string]interface{}{
		"product_id": prodID,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/assign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestAssign_WithVariant_Success(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	varID := "123e4567-e89b-12d3-a456-426614174001"
	mockSvc.On("EnsureBarcode", mock.Anything, "biz-123", prodID, varID).Return("VAR-123456", nil)

	router := gin.New()
	router.POST("/barcodes/assign", func(c *gin.Context) {
		createBarcodeTestContext(c, "user-123", "biz-123")
		handler.Assign(c)
	})

	reqBody := map[string]interface{}{
		"product_id": prodID,
		"variant_id": varID,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/assign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestAssign_MissingProductID(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/barcodes/assign", func(c *gin.Context) {
		createBarcodeTestContext(c, "user-123", "biz-123")
		handler.Assign(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/assign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestAssign_Unauthorized(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/barcodes/assign", func(c *gin.Context) {
		handler.Assign(c)
	})

	reqBody := map[string]interface{}{
		"product_id": "123e4567-e89b-12d3-a456-426614174000",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/assign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

func TestAssign_Error(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	mockSvc.On("EnsureBarcode", mock.Anything, "biz-123", prodID, "").Return("", errors.New("database error"))

	router := gin.New()
	router.POST("/barcodes/assign", func(c *gin.Context) {
		createBarcodeTestContext(c, "user-123", "biz-123")
		handler.Assign(c)
	})

	reqBody := map[string]interface{}{
		"product_id": prodID,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/assign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Lookup Tests
// =============================================================================

func TestLookup_Success(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	result := map[string]interface{}{
		"entity_type": "product",
		"product":     models.Product{ID: "prod-123", Name: "Test Product"},
	}
	mockSvc.On("Lookup", mock.Anything, "biz-123", "123456").Return(result, nil)

	router := gin.New()
	router.GET("/barcodes/lookup", func(c *gin.Context) {
		createBarcodeTestContext(c, "user-123", "biz-123")
		handler.Lookup(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/barcodes/lookup?value=123456", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestLookup_NotFound(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	mockSvc.On("Lookup", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/barcodes/lookup", func(c *gin.Context) {
		createBarcodeTestContext(c, "user-123", "biz-123")
		handler.Lookup(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/barcodes/lookup?value=nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestLookup_Unauthorized(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/barcodes/lookup", func(c *gin.Context) {
		handler.Lookup(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/barcodes/lookup?value=123456", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

func TestLookup_Error(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	mockSvc.On("Lookup", mock.Anything, "biz-123", "123456").Return(nil, errors.New("database error"))

	router := gin.New()
	router.GET("/barcodes/lookup", func(c *gin.Context) {
		createBarcodeTestContext(c, "user-123", "biz-123")
		handler.Lookup(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/barcodes/lookup?value=123456", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// RenderPNG Tests
// =============================================================================

func TestRenderPNG_Success(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	mockSvc.On("RenderPNG", mock.Anything).Return(pngData, nil)

	router := gin.New()
	router.POST("/barcodes/render/png", func(c *gin.Context) {
		handler.RenderPNG(c)
	})

	reqBody := map[string]interface{}{
		"value": "123456",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/render/png", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("expected image/png, got %s", res.Header().Get("Content-Type"))
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderPNG_MissingValue(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/barcodes/render/png", func(c *gin.Context) {
		handler.RenderPNG(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/render/png", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestRenderPNG_Error(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	mockSvc.On("RenderPNG", mock.Anything).Return(nil, errors.New("invalid barcode value"))

	router := gin.New()
	router.POST("/barcodes/render/png", func(c *gin.Context) {
		handler.RenderPNG(c)
	})

	reqBody := map[string]interface{}{
		"value": "123456",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/render/png", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// RenderSVG Tests
// =============================================================================

func TestRenderSVG_Success(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	mockSvc.On("RenderSVG", mock.Anything).Return(svgData, nil)

	router := gin.New()
	router.POST("/barcodes/render/svg", func(c *gin.Context) {
		handler.RenderSVG(c)
	})

	reqBody := map[string]interface{}{
		"value": "123456",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/render/svg", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("expected image/svg+xml, got %s", res.Header().Get("Content-Type"))
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderSVG_MissingValue(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/barcodes/render/svg", func(c *gin.Context) {
		handler.RenderSVG(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/render/svg", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// RenderPDF Tests
// =============================================================================

func TestRenderPDF_Success(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	pdfData := []byte(`%PDF-1.4`)
	mockSvc.On("RenderPDF", mock.Anything).Return(pdfData, nil)

	router := gin.New()
	router.POST("/barcodes/render/pdf", func(c *gin.Context) {
		handler.RenderPDF(c)
	})

	reqBody := map[string]interface{}{
		"value": "123456",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/render/pdf", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("expected application/pdf, got %s", res.Header().Get("Content-Type"))
	}

	mockSvc.AssertExpectations(t)
}

func TestRenderPDF_MissingValue(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/barcodes/render/pdf", func(c *gin.Context) {
		handler.RenderPDF(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/render/pdf", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestRenderPDF_Error(t *testing.T) {
	mockSvc := new(MockBarcodeService)
	log := logger.New()
	handler := NewBarcodeHandlerTestable(mockSvc, log)

	mockSvc.On("RenderPDF", mock.Anything).Return(nil, errors.New("failed to render PDF"))

	router := gin.New()
	router.POST("/barcodes/render/pdf", func(c *gin.Context) {
		handler.RenderPDF(c)
	})

	reqBody := map[string]interface{}{
		"value": "123456",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/barcodes/render/pdf", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
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

func createBarcodeTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
}
