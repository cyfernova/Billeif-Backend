package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// MockA2ABargainingService mocks the A2ABargainingService for testing
type MockA2ABargainingService struct {
	mock.Mock
}

func (m *MockA2ABargainingService) StartNegotiation(ctx context.Context, buyerAgentID, sellerAgentID string, initialAmount float64) (*A2ASession, error) {
	args := m.Called(ctx, buyerAgentID, sellerAgentID, initialAmount)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*A2ASession), args.Error(1)
}

func (m *MockA2ABargainingService) GetSessionProgress(sessionID string) *A2ASession {
	args := m.Called(sessionID)
	if args.Get(0) == nil {
		return &A2ASession{Round: -1}
	}
	return args.Get(0).(*A2ASession)
}

func (m *MockA2ABargainingService) StopNegotiation(sessionID string) {
	m.Called(sessionID)
}

// A2ASession represents an A2A negotiation session (mirrors services.A2ASession)
type A2ASession struct {
	NegotiationID string
	BuyerAgentID  string
	SellerAgentID string
	InitialAmount float64
	CurrentAmount float64
	Round         int
	MaxRounds     int
	Status        string
	StartTime     time.Time
}

// MockAgentServiceForA2ABargaining mocks AgentService methods
type MockAgentServiceForA2ABargaining struct {
	mock.Mock
}

func (m *MockAgentServiceForA2ABargaining) GetAgentByID(ctx context.Context, agentID string) (*models.Agent, error) {
	args := m.Called(ctx, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}

// A2ABargainingHandlerTestable is a testable version of the A2A Bargaining handler
type A2ABargainingHandlerTestable struct {
	a2aBargaining *MockA2ABargainingService
	agentService  *MockAgentServiceForA2ABargaining
	cfg           *config.Config
	log           *logger.Logger
}

func NewA2ABargainingHandlerTestable(
	a2aBargaining *MockA2ABargainingService,
	agentService *MockAgentServiceForA2ABargaining,
	cfg *config.Config,
	log *logger.Logger,
) *A2ABargainingHandlerTestable {
	return &A2ABargainingHandlerTestable{
		a2aBargaining: a2aBargaining,
		agentService:  agentService,
		cfg:           cfg,
		log:           log,
	}
}

func (h *A2ABargainingHandlerTestable) defaultA2AMessageEndpoint() string {
	if h.cfg == nil {
		return "/api/v1/a2a/message"
	}
	return h.cfg.Server.A2AMessageEndpoint()
}

type StartA2ANegotiationRequest struct {
	BuyerAgentID  string  `json:"buyer_agent_id" binding:"required,uuid"`
	SellerAgentID string  `json:"seller_agent_id" binding:"required,uuid"`
	InitialAmount float64 `json:"initial_amount" binding:"required,gt=0"`
	MaxRounds     *int    `json:"max_rounds" binding:"omitempty,gte=1,lte=10"`
}

type AgentInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

type A2ANegotiationSession struct {
	SessionID     string     `json:"session_id"`
	BuyerAgent    *AgentInfo `json:"buyer_agent"`
	SellerAgent   *AgentInfo `json:"seller_agent"`
	InitialAmount float64    `json:"initial_amount"`
	CurrentAmount float64    `json:"current_amount"`
	Round         int        `json:"round"`
	MaxRounds     int        `json:"max_rounds"`
	Status        string     `json:"status"`
	StartTime     string     `json:"start_time"`
}

func (h *A2ABargainingHandlerTestable) StartNegotiation(c *gin.Context) {
	var req StartA2ANegotiationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("invalid start negotiation request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	buyerAgent, err := h.agentService.GetAgentByID(c.Request.Context(), req.BuyerAgentID)
	if err != nil {
		h.log.Error("buyer agent not found", "agent_id", req.BuyerAgentID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "buyer agent not found"})
		return
	}

	sellerAgent, err := h.agentService.GetAgentByID(c.Request.Context(), req.SellerAgentID)
	if err != nil {
		h.log.Error("seller agent not found", "agent_id", req.SellerAgentID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "seller agent not found"})
		return
	}

	if buyerAgent.Type != "shopping" {
		h.log.Warn("buyer agent must be shopping type", "agent_type", buyerAgent.Type)
		c.JSON(http.StatusBadRequest, gin.H{"error": "buyer agent must be of type 'shopping'"})
		return
	}

	if sellerAgent.Type != "merchant" {
		h.log.Warn("seller agent must be merchant type", "agent_type", sellerAgent.Type)
		c.JSON(http.StatusBadRequest, gin.H{"error": "seller agent must be of type 'merchant'"})
		return
	}

	session, err := h.a2aBargaining.StartNegotiation(
		c.Request.Context(),
		req.BuyerAgentID,
		req.SellerAgentID,
		req.InitialAmount,
	)

	if err != nil {
		h.log.Error("failed to start A2A negotiation", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start negotiation"})
		return
	}

	buyerEndpoint := h.defaultA2AMessageEndpoint()
	if buyerAgent.A2AEndpoint != nil {
		buyerEndpoint = *buyerAgent.A2AEndpoint
	}

	sellerEndpoint := h.defaultA2AMessageEndpoint()
	if sellerAgent.A2AEndpoint != nil {
		sellerEndpoint = *sellerAgent.A2AEndpoint
	}

	maxRounds := 5
	if req.MaxRounds != nil {
		maxRounds = *req.MaxRounds
	}

	response := A2ANegotiationSession{
		SessionID: session.NegotiationID,
		BuyerAgent: &AgentInfo{
			ID:       buyerAgent.ID,
			Name:     buyerAgent.Name,
			Type:     buyerAgent.Type,
			Endpoint: buyerEndpoint,
		},
		SellerAgent: &AgentInfo{
			ID:       sellerAgent.ID,
			Name:     sellerAgent.Name,
			Type:     sellerAgent.Type,
			Endpoint: sellerEndpoint,
		},
		InitialAmount: session.InitialAmount,
		CurrentAmount: session.CurrentAmount,
		Round:         session.Round,
		MaxRounds:     maxRounds,
		Status:        "running",
		StartTime:     session.StartTime.Format("2006-01-02T15:04:05Z"),
	}

	h.log.Info("A2A negotiation started", "session_id", session.NegotiationID, "buyer", req.BuyerAgentID, "seller", req.SellerAgentID)

	c.JSON(http.StatusCreated, response)
}

func (h *A2ABargainingHandlerTestable) GetSessionProgress(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	progress := h.a2aBargaining.GetSessionProgress(sessionID)

	if progress.Round == -1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "negotiation session not found"})
		return
	}

	h.log.Info("A2A negotiation progress retrieved", "session_id", sessionID, "round", progress.Round, "current_amount", progress.CurrentAmount)

	c.JSON(http.StatusOK, progress)
}

