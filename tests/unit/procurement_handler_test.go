package unit

//   go test -v ./tests/unit/... -run "TestCreateProcurementRun|TestGetProcurementRun|TestCancelProcurementRun|TestStartAgent"
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
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock ProcurementService
// =============================================================================

type MockProcurementService struct {
	mock.Mock
}

func (m *MockProcurementService) StartProcurement(ctx context.Context, req *StartProcurementRequest) (*models.ProcurementRun, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ProcurementRun), args.Error(1)
}

func (m *MockProcurementService) GetProcurementRun(ctx context.Context, userID, shoppingAgentID, runID string) (*models.ProcurementRun, error) {
	args := m.Called(ctx, userID, shoppingAgentID, runID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ProcurementRun), args.Error(1)
}

func (m *MockProcurementService) CancelProcurement(ctx context.Context, userID, shoppingAgentID, runID string) (*models.ProcurementRun, error) {
	args := m.Called(ctx, userID, shoppingAgentID, runID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ProcurementRun), args.Error(1)
}

// Request types for service layer
type StartProcurementRequest struct {
	UserID          string
	ShoppingAgentID string
	Intent          string
	Quantity        int
	MaxBudget       float64
	Currency        string
	PaymentTerms    []string
	AutoBuy         bool
	MaxSellers      int
	MaxRounds       int
	IdempotencyKey  string
}

// =============================================================================
// Testable wrapper
// =============================================================================

type ProcurementHandlerTestable struct {
	svc     *MockProcurementService
	ap2Repo *MockAP2Repository
	log     *logger.Logger
}

func NewProcurementHandlerTestable(svc *MockProcurementService, ap2Repo *MockAP2Repository, log *logger.Logger) *ProcurementHandlerTestable {
	return &ProcurementHandlerTestable{
		svc:     svc,
		ap2Repo: ap2Repo,
		log:     log,
	}
}

