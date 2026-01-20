package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/websocket"
)

// WebSocketHandler handles WebSocket upgrade requests
type WebSocketHandler struct {
	hub *websocket.Hub
	log *logger.Logger
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(hub *websocket.Hub, log *logger.Logger) *WebSocketHandler {
	return &WebSocketHandler{
		hub: hub,
		log: log,
	}
}

// upgrader configures WebSocket upgrade options
var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, validate origin against allowed domains
		return true
	},
}

// HandleConnection handles WebSocket connection upgrades
// GET /ws
func (h *WebSocketHandler) HandleConnection(c *gin.Context) {
	// Get user ID from context (set by authentication middleware)
	userID, exists := c.Get("user_id")
	if !exists {
		h.log.Warn("websocket connection without user_id")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "authentication required",
		})
		return
	}

	userIDStr := userID.(string)
	if userIDStr == "" {
		h.log.Warn("websocket connection with empty user_id")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid user_id",
		})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.log.Error("websocket upgrade failed", "user_id", userIDStr, "error", err)
		return
	}

	// Create client
	client := websocket.NewClient(userIDStr, conn, h.hub, h.log)

	// Register client with hub
	h.hub.RegisterClient(client)

	h.log.Info("websocket client connected", "user_id", userIDStr, "remote_addr", c.RemoteIP(), "total_clients", h.hub.GetClientCount())

	// Start read and write pumps
	go client.ReadPump()
	go client.WritePump()

	// Send welcome message
	welcomeMsg := &websocket.Message{
		Type:      websocket.MessageTypeHeartbeat,
		Sender:    "system",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"message": "connected",
			"server_time": time.Now(),
			"client_count": h.hub.GetClientCount(),
		},
	}

	if err := client.SendMessage(welcomeMsg); err != nil {
		h.log.Error("failed to send welcome message", "user_id", userIDStr, "error", err)
	}
}

// BroadcastOrderUpdate broadcasts an order status update to a user
func (h *WebSocketHandler) BroadcastOrderUpdate(userID string, orderID string, status string, message string) error {
	update := &websocket.OrderStatusUpdate{
		OrderID:   orderID,
		Status:    status,
		UpdatedAt: time.Now(),
		Message:   message,
	}

	msg := websocket.NewOrderStatusUpdateMessage(userID, update)
	h.hub.BroadcastToUser(userID, msg)
	return nil
}

// BroadcastPaymentSuccess broadcasts a payment success notification
func (h *WebSocketHandler) BroadcastPaymentSuccess(userID string, orderID string, amount float64, razorpayID string) error {
	updateData := &websocket.PaymentUpdateData{
		OrderID:    orderID,
		Amount:     amount,
		Status:     "success",
		RazorpayID: razorpayID,
		Timestamp:  time.Now(),
	}

	msg := websocket.NewPaymentUpdateMessage(userID, updateData)
	h.hub.BroadcastToUser(userID, msg)
	h.log.Info("payment success notification sent", "user_id", userID, "order_id", orderID)
	return nil
}

// BroadcastPaymentFailure broadcasts a payment failure notification
func (h *WebSocketHandler) BroadcastPaymentFailure(userID string, orderID string, amount float64, errorMsg string) error {
	updateData := &websocket.PaymentUpdateData{
		OrderID:   orderID,
		Amount:    amount,
		Status:    "failed",
		Error:     errorMsg,
		Timestamp: time.Now(),
	}

	msg := websocket.NewPaymentUpdateMessage(userID, updateData)
	h.hub.BroadcastToUser(userID, msg)
	h.log.Error("payment failure notification sent", "user_id", userID, "order_id", orderID, "error", errorMsg)
	return nil
}

// BroadcastAgentAction broadcasts an agent action notification
func (h *WebSocketHandler) BroadcastAgentAction(userID string, agentID string, action string, data map[string]interface{}, status string) error {
	actionData := &websocket.AgentActionData{
		AgentID: agentID,
		Action:  action,
		Data:    data,
		Status:  status,
	}

	msg := websocket.NewAgentActionMessage(userID, actionData)
	h.hub.BroadcastToUser(userID, msg)
	h.log.Info("agent action notification sent", "user_id", userID, "agent_id", agentID, "action", action)
	return nil
}

