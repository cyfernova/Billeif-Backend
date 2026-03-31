package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock AgentService
// =============================================================================

type MockAgentService struct {
	mock.Mock
}

func (m *MockAgentService) CreatePersonalAgent(ctx context.Context, req *CreatePersonalAgentRequest) (*models.Agent, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}

func (m *MockAgentService) CreateMerchantAgent(ctx context.Context, req *CreateMerchantAgentRequest) (*models.Agent, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}

func (m *MockAgentService) GetAgentByID(ctx context.Context, agentID string) (*models.Agent, error) {
	args := m.Called(ctx, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}

func (m *MockAgentService) GetAgentsByUser(ctx context.Context, userID string, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *MockAgentService) GetAgentsByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *MockAgentService) UpdateAgent(ctx context.Context, agentID string, updates map[string]interface{}) error {
	args := m.Called(ctx, agentID, updates)
	return args.Error(0)
}

func (m *MockAgentService) DeleteAgent(ctx context.Context, agentID string) error {
	args := m.Called(ctx, agentID)
	return args.Error(0)
}

func (m *MockAgentService) AddCapability(ctx context.Context, agentID, capabilityType, description string, config map[string]interface{}) error {
	args := m.Called(ctx, agentID, capabilityType, description, config)
	return args.Error(0)
}

func (m *MockAgentService) GetAgentCapabilities(ctx context.Context, agentID string) ([]models.AgentCapability, error) {
	args := m.Called(ctx, agentID)
	return args.Get(0).([]models.AgentCapability), args.Error(1)
}

func (m *MockAgentService) RemoveCapability(ctx context.Context, capabilityID string) error {
	args := m.Called(ctx, capabilityID)
	return args.Error(0)
}

func (m *MockAgentService) CreateCredentialProviderAgent(ctx context.Context, businessID, name, description string) (*models.Agent, error) {
	args := m.Called(ctx, businessID, name, description)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}

func (m *MockAgentService) CreatePaymentProcessorAgent(ctx context.Context, businessID, name, description string) (*models.Agent, error) {
	args := m.Called(ctx, businessID, name, description)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}

// =============================================================================
// Request Types
// =============================================================================

type CreatePersonalAgentRequest struct {
	UserID      string
	BusinessID  string
	Name        string
	Description string
	Config      map[string]interface{}
}

type CreateMerchantAgentRequest struct {
	BusinessID  string
	Name        string
	Description string
	ProductIDs  []string
}

// =============================================================================
// AgentHandlerTestable
// =============================================================================

type AgentHandlerTestable struct {
	svc *MockAgentService
	log *logger.Logger
}

func NewAgentHandlerTestable(svc *MockAgentService, log *logger.Logger) *AgentHandlerTestable {
	return &AgentHandlerTestable{svc: svc, log: log}
}

// =============================================================================
// Helper Functions
// =============================================================================

func createTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
}

// =============================================================================
// CreateAgent Tests
// =============================================================================

