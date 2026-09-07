package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/websocket"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
)

// WebSocketHandler handles WebSocket connection and notification management.
type WebSocketHandler struct {
	hub           *websocket.Hub
	connectionSvc *services.WebSocketConnectionService
	ticketSvc     websocketTicketConsumer
	log           *logger.Logger
}

type websocketTicketConsumer interface {
	Consume(ctx context.Context, ticketValue string) (*models.WebSocketTicket, error)
}

// NewWebSocketHandler creates a new WebSocket handler.
func NewWebSocketHandler(
	hub *websocket.Hub,
	connectionSvc *services.WebSocketConnectionService,
	ticketSvc *services.WebSocketTicketService,
	log *logger.Logger,
) *WebSocketHandler {
	return &WebSocketHandler{
		hub:           hub,
		connectionSvc: connectionSvc,
		ticketSvc:     ticketSvc,
		log:           log,
	}
}

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// HandleConnection handles local WebSocket connection upgrades.
// @Summary WebSocket connection
// @Description Upgrades to a WebSocket connection
// @Tags WebSocket
// @Produce json
// @Security BearerAuth
// @Success 101 {string} string "Switching Protocols"
// @Failure 401 {object} map[string]string
// @Router /ws [get]
func (h *WebSocketHandler) HandleConnection(c *gin.Context) {
	if h.ticketSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "websocket tickets are unavailable"})
		return
	}
	ticket, err := h.ticketSvc.Consume(c.Request.Context(), strings.TrimSpace(c.Query("ticket")))
	if err != nil {
		h.log.Warn("websocket connection ticket rejected", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid websocket ticket"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.log.Error("websocket upgrade failed", "user_id", ticket.Subject, "business_id", ticket.BusinessID, "error", err)
		return
	}

	client := websocket.NewClient(ticket.Subject, conn, h.hub, h.log)
	h.hub.RegisterClient(client)

	h.log.Info("websocket client connected", "user_id", ticket.Subject, "business_id", ticket.BusinessID, "remote_addr", c.RemoteIP(), "total_clients", h.hub.GetClientCount())

	go client.ReadPump()
	go client.WritePump()

	welcomeMsg := &websocket.Message{
		Type:      websocket.MessageTypeHeartbeat,
		Sender:    "system",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"message":      "connected",
			"server_time":  time.Now(),
			"client_count": h.hub.GetClientCount(),
		},
	}

	if err := client.SendMessage(welcomeMsg); err != nil {
		h.log.Error("failed to send welcome message", "user_id", ticket.Subject, "business_id", ticket.BusinessID, "error", err)
	}
}

func (h *WebSocketHandler) BroadcastOrderUpdate(userID string, orderID string, status string, message string) error {
	update := &websocket.OrderStatusUpdate{OrderID: orderID, Status: status, UpdatedAt: time.Now(), Message: message}
	msg := websocket.NewOrderStatusUpdateMessage(userID, update)
	return h.sendToUser(contextBackground(), userID, msg)
}

func (h *WebSocketHandler) BroadcastPaymentSuccess(userID string, orderID string, amount float64, razorpayID string) error {
	updateData := &websocket.PaymentUpdateData{OrderID: orderID, Amount: amount, Status: "success", RazorpayID: razorpayID, Timestamp: time.Now()}
	msg := websocket.NewPaymentUpdateMessage(userID, updateData)
	h.log.Info("payment success notification sent", "user_id", userID, "order_id", orderID)
	return h.sendToUser(contextBackground(), userID, msg)
}

func (h *WebSocketHandler) BroadcastPaymentFailure(userID string, orderID string, amount float64, errorMsg string) error {
	updateData := &websocket.PaymentUpdateData{OrderID: orderID, Amount: amount, Status: "failed", Error: errorMsg, Timestamp: time.Now()}
	msg := websocket.NewPaymentUpdateMessage(userID, updateData)
	h.log.Error("payment failure notification sent", "user_id", userID, "order_id", orderID, "error", errorMsg)
	return h.sendToUser(contextBackground(), userID, msg)
}

func (h *WebSocketHandler) BroadcastAgentAction(userID string, agentID string, action string, data map[string]interface{}, status string) error {
	actionData := &websocket.AgentActionData{AgentID: agentID, Action: action, Data: data, Status: status}
	msg := websocket.NewAgentActionMessage(userID, actionData)
	h.log.Info("agent action notification sent", "user_id", userID, "agent_id", agentID, "action", action)
	return h.sendToUser(contextBackground(), userID, msg)
}

func (h *WebSocketHandler) BroadcastTaskUpdate(userID string, taskID string, status string, progress int, message string, data map[string]interface{}) error {
	updateData := &websocket.TaskUpdateData{TaskID: taskID, Status: status, Progress: progress, Message: message, Data: data, Timestamp: time.Now()}
	msg := websocket.NewTaskUpdateMessage(userID, updateData)
	h.log.Info("task update notification sent", "user_id", userID, "task_id", taskID, "status", status, "progress", progress)
	return h.sendToUser(contextBackground(), userID, msg)
}

func (h *WebSocketHandler) BroadcastToUsers(userIDs []string, messageType websocket.MessageType, data interface{}) error {
	msg := &websocket.Message{Type: messageType, Sender: "system", Recipients: userIDs, Timestamp: time.Now(), Data: data}
	if h.connectionSvc != nil {
		for _, userID := range userIDs {
			if err := h.connectionSvc.SendMessageToUser(contextBackground(), userID, msg); err != nil {
				h.log.Warn("failed to send websocket message to user", "user_id", userID, "error", err)
			}
		}
		return nil
	}
	h.hub.BroadcastToUsers(userIDs, msg)
	return nil
}