func (h *A2ABargainingHandlerTestable) StopNegotiation(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	h.a2aBargaining.StopNegotiation(sessionID)

	h.log.Info("A2A negotiation stopped", "session_id", sessionID)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Negotiation stopped",
		"session_id": sessionID,
		"status":     "stopped",
	})
}

func setupA2ABargainingTestRouter(handler *A2ABargainingHandlerTestable) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/a2a-bargaining/start", handler.StartNegotiation)
	router.GET("/a2a-bargaining/progress/:sessionId", handler.GetSessionProgress)
	router.POST("/a2a-bargaining/stop/:sessionId", handler.StopNegotiation)
	return router
}

// =============================================================================
// StartNegotiation Tests
// =============================================================================

func TestStartNegotiation_Success(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "https://api.example.com"},
	}
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, cfg, log)

	buyerAgent := &models.Agent{
		ID:   "550e8400-e29b-41d4-a716-446655440000",
		Name: "Buyer Agent",
		Type: "shopping",
	}
	sellerAgent := &models.Agent{
		ID:   "550e8400-e29b-41d4-a716-446655440001",
		Name: "Seller Agent",
		Type: "merchant",
	}

	session := &A2ASession{
		NegotiationID: "a2a_neg_123",
		BuyerAgentID:  buyerAgent.ID,
		SellerAgentID: sellerAgent.ID,
		InitialAmount: 1000.00,
		CurrentAmount: 1000.00,
		Round:         0,
		MaxRounds:     5,
		Status:        "running",
		StartTime:     time.Now(),
	}

	mockAgent.On("GetAgentByID", mock.Anything, buyerAgent.ID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", mock.Anything, sellerAgent.ID).Return(sellerAgent, nil)
	mockA2A.On("StartNegotiation", mock.Anything, buyerAgent.ID, sellerAgent.ID, 1000.00).Return(session, nil)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  buyerAgent.ID,
		"seller_agent_id": sellerAgent.ID,
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	var response A2ANegotiationSession
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.SessionID != session.NegotiationID {
		t.Errorf("expected session ID %q, got %q", session.NegotiationID, response.SessionID)
	}
	if response.InitialAmount != 1000.00 {
		t.Errorf("expected initial amount 1000.00, got %f", response.InitialAmount)
	}
	if response.Status != "running" {
		t.Errorf("expected status 'running', got %q", response.Status)
	}
	if response.BuyerAgent.Type != "shopping" {
		t.Errorf("expected buyer type 'shopping', got %q", response.BuyerAgent.Type)
	}
	if response.SellerAgent.Type != "merchant" {
		t.Errorf("expected seller type 'merchant', got %q", response.SellerAgent.Type)
	}

	mockAgent.AssertExpectations(t)
	mockA2A.AssertExpectations(t)
}

