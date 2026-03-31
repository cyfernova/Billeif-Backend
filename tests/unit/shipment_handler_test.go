package unit

// go test -v ./tests/unit/... -run "TestShipment"
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
// Mock ShippingService
// =============================================================================

type MockShippingService struct {
	mock.Mock
}

func (m *MockShippingService) GetShipmentByDocument(ctx context.Context, businessID, documentID string) (*models.Shipment, error) {
	args := m.Called(ctx, businessID, documentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Shipment), args.Error(1)
}

func (m *MockShippingService) CreateOrUpdateShipmentForDocument(ctx context.Context, document *models.Document, input services.ShippingRequestInput) (*models.Shipment, error) {
	args := m.Called(ctx, document, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Shipment), args.Error(1)
}

func (m *MockShippingService) GetShippingLabelByDocument(ctx context.Context, businessID, documentID string) (*models.ShippingLabel, error) {
	args := m.Called(ctx, businessID, documentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ShippingLabel), args.Error(1)
}

func (m *MockShippingService) CreateLabelForDocument(ctx context.Context, document *models.Document, input services.ShippingRequestInput) (*services.ShipmentResult, error) {
	args := m.Called(ctx, document, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.ShipmentResult), args.Error(1)
}

// =============================================================================
// Mock DocumentService for Shipment tests
// =============================================================================

type MockDocumentServiceForShipment struct {
	mock.Mock
}

func (m *MockDocumentServiceForShipment) GetByBusiness(ctx context.Context, businessID, id string) (*models.Document, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Document), args.Error(1)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type ShipmentHandlerTestable struct {
	shipping  *MockShippingService
	documents *MockDocumentServiceForShipment
	log       *logger.Logger
}

func NewShipmentHandlerTestable(shipping *MockShippingService, documents *MockDocumentServiceForShipment, log *logger.Logger) *ShipmentHandlerTestable {
	return &ShipmentHandlerTestable{shipping: shipping, documents: documents, log: log}
}

func (h *ShipmentHandlerTestable) GetByDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	shipment, err := h.shipping.GetShipmentByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}
	c.JSON(http.StatusOK, shipment)
}

func (h *ShipmentHandlerTestable) UpsertByDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	document, err := h.documents.GetByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	var input services.ShippingRequestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	shipment, err := h.shipping.CreateOrUpdateShipmentForDocument(c.Request.Context(), document, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, shipment)
}

func (h *ShipmentHandlerTestable) GetLabelByDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	label, err := h.shipping.GetShippingLabelByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipping label not found"})
		return
	}
	c.JSON(http.StatusOK, label)
}

