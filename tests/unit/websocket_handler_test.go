package unit

// go test -v ./tests/unit/... -run "TestWebSocket"
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/middleware"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/websocket"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock WebSocketConnectionSvc (for testing - matches actual service interface)
// =============================================================================

type MockWebSocketConnectionSvc struct {
	mock.Mock
}

func (m *MockWebSocketConnectionSvc) GetClientCount(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockWebSocketConnectionSvc) GetConnectedUsers(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockWebSocketConnectionSvc) IsUserConnected(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func (m *MockWebSocketConnectionSvc) SendMessageToUser(ctx context.Context, userID string, msg *websocket.Message) error {
	args := m.Called(ctx, userID, msg)
	return args.Error(0)
}

func (m *MockWebSocketConnectionSvc) BroadcastToAll(ctx context.Context, msg *websocket.Message) error {
	args := m.Called(ctx, msg)
	return args.Error(0)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type WebSocketHandlerTestable struct {
	hub *websocket.Hub
	svc *MockWebSocketConnectionSvc
	log *logger.Logger
}

func NewWebSocketHandlerTestable(hub *websocket.Hub, svc *MockWebSocketConnectionSvc, log *logger.Logger) *WebSocketHandlerTestable {
	return &WebSocketHandlerTestable{
		hub: hub,
		svc: svc,
		log: log,
	}
}

func (h *WebSocketHandlerTestable) GetStats(c *gin.Context) {
	if h.svc != nil {
		count, countErr := h.svc.GetClientCount(c.Request.Context())
		users, usersErr := h.svc.GetConnectedUsers(c.Request.Context())
		if countErr == nil && usersErr == nil {
			c.JSON(http.StatusOK, gin.H{
				"connected_clients": count,
				"connected_users":   users,
				"timestamp":         time.Now(),
			})
			return
		}
	}

	connectedUsers := h.hub.GetConnectedUsers()
	c.JSON(http.StatusOK, gin.H{
		"connected_clients": h.hub.GetClientCount(),
		"connected_users":   connectedUsers,
		"timestamp":         time.Now(),
	})
}

func (h *WebSocketHandlerTestable) GetConnectionStatus(c *gin.Context) {
	userID := c.Param("userID")

	if h.svc != nil {
		isConnected, err := h.svc.IsUserConnected(c.Request.Context(), userID)
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"user_id": userID, "is_connected": isConnected, "timestamp": time.Now()})
			return
		}
	}

	isConnected := h.hub.IsClientConnected(userID)
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "is_connected": isConnected, "timestamp": time.Now()})
}

func (h *WebSocketHandlerTestable) SendNotification(c *gin.Context) {
	userID := c.Param("userID")

	var request struct {
		MessageType string                 `json:"message_type" binding:"required"`
		Data        map[string]interface{} `json:"data" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	msg := &websocket.Message{
		Type:       websocket.MessageType(request.MessageType),
		Sender:     "system",
		Recipients: []string{userID},
		Timestamp:  time.Now(),
		Data:       request.Data,
	}

	if h.svc != nil {
		if err := h.svc.SendMessageToUser(c.Request.Context(), userID, msg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "notification sent", "user_id": userID})
		return
	}

	h.hub.BroadcastToUser(userID, msg)
	c.JSON(http.StatusOK, gin.H{"message": "notification sent", "user_id": userID})
}

func (h *WebSocketHandlerTestable) SendNotificationToAll(c *gin.Context) {
	var request struct {
		MessageType string                 `json:"message_type" binding:"required"`
		Data        map[string]interface{} `json:"data" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	msg := &websocket.Message{Type: websocket.MessageType(request.MessageType), Sender: "system", Timestamp: time.Now(), Data: request.Data}
	if h.svc != nil {
		if err := h.svc.BroadcastToAll(c.Request.Context(), msg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		count, _ := h.svc.GetClientCount(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"message": "notification sent to all clients", "client_count": count})
		return
	}

	h.hub.BroadcastToAll(msg)
	c.JSON(http.StatusOK, gin.H{"message": "notification sent to all clients", "client_count": h.hub.GetClientCount()})
}

func (h *WebSocketHandlerTestable) GetConnectedUsers(c *gin.Context) {
	if h.svc != nil {
		users, err := h.svc.GetConnectedUsers(c.Request.Context())
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"connected_users": users, "count": len(users), "timestamp": time.Now()})
			return
		}
	}

	users := h.hub.GetConnectedUsers()
	c.JSON(http.StatusOK, gin.H{"connected_users": users, "count": len(users), "timestamp": time.Now()})
}

