package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock BillingOpsService
// =============================================================================

type MockBillingOpsService struct {
	mock.Mock
}

func (m *MockBillingOpsService) ListPriceLists(ctx context.Context, businessID string, page, limit int) ([]interface{}, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]interface{}), args.Get(1).(int64), args.Error(2)
}

func (m *MockBillingOpsService) GetPriceList(ctx context.Context, businessID, id string) (*PriceListDetail, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*PriceListDetail), args.Error(1)
}

func (m *MockBillingOpsService) CreatePriceList(ctx context.Context, input CreatePriceListInput) (*PriceListDetail, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*PriceListDetail), args.Error(1)
}

func (m *MockBillingOpsService) UpdatePriceList(ctx context.Context, businessID, id string, input UpdatePriceListInput) (*PriceListDetail, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*PriceListDetail), args.Error(1)
}

func (m *MockBillingOpsService) DeletePriceList(ctx context.Context, businessID, id string) error {
	args := m.Called(ctx, businessID, id)
	return args.Error(0)
}

func (m *MockBillingOpsService) ListPartyGroups(ctx context.Context, businessID string, page, limit int) ([]interface{}, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]interface{}), args.Get(1).(int64), args.Error(2)
}

func (m *MockBillingOpsService) GetPartyGroup(ctx context.Context, businessID, id string) (*models.PartyGroup, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PartyGroup), args.Error(1)
}

func (m *MockBillingOpsService) GetPartyGroupLedger(ctx context.Context, businessID, id string) (interface{}, error) {
	args := m.Called(ctx, businessID, id)
	return args.Get(0), args.Error(1)
}

func (m *MockBillingOpsService) CreatePartyGroup(ctx context.Context, input CreatePartyGroupInput) (*models.PartyGroup, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PartyGroup), args.Error(1)
}

func (m *MockBillingOpsService) UpdatePartyGroup(ctx context.Context, businessID, id string, input UpdatePartyGroupInput) (*models.PartyGroup, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PartyGroup), args.Error(1)
}

func (m *MockBillingOpsService) DeletePartyGroup(ctx context.Context, businessID, id string) error {
	args := m.Called(ctx, businessID, id)
	return args.Error(0)
}

func (m *MockBillingOpsService) ListActivityLogs(ctx context.Context, filter ActivityLogFilter) ([]interface{}, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]interface{}), args.Get(1).(int64), args.Error(2)
}

func (m *MockBillingOpsService) ListSignatureProfiles(ctx context.Context, businessID string) ([]interface{}, error) {
	args := m.Called(ctx, businessID)
	return args.Get(0).([]interface{}), args.Error(1)
}

func (m *MockBillingOpsService) GetSignatureProfile(ctx context.Context, businessID, id string) (*models.SignatureProfile, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.SignatureProfile), args.Error(1)
}

func (m *MockBillingOpsService) CreateSignatureProfile(ctx context.Context, input CreateSignatureProfileInput) (*models.SignatureProfile, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.SignatureProfile), args.Error(1)
}

func (m *MockBillingOpsService) SignInvoice(ctx context.Context, businessID, invoiceID, profileID, passphrase, reason string) (interface{}, error) {
	args := m.Called(ctx, businessID, invoiceID, profileID, passphrase, reason)
	return args.Get(0), args.Error(1)
}

func (m *MockBillingOpsService) SignDocument(ctx context.Context, businessID, documentID, profileID, passphrase, reason string) (interface{}, error) {
	args := m.Called(ctx, businessID, documentID, profileID, passphrase, reason)
	return args.Get(0), args.Error(1)
}

func (m *MockBillingOpsService) ListBulkJobs(ctx context.Context, businessID string, page, limit int) ([]interface{}, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]interface{}), args.Get(1).(int64), args.Error(2)
}

func (m *MockBillingOpsService) GetBulkJob(ctx context.Context, businessID, id string) (*models.BulkJob, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.BulkJob), args.Error(1)
}