func TestStartNegotiation_WithMaxRounds(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "https://api.example.com"},
	}
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, cfg, log)

	buyerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440000", Name: "Buyer", Type: "shopping"}
	sellerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440001", Name: "Seller", Type: "merchant"}
	maxRounds := 10

	session := &A2ASession{
		NegotiationID: "a2a_neg_123",
		BuyerAgentID:  buyerAgent.ID,
		SellerAgentID: sellerAgent.ID,
		InitialAmount: 500.00,
		CurrentAmount: 500.00,
		Round:         0,
		MaxRounds:     maxRounds,
		Status:        "running",
		StartTime:     time.Now(),
	}

	mockAgent.On("GetAgentByID", mock.Anything, buyerAgent.ID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", mock.Anything, sellerAgent.ID).Return(sellerAgent, nil)
	mockA2A.On("StartNegotiation", mock.Anything, buyerAgent.ID, sellerAgent.ID, 500.00).Return(session, nil)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  buyerAgent.ID,
		"seller_agent_id": sellerAgent.ID,
		"initial_amount":  500.00,
		"max_rounds":      maxRounds,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	var response A2ANegotiationSession
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.MaxRounds != maxRounds {
		t.Errorf("expected max rounds %d, got %d", maxRounds, response.MaxRounds)
	}
}