func (h *WebSocketHandler) BroadcastToAll(messageType websocket.MessageType, data interface{}) error {
	msg := &websocket.Message{Type: messageType, Sender: "system", Timestamp: time.Now(), Data: data}
	if h.connectionSvc != nil {
		return h.connectionSvc.BroadcastToAll(contextBackground(), msg)
	}
	h.hub.BroadcastToAll(msg)
	return nil
}

// GetStats returns WebSocket statistics.
// @Summary Get WebSocket stats
// @Description Returns WebSocket connection statistics
// @Tags WebSocket
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /ws/stats [get]
func (h *WebSocketHandler) GetStats(c *gin.Context) {
	if h.connectionSvc != nil {
		count, countErr := h.connectionSvc.GetClientCount(c.Request.Context())
		users, usersErr := h.connectionSvc.GetConnectedUsers(c.Request.Context())
		if countErr == nil && usersErr == nil {
			c.JSON(http.StatusOK, gin.H{
				"connected_clients": count,
				"connected_users":   users,
				"timestamp":         time.Now(),
			})
			return
		}
		h.log.Warn("failed to get websocket stats from connection service", "count_error", countErr, "users_error", usersErr)
	}

	connectedUsers := h.hub.GetConnectedUsers()
	c.JSON(http.StatusOK, gin.H{
		"connected_clients": h.hub.GetClientCount(),
		"connected_users":   connectedUsers,
		"timestamp":         time.Now(),
	})
}

// GetConnectionStatus checks if a user is connected.
// @Summary Get WebSocket connection status
// @Description Checks if a specific user is connected via WebSocket
// @Tags WebSocket
// @Produce json
// @Security BearerAuth
// @Param userID path string true "User ID"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /ws/status/{userID} [get]
func (h *WebSocketHandler) GetConnectionStatus(c *gin.Context) {
	userID := c.Param("userID")

	if h.connectionSvc != nil {
		isConnected, err := h.connectionSvc.IsUserConnected(c.Request.Context(), userID)
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"user_id": userID, "is_connected": isConnected, "timestamp": time.Now()})
			return
		}
		h.log.Warn("failed to get websocket connection status from connection service", "user_id", userID, "error", err)
	}

	isConnected := h.hub.IsClientConnected(userID)
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "is_connected": isConnected, "timestamp": time.Now()})
}

// WebsocketNotificationRequest represents a WebSocket notification request
type WebsocketNotificationRequest struct {
	MessageType string                 `json:"message_type" binding:"required"`
	Data        map[string]interface{} `json:"data" binding:"required"`
}

// SendNotification sends a notification to a specific user.
// @Summary Send WebSocket notification
// @Description Sends a notification to a specific user via WebSocket
// @Tags WebSocket
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param userID path string true "User ID"
// @Param input body WebsocketNotificationRequest true "Notification details"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /ws/notify/{userID} [post]
func (h *WebSocketHandler) SendNotification(c *gin.Context) {
	userID := c.Param("userID")

	var request WebsocketNotificationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	msg := &websocket.Message{
		Type:       websocket.MessageType(request.MessageType),
		Sender:     "system",
		Recipients: []string{userID},
		Timestamp:  time.Now(),
		Data:       request.Data,
	}

	if err := h.sendToUser(c.Request.Context(), userID, msg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "notification sent", "user_id": userID})
}

// SendNotificationToAll broadcasts a notification to all connected clients.
// @Summary Broadcast WebSocket notification
// @Description Broadcasts a notification to all connected WebSocket clients
// @Tags WebSocket
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body WebsocketNotificationRequest true "Notification details"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /ws/notify-all [post]
func (h *WebSocketHandler) SendNotificationToAll(c *gin.Context) {
	var request WebsocketNotificationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	msg := &websocket.Message{Type: websocket.MessageType(request.MessageType), Sender: "system", Timestamp: time.Now(), Data: request.Data}
	if h.connectionSvc != nil {
		if err := h.connectionSvc.BroadcastToAll(c.Request.Context(), msg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		count, _ := h.connectionSvc.GetClientCount(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"message": "notification sent to all clients", "client_count": count})
		return
	}

	h.hub.BroadcastToAll(msg)
	c.JSON(http.StatusOK, gin.H{"message": "notification sent to all clients", "client_count": h.hub.GetClientCount()})
}

// GetConnectedUsers returns list of connected user IDs.
// @Summary List connected WebSocket users
// @Description Returns all connected WebSocket user IDs
// @Tags WebSocket
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /ws/users [get]
func (h *WebSocketHandler) GetConnectedUsers(c *gin.Context) {
	if h.connectionSvc != nil {
		users, err := h.connectionSvc.GetConnectedUsers(c.Request.Context())
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"connected_users": users, "count": len(users), "timestamp": time.Now()})
			return
		}
		h.log.Warn("failed to list connected users from connection service", "error", err)
	}

	users := h.hub.GetConnectedUsers()
	c.JSON(http.StatusOK, gin.H{"connected_users": users, "count": len(users), "timestamp": time.Now()})
}

// HealthCheck performs a health check on the WebSocket subsystem.
// @Summary WebSocket health check
// @Description Returns the health status of the WebSocket subsystem
// @Tags WebSocket
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /ws/health [get]
func (h *WebSocketHandler) HealthCheck(c *gin.Context) {
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

func (h *WebSocketHandler) sendToUser(ctx context.Context, userID string, msg *websocket.Message) error {
	if h.connectionSvc != nil {
		return h.connectionSvc.SendMessageToUser(ctx, userID, msg)
	}
	h.hub.BroadcastToUser(userID, msg)
	return nil
}

func contextBackground() context.Context {
	return context.Background()
}
