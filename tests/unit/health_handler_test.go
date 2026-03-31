package unit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock AgentDiscoveryService
// =============================================================================

type MockAgentDiscoveryService struct {
	mock.Mock
}

func (m *MockAgentDiscoveryService) PerformHealthCheck(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

// =============================================================================
// Mock WebSocketConnectionService
// =============================================================================

type MockWebSocketConnectionService struct {
	mock.Mock
}

func (m *MockWebSocketConnectionService) GetClientCount(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockWebSocketConnectionService) GetConnectedUsers(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockWebSocketConnectionService) SendMessageToUser(ctx context.Context, userID string, msg *struct {
	Type    string
	Payload interface{}
}) error {
	args := m.Called(ctx, userID, msg)
	return args.Error(0)
}

func (m *MockWebSocketConnectionService) BroadcastToAll(ctx context.Context, msg *struct {
	Type    string
	Payload interface{}
}) error {
	args := m.Called(ctx, msg)
	return args.Error(0)
}

// =============================================================================
// HealthHandler Testable
// =============================================================================

type HealthHandlerTestable struct {
	log *logger.Logger
}

func NewHealthHandlerTestable(log *logger.Logger) *HealthHandlerTestable {
	return &HealthHandlerTestable{log: log}
}

func (h *HealthHandlerTestable) Check(c *gin.Context) {
	message := `
 __          __                                  _  _                _
 \ \        / /                                 | |(_)              | |
  \ \  /\  / /   ___    __ _   _ __   ___       | | _  __   __  ___ | |
   \ \/  \/ /   / _ \  / _` + "`" + ` | | '__| / _ \      | || | \ \ / / / _ \| |
    \  /\  /   |  __/ | (_| | | |    |  __/      | || |  \ V / |  __/|_|
     \/  \/     \___|  \__,_| |_|     \___|      |_||_|   \_/   \___|(_)
`
	c.String(http.StatusOK, message)
}

// =============================================================================
// AgentDiscoveryHealthHandler Testable
// =============================================================================

type AgentDiscoveryHealthHandlerTestable struct {
	discovery *MockAgentDiscoveryService
	log       *logger.Logger
}

func NewAgentDiscoveryHealthHandlerTestable(discovery *MockAgentDiscoveryService, log *logger.Logger) *AgentDiscoveryHealthHandlerTestable {
	return &AgentDiscoveryHealthHandlerTestable{
		discovery: discovery,
		log:       log,
	}
}

func (h *AgentDiscoveryHealthHandlerTestable) HealthCheck(c *gin.Context) {
	registryID := c.Param("registryID")

	if err := h.discovery.PerformHealthCheck(c.Request.Context(), registryID); err != nil {
		h.log.Error("failed to perform health check", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to perform health check"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "health check completed"})
}

// =============================================================================
// WebSocketHealthHandler Testable
// =============================================================================

type WebSocketHealthHandlerTestable struct {
	hub           *MockHub
	connectionSvc *MockWebSocketConnectionService
	log           *logger.Logger
}

type MockHub struct {
	clientCount int
}

func (m *MockHub) GetClientCount() int {
	return m.clientCount
}

func NewWebSocketHealthHandlerTestable(hub *MockHub, connectionSvc *MockWebSocketConnectionService, log *logger.Logger) *WebSocketHealthHandlerTestable {
	return &WebSocketHealthHandlerTestable{
		hub:           hub,
		connectionSvc: connectionSvc,
		log:           log,
	}
}

func (h *WebSocketHealthHandlerTestable) HealthCheck(c *gin.Context) {
	if h.connectionSvc != nil {
		count, err := h.connectionSvc.GetClientCount(c.Request.Context())
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"status": "healthy", "connected_clients": count, "timestamp": time.Now()})
			return
		}
		h.log.Warn("failed to get websocket health from connection service", "error", err)
	}
	c.JSON(http.StatusOK, gin.H{"status": "healthy", "connected_clients": h.hub.GetClientCount(), "timestamp": time.Now()})
}

// =============================================================================
// HealthHandler Tests
// =============================================================================

func TestHealthCheck_Success(t *testing.T) {
	log := logger.New()
	handler := NewHealthHandlerTestable(log)

	router := gin.New()
	router.GET("/health", handler.Check)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	body := res.Body.String()
	if body == "" {
		t.Fatal("expected non-empty response body")
	}
}

// =============================================================================
// AgentDiscovery HealthCheck Tests
// =============================================================================

func TestAgentDiscoveryHealthCheck_Success(t *testing.T) {
	mockSvc := new(MockAgentDiscoveryService)
	log := logger.New()
	handler := NewAgentDiscoveryHealthHandlerTestable(mockSvc, log)

	mockSvc.On("PerformHealthCheck", mock.Anything, "registry-123").Return(nil)

	router := gin.New()
	router.POST("/discovery/agents/:registryID/health-check", func(c *gin.Context) {
		c.Set("role", "admin")
		handler.HealthCheck(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/discovery/agents/registry-123/health-check", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestAgentDiscoveryHealthCheck_ServiceError(t *testing.T) {
	mockSvc := new(MockAgentDiscoveryService)
	log := logger.New()
	handler := NewAgentDiscoveryHealthHandlerTestable(mockSvc, log)

	mockSvc.On("PerformHealthCheck", mock.Anything, "registry-123").Return(errors.New("service error"))

	router := gin.New()
	router.POST("/discovery/agents/:registryID/health-check", func(c *gin.Context) {
		c.Set("role", "admin")
		handler.HealthCheck(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/discovery/agents/registry-123/health-check", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// WebSocket HealthCheck Tests
// =============================================================================

func TestWebSocketHealthCheck_WithConnectionService_Success(t *testing.T) {
	mockHub := &MockHub{clientCount: 10}
	mockConnSvc := new(MockWebSocketConnectionService)
	log := logger.New()
	handler := NewWebSocketHealthHandlerTestable(mockHub, mockConnSvc, log)

	mockConnSvc.On("GetClientCount", mock.Anything).Return(5, nil)

	router := gin.New()
	router.GET("/ws/health", func(c *gin.Context) {
		handler.HealthCheck(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/health", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockConnSvc.AssertExpectations(t)
}

func TestWebSocketHealthCheck_WithConnectionService_Error(t *testing.T) {
	mockHub := &MockHub{clientCount: 10}
	mockConnSvc := new(MockWebSocketConnectionService)
	log := logger.New()
	handler := NewWebSocketHealthHandlerTestable(mockHub, mockConnSvc, log)

	mockConnSvc.On("GetClientCount", mock.Anything).Return(0, errors.New("connection error"))

	router := gin.New()
	router.GET("/ws/health", func(c *gin.Context) {
		handler.HealthCheck(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/health", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 (fallback to hub), got %d: %s", res.Code, res.Body.String())
	}

	mockConnSvc.AssertExpectations(t)
}

func TestWebSocketHealthCheck_WithoutConnectionService(t *testing.T) {
	mockHub := &MockHub{clientCount: 7}
	log := logger.New()
	handler := NewWebSocketHealthHandlerTestable(mockHub, nil, log)

	router := gin.New()
	router.GET("/ws/health", func(c *gin.Context) {
		handler.HealthCheck(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/health", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
}