func (h *WebSocketHandlerTestable) HealthCheck(c *gin.Context) {
	if h.svc != nil {
		count, err := h.svc.GetClientCount(c.Request.Context())
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"status": "healthy", "connected_clients": count, "timestamp": time.Now()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "healthy", "connected_clients": h.hub.GetClientCount(), "timestamp": time.Now()})
}

func (h *WebSocketHandlerTestable) HandleConnection(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	userIDStr := userID.(string)
	if userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user_id"})
		return
	}

	c.JSON(http.StatusUnauthorized, gin.H{"error": "websocket upgrade not supported in tests"})
}

// =============================================================================
// GetStats Tests
// =============================================================================

func TestWebSocketGetStats_WithConnectionService(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	users := []string{"user-1", "user-2", "user-3"}
	mockSvc.On("GetClientCount", mock.Anything).Return(3, nil)
	mockSvc.On("GetConnectedUsers", mock.Anything).Return(users, nil)

	router := gin.New()
	router.GET("/ws/stats", func(c *gin.Context) {
		handler.GetStats(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/stats", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["connected_clients"] != float64(3) {
		t.Fatalf("expected 3 connected clients, got %v", resp["connected_clients"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketGetStats_FallbackToHub(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("GetClientCount", mock.Anything).Return(0, errors.New("service error"))
	mockSvc.On("GetConnectedUsers", mock.Anything).Return(nil, errors.New("service error"))

	router := gin.New()
	router.GET("/ws/stats", func(c *gin.Context) {
		handler.GetStats(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/stats", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["connected_clients"] != float64(0) {
		t.Fatalf("expected 0 connected clients, got %v", resp["connected_clients"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketGetStats_NilConnectionService(t *testing.T) {
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, nil, log)

	router := gin.New()
	router.GET("/ws/stats", func(c *gin.Context) {
		handler.GetStats(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/stats", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
}

// =============================================================================
// GetConnectionStatus Tests
// =============================================================================

func TestWebSocketGetConnectionStatus_WithConnectionService(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("IsUserConnected", mock.Anything, "user-123").Return(true, nil)

	router := gin.New()
	router.GET("/ws/status/:userID", func(c *gin.Context) {
		handler.GetConnectionStatus(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/status/user-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["user_id"] != "user-123" {
		t.Fatalf("expected user_id 'user-123', got %v", resp["user_id"])
	}
	if resp["is_connected"] != true {
		t.Fatalf("expected is_connected true, got %v", resp["is_connected"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketGetConnectionStatus_NotConnected(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("IsUserConnected", mock.Anything, "user-456").Return(false, nil)

	router := gin.New()
	router.GET("/ws/status/:userID", func(c *gin.Context) {
		handler.GetConnectionStatus(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/status/user-456", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["is_connected"] != false {
		t.Fatalf("expected is_connected false, got %v", resp["is_connected"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketGetConnectionStatus_FallbackToHub(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("IsUserConnected", mock.Anything, "user-789").Return(false, errors.New("service error"))

	router := gin.New()
	router.GET("/ws/status/:userID", func(c *gin.Context) {
		handler.GetConnectionStatus(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/status/user-789", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// SendNotification Tests
// =============================================================================

func TestWebSocketSendNotification_Success(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("SendMessageToUser", mock.Anything, "user-123", mock.Anything).Return(nil)

	router := gin.New()
	router.POST("/ws/notify/:userID", func(c *gin.Context) {
		handler.SendNotification(c)
	})

	reqBody := map[string]interface{}{
		"message_type": "order_status_update",
		"data":         map[string]interface{}{"order_id": "order-123", "status": "shipped"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/ws/notify/user-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["message"] != "notification sent" {
		t.Fatalf("expected message 'notification sent', got %v", resp["message"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketSendNotification_InvalidInput(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	router := gin.New()
	router.POST("/ws/notify/:userID", func(c *gin.Context) {
		handler.SendNotification(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ws/notify/user-123", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestWebSocketSendNotification_MissingMessageType(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	router := gin.New()
	router.POST("/ws/notify/:userID", func(c *gin.Context) {
		handler.SendNotification(c)
	})

	reqBody := map[string]interface{}{
		"data": map[string]interface{}{"order_id": "order-123"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/ws/notify/user-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestWebSocketSendNotification_ServiceError(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("SendMessageToUser", mock.Anything, "user-123", mock.Anything).Return(errors.New("send failed"))

	router := gin.New()
	router.POST("/ws/notify/:userID", func(c *gin.Context) {
		handler.SendNotification(c)
	})

	reqBody := map[string]interface{}{
		"message_type": "order_status_update",
		"data":         map[string]interface{}{"order_id": "order-123"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/ws/notify/user-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketSendNotification_NilConnectionService(t *testing.T) {
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, nil, log)

	router := gin.New()
	router.POST("/ws/notify/:userID", func(c *gin.Context) {
		handler.SendNotification(c)
	})

	reqBody := map[string]interface{}{
		"message_type": "order_status_update",
		"data":         map[string]interface{}{"order_id": "order-123"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/ws/notify/user-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["message"] != "notification sent" {
		t.Fatalf("expected message 'notification sent', got %v", resp["message"])
	}
}

// =============================================================================
// SendNotificationToAll Tests
// =============================================================================

func TestWebSocketSendNotificationToAll_Success(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("BroadcastToAll", mock.Anything, mock.Anything).Return(nil)
	mockSvc.On("GetClientCount", mock.Anything).Return(5, nil)

	router := gin.New()
	router.POST("/ws/notify-all", func(c *gin.Context) {
		handler.SendNotificationToAll(c)
	})

	reqBody := map[string]interface{}{
		"message_type": "announcement",
		"data":         map[string]interface{}{"message": "System maintenance at 10pm"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/ws/notify-all", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["message"] != "notification sent to all clients" {
		t.Fatalf("expected message 'notification sent to all clients', got %v", resp["message"])
	}
	if resp["client_count"] != float64(5) {
		t.Fatalf("expected client_count 5, got %v", resp["client_count"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketSendNotificationToAll_InvalidInput(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	router := gin.New()
	router.POST("/ws/notify-all", func(c *gin.Context) {
		handler.SendNotificationToAll(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ws/notify-all", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestWebSocketSendNotificationToAll_ServiceError(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("BroadcastToAll", mock.Anything, mock.Anything).Return(errors.New("broadcast failed"))

	router := gin.New()
	router.POST("/ws/notify-all", func(c *gin.Context) {
		handler.SendNotificationToAll(c)
	})

	reqBody := map[string]interface{}{
		"message_type": "announcement",
		"data":         map[string]interface{}{"message": "Test"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/ws/notify-all", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketSendNotificationToAll_NilConnectionService(t *testing.T) {
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, nil, log)

	router := gin.New()
	router.POST("/ws/notify-all", func(c *gin.Context) {
		handler.SendNotificationToAll(c)
	})

	reqBody := map[string]interface{}{
		"message_type": "announcement",
		"data":         map[string]interface{}{"message": "Test"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/ws/notify-all", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["message"] != "notification sent to all clients" {
		t.Fatalf("expected message 'notification sent to all clients', got %v", resp["message"])
	}
}

// =============================================================================
// GetConnectedUsers Tests
// =============================================================================

func TestWebSocketGetConnectedUsers_WithConnectionService(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	users := []string{"alice", "bob", "charlie"}
	mockSvc.On("GetConnectedUsers", mock.Anything).Return(users, nil)

	router := gin.New()
	router.GET("/ws/users", func(c *gin.Context) {
		handler.GetConnectedUsers(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/users", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["count"] != float64(3) {
		t.Fatalf("expected count 3, got %v", resp["count"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketGetConnectedUsers_Empty(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("GetConnectedUsers", mock.Anything).Return([]string{}, nil)

	router := gin.New()
	router.GET("/ws/users", func(c *gin.Context) {
		handler.GetConnectedUsers(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/users", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["count"] != float64(0) {
		t.Fatalf("expected count 0, got %v", resp["count"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketGetConnectedUsers_FallbackToHub(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("GetConnectedUsers", mock.Anything).Return(nil, errors.New("service error"))

	router := gin.New()
	router.GET("/ws/users", func(c *gin.Context) {
		handler.GetConnectedUsers(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/users", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// HealthCheck Tests
// =============================================================================

func TestWebSocketHealthCheck_WithConnectionService(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("GetClientCount", mock.Anything).Return(10, nil)

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

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Fatalf("expected status 'healthy', got %v", resp["status"])
	}
	if resp["connected_clients"] != float64(10) {
		t.Fatalf("expected connected_clients 10, got %v", resp["connected_clients"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketHealthCheck_FallbackToHub(t *testing.T) {
	mockSvc := new(MockWebSocketConnectionSvc)
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, mockSvc, log)

	mockSvc.On("GetClientCount", mock.Anything).Return(0, errors.New("service error"))

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

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Fatalf("expected status 'healthy', got %v", resp["status"])
	}

	mockSvc.AssertExpectations(t)
}

func TestWebSocketHealthCheck_NilConnectionService(t *testing.T) {
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, nil, log)

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

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Fatalf("expected status 'healthy', got %v", resp["status"])
	}
}

// =============================================================================
// HandleConnection Tests
// =============================================================================

func TestWebSocketHandleConnection_MissingUser(t *testing.T) {
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, nil, log)

	router := gin.New()
	router.GET("/ws", func(c *gin.Context) {
		handler.HandleConnection(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["error"] != "authentication required" {
		t.Fatalf("expected error 'authentication required', got %v", resp["error"])
	}
}

func TestWebSocketHandleConnection_EmptyUserID(t *testing.T) {
	hub := websocket.NewHub(logger.New())
	log := logger.New()
	handler := NewWebSocketHandlerTestable(hub, nil, log)

	router := gin.New()
	router.GET("/ws", func(c *gin.Context) {
		c.Set("user_id", "")
		handler.HandleConnection(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["error"] != "invalid user_id" {
		t.Fatalf("expected error 'invalid user_id', got %v", resp["error"])
	}
}

func TestWebSocketManagementEndpoints_ForbiddenForNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("role", c.GetHeader("X-Role"))
		c.Next()
	})
	router.GET("/ws/stats", middleware.RequireRole("admin"), func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/ws/status/:userID", middleware.RequireRole("admin"), func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/ws/users", middleware.RequireRole("admin"), func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/ws/notify/:userID", middleware.RequireRole("admin"), func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/ws/notify-all", middleware.RequireRole("admin"), func(c *gin.Context) { c.Status(http.StatusOK) })

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "stats", method: http.MethodGet, path: "/ws/stats"},
		{name: "status", method: http.MethodGet, path: "/ws/status/user-1"},
		{name: "users", method: http.MethodGet, path: "/ws/users"},
		{name: "notify user", method: http.MethodPost, path: "/ws/notify/user-1"},
		{name: "notify all", method: http.MethodPost, path: "/ws/notify-all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("X-Role", "viewer")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d", res.Code)
			}
		})
	}
}