func (m *MockBillingOpsService) CreateBulkJob(ctx context.Context, input CreateBulkJobInput) (*models.BulkJob, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.BulkJob), args.Error(1)
}

func (m *MockBillingOpsService) ListInvoiceSubscriptions(ctx context.Context, businessID string, page, limit int) ([]interface{}, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]interface{}), args.Get(1).(int64), args.Error(2)
}

func (m *MockBillingOpsService) GetInvoiceSubscription(ctx context.Context, businessID, id string) (*models.InvoiceSubscription, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.InvoiceSubscription), args.Error(1)
}

func (m *MockBillingOpsService) CreateInvoiceSubscription(ctx context.Context, input CreateInvoiceSubscriptionInput) (*models.InvoiceSubscription, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.InvoiceSubscription), args.Error(1)
}

func (m *MockBillingOpsService) UpdateInvoiceSubscription(ctx context.Context, businessID, id string, input UpdateInvoiceSubscriptionInput) (*models.InvoiceSubscription, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.InvoiceSubscription), args.Error(1)
}

func (m *MockBillingOpsService) PauseInvoiceSubscription(ctx context.Context, businessID, id string) (*models.InvoiceSubscription, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.InvoiceSubscription), args.Error(1)
}

func (m *MockBillingOpsService) ResumeInvoiceSubscription(ctx context.Context, businessID, id string) (*models.InvoiceSubscription, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.InvoiceSubscription), args.Error(1)
}

func (m *MockBillingOpsService) GenerateInvoiceSubscriptionNow(ctx context.Context, businessID, id string) (*models.InvoiceSubscriptionRun, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.InvoiceSubscriptionRun), args.Error(1)
}

func (m *MockBillingOpsService) ListInvoiceSubscriptionRuns(ctx context.Context, businessID, subscriptionID string, page, limit int) ([]interface{}, int64, error) {
	args := m.Called(ctx, businessID, subscriptionID, page, limit)
	return args.Get(0).([]interface{}), args.Get(1).(int64), args.Error(2)
}

// =============================================================================
// Input Types (mirrors services package)
// =============================================================================

