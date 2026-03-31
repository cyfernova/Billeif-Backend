package unit

// go test -v ./tests/unit/... -run "TestDocument"
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
// Mock DocumentService
// =============================================================================

type MockDocumentService struct {
	mock.Mock
}

func (m *MockDocumentService) ListByType(ctx context.Context, businessID, documentType string, page, limit int) ([]*models.Document, int64, error) {
	args := m.Called(ctx, businessID, documentType, page, limit)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]*models.Document), args.Get(1).(int64), args.Error(2)
}

func (m *MockDocumentService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Document, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

func (m *MockDocumentService) CreateByType(ctx context.Context, businessID, documentType string, input services.CreateDocumentInput) (*models.Document, error) {
	args := m.Called(ctx, businessID, documentType, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

func (m *MockDocumentService) UpdateByType(ctx context.Context, businessID, id, documentType string, input services.CreateDocumentInput) (*models.Document, error) {
	args := m.Called(ctx, businessID, id, documentType, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

func (m *MockDocumentService) DeleteByType(ctx context.Context, businessID, id, documentType string) error {
	args := m.Called(ctx, businessID, id, documentType)
	return args.Error(0)
}

func (m *MockDocumentService) CancelByType(ctx context.Context, businessID, id, documentType, reason string) (*models.Document, error) {
	args := m.Called(ctx, businessID, id, documentType, reason)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

func (m *MockDocumentService) GetHistory(ctx context.Context, businessID, id string) (*services.DocumentHistory, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.DocumentHistory), args.Error(1)
}

func (m *MockDocumentService) DuplicateByBusiness(ctx context.Context, businessID, id string) (*models.Document, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

func (m *MockDocumentService) MergeByBusiness(ctx context.Context, businessID string, input services.MergeDocumentsInput) (*models.Document, error) {
	args := m.Called(ctx, businessID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

func (m *MockDocumentService) ConvertByBusiness(ctx context.Context, businessID, id string, input services.ConvertDocumentInput) (*models.Document, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

func (m *MockDocumentService) RequestRenderByBusiness(ctx context.Context, businessID, documentID string, input services.RenderDocumentInput) (*models.DocumentRenderJob, error) {
	args := m.Called(ctx, businessID, documentID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.DocumentRenderJob), args.Error(1)
}

func (m *MockDocumentService) GetPDFURLByBusiness(ctx context.Context, businessID, documentID string) (string, error) {
	args := m.Called(ctx, businessID, documentID)
	return args.String(0), args.Error(1)
}

// =============================================================================
// Mock TaxComplianceService
// =============================================================================

type MockTaxComplianceService struct {
	mock.Mock
}

func (m *MockTaxComplianceService) GetComplianceStatus(ctx context.Context, businessID, documentID string) (map[string]interface{}, error) {
	args := m.Called(ctx, businessID, documentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockTaxComplianceService) GenerateEInvoiceByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input services.GenerateEInvoiceInput) (*models.GSTSubmissionJob, error) {
	args := m.Called(ctx, businessID, documentID, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTSubmissionJob), args.Error(1)
}

func (m *MockTaxComplianceService) GetEInvoiceByDocument(ctx context.Context, businessID, documentID string) (map[string]interface{}, error) {
	args := m.Called(ctx, businessID, documentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockTaxComplianceService) CancelEInvoiceByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input services.CancelEInvoiceInput) (*models.GSTSubmissionJob, error) {
	args := m.Called(ctx, businessID, documentID, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTSubmissionJob), args.Error(1)
}

func (m *MockTaxComplianceService) GenerateEWayBillByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input services.GenerateEWayBillInput) (*models.GSTSubmissionJob, error) {
	args := m.Called(ctx, businessID, documentID, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTSubmissionJob), args.Error(1)
}

func (m *MockTaxComplianceService) GetEWayBillByDocument(ctx context.Context, businessID, documentID string) (map[string]interface{}, error) {
	args := m.Called(ctx, businessID, documentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockTaxComplianceService) GetEWayBillPDFByDocument(ctx context.Context, businessID, documentID string) (string, error) {
	args := m.Called(ctx, businessID, documentID)
	return args.String(0), args.Error(1)
}

func (m *MockTaxComplianceService) UpdateEWayPartBByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input services.UpdateEWayPartBInput) (*models.GSTSubmissionJob, error) {
	args := m.Called(ctx, businessID, documentID, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTSubmissionJob), args.Error(1)
}

func (m *MockTaxComplianceService) InitiateMultiVehicleByDocument(ctx context.Context, businessID, documentID, idempotencyKey string, input services.MultiVehicleInput) (*models.GSTSubmissionJob, error) {
	args := m.Called(ctx, businessID, documentID, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.GSTSubmissionJob), args.Error(1)
}

// =============================================================================
// Testable wrappers
// =============================================================================

type DocumentHandlerTestable struct {
	svc          *MockDocumentService
	documentType string
	log          *logger.Logger
}

func NewDocumentHandlerTestable(svc *MockDocumentService, documentType string, log *logger.Logger) *DocumentHandlerTestable {
	return &DocumentHandlerTestable{
		svc:          svc,
		documentType: documentType,
		log:          log,
	}
}

type DocumentUtilityHandlerTestable struct {
	svc *MockDocumentService
	tax *MockTaxComplianceService
	log *logger.Logger
}

func NewDocumentUtilityHandlerTestable(svc *MockDocumentService, tax *MockTaxComplianceService, log *logger.Logger) *DocumentUtilityHandlerTestable {
	return &DocumentUtilityHandlerTestable{
		svc: svc,
		tax: tax,
		log: log,
	}
}

// DocumentHandler methods
func (h *DocumentHandlerTestable) List(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page := 1
	limit := 20
	documents, total, err := h.svc.ListByType(c.Request.Context(), businessID, h.documentType, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": documents, "total": total, "page": page, "limit": limit})
}

func (h *DocumentHandlerTestable) Get(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	document, err := h.svc.GetByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	if document.DocumentType != h.documentType {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	c.JSON(http.StatusOK, document)
}

func (h *DocumentHandlerTestable) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	document, err := h.svc.CreateByType(c.Request.Context(), businessID, h.documentType, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *DocumentHandlerTestable) Update(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	document, err := h.svc.UpdateByType(c.Request.Context(), businessID, c.Param("id"), h.documentType, input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only draft documents can be updated" || err.Error() == "document type mismatch" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, document)
}

func (h *DocumentHandlerTestable) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteByType(c.Request.Context(), businessID, c.Param("id"), h.documentType); err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only draft documents can be deleted" || err.Error() == "document type mismatch" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *DocumentHandlerTestable) Cancel(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	document, err := h.svc.CancelByType(c.Request.Context(), businessID, c.Param("id"), h.documentType, body.Reason)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, document)
}

func (h *DocumentHandlerTestable) GetPDF(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	url, err := h.svc.GetPDFURLByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusNotFound
		if !errors.Is(err, ErrNotFound) && err.Error() != "PDF not yet generated" {
			statusCode = http.StatusInternalServerError
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pdf_url": url})
}

// DocumentUtilityHandler methods
func (h *DocumentUtilityHandlerTestable) Convert(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.ConvertDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	document, err := h.svc.ConvertByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "no convertible line items found" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *DocumentUtilityHandlerTestable) Merge(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.MergeDocumentsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	document, err := h.svc.MergeByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "documents are not merge-compatible" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *DocumentUtilityHandlerTestable) Duplicate(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	document, err := h.svc.DuplicateByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *DocumentUtilityHandlerTestable) History(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	history, err := h.svc.GetHistory(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, history)
}

func (h *DocumentUtilityHandlerTestable) Render(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.RenderDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.svc.RequestRenderByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandlerTestable) GetComplianceStatus(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	status, err := h.tax.GetComplianceStatus(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

func (h *DocumentUtilityHandlerTestable) GenerateEInvoice(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	var input services.GenerateEInvoiceInput
	_ = c.ShouldBindJSON(&input)
	job, err := h.tax.GenerateEInvoiceByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandlerTestable) GetEInvoice(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	record, err := h.tax.GetEInvoiceByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "e-invoice not found"})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *DocumentUtilityHandlerTestable) CancelEInvoice(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	var input services.CancelEInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.tax.CancelEInvoiceByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandlerTestable) GenerateEWayBill(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	var input services.GenerateEWayBillInput
	_ = c.ShouldBindJSON(&input)
	job, err := h.tax.GenerateEWayBillByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandlerTestable) GetEWayBill(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	record, err := h.tax.GetEWayBillByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "e-way bill not found"})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *DocumentUtilityHandlerTestable) GetEWayBillPDF(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	pdfURL, err := h.tax.GetEWayBillPDFByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pdf_url": pdfURL})
}

func (h *DocumentUtilityHandlerTestable) UpdateEWayPartB(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	var input services.UpdateEWayPartBInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.tax.UpdateEWayPartBByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandlerTestable) InitiateMultiVehicle(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	var input services.MultiVehicleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.tax.InitiateMultiVehicleByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

// =============================================================================
// DocumentHandler Tests - List
// =============================================================================

func TestDocumentList_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	documents := []*models.Document{
		{ID: "doc-1", BusinessID: "biz-123", DocumentType: "invoice", Status: "draft"},
		{ID: "doc-2", BusinessID: "biz-123", DocumentType: "invoice", Status: "active"},
	}
	mockSvc.On("ListByType", mock.Anything, "biz-123", "invoice", 1, 20).Return(documents, int64(2), nil)

	router := gin.New()
	router.GET("/documents", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestDocumentList_InternalError(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	mockSvc.On("ListByType", mock.Anything, "biz-123", "invoice", 1, 20).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/documents", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentHandler Tests - Get
// =============================================================================

func TestDocumentGet_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	doc := &models.Document{ID: "doc-123", BusinessID: "biz-123", DocumentType: "invoice", Status: "draft"}
	mockSvc.On("GetByBusiness", mock.Anything, "biz-123", "doc-123").Return(doc, nil)

	router := gin.New()
	router.GET("/documents/:id", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/doc-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestDocumentGet_NotFound(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	mockSvc.On("GetByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/documents/:id", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDocumentGet_TypeMismatch(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	doc := &models.Document{ID: "doc-123", BusinessID: "biz-123", DocumentType: "bill", Status: "draft"}
	mockSvc.On("GetByBusiness", mock.Anything, "biz-123", "doc-123").Return(doc, nil)

	router := gin.New()
	router.GET("/documents/:id", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Get(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/doc-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentHandler Tests - Create
// =============================================================================

func TestDocumentCreate_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	doc := &models.Document{ID: "doc-123", BusinessID: "biz-123", DocumentType: "invoice", Status: "draft"}
	mockSvc.On("CreateByType", mock.Anything, "biz-123", "invoice", mock.Anything).Return(doc, nil)

	router := gin.New()
	router.POST("/documents", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"party_type": "customer",
		"lines": []map[string]interface{}{
			{"product_id": "prod-123", "description": "Test item", "quantity": 1, "unit_price": 100},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/documents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestDocumentCreate_InvalidInput(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	router := gin.New()
	router.POST("/documents", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/documents", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// DocumentHandler Tests - Update
// =============================================================================

func TestDocumentUpdate_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	doc := &models.Document{ID: "doc-123", BusinessID: "biz-123", DocumentType: "invoice", Status: "draft"}
	mockSvc.On("UpdateByType", mock.Anything, "biz-123", "doc-123", "invoice", mock.Anything).Return(doc, nil)

	router := gin.New()
	router.PUT("/documents/:id", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"status": "draft",
		"lines": []map[string]interface{}{
			{"product_id": "prod-123", "description": "Test item", "quantity": 1, "unit_price": 100},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/documents/doc-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestDocumentUpdate_NotFound(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	mockSvc.On("UpdateByType", mock.Anything, "biz-123", "nonexistent", "invoice", mock.Anything).Return(nil, ErrNotFound)

	router := gin.New()
	router.PUT("/documents/:id", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"status": "draft",
		"lines": []map[string]interface{}{
			{"product_id": "prod-123", "description": "Test item", "quantity": 1, "unit_price": 100},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/documents/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentHandler Tests - Delete
// =============================================================================

func TestDocumentDelete_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	mockSvc.On("DeleteByType", mock.Anything, "biz-123", "doc-123", "invoice").Return(nil)

	router := gin.New()
	router.DELETE("/documents/:id", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/documents/doc-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDocumentDelete_NotFound(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	mockSvc.On("DeleteByType", mock.Anything, "biz-123", "nonexistent", "invoice").Return(ErrNotFound)

	router := gin.New()
	router.DELETE("/documents/:id", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/documents/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentHandler Tests - Cancel
// =============================================================================

func TestDocumentCancel_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	doc := &models.Document{ID: "doc-123", BusinessID: "biz-123", DocumentType: "invoice", Status: "cancelled"}
	mockSvc.On("CancelByType", mock.Anything, "biz-123", "doc-123", "invoice", "Test reason").Return(doc, nil)

	router := gin.New()
	router.POST("/documents/:id/cancel", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Cancel(c)
	})

	reqBody := map[string]interface{}{
		"reason": "Test reason",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/documents/doc-123/cancel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentHandler Tests - GetPDF
// =============================================================================

func TestDocumentGetPDF_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentHandlerTestable(mockSvc, "invoice", log)

	mockSvc.On("GetPDFURLByBusiness", mock.Anything, "biz-123", "doc-123").Return("https://example.com/pdf/doc-123", nil)

	router := gin.New()
	router.GET("/documents/:id/pdf", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.GetPDF(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/doc-123/pdf", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentUtilityHandler Tests - Duplicate
// =============================================================================

func TestDocumentDuplicate_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, nil, log)

	doc := &models.Document{ID: "doc-new", BusinessID: "biz-123", DocumentType: "invoice", Status: "draft"}
	mockSvc.On("DuplicateByBusiness", mock.Anything, "biz-123", "doc-123").Return(doc, nil)

	router := gin.New()
	router.POST("/documents/:id/duplicate", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Duplicate(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/documents/doc-123/duplicate", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestDocumentDuplicate_NotFound(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, nil, log)

	mockSvc.On("DuplicateByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/documents/:id/duplicate", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Duplicate(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/documents/nonexistent/duplicate", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentUtilityHandler Tests - History
// =============================================================================

func TestDocumentHistory_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, nil, log)

	history := &services.DocumentHistory{Document: &models.Document{ID: "doc-123"}}
	mockSvc.On("GetHistory", mock.Anything, "biz-123", "doc-123").Return(history, nil)

	router := gin.New()
	router.GET("/documents/:id/history", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.History(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/doc-123/history", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentUtilityHandler Tests - Merge
// =============================================================================

func TestDocumentMerge_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, nil, log)

	doc := &models.Document{ID: "doc-merged", BusinessID: "biz-123", DocumentType: "invoice", Status: "draft"}
	mockSvc.On("MergeByBusiness", mock.Anything, "biz-123", mock.Anything).Return(doc, nil)

	router := gin.New()
	router.POST("/documents/merge", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Merge(c)
	})

	reqBody := map[string]interface{}{
		"document_ids": []string{"doc-1", "doc-2"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/documents/merge", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentUtilityHandler Tests - Render
// =============================================================================

func TestDocumentRender_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, nil, log)

	job := &models.DocumentRenderJob{ID: "job-123", Status: "processing"}
	mockSvc.On("RequestRenderByBusiness", mock.Anything, "biz-123", "doc-123", mock.Anything).Return(job, nil)

	router := gin.New()
	router.POST("/documents/:id/render", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.Render(c)
	})

	reqBody := map[string]interface{}{
		"profile_id": "default",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/documents/doc-123/render", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DocumentUtilityHandler Tests - EInvoice
// =============================================================================

func TestDocumentGenerateEInvoice_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	mockTax := new(MockTaxComplianceService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, mockTax, log)

	job := &models.GSTSubmissionJob{ID: "job-123", Status: "processing"}
	mockTax.On("GenerateEInvoiceByDocument", mock.Anything, "biz-123", "doc-123", "idem-key-123", mock.Anything).Return(job, nil)

	router := gin.New()
	router.POST("/documents/:id/einvoice", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.GenerateEInvoice(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/documents/doc-123/einvoice", nil)
	req.Header.Set("Idempotency-Key", "idem-key-123")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", res.Code, res.Body.String())
	}

	mockTax.AssertExpectations(t)
}

func TestDocumentGetEInvoice_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	mockTax := new(MockTaxComplianceService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, mockTax, log)

	record := map[string]interface{}{"irn": "123456", "status": "generated"}
	mockTax.On("GetEInvoiceByDocument", mock.Anything, "biz-123", "doc-123").Return(record, nil)

	router := gin.New()
	router.GET("/documents/:id/einvoice", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.GetEInvoice(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/doc-123/einvoice", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockTax.AssertExpectations(t)
}

func TestDocumentGetEInvoice_NotFound(t *testing.T) {
	mockSvc := new(MockDocumentService)
	mockTax := new(MockTaxComplianceService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, mockTax, log)

	mockTax.On("GetEInvoiceByDocument", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/documents/:id/einvoice", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.GetEInvoice(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/nonexistent/einvoice", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockTax.AssertExpectations(t)
}

// =============================================================================
// DocumentUtilityHandler Tests - EWayBill
// =============================================================================

func TestDocumentGenerateEWayBill_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	mockTax := new(MockTaxComplianceService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, mockTax, log)

	job := &models.GSTSubmissionJob{ID: "job-123", Status: "processing"}
	mockTax.On("GenerateEWayBillByDocument", mock.Anything, "biz-123", "doc-123", "idem-key-456", mock.Anything).Return(job, nil)

	router := gin.New()
	router.POST("/documents/:id/ewaybill", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.GenerateEWayBill(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/documents/doc-123/ewaybill", nil)
	req.Header.Set("Idempotency-Key", "idem-key-456")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", res.Code, res.Body.String())
	}

	mockTax.AssertExpectations(t)
}

func TestDocumentGetEWayBill_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	mockTax := new(MockTaxComplianceService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, mockTax, log)

	record := map[string]interface{}{"ewaybill_number": "EWB-123456"}
	mockTax.On("GetEWayBillByDocument", mock.Anything, "biz-123", "doc-123").Return(record, nil)

	router := gin.New()
	router.GET("/documents/:id/ewaybill", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.GetEWayBill(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/doc-123/ewaybill", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockTax.AssertExpectations(t)
}

func TestDocumentGetEWayBill_NotFound(t *testing.T) {
	mockSvc := new(MockDocumentService)
	mockTax := new(MockTaxComplianceService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, mockTax, log)

	mockTax.On("GetEWayBillByDocument", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/documents/:id/ewaybill", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.GetEWayBill(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/nonexistent/ewaybill", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockTax.AssertExpectations(t)
}

func TestDocumentGetEWayBillPDF_Success(t *testing.T) {
	mockSvc := new(MockDocumentService)
	mockTax := new(MockTaxComplianceService)
	log := logger.New()
	handler := NewDocumentUtilityHandlerTestable(mockSvc, mockTax, log)

	mockTax.On("GetEWayBillPDFByDocument", mock.Anything, "biz-123", "doc-123").Return("https://example.com/ewaybill/pdf/doc-123", nil)

	router := gin.New()
	router.GET("/documents/:id/ewaybill/pdf", func(c *gin.Context) {
		createDocumentTestContext(c, "user-123", "biz-123")
		handler.GetEWayBillPDF(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/documents/doc-123/ewaybill/pdf", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockTax.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func createDocumentTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}