// BroadcastTaskUpdate broadcasts a task status update
func (h *WebSocketHandler) BroadcastTaskUpdate(userID string, taskID string, status string, progress int, message string, data map[string]interface{}) error {
	updateData := &websocket.TaskUpdateData{
		TaskID:    taskID,
		Status:    status,
		Progress:  progress,
		Message:   message,
		Data:      data,
		Timestamp: time.Now(),
	}

	msg := websocket.NewTaskUpdateMessage(userID, updateData)
	h.hub.BroadcastToUser(userID, msg)
	h.log.Info("task update notification sent", "user_id", userID, "task_id", taskID, "status", status, "progress", progress)
	return nil
}

// BroadcastToUsers sends a message to multiple users
func (h *WebSocketHandler) BroadcastToUsers(userIDs []string, messageType websocket.MessageType, data interface{}) error {
	msg := &websocket.Message{
		Type:      messageType,
		Sender:    "system",
		Recipients: userIDs,
		Timestamp: time.Now(),
		Data:      data,
	}

	h.hub.BroadcastToUsers(userIDs, msg)
	return nil
}

// BroadcastToAll sends a message to all connected clients
func (h *WebSocketHandler) BroadcastToAll(messageType websocket.MessageType, data interface{}) error {
	msg := &websocket.Message{
		Type:      messageType,
		Sender:    "system",
		Timestamp: time.Now(),
		Data:      data,
	}

	h.hub.BroadcastToAll(msg)
	return nil
}

// GetStats returns WebSocket statistics
// GET /ws/stats
func (h *WebSocketHandler) GetStats(c *gin.Context) {
	connectedUsers := h.hub.GetConnectedUsers()

	c.JSON(http.StatusOK, gin.H{
		"connected_clients": h.hub.GetClientCount(),
		"connected_users":   connectedUsers,
		"timestamp":         time.Now(),
	})
}

// GetConnectionStatus checks if a user is connected
// GET /ws/status/:userID
func (h *WebSocketHandler) GetConnectionStatus(c *gin.Context) {
	userID := c.Param("userID")

	isConnected := h.hub.IsClientConnected(userID)

	c.JSON(http.StatusOK, gin.H{
		"user_id":      userID,
		"is_connected": isConnected,
		"timestamp":    time.Now(),
	})
}

// SendNotification sends a notification to a specific user
// POST /ws/notify/:userID
func (h *WebSocketHandler) SendNotification(c *gin.Context) {
	userID := c.Param("userID")

	var request struct {
		MessageType string                 `json:"message_type" binding:"required"`
		Data        map[string]interface{} `json:"data" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	msg := &websocket.Message{
		Type:      websocket.MessageType(request.MessageType),
		Sender:    "system",
		Recipients: []string{userID},
		Timestamp: time.Now(),
		Data:      request.Data,
	}

	h.hub.BroadcastToUser(userID, msg)

	c.JSON(http.StatusOK, gin.H{
		"message": "notification sent",
		"user_id": userID,
	})
}

// SendNotificationToAll broadcasts a notification to all connected clients
// POST /ws/notify-all
func (h *WebSocketHandler) SendNotificationToAll(c *gin.Context) {
	var request struct {
		MessageType string                 `json:"message_type" binding:"required"`
		Data        map[string]interface{} `json:"data" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	msg := &websocket.Message{
		Type:      websocket.MessageType(request.MessageType),
		Sender:    "system",
		Timestamp: time.Now(),
		Data:      request.Data,
	}

	h.hub.BroadcastToAll(msg)

	c.JSON(http.StatusOK, gin.H{
		"message": "notification sent to all clients",
		"client_count": h.hub.GetClientCount(),
	})
}

// GetConnectedUsers returns list of connected user IDs
// GET /ws/users
func (h *WebSocketHandler) GetConnectedUsers(c *gin.Context) {
	users := h.hub.GetConnectedUsers()

	c.JSON(http.StatusOK, gin.H{
		"connected_users": users,
		"count":          len(users),
		"timestamp":      time.Now(),
	})
}

// HealthCheck performs a health check on the WebSocket hub
// GET /ws/health
func (h *WebSocketHandler) HealthCheck(c *gin.Context) {
	clientCount := h.hub.GetClientCount()

	c.JSON(http.StatusOK, gin.H{
		"status":            "healthy",
		"connected_clients": clientCount,
		"timestamp":         time.Now(),
	})
}