type PriceListItemInput struct {
	EntityType string                 `json:"entity_type,omitempty"`
	ProductID  string                 `json:"product_id,omitempty"`
	VariantID  string                 `json:"variant_id,omitempty"`
	Price      float64                `json:"price"`
	MRP        float64                `json:"mrp"`
	CessRate   float64                `json:"cess_rate"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type PriceListAssignmentInput struct {
	ScopeType string                 `json:"scope_type" binding:"required"`
	ScopeID   string                 `json:"scope_id" binding:"required"`
	Priority  int                    `json:"priority"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type CreatePriceListInput struct {
	BusinessID  string                     `json:"business_id,omitempty"`
	Name        string                     `json:"name" binding:"required"`
	Code        string                     `json:"code" binding:"required"`
	Description string                     `json:"description"`
	Currency    string                     `json:"currency"`
	IsDefault   bool                       `json:"is_default"`
	IsActive    *bool                      `json:"is_active,omitempty"`
	Metadata    map[string]interface{}     `json:"metadata,omitempty"`
	Items       []PriceListItemInput       `json:"items,omitempty"`
	Assignments []PriceListAssignmentInput `json:"assignments,omitempty"`
}

type UpdatePriceListInput struct {
	Name        string                     `json:"name"`
	Code        string                     `json:"code"`
	Description string                     `json:"description"`
	Currency    string                     `json:"currency"`
	IsDefault   *bool                      `json:"is_default,omitempty"`
	IsActive    *bool                      `json:"is_active,omitempty"`
	Metadata    map[string]interface{}     `json:"metadata,omitempty"`
	Items       []PriceListItemInput       `json:"items,omitempty"`
	Assignments []PriceListAssignmentInput `json:"assignments,omitempty"`
}

type PriceListDetail struct {
	PriceList   *models.PriceList             `json:"price_list"`
	Assignments []*models.PriceListAssignment `json:"assignments,omitempty"`
}

type PartyGroupMemberInput struct {
	PartyType string `json:"party_type" binding:"required"`
	PartyID   string `json:"party_id" binding:"required"`
}

type CreatePartyGroupInput struct {
	BusinessID string                  `json:"business_id,omitempty"`
	Name       string                  `json:"name" binding:"required"`
	GroupType  string                  `json:"group_type" binding:"required"`
	Members    []PartyGroupMemberInput `json:"members,omitempty"`
	Metadata   map[string]interface{}  `json:"metadata,omitempty"`
}

type UpdatePartyGroupInput struct {
	Name      string                 `json:"name"`
	GroupType string                 `json:"group_type"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type ActivityLogFilter struct {
	BusinessID string
	EntityType string
	EntityID   string
	Action     string
	Page       int
	Limit      int
}

type CreateSignatureProfileInput struct {
	BusinessID    string
	Name          string
	Provider      string
	SignerName    string
	CertificateSN string
	FileName      string
	FileContent   []byte
}

type CreateBulkJobInput struct {
	BusinessID     string
	CreatedBy      string
	JobType        string
	Action         string
	EntityIDs      []string
	FileName       string
	ContentType    string
	FileContent    []byte
	RequestPayload map[string]interface{}
}

type CreateInvoiceSubscriptionInput struct {
	BusinessID        string                 `json:"business_id,omitempty"`
	Name              string                 `json:"name" binding:"required"`
	CustomerID        string                 `json:"customer_id" binding:"required"`
	InvoiceTemplateID string                 `json:"invoice_template_id" binding:"required"`
	Cadence           string                 `json:"cadence" binding:"required"`
	Timezone          string                 `json:"timezone"`
	NextRunAt         string                 `json:"next_run_at,omitempty"`
	AutoSend          *bool                  `json:"auto_send,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

type UpdateInvoiceSubscriptionInput struct {
	Name              string                 `json:"name"`
	InvoiceTemplateID string                 `json:"invoice_template_id"`
	Cadence           string                 `json:"cadence"`
	Timezone          string                 `json:"timezone"`
	NextRunAt         string                 `json:"next_run_at,omitempty"`
	AutoSend          *bool                  `json:"auto_send,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// =============================================================================
// BillingOpsHandlerTestable
// =============================================================================

type BillingOpsHandlerTestable struct {
	svc *MockBillingOpsService
	log *logger.Logger
}

func NewBillingOpsHandlerTestable(svc *MockBillingOpsService, log *logger.Logger) *BillingOpsHandlerTestable {
	return &BillingOpsHandlerTestable{svc: svc, log: log}
}

// Helper to create test context with business_id
func createTestContextWithBusinessID(c *gin.Context, businessID string) {
	c.Set("business_id", businessID)
}

// Helper to create test context with user_id
func createTestContextWithUserID(c *gin.Context, userID string) {
	c.Set("user_id", userID)
}

// =============================================================================
// Price List Endpoint Tests
// =============================================================================

func TestListPriceLists_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("ListPriceLists", mock.Anything, "biz-123", 1, 10).Return([]interface{}{}, int64(0), nil)

	router := gin.New()
	router.GET("/price-lists", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.ListPriceLists(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/price-lists?page=1&limit=10", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response map[string]interface{}
	json.Unmarshal(res.Body.Bytes(), &response)

	if response["data"] == nil {
		t.Error("expected data field in response")
	}
	if response["total"] != float64(0) {
		t.Errorf("expected total 0, got %v", response["total"])
	}

	mockSvc.AssertExpectations(t)
}

func TestListPriceLists_Forbidden(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/price-lists", func(c *gin.Context) {
		handler.ListPriceLists(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/price-lists", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

func TestListPriceLists_Error(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("ListPriceLists", mock.Anything, "biz-123", 1, 10).Return([]interface{}{}, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/price-lists", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.ListPriceLists(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/price-lists", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestGetPriceList_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	detail := &PriceListDetail{
		PriceList: &models.PriceList{ID: "pl-123", Name: "Test Price List", BusinessID: "biz-123"},
	}
	mockSvc.On("GetPriceList", mock.Anything, "biz-123", "pl-123").Return(detail, nil)

	router := gin.New()
	router.GET("/price-lists/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetPriceList(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/price-lists/pl-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetPriceList_NotFound(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("GetPriceList", mock.Anything, "biz-123", "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.GET("/price-lists/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetPriceList(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/price-lists/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestCreatePriceList_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	detail := &PriceListDetail{
		PriceList: &models.PriceList{ID: "pl-123", Name: "New Price List", BusinessID: "biz-123"},
	}
	mockSvc.On("CreatePriceList", mock.Anything, mock.MatchedBy(func(input CreatePriceListInput) bool {
		return input.Name == "New Price List" && input.Code == "PL001"
	})).Return(detail, nil)

	router := gin.New()
	router.POST("/price-lists", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.CreatePriceList(c)
	})

	reqBody := map[string]interface{}{
		"name": "New Price List",
		"code": "PL001",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/price-lists", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreatePriceList_BadRequest_MissingName(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/price-lists", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.CreatePriceList(c)
	})

	reqBody := map[string]interface{}{
		"code": "PL001",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/price-lists", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCreatePriceList_BadRequest_MissingCode(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/price-lists", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.CreatePriceList(c)
	})

	reqBody := map[string]interface{}{
		"name": "New Price List",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/price-lists", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestUpdatePriceList_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	detail := &PriceListDetail{
		PriceList: &models.PriceList{ID: "pl-123", Name: "Updated Price List"},
	}
	mockSvc.On("UpdatePriceList", mock.Anything, "biz-123", "pl-123", mock.Anything).Return(detail, nil)

	router := gin.New()
	router.PUT("/price-lists/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.UpdatePriceList(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Price List",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/price-lists/pl-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdatePriceList_NotFound(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("UpdatePriceList", mock.Anything, "biz-123", "nonexistent", mock.Anything).Return(nil, errors.New("not found"))

	router := gin.New()
	router.PUT("/price-lists/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.UpdatePriceList(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/price-lists/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDeletePriceList_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("DeletePriceList", mock.Anything, "biz-123", "pl-123").Return(nil)

	router := gin.New()
	router.DELETE("/price-lists/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.DeletePriceList(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/price-lists/pl-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDeletePriceList_NotFound(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("DeletePriceList", mock.Anything, "biz-123", "nonexistent").Return(errors.New("not found"))

	router := gin.New()
	router.DELETE("/price-lists/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.DeletePriceList(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/price-lists/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Party Group Endpoint Tests
// =============================================================================

func TestListPartyGroups_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("ListPartyGroups", mock.Anything, "biz-123", 1, 10).Return([]interface{}{}, int64(0), nil)

	router := gin.New()
	router.GET("/party-groups", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.ListPartyGroups(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/party-groups?page=1&limit=10", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetPartyGroup_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	group := &models.PartyGroup{ID: "pg-123", Name: "Test Group", BusinessID: "biz-123"}
	mockSvc.On("GetPartyGroup", mock.Anything, "biz-123", "pg-123").Return(group, nil)

	router := gin.New()
	router.GET("/party-groups/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetPartyGroup(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/party-groups/pg-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetPartyGroup_NotFound(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("GetPartyGroup", mock.Anything, "biz-123", "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.GET("/party-groups/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetPartyGroup(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/party-groups/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestCreatePartyGroup_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	group := &models.PartyGroup{ID: "pg-123", Name: "New Group", BusinessID: "biz-123"}
	mockSvc.On("CreatePartyGroup", mock.Anything, mock.MatchedBy(func(input CreatePartyGroupInput) bool {
		return input.Name == "New Group" && input.GroupType == "customer_group"
	})).Return(group, nil)

	router := gin.New()
	router.POST("/party-groups", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.CreatePartyGroup(c)
	})

	reqBody := map[string]interface{}{
		"name":       "New Group",
		"group_type": "customer_group",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/party-groups", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreatePartyGroup_BadRequest_MissingName(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/party-groups", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.CreatePartyGroup(c)
	})

	reqBody := map[string]interface{}{
		"group_type": "customer_group",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/party-groups", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestUpdatePartyGroup_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	group := &models.PartyGroup{ID: "pg-123", Name: "Updated Group"}
	mockSvc.On("UpdatePartyGroup", mock.Anything, "biz-123", "pg-123", mock.Anything).Return(group, nil)

	router := gin.New()
	router.PUT("/party-groups/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.UpdatePartyGroup(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Group",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/party-groups/pg-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestDeletePartyGroup_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("DeletePartyGroup", mock.Anything, "biz-123", "pg-123").Return(nil)

	router := gin.New()
	router.DELETE("/party-groups/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.DeletePartyGroup(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/party-groups/pg-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Activity Log Endpoint Tests
// =============================================================================

func TestListActivityLogs_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("ListActivityLogs", mock.Anything, mock.MatchedBy(func(filter ActivityLogFilter) bool {
		return filter.BusinessID == "biz-123" && filter.EntityType == "invoice"
	})).Return([]interface{}{}, int64(0), nil)

	router := gin.New()
	router.GET("/activity-logs", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.ListActivityLogs(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/activity-logs?entity_type=invoice", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListActivityLogs_Forbidden(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/activity-logs", func(c *gin.Context) {
		handler.ListActivityLogs(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/activity-logs", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// Signature Profile Endpoint Tests
// =============================================================================

func TestListSignatureProfiles_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("ListSignatureProfiles", mock.Anything, "biz-123").Return([]interface{}{}, nil)

	router := gin.New()
	router.GET("/signature-profiles", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.ListSignatureProfiles(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/signature-profiles", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetSignatureProfile_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	profile := &models.SignatureProfile{ID: "sp-123", Name: "Test Profile", BusinessID: "biz-123"}
	mockSvc.On("GetSignatureProfile", mock.Anything, "biz-123", "sp-123").Return(profile, nil)

	router := gin.New()
	router.GET("/signature-profiles/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetSignatureProfile(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/signature-profiles/sp-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetSignatureProfile_NotFound(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("GetSignatureProfile", mock.Anything, "biz-123", "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.GET("/signature-profiles/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetSignatureProfile(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/signature-profiles/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Sign Invoice/Document Endpoint Tests
// =============================================================================

func TestSignInvoice_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("SignInvoice", mock.Anything, "biz-123", "inv-123", "sp-123", "passphrase", "Signing").Return(map[string]interface{}{"status": "signed"}, nil)

	router := gin.New()
	router.POST("/invoices/:id/sign", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.SignInvoice(c)
	})

	reqBody := map[string]interface{}{
		"signature_profile_id": "sp-123",
		"passphrase":           "passphrase",
		"reason":               "Signing",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/invoices/inv-123/sign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestSignInvoice_BadRequest_MissingProfileID(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/invoices/:id/sign", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.SignInvoice(c)
	})

	reqBody := map[string]interface{}{
		"passphrase": "passphrase",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/invoices/inv-123/sign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestSignDocument_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("SignDocument", mock.Anything, "biz-123", "doc-123", "sp-123", "passphrase", "Signing").Return(map[string]interface{}{"status": "signed"}, nil)

	router := gin.New()
	router.POST("/documents/:id/sign", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.SignDocument(c)
	})

	reqBody := map[string]interface{}{
		"signature_profile_id": "sp-123",
		"passphrase":           "passphrase",
		"reason":               "Signing",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/documents/doc-123/sign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Bulk Job Endpoint Tests
// =============================================================================

func TestListBulkJobs_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("ListBulkJobs", mock.Anything, "biz-123", 1, 10).Return([]interface{}{}, int64(0), nil)

	router := gin.New()
	router.GET("/bulk-jobs", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.ListBulkJobs(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/bulk-jobs", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetBulkJob_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	job := &models.BulkJob{ID: "job-123", BusinessID: "biz-123", JobType: models.BulkJobTypeImportCustomers}
	mockSvc.On("GetBulkJob", mock.Anything, "biz-123", "job-123").Return(job, nil)

	router := gin.New()
	router.GET("/bulk-jobs/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetBulkJob(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/bulk-jobs/job-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetBulkJob_NotFound(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("GetBulkJob", mock.Anything, "biz-123", "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.GET("/bulk-jobs/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetBulkJob(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/bulk-jobs/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Invoice Subscription Endpoint Tests
// =============================================================================

func TestListInvoiceSubscriptions_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("ListInvoiceSubscriptions", mock.Anything, "biz-123", 1, 10).Return([]interface{}{}, int64(0), nil)

	router := gin.New()
	router.GET("/invoice-subscriptions", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.ListInvoiceSubscriptions(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/invoice-subscriptions", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetInvoiceSubscription_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	subscription := &models.InvoiceSubscription{ID: "sub-123", Name: "Monthly Invoice", BusinessID: "biz-123"}
	mockSvc.On("GetInvoiceSubscription", mock.Anything, "biz-123", "sub-123").Return(subscription, nil)

	router := gin.New()
	router.GET("/invoice-subscriptions/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.GetInvoiceSubscription(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/invoice-subscriptions/sub-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateInvoiceSubscription_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	subscription := &models.InvoiceSubscription{ID: "sub-123", Name: "Monthly Subscription", BusinessID: "biz-123"}
	mockSvc.On("CreateInvoiceSubscription", mock.Anything, mock.MatchedBy(func(input CreateInvoiceSubscriptionInput) bool {
		return input.Name == "Monthly Subscription" && input.CustomerID == "cust-123"
	})).Return(subscription, nil)

	router := gin.New()
	router.POST("/invoice-subscriptions", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.CreateInvoiceSubscription(c)
	})

	reqBody := map[string]interface{}{
		"name":                "Monthly Subscription",
		"customer_id":         "cust-123",
		"invoice_template_id": "tmpl-123",
		"cadence":             "monthly",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/invoice-subscriptions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateInvoiceSubscription_BadRequest_MissingFields(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/invoice-subscriptions", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.CreateInvoiceSubscription(c)
	})

	reqBody := map[string]interface{}{
		"name": "Monthly Subscription",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/invoice-subscriptions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestUpdateInvoiceSubscription_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	subscription := &models.InvoiceSubscription{ID: "sub-123", Name: "Updated Subscription"}
	mockSvc.On("UpdateInvoiceSubscription", mock.Anything, "biz-123", "sub-123", mock.Anything).Return(subscription, nil)

	router := gin.New()
	router.PUT("/invoice-subscriptions/:id", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.UpdateInvoiceSubscription(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Subscription",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/invoice-subscriptions/sub-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPauseInvoiceSubscription_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	subscription := &models.InvoiceSubscription{ID: "sub-123", Name: "Paused Subscription"}
	mockSvc.On("PauseInvoiceSubscription", mock.Anything, "biz-123", "sub-123").Return(subscription, nil)

	router := gin.New()
	router.POST("/invoice-subscriptions/:id/pause", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.PauseInvoiceSubscription(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/invoice-subscriptions/sub-123/pause", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestResumeInvoiceSubscription_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	subscription := &models.InvoiceSubscription{ID: "sub-123", Name: "Resumed Subscription"}
	mockSvc.On("ResumeInvoiceSubscription", mock.Anything, "biz-123", "sub-123").Return(subscription, nil)

	router := gin.New()
	router.POST("/invoice-subscriptions/:id/resume", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.ResumeInvoiceSubscription(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/invoice-subscriptions/sub-123/resume", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGenerateInvoiceSubscriptionNow_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	run := &models.InvoiceSubscriptionRun{ID: "run-123", SubscriptionID: "sub-123"}
	mockSvc.On("GenerateInvoiceSubscriptionNow", mock.Anything, "biz-123", "sub-123").Return(run, nil)

	router := gin.New()
	router.POST("/invoice-subscriptions/:id/generate", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		createTestContextWithUserID(c, "user-123")
		handler.GenerateInvoiceSubscriptionNow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/invoice-subscriptions/sub-123/generate", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListInvoiceSubscriptionRuns_Success(t *testing.T) {
	mockSvc := new(MockBillingOpsService)
	log := logger.New()
	handler := NewBillingOpsHandlerTestable(mockSvc, log)

	mockSvc.On("ListInvoiceSubscriptionRuns", mock.Anything, "biz-123", "sub-123", 1, 10).Return([]interface{}{}, int64(0), nil)

	router := gin.New()
	router.GET("/invoice-subscriptions/:id/runs", func(c *gin.Context) {
		createTestContextWithBusinessID(c, "biz-123")
		handler.ListInvoiceSubscriptionRuns(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/invoice-subscriptions/sub-123/runs", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Handler Methods (testable wrappers)
// =============================================================================

func (h *BillingOpsHandlerTestable) ListPriceLists(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	page, limit := parsePaginationTest(c)
	rows, total, err := h.svc.ListPriceLists(c.Request.Context(), businessID.(string), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandlerTestable) GetPriceList(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	detail, err := h.svc.GetPriceList(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "price list not found"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h *BillingOpsHandlerTestable) CreatePriceList(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	var input CreatePriceListInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID.(string)
	detail, err := h.svc.CreatePriceList(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, detail)
}

func (h *BillingOpsHandlerTestable) UpdatePriceList(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	var input UpdatePriceListInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	detail, err := h.svc.UpdatePriceList(c.Request.Context(), businessID.(string), c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "price list not found"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h *BillingOpsHandlerTestable) DeletePriceList(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	if err := h.svc.DeletePriceList(c.Request.Context(), businessID.(string), c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "price list not found"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *BillingOpsHandlerTestable) ListPartyGroups(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	page, limit := parsePaginationTest(c)
	rows, total, err := h.svc.ListPartyGroups(c.Request.Context(), businessID.(string), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandlerTestable) GetPartyGroup(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	group, err := h.svc.GetPartyGroup(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "party group not found"})
		return
	}
	c.JSON(http.StatusOK, group)
}

func (h *BillingOpsHandlerTestable) GetPartyGroupLedger(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	summary, err := h.svc.GetPartyGroupLedger(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "party group not found"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *BillingOpsHandlerTestable) CreatePartyGroup(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	var input CreatePartyGroupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID.(string)
	group, err := h.svc.CreatePartyGroup(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, group)
}

func (h *BillingOpsHandlerTestable) UpdatePartyGroup(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	var input UpdatePartyGroupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	group, err := h.svc.UpdatePartyGroup(c.Request.Context(), businessID.(string), c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "party group not found"})
		return
	}
	c.JSON(http.StatusOK, group)
}

func (h *BillingOpsHandlerTestable) DeletePartyGroup(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	if err := h.svc.DeletePartyGroup(c.Request.Context(), businessID.(string), c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "party group not found"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *BillingOpsHandlerTestable) ListActivityLogs(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	page, limit := parsePaginationTest(c)
	rows, total, err := h.svc.ListActivityLogs(c.Request.Context(), ActivityLogFilter{
		BusinessID: businessID.(string),
		EntityType: c.Query("entity_type"),
		EntityID:   c.Query("entity_id"),
		Action:     c.Query("action"),
		Page:       page,
		Limit:      limit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandlerTestable) ListSignatureProfiles(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	rows, err := h.svc.ListSignatureProfiles(c.Request.Context(), businessID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *BillingOpsHandlerTestable) GetSignatureProfile(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	profile, err := h.svc.GetSignatureProfile(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "signature profile not found"})
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *BillingOpsHandlerTestable) CreateSignatureProfile(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	fh, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer fh.Close()
	content, err := io.ReadAll(fh)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	profile, err := h.svc.CreateSignatureProfile(c.Request.Context(), CreateSignatureProfileInput{
		BusinessID:    businessID.(string),
		Name:          c.PostForm("name"),
		Provider:      c.PostForm("provider"),
		SignerName:    c.PostForm("signer_name"),
		CertificateSN: c.PostForm("certificate_sn"),
		FileName:      file.Filename,
		FileContent:   content,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, profile)
}

func (h *BillingOpsHandlerTestable) SignInvoice(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	var body struct {
		SignatureProfileID string `json:"signature_profile_id" binding:"required"`
		Passphrase         string `json:"passphrase"`
		Reason             string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	artifact, err := h.svc.SignInvoice(c.Request.Context(), businessID.(string), c.Param("id"), body.SignatureProfileID, body.Passphrase, body.Reason)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, artifact)
}

func (h *BillingOpsHandlerTestable) SignDocument(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	var body struct {
		SignatureProfileID string `json:"signature_profile_id" binding:"required"`
		Passphrase         string `json:"passphrase"`
		Reason             string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	artifact, err := h.svc.SignDocument(c.Request.Context(), businessID.(string), c.Param("id"), body.SignatureProfileID, body.Passphrase, body.Reason)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, artifact)
}

func (h *BillingOpsHandlerTestable) ListBulkJobs(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	page, limit := parsePaginationTest(c)
	rows, total, err := h.svc.ListBulkJobs(c.Request.Context(), businessID.(string), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandlerTestable) GetBulkJob(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	job, err := h.svc.GetBulkJob(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bulk job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *BillingOpsHandlerTestable) ListInvoiceSubscriptions(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	page, limit := parsePaginationTest(c)
	rows, total, err := h.svc.ListInvoiceSubscriptions(c.Request.Context(), businessID.(string), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

func (h *BillingOpsHandlerTestable) GetInvoiceSubscription(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	subscription, err := h.svc.GetInvoiceSubscription(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice subscription not found"})
		return
	}
	c.JSON(http.StatusOK, subscription)
}

func (h *BillingOpsHandlerTestable) CreateInvoiceSubscription(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	var input CreateInvoiceSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID.(string)
	subscription, err := h.svc.CreateInvoiceSubscription(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, subscription)
}

func (h *BillingOpsHandlerTestable) UpdateInvoiceSubscription(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	var input UpdateInvoiceSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	subscription, err := h.svc.UpdateInvoiceSubscription(c.Request.Context(), businessID.(string), c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice subscription not found"})
		return
	}
	c.JSON(http.StatusOK, subscription)
}

func (h *BillingOpsHandlerTestable) PauseInvoiceSubscription(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	subscription, err := h.svc.PauseInvoiceSubscription(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice subscription not found"})
		return
	}
	c.JSON(http.StatusOK, subscription)
}

func (h *BillingOpsHandlerTestable) ResumeInvoiceSubscription(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	subscription, err := h.svc.ResumeInvoiceSubscription(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice subscription not found"})
		return
	}
	c.JSON(http.StatusOK, subscription)
}

func (h *BillingOpsHandlerTestable) GenerateInvoiceSubscriptionNow(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	run, err := h.svc.GenerateInvoiceSubscriptionNow(c.Request.Context(), businessID.(string), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, run)
}

func (h *BillingOpsHandlerTestable) ListInvoiceSubscriptionRuns(c *gin.Context) {
	businessID, ok := c.Get("business_id")
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	page, limit := parsePaginationTest(c)
	rows, total, err := h.svc.ListInvoiceSubscriptionRuns(c.Request.Context(), businessID.(string), c.Param("id"), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "limit": limit})
}

// Helper function for pagination in tests
func parsePaginationTest(c *gin.Context) (page, limit int) {
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "10")
	fmt.Sscanf(pageStr, "%d", &page)
	fmt.Sscanf(limitStr, "%d", &limit)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	return
}