func (h *ShipmentHandlerTestable) GenerateLabelByDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	document, err := h.documents.GetByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	var input services.ShippingRequestInput
	if err := c.ShouldBindJSON(&input); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.shipping.CreateLabelForDocument(c.Request.Context(), document, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// =============================================================================
// GetByDocument Tests
// =============================================================================

func TestShipmentGetByDocument_Success(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	shipment := &models.Shipment{
		ID:         "ship-123",
		BusinessID: "biz-123",
		DocumentID: "doc-123",
		Provider:   "Delhivery",
		Status:     "shipped",
	}

	mockShip.On("GetShipmentByDocument", mock.Anything, "biz-123", "doc-123").Return(shipment, nil)

	router := gin.New()
	router.GET("/shipments/documents/:id", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.GetByDocument(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/shipments/documents/doc-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockShip.AssertExpectations(t)
}

func TestShipmentGetByDocument_NotFound(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	mockShip.On("GetShipmentByDocument", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/shipments/documents/:id", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.GetByDocument(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/shipments/documents/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockShip.AssertExpectations(t)
}

// =============================================================================
// UpsertByDocument Tests
// =============================================================================

func TestShipmentUpsertByDocument_Success(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	document := &models.Document{ID: "doc-123", BusinessID: "biz-123"}

	shipment := &models.Shipment{
		ID:         "ship-123",
		BusinessID: "biz-123",
		DocumentID: "doc-123",
		Provider:   "Delhivery",
		Status:     "draft",
	}

	input := services.ShippingRequestInput{
		Provider:     "Delhivery",
		PackageCount: 1,
		WeightKG:     0.5,
	}

	mockDoc.On("GetByBusiness", mock.Anything, "biz-123", "doc-123").Return(document, nil)
	mockShip.On("CreateOrUpdateShipmentForDocument", mock.Anything, document, input).Return(shipment, nil)

	router := gin.New()
	router.POST("/shipments/documents/:id", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.UpsertByDocument(c)
	})

	reqBody := map[string]interface{}{
		"provider":       "Delhivery",
		"package_count":  1,
		"weight_kg":      0.5,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/shipments/documents/doc-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockDoc.AssertExpectations(t)
	mockShip.AssertExpectations(t)
}

func TestShipmentUpsertByDocument_DocumentNotFound(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	mockDoc.On("GetByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/shipments/documents/:id", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.UpsertByDocument(c)
	})

	reqBody := map[string]interface{}{
		"provider": "Delhivery",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/shipments/documents/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockDoc.AssertExpectations(t)
}

func TestShipmentUpsertByDocument_InvalidInput(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	document := &models.Document{ID: "doc-123", BusinessID: "biz-123"}
	mockDoc.On("GetByBusiness", mock.Anything, "biz-123", "doc-123").Return(document, nil)

	router := gin.New()
	router.POST("/shipments/documents/:id", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.UpsertByDocument(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/shipments/documents/doc-123", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockDoc.AssertExpectations(t)
}

func TestShipmentUpsertByDocument_ServiceError(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	document := &models.Document{ID: "doc-123", BusinessID: "biz-123"}

	input := services.ShippingRequestInput{
		Provider:     "Delhivery",
		PackageCount: 1,
	}

	mockDoc.On("GetByBusiness", mock.Anything, "biz-123", "doc-123").Return(document, nil)
	mockShip.On("CreateOrUpdateShipmentForDocument", mock.Anything, document, input).Return(nil, errors.New("service error"))

	router := gin.New()
	router.POST("/shipments/documents/:id", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.UpsertByDocument(c)
	})

	reqBody := map[string]interface{}{
		"provider":      "Delhivery",
		"package_count": 1,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/shipments/documents/doc-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockDoc.AssertExpectations(t)
	mockShip.AssertExpectations(t)
}

// =============================================================================
// GetLabelByDocument Tests
// =============================================================================

func TestShipmentGetLabelByDocument_Success(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	label := &models.ShippingLabel{
		ID:              "label-123",
		BusinessID:      "biz-123",
		DocumentID:     "doc-123",
		LabelFormat:     "PDF",
		ProviderLabelID: "DEL123456",
	}

	mockShip.On("GetShippingLabelByDocument", mock.Anything, "biz-123", "doc-123").Return(label, nil)

	router := gin.New()
	router.GET("/shipments/documents/:id/label", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.GetLabelByDocument(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/shipments/documents/doc-123/label", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockShip.AssertExpectations(t)
}

func TestShipmentGetLabelByDocument_NotFound(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	mockShip.On("GetShippingLabelByDocument", mock.Anything, "biz-123", "doc-123").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/shipments/documents/:id/label", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.GetLabelByDocument(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/shipments/documents/doc-123/label", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockShip.AssertExpectations(t)
}

// =============================================================================
// GenerateLabelByDocument Tests
// =============================================================================

func TestShipmentGenerateLabelByDocument_Success(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	document := &models.Document{ID: "doc-123", BusinessID: "biz-123"}

	result := &services.ShipmentResult{
		Shipment: &models.Shipment{ID: "ship-123", Status: "shipped"},
		Label:    &models.ShippingLabel{ID: "label-123", LabelFormat: "PDF"},
	}

	input := services.ShippingRequestInput{
		Provider: "Delhivery",
	}

	mockDoc.On("GetByBusiness", mock.Anything, "biz-123", "doc-123").Return(document, nil)
	mockShip.On("CreateLabelForDocument", mock.Anything, document, input).Return(result, nil)

	router := gin.New()
	router.POST("/shipments/documents/:id/label", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.GenerateLabelByDocument(c)
	})

	reqBody := map[string]interface{}{
		"provider": "Delhivery",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/shipments/documents/doc-123/label", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockDoc.AssertExpectations(t)
	mockShip.AssertExpectations(t)
}

func TestShipmentGenerateLabelByDocument_DocumentNotFound(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	mockDoc.On("GetByBusiness", mock.Anything, "biz-123", "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/shipments/documents/:id/label", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.GenerateLabelByDocument(c)
	})

	reqBody := map[string]interface{}{
		"provider": "Delhivery",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/shipments/documents/nonexistent/label", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockDoc.AssertExpectations(t)
}

func TestShipmentGenerateLabelByDocument_ServiceError(t *testing.T) {
	mockShip := new(MockShippingService)
	mockDoc := new(MockDocumentServiceForShipment)
	log := logger.New()
	handler := NewShipmentHandlerTestable(mockShip, mockDoc, log)

	document := &models.Document{ID: "doc-123", BusinessID: "biz-123"}

	mockDoc.On("GetByBusiness", mock.Anything, "biz-123", "doc-123").Return(document, nil)
	mockShip.On("CreateLabelForDocument", mock.Anything, document, services.ShippingRequestInput{}).Return(nil, errors.New("label generation failed"))

	router := gin.New()
	router.POST("/shipments/documents/:id/label", func(c *gin.Context) {
		createShipmentTestContext(c, "user-123", "biz-123")
		handler.GenerateLabelByDocument(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/shipments/documents/doc-123/label", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockDoc.AssertExpectations(t)
	mockShip.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func createShipmentTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}