func TestStartNegotiation_MissingBuyerAgentID(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_MissingSellerAgentID(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id": "550e8400-e29b-41d4-a716-446655440000",
		"initial_amount": 1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_MissingInitialAmount(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
		"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_InvalidInitialAmountZero(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
		"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
		"initial_amount":  0,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_InvalidInitialAmountNegative(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
		"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
		"initial_amount":  -100.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_InvalidBuyerAgentIDFormat(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  "not-a-uuid",
		"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_InvalidSellerAgentIDFormat(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
		"seller_agent_id": "not-a-uuid",
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_BuyerAgentNotFound(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	mockAgent.On("GetAgentByID", mock.Anything, "550e8400-e29b-41d4-a716-446655440000").Return(nil, ErrAgentNotFound)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
		"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_SellerAgentNotFound(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	buyerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440000", Name: "Buyer", Type: "shopping"}

	mockAgent.On("GetAgentByID", mock.Anything, buyerAgent.ID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", mock.Anything, "550e8400-e29b-41d4-a716-446655440001").Return(nil, ErrAgentNotFound)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  buyerAgent.ID,
		"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_BuyerAgentWrongType(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	buyerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440000", Name: "Buyer", Type: "seller"} // wrong type
	sellerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440001", Name: "Seller", Type: "merchant"}

	mockAgent.On("GetAgentByID", mock.Anything, buyerAgent.ID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", mock.Anything, sellerAgent.ID).Return(sellerAgent, nil)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  buyerAgent.ID,
		"seller_agent_id": sellerAgent.ID,
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	var response map[string]string
	json.Unmarshal(res.Body.Bytes(), &response)
	if response["error"] != "buyer agent must be of type 'shopping'" {
		t.Errorf("unexpected error message: %s", response["error"])
	}
}

func TestStartNegotiation_SellerAgentWrongType(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	buyerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440000", Name: "Buyer", Type: "shopping"}
	sellerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440001", Name: "Seller", Type: "shopping"} // wrong type

	mockAgent.On("GetAgentByID", mock.Anything, buyerAgent.ID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", mock.Anything, sellerAgent.ID).Return(sellerAgent, nil)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  buyerAgent.ID,
		"seller_agent_id": sellerAgent.ID,
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	var response map[string]string
	json.Unmarshal(res.Body.Bytes(), &response)
	if response["error"] != "seller agent must be of type 'merchant'" {
		t.Errorf("unexpected error message: %s", response["error"])
	}
}

func TestStartNegotiation_StartNegotiationFails(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	buyerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440000", Name: "Buyer", Type: "shopping"}
	sellerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440001", Name: "Seller", Type: "merchant"}

	mockAgent.On("GetAgentByID", mock.Anything, buyerAgent.ID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", mock.Anything, sellerAgent.ID).Return(sellerAgent, nil)
	mockA2A.On("StartNegotiation", mock.Anything, buyerAgent.ID, sellerAgent.ID, 1000.00).Return(nil, ErrStartNegotiationFailed)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  buyerAgent.ID,
		"seller_agent_id": sellerAgent.ID,
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", res.Code, res.Body.String())
	}
}

func TestStartNegotiation_WithCustomA2AEndpoint(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "https://api.example.com"},
	}
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, cfg, log)

	customEndpoint := "https://custom.endpoint.com/a2a/message"
	buyerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440000", Name: "Buyer", Type: "shopping", A2AEndpoint: &customEndpoint}
	sellerAgent := &models.Agent{ID: "550e8400-e29b-41d4-a716-446655440001", Name: "Seller", Type: "merchant"}

	session := &A2ASession{
		NegotiationID: "a2a_neg_123",
		BuyerAgentID:  buyerAgent.ID,
		SellerAgentID: sellerAgent.ID,
		InitialAmount: 1000.00,
		CurrentAmount: 1000.00,
		Round:         0,
		MaxRounds:     5,
		Status:        "running",
		StartTime:     time.Now(),
	}

	mockAgent.On("GetAgentByID", mock.Anything, buyerAgent.ID).Return(buyerAgent, nil)
	mockAgent.On("GetAgentByID", mock.Anything, sellerAgent.ID).Return(sellerAgent, nil)
	mockA2A.On("StartNegotiation", mock.Anything, buyerAgent.ID, sellerAgent.ID, 1000.00).Return(session, nil)

	router := setupA2ABargainingTestRouter(handler)

	reqBody := map[string]interface{}{
		"buyer_agent_id":  buyerAgent.ID,
		"seller_agent_id": sellerAgent.ID,
		"initial_amount":  1000.00,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	var response A2ANegotiationSession
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.BuyerAgent.Endpoint != customEndpoint {
		t.Errorf("expected buyer endpoint %q, got %q", customEndpoint, response.BuyerAgent.Endpoint)
	}
}

// =============================================================================
// GetSessionProgress Tests
// =============================================================================

func TestGetSessionProgress_Success(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	session := &A2ASession{
		NegotiationID: "a2a_neg_123",
		BuyerAgentID:  "550e8400-e29b-41d4-a716-446655440000",
		SellerAgentID: "550e8400-e29b-41d4-a716-446655440001",
		InitialAmount: 1000.00,
		CurrentAmount: 950.00,
		Round:         3,
		MaxRounds:     5,
		Status:        "running",
		StartTime:     time.Now(),
	}

	mockA2A.On("GetSessionProgress", "a2a_neg_123").Return(session)

	router := setupA2ABargainingTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/a2a-bargaining/progress/a2a_neg_123", nil)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response A2ASession
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Round != 3 {
		t.Errorf("expected round 3, got %d", response.Round)
	}
	if response.CurrentAmount != 950.00 {
		t.Errorf("expected current amount 950.00, got %f", response.CurrentAmount)
	}

	mockA2A.AssertExpectations(t)
}

func TestGetSessionProgress_SessionNotFound(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	mockA2A.On("GetSessionProgress", "nonexistent").Return(&A2ASession{Round: -1})

	router := setupA2ABargainingTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/a2a-bargaining/progress/nonexistent", nil)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", res.Code, res.Body.String())
	}
}

// =============================================================================
// StopNegotiation Tests
// =============================================================================

func TestStopNegotiation_Success(t *testing.T) {
	mockA2A := new(MockA2ABargainingService)
	mockAgent := new(MockAgentServiceForA2ABargaining)
	log := logger.New()
	handler := NewA2ABargainingHandlerTestable(mockA2A, mockAgent, nil, log)

	mockA2A.On("StopNegotiation", "a2a_neg_123").Return()

	router := setupA2ABargainingTestRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/a2a-bargaining/stop/a2a_neg_123", nil)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response["status"] != "stopped" {
		t.Errorf("expected status 'stopped', got %q", response["status"])
	}
	if response["session_id"] != "a2a_neg_123" {
		t.Errorf("expected session_id 'a2a_neg_123', got %q", response["session_id"])
	}

	mockA2A.AssertExpectations(t)
}

// =============================================================================
// Helper Errors
// =============================================================================

var ErrAgentNotFound = fmt.Errorf("agent not found")
var ErrStartNegotiationFailed = fmt.Errorf("start negotiation failed")