func TestCreateAgent_ShoppingAgent_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{
		ID:         "agent-123",
		Name:       "Shopping Agent",
		Type:       "shopping",
		BusinessID: "biz-123",
		OwnerID:    "user-123",
	}

	mockSvc.On("CreatePersonalAgent", mock.Anything, mock.MatchedBy(func(req *CreatePersonalAgentRequest) bool {
		return req.Name == "Shopping Agent" && req.BusinessID == "biz-123"
	})).Return(agent, nil)

	router := gin.New()
	router.POST("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreateAgent(c)
	})

	reqBody := map[string]interface{}{
		"type": "shopping",
		"name": "Shopping Agent",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateAgent_MerchantAgent_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{
		ID:         "agent-456",
		Name:       "Merchant Agent",
		Type:       "merchant",
		BusinessID: "biz-123",
	}

	mockSvc.On("CreateMerchantAgent", mock.Anything, mock.MatchedBy(func(req *CreateMerchantAgentRequest) bool {
		return req.Name == "Merchant Agent" && req.BusinessID == "biz-123"
	})).Return(agent, nil)

	router := gin.New()
	router.POST("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreateAgent(c)
	})

	reqBody := map[string]interface{}{
		"type": "merchant",
		"name": "Merchant Agent",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateAgent_BadRequest_MissingType(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreateAgent(c)
	})

	reqBody := map[string]interface{}{
		"name": "Test Agent",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCreateAgent_BadRequest_MissingName(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreateAgent(c)
	})

	reqBody := map[string]interface{}{
		"type": "shopping",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCreateAgent_BadRequest_MissingBusinessID(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "") // no business_id
		handler.CreateAgent(c)
	})

	reqBody := map[string]interface{}{
		"type": "shopping",
		"name": "Test Agent",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCreateAgent_BadRequest_InvalidAgentType(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreateAgent(c)
	})

	reqBody := map[string]interface{}{
		"type": "invalid_type",
		"name": "Test Agent",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCreateAgent_InternalError(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	mockSvc.On("CreatePersonalAgent", mock.Anything, mock.Anything).Return(nil, errors.New("creation failed"))

	router := gin.New()
	router.POST("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreateAgent(c)
	})

	reqBody := map[string]interface{}{
		"type": "shopping",
		"name": "Test Agent",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListAgents Tests
// =============================================================================

func TestListAgents_ByUser_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agents := []*models.Agent{
		{ID: "agent-1", Name: "Agent 1", Type: "shopping"},
		{ID: "agent-2", Name: "Agent 2", Type: "shopping"},
	}
	mockSvc.On("GetAgentsByUser", mock.Anything, "user-123", 1, 10).Return(agents, int64(2), nil)

	router := gin.New()
	router.GET("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.ListAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response map[string]interface{}
	json.Unmarshal(res.Body.Bytes(), &response)

	if response["data"] == nil {
		t.Error("expected data field")
	}
	if response["total"] != float64(2) {
		t.Errorf("expected total 2, got %v", response["total"])
	}

	mockSvc.AssertExpectations(t)
}

func TestListAgents_ByBusiness_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agents := []*models.Agent{
		{ID: "agent-1", Name: "Merchant 1", Type: "merchant", BusinessID: "biz-123"},
	}
	mockSvc.On("GetAgentsByBusiness", mock.Anything, "biz-123", 1, 10).Return(agents, int64(1), nil)

	router := gin.New()
	router.GET("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.ListAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents?business_id=biz-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListAgents_Unauthorized(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/agents", func(c *gin.Context) {
		handler.ListAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestListAgents_Error(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	mockSvc.On("GetAgentsByUser", mock.Anything, "user-123", 1, 10).Return(([]*models.Agent)(nil), int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/agents", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetAgent Tests
// =============================================================================

func TestGetAgent_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", Type: "shopping", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	router := gin.New()
	router.GET("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAgent(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/agent-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetAgent_NotFound(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	mockSvc.On("GetAgentByID", mock.Anything, "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.GET("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAgent(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestGetAgent_Unauthorized(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/agents/:id", func(c *gin.Context) {
		handler.GetAgent(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/agent-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

// =============================================================================
// UpdateAgent Tests
// =============================================================================

func TestUpdateAgent_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("UpdateAgent", mock.Anything, "agent-123", mock.Anything).Return(nil)

	router := gin.New()
	router.PUT("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.UpdateAgent(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Agent",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/agents/agent-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdateAgent_BadRequest_InvalidJSON(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	router := gin.New()
	router.PUT("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.UpdateAgent(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/agents/agent-123", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdateAgent_NotFound(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	mockSvc.On("GetAgentByID", mock.Anything, "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.PUT("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.UpdateAgent(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/agents/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdateAgent_InternalError(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("UpdateAgent", mock.Anything, "agent-123", mock.Anything).Return(errors.New("update failed"))

	router := gin.New()
	router.PUT("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.UpdateAgent(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/agents/agent-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DeleteAgent Tests
// =============================================================================

func TestDeleteAgent_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("DeleteAgent", mock.Anything, "agent-123").Return(nil)

	router := gin.New()
	router.DELETE("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.DeleteAgent(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/agents/agent-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDeleteAgent_NotFound(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	mockSvc.On("GetAgentByID", mock.Anything, "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.DELETE("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.DeleteAgent(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/agents/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDeleteAgent_InternalError(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("DeleteAgent", mock.Anything, "agent-123").Return(errors.New("delete failed"))

	router := gin.New()
	router.DELETE("/agents/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.DeleteAgent(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/agents/agent-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// AddCapability Tests
// =============================================================================

func TestAddCapability_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("AddCapability", mock.Anything, "agent-123", "shopping.search", "", mock.Anything).Return(nil)

	router := gin.New()
	router.POST("/agents/:id/capabilities", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddCapability(c)
	})

	reqBody := map[string]interface{}{
		"capability_type": "shopping.search",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/capabilities", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestAddCapability_BadRequest_MissingCapabilityType(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	router := gin.New()
	router.POST("/agents/:id/capabilities", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddCapability(c)
	})

	reqBody := map[string]interface{}{
		"description": "Test capability",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/capabilities", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestAddCapability_AgentNotFound(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	mockSvc.On("GetAgentByID", mock.Anything, "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.POST("/agents/:id/capabilities", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddCapability(c)
	})

	reqBody := map[string]interface{}{
		"capability_type": "shopping.search",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/nonexistent/capabilities", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListCapabilities Tests
// =============================================================================

func TestListCapabilities_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	capabilities := []models.AgentCapability{
		{ID: "cap-1", CapabilityType: "Shopping Search"},
		{ID: "cap-2", CapabilityType: "Procurement"},
	}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("GetAgentCapabilities", mock.Anything, "agent-123").Return(capabilities, nil)

	router := gin.New()
	router.GET("/agents/:id/capabilities", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListCapabilities(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/agent-123/capabilities", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// RemoveCapability Tests
// =============================================================================

func TestRemoveCapability_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("RemoveCapability", mock.Anything, "cap-1").Return(nil)

	router := gin.New()
	router.DELETE("/agents/:id/capabilities/:capability_id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.RemoveCapability(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/agents/agent-123/capabilities/cap-1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestRemoveCapability_InternalError(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("RemoveCapability", mock.Anything, "cap-1").Return(errors.New("remove failed"))

	router := gin.New()
	router.DELETE("/agents/:id/capabilities/:capability_id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.RemoveCapability(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/agents/agent-123/capabilities/cap-1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetAgentCapabilities Tests
// =============================================================================

func TestGetAgentCapabilities_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	capabilities := []models.AgentCapability{
		{ID: "cap-1", CapabilityType: "Shopping Search"},
	}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("GetAgentCapabilities", mock.Anything, "agent-123").Return(capabilities, nil)

	router := gin.New()
	router.GET("/agents/:id/capabilities", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAgentCapabilities(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/agent-123/capabilities", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ValidateAgentPermissions Tests
// =============================================================================

func TestValidateAgentPermissions_AgentExists(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	router := gin.New()
	router.POST("/agents/validate-permissions/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ValidateAgentPermissions(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/agents/validate-permissions/agent-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response map[string]bool
	json.Unmarshal(res.Body.Bytes(), &response)

	if !response["has_permission"] {
		t.Error("expected has_permission to be true")
	}

	mockSvc.AssertExpectations(t)
}

func TestValidateAgentPermissions_AgentNotFound(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	mockSvc.On("GetAgentByID", mock.Anything, "nonexistent").Return(nil, errors.New("not found"))

	router := gin.New()
	router.POST("/agents/validate-permissions/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ValidateAgentPermissions(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/agents/validate-permissions/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response map[string]bool
	json.Unmarshal(res.Body.Bytes(), &response)

	if response["has_permission"] {
		t.Error("expected has_permission to be false")
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CreateCredentialProviderAgent Tests
// =============================================================================

func TestCreateCredentialProviderAgent_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Credential Provider",
		Type: "credential_provider",
	}
	mockSvc.On("CreateCredentialProviderAgent", mock.Anything, "biz-123", "Cred Provider", "Provides credentials").
		Return(agent, nil)

	router := gin.New()
	router.POST("/agents/credential-provider", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreateCredentialProviderAgent(c)
	})

	reqBody := map[string]interface{}{
		"name":        "Cred Provider",
		"description": "Provides credentials",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credential-provider", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateCredentialProviderAgent_BadRequest_MissingName(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/credential-provider", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreateCredentialProviderAgent(c)
	})

	reqBody := map[string]interface{}{
		"description": "Some description",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credential-provider", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestCreateCredentialProviderAgent_Forbidden(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/credential-provider", func(c *gin.Context) {
		handler.CreateCredentialProviderAgent(c)
	})

	reqBody := map[string]interface{}{
		"name":        "Cred Provider",
		"description": "Description",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credential-provider", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// CreatePaymentProcessorAgent Tests
// =============================================================================

func TestCreatePaymentProcessorAgent_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{
		ID:   "agent-456",
		Name: "Payment Processor",
		Type: "payment_processor",
	}
	mockSvc.On("CreatePaymentProcessorAgent", mock.Anything, "biz-123", "Pay Processor", "Processes payments").
		Return(agent, nil)

	router := gin.New()
	router.POST("/agents/payment-processor", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreatePaymentProcessorAgent(c)
	})

	reqBody := map[string]interface{}{
		"name":        "Pay Processor",
		"description": "Processes payments",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/payment-processor", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreatePaymentProcessorAgent_BadRequest_MissingDescription(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/payment-processor", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.CreatePaymentProcessorAgent(c)
	})

	reqBody := map[string]interface{}{
		"name": "Pay Processor",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/payment-processor", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// UpdateAgentStatus Tests
// =============================================================================

func TestUpdateAgentStatus_Activate_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123", IsActive: false}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("UpdateAgent", mock.Anything, "agent-123", mock.Anything).Return(nil)

	router := gin.New()
	router.POST("/agents/:id/status", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.UpdateAgentStatus(c)
	})

	reqBody := map[string]interface{}{
		"is_active": true,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/status", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdateAgentStatus_Deactivate_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123", IsActive: true}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)
	mockSvc.On("UpdateAgent", mock.Anything, "agent-123", mock.Anything).Return(nil)

	router := gin.New()
	router.POST("/agents/:id/status", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.UpdateAgentStatus(c)
	})

	reqBody := map[string]interface{}{
		"is_active": false,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/status", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdateAgentStatus_BadRequest_MissingIsActive(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agent := &models.Agent{ID: "agent-123", Name: "Test Agent", OwnerID: "user-123"}
	mockSvc.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	router := gin.New()
	router.POST("/agents/:id/status", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.UpdateAgentStatus(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/status", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// GetActiveAgents Tests
// =============================================================================

func TestGetActiveAgents_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agents := []*models.Agent{
		{ID: "agent-1", Name: "Active Merchant 1", Type: "merchant", IsActive: true},
		{ID: "agent-2", Name: "Active Merchant 2", Type: "merchant", IsActive: true},
	}
	mockSvc.On("GetAgentsByBusiness", mock.Anything, "biz-123", 1, 1000).Return(agents, int64(2), nil)

	router := gin.New()
	router.GET("/agents/active", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.GetActiveAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/active?type=merchant", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetActiveAgents_BadRequest_MissingType(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/agents/active", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.GetActiveAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/active", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestGetActiveAgents_Forbidden(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/agents/active", func(c *gin.Context) {
		handler.GetActiveAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/active?type=merchant", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// GetAgentByType Tests
// =============================================================================

func TestGetAgentByType_Shopping_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agents := []*models.Agent{
		{ID: "agent-1", Name: "Shopping Agent 1", Type: "shopping"},
	}
	mockSvc.On("GetAgentsByUser", mock.Anything, "user-123", 1, 10).Return(agents, int64(1), nil)

	router := gin.New()
	router.GET("/agents/type/:type", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAgentByType(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/type/shopping", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetAgentByType_Merchant_Success(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	agents := []*models.Agent{
		{ID: "agent-1", Name: "Merchant Agent 1", Type: "merchant", BusinessID: "biz-123"},
	}
	mockSvc.On("GetAgentsByBusiness", mock.Anything, "biz-123", 1, 10).Return(agents, int64(1), nil)

	router := gin.New()
	router.GET("/agents/type/:type", func(c *gin.Context) {
		createTestContext(c, "user-123", "biz-123")
		handler.GetAgentByType(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/type/merchant", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetAgentByType_InvalidType(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/agents/type/:type", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetAgentByType(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/type/invalid_type", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestGetAgentByType_Merchant_Forbidden(t *testing.T) {
	mockSvc := new(MockAgentService)
	log := logger.New()
	handler := NewAgentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/agents/type/:type", func(c *gin.Context) {
		createTestContext(c, "user-123", "") // no business_id
		handler.GetAgentByType(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/type/merchant", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// Handler Methods (testable wrappers)
// =============================================================================

func (h *AgentHandlerTestable) CreateAgent(c *gin.Context) {
	businessID := c.GetString("business_id")
	userID := c.GetString("user_id")

	var req CreateAgentRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		h.log.Warn("invalid create agent payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.BusinessID != "" {
		businessID = req.BusinessID
	}

	if businessID == "" {
		h.log.Warn("business_id is required")
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	req.Config = sanitizeAgentConfig(req.Config)
	req.Type = normalizeAgentType(req.Type)

	var agent *models.Agent
	var err error

	switch req.Type {
	case "shopping":
		personalReq := &CreatePersonalAgentRequest{
			UserID:      userID,
			BusinessID:  businessID,
			Name:        req.Name,
			Description: req.Description,
			Config:      req.Config,
		}
		agent, err = h.svc.CreatePersonalAgent(c.Request.Context(), personalReq)
	case "merchant":
		agent, err = h.svc.CreateMerchantAgent(c.Request.Context(), &CreateMerchantAgentRequest{
			BusinessID:  businessID,
			Name:        req.Name,
			Description: req.Description,
			ProductIDs:  extractProductIDs(req.Config),
		})
	default:
		h.log.Warn("invalid agent type", "type", req.Type)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent type"})
		return
	}

	if err != nil {
		h.log.Error("failed to create agent", "error", err, "type", req.Type)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, agent)
}

func (h *AgentHandlerTestable) ListAgents(c *gin.Context) {
	userID := c.GetString("user_id")
	page, limit := parsePaginationTestAgent(c)

	var agents []*models.Agent
	var total int64
	var err error

	if c.Query("business_id") != "" {
		businessID := c.GetString("business_id")
		if businessID == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
			return
		}
		agents, total, err = h.svc.GetAgentsByBusiness(c.Request.Context(), businessID, page, limit)
	} else {
		if userID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		agents, total, err = h.svc.GetAgentsByUser(c.Request.Context(), userID, page, limit)
	}

	if err != nil {
		h.log.Error("failed to list agents", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  agents,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *AgentHandlerTestable) GetAgent(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	if userID == "" && businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	c.JSON(http.StatusOK, agent)
}

func (h *AgentHandlerTestable) UpdateAgent(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var req UpdateAgentRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		h.log.Warn("invalid update agent payload", "error", err, "agent_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if req.Config != nil {
		updates["config"] = sanitizeAgentConfig(req.Config)
	}

	if err := h.svc.UpdateAgent(c.Request.Context(), id, updates); err != nil {
		h.log.Error("failed to update agent", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent updated successfully"})
}

func (h *AgentHandlerTestable) DeleteAgent(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	if err := h.svc.DeleteAgent(c.Request.Context(), id); err != nil {
		h.log.Error("failed to delete agent", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *AgentHandlerTestable) AddCapability(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var req AddCapabilityRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		h.log.Warn("invalid add capability payload", "error", err, "agent_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.AddCapability(c.Request.Context(), id, req.CapabilityType, req.Description, req.Config); err != nil {
		h.log.Error("failed to add capability", "error", err, "agent_id", id, "capability_type", req.CapabilityType)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "capability added successfully"})
}

func (h *AgentHandlerTestable) ListCapabilities(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	capabilities, err := h.svc.GetAgentCapabilities(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to list capabilities", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, capabilities)
}

func (h *AgentHandlerTestable) RemoveCapability(c *gin.Context) {
	id := c.Param("id")
	capabilityID := c.Param("capability_id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	if err := h.svc.RemoveCapability(c.Request.Context(), capabilityID); err != nil {
		h.log.Error("failed to remove capability", "error", err, "capability_id", capabilityID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "capability removed successfully"})
}

func (h *AgentHandlerTestable) GetAgentCapabilities(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	capabilities, err := h.svc.GetAgentCapabilities(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent capabilities", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, capabilities)
}

func (h *AgentHandlerTestable) ValidateAgentPermissions(c *gin.Context) {
	agentID := c.Param("id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	agent, err := h.svc.GetAgentByID(c.Request.Context(), agentID)
	if err != nil || agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusOK, gin.H{"has_permission": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"has_permission": true})
}

func (h *AgentHandlerTestable) CreateCredentialProviderAgent(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		h.log.Warn("invalid credential provider agent payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent, err := h.svc.CreateCredentialProviderAgent(c.Request.Context(), businessID, req.Name, req.Description)
	if err != nil {
		h.log.Error("failed to create credential provider agent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, agent)
}

func (h *AgentHandlerTestable) CreatePaymentProcessorAgent(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		h.log.Warn("invalid payment processor agent payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent, err := h.svc.CreatePaymentProcessorAgent(c.Request.Context(), businessID, req.Name, req.Description)
	if err != nil {
		h.log.Error("failed to create payment processor agent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, agent)
}

func (h *AgentHandlerTestable) UpdateAgentStatus(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var req struct {
		IsActive *bool `json:"is_active" binding:"required"`
	}
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		h.log.Warn("invalid update agent status payload", "error", err, "agent_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{"is_active": *req.IsActive}
	if err := h.svc.UpdateAgent(c.Request.Context(), id, updates); err != nil {
		h.log.Error("failed to update agent status", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent status updated successfully"})
}

func (h *AgentHandlerTestable) GetActiveAgents(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return
	}
	agentType := normalizeAgentType(c.Query("type"))
	if agentType == "" {
		h.log.Warn("missing type query param for active agents")
		c.JSON(http.StatusBadRequest, gin.H{"error": "type parameter is required"})
		return
	}

	agents, _, err := h.svc.GetAgentsByBusiness(c.Request.Context(), businessID, 1, 1000)
	if err != nil {
		h.log.Error("failed to get active agents", "error", err, "type", agentType)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	filtered := make([]*models.Agent, 0, len(agents))
	for _, agent := range agents {
		if agent.IsActive && agent.Type == agentType {
			filtered = append(filtered, agent)
		}
	}

	c.JSON(http.StatusOK, filtered)
}

func (h *AgentHandlerTestable) GetAgentByType(c *gin.Context) {
	agentType := normalizeAgentType(c.Param("type"))
	page, limit := parsePaginationTestAgent(c)
	userID := c.GetString("user_id")
	businessID := c.GetString("business_id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var agents []*models.Agent
	var total int64
	var err error

	if agentType == "shopping" {
		agents, _, err = h.svc.GetAgentsByUser(c.Request.Context(), userID, page, limit)
		filtered := make([]*models.Agent, 0, len(agents))
		for _, agent := range agents {
			if agent.Type == "shopping" {
				filtered = append(filtered, agent)
			}
		}
		agents = filtered
		total = int64(len(filtered))
	} else if agentType == "merchant" {
		if businessID == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
			return
		}
		agents, _, err = h.svc.GetAgentsByBusiness(c.Request.Context(), businessID, page, limit)
		filtered := make([]*models.Agent, 0, len(agents))
		for _, agent := range agents {
			if agent.Type == "merchant" {
				filtered = append(filtered, agent)
			}
		}
		agents = filtered
		total = int64(len(filtered))
	} else {
		h.log.Warn("invalid agent type", "type", agentType)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent type"})
		return
	}

	if err != nil {
		h.log.Error("failed to get agents by type", "error", err, "type", agentType)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  agents,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// =============================================================================
// Helper Types and Functions
// =============================================================================

type CreateAgentRequest struct {
	Type        string                 `json:"type" binding:"required"`
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Config      map[string]interface{} `json:"config"`
	BusinessID  string                 `json:"business_id"`
}

type UpdateAgentRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	IsActive    *bool                  `json:"is_active"`
	Config      map[string]interface{} `json:"config"`
}

type AddCapabilityRequest struct {
	CapabilityType string                 `json:"capability_type" binding:"required"`
	Description    string                 `json:"description"`
	Config         map[string]interface{} `json:"config"`
}

var allowedAgentConfigKeys = map[string]bool{
	"type":                 true,
	"product_ids":          true,
	"volatility":           true,
	"preferred_strategies": true,
	"max_budget":           true,
	"auto_approve":         true,
	"notification_url":     true,
}

func sanitizeAgentConfig(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return nil
	}
	clean := make(map[string]interface{}, len(config))
	for k, v := range config {
		if allowedAgentConfigKeys[k] {
			clean[k] = v
		}
	}
	return clean
}

func extractProductIDs(config map[string]interface{}) []string {
	if config == nil {
		return nil
	}
	if productIDs, ok := config["product_ids"].([]interface{}); ok {
		var ids []string
		for _, id := range productIDs {
			if s, ok := id.(string); ok {
				ids = append(ids, s)
			}
		}
		return ids
	}
	return nil
}

func normalizeAgentType(agentType string) string {
	switch agentType {
	case "shopping", "merchant", "credential_provider", "payment_processor":
		return agentType
	default:
		return ""
	}
}

func parsePaginationTestAgent(c *gin.Context) (Page, limit int) {
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "10")
	fmt.Sscanf(pageStr, "%d", &Page)
	fmt.Sscanf(limitStr, "%d", &limit)
	if Page < 1 {
		Page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	return
}