func (h *ProcurementHandlerTestable) startProcurement(c *gin.Context) {
	shoppingAgentID := c.Param("id")
	userID := c.GetString("user_id")

	agent, err := h.ap2Repo.GetAgentByID(c.Request.Context(), shoppingAgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if agent.Type != "shopping" {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var req struct {
		Intent       string   `json:"intent" binding:"required"`
		Quantity     int      `json:"quantity"`
		MaxBudget    float64  `json:"max_budget" binding:"required,gt=0"`
		Currency     string   `json:"currency"`
		PaymentTerms []string `json:"payment_terms"`
		AutoBuy      *bool    `json:"auto_buy"`
		MaxSellers   int      `json:"max_sellers"`
		MaxRounds    int      `json:"max_rounds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	autoBuy := true
	if req.AutoBuy != nil {
		autoBuy = *req.AutoBuy
	}

	run, err := h.svc.StartProcurement(c.Request.Context(), &StartProcurementRequest{
		UserID:          userID,
		ShoppingAgentID: shoppingAgentID,
		Intent:          req.Intent,
		Quantity:        req.Quantity,
		MaxBudget:       req.MaxBudget,
		Currency:        req.Currency,
		PaymentTerms:    req.PaymentTerms,
		AutoBuy:         autoBuy,
		MaxSellers:      req.MaxSellers,
		MaxRounds:       req.MaxRounds,
	})
	if err != nil {
		h.log.Error("failed to create procurement run", "shopping_agent_id", shoppingAgentID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, run)
}

func (h *ProcurementHandlerTestable) CreateProcurementRun(c *gin.Context) {
	h.startProcurement(c)
}

func (h *ProcurementHandlerTestable) StartAgent(c *gin.Context) {
	h.startProcurement(c)
}

func (h *ProcurementHandlerTestable) GetProcurementRun(c *gin.Context) {
	shoppingAgentID := c.Param("id")
	runID := c.Param("run_id")
	userID := c.GetString("user_id")

	if _, err := h.ap2Repo.GetAgentByID(c.Request.Context(), shoppingAgentID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	run, err := h.svc.GetProcurementRun(c.Request.Context(), userID, shoppingAgentID, runID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "procurement run not found"})
		return
	}

	c.JSON(http.StatusOK, run)
}

func (h *ProcurementHandlerTestable) CancelProcurementRun(c *gin.Context) {
	shoppingAgentID := c.Param("id")
	runID := c.Param("run_id")
	userID := c.GetString("user_id")

	if _, err := h.ap2Repo.GetAgentByID(c.Request.Context(), shoppingAgentID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	run, err := h.svc.CancelProcurement(c.Request.Context(), userID, shoppingAgentID, runID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "procurement run not found"})
		return
	}

	c.JSON(http.StatusOK, run)
}

// =============================================================================
// CreateProcurementRun Tests
// =============================================================================

func TestCreateProcurementRun_Success(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	now := time.Now()
	run := &models.ProcurementRun{
		ID:              "run-123",
		UserID:          "user-123",
		ShoppingAgentID: "agent-123",
		Intent:          "Buy 10 laptops",
		Quantity:        10,
		MaxBudget:       50000.00,
		Currency:        "INR",
		Status:          "pending",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	mockSvc.On("StartProcurement", mock.Anything, mock.Anything).Return(run, nil)

	router := gin.New()
	router.POST("/agents/:id/procurement-runs", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateProcurementRun(c)
	})

	reqBody := map[string]interface{}{
		"intent":     "Buy 10 laptops",
		"quantity":   10,
		"max_budget": 50000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/procurement-runs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestCreateProcurementRun_MissingIntent(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	router := gin.New()
	router.POST("/agents/:id/procurement-runs", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateProcurementRun(c)
	})

	reqBody := map[string]interface{}{
		"max_budget": 50000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/procurement-runs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockRepo.AssertExpectations(t)
}

func TestCreateProcurementRun_MissingMaxBudget(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	router := gin.New()
	router.POST("/agents/:id/procurement-runs", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateProcurementRun(c)
	})

	reqBody := map[string]interface{}{
		"intent": "Buy 10 laptops",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/procurement-runs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}

	mockRepo.AssertExpectations(t)
}

func TestCreateProcurementRun_AgentNotFound(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	mockRepo.On("GetAgentByID", mock.Anything, "nonexistent").Return(nil, errors.New("agent not found"))

	router := gin.New()
	router.POST("/agents/:id/procurement-runs", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateProcurementRun(c)
	})

	reqBody := map[string]interface{}{
		"intent":     "Buy 10 laptops",
		"max_budget": 50000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/nonexistent/procurement-runs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockRepo.AssertExpectations(t)
}

func TestCreateProcurementRun_InvalidAgentType(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "merchant", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	router := gin.New()
	router.POST("/agents/:id/procurement-runs", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateProcurementRun(c)
	})

	reqBody := map[string]interface{}{
		"intent":     "Buy 10 laptops",
		"max_budget": 50000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/procurement-runs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockRepo.AssertExpectations(t)
}

func TestCreateProcurementRun_InternalError(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	mockSvc.On("StartProcurement", mock.Anything, mock.Anything).Return(nil, errors.New("failed to start procurement"))

	router := gin.New()
	router.POST("/agents/:id/procurement-runs", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CreateProcurementRun(c)
	})

	reqBody := map[string]interface{}{
		"intent":     "Buy 10 laptops",
		"max_budget": 50000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/procurement-runs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// =============================================================================
// GetProcurementRun Tests
// =============================================================================

func TestGetProcurementRun_Success(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	now := time.Now()
	run := &models.ProcurementRun{
		ID:              "run-123",
		UserID:          "user-123",
		ShoppingAgentID: "agent-123",
		Intent:          "Buy 10 laptops",
		Quantity:        10,
		MaxBudget:       50000.00,
		Currency:        "INR",
		Status:          "pending",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	mockSvc.On("GetProcurementRun", mock.Anything, "user-123", "agent-123", "run-123").Return(run, nil)

	router := gin.New()
	router.GET("/agents/:id/procurement-runs/:run_id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetProcurementRun(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/agent-123/procurement-runs/run-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestGetProcurementRun_AgentNotFound(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	mockRepo.On("GetAgentByID", mock.Anything, "nonexistent").Return(nil, errors.New("agent not found"))

	router := gin.New()
	router.GET("/agents/:id/procurement-runs/:run_id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetProcurementRun(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/nonexistent/procurement-runs/run-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockRepo.AssertExpectations(t)
}

func TestGetProcurementRun_RunNotFound(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	mockSvc.On("GetProcurementRun", mock.Anything, "user-123", "agent-123", "nonexistent").Return(nil, errors.New("procurement run not found"))

	router := gin.New()
	router.GET("/agents/:id/procurement-runs/:run_id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetProcurementRun(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/agent-123/procurement-runs/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// =============================================================================
// CancelProcurementRun Tests
// =============================================================================

func TestCancelProcurementRun_Success(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	now := time.Now()
	cancelledAt := now
	run := &models.ProcurementRun{
		ID:              "run-123",
		UserID:          "user-123",
		ShoppingAgentID: "agent-123",
		Intent:          "Buy 10 laptops",
		Quantity:        10,
		MaxBudget:       50000.00,
		Currency:        "INR",
		Status:          "cancelled",
		CancelledAt:     &cancelledAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	mockSvc.On("CancelProcurement", mock.Anything, "user-123", "agent-123", "run-123").Return(run, nil)

	router := gin.New()
	router.POST("/agents/:id/procurement-runs/:run_id/cancel", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CancelProcurementRun(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/procurement-runs/run-123/cancel", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestCancelProcurementRun_AgentNotFound(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	mockRepo.On("GetAgentByID", mock.Anything, "nonexistent").Return(nil, errors.New("agent not found"))

	router := gin.New()
	router.POST("/agents/:id/procurement-runs/:run_id/cancel", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CancelProcurementRun(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/agents/nonexistent/procurement-runs/run-123/cancel", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockRepo.AssertExpectations(t)
}

func TestCancelProcurementRun_RunNotFound(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	mockSvc.On("CancelProcurement", mock.Anything, "user-123", "agent-123", "nonexistent").Return(nil, errors.New("procurement run not found"))

	router := gin.New()
	router.POST("/agents/:id/procurement-runs/:run_id/cancel", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.CancelProcurementRun(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/procurement-runs/nonexistent/cancel", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// =============================================================================
// StartAgent Tests (alias for CreateProcurementRun)
// =============================================================================

func TestStartAgent_Success(t *testing.T) {
	mockSvc := new(MockProcurementService)
	mockRepo := new(MockAP2Repository)
	log := logger.New()
	handler := NewProcurementHandlerTestable(mockSvc, mockRepo, log)

	agent := &models.Agent{ID: "agent-123", Type: "shopping", OwnerID: "user-123"}
	mockRepo.On("GetAgentByID", mock.Anything, "agent-123").Return(agent, nil)

	now := time.Now()
	run := &models.ProcurementRun{
		ID:              "run-123",
		UserID:          "user-123",
		ShoppingAgentID: "agent-123",
		Intent:          "Buy 5 monitors",
		Quantity:        5,
		MaxBudget:       25000.00,
		Currency:        "INR",
		Status:          "pending",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	mockSvc.On("StartProcurement", mock.Anything, mock.Anything).Return(run, nil)

	router := gin.New()
	router.POST("/agents/:id/procurement-runs", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.StartAgent(c)
	})

	reqBody := map[string]interface{}{
		"intent":     "Buy 5 monitors",
		"quantity":   5,
		"max_budget": 25000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/agent-123/procurement-runs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}
