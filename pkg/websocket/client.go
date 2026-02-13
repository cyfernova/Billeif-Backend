package websocket

import (
	"encoding/json"
	"fmt"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period
	pingInterval = (pongWait * 9) / 10

	// Maximum message size
	maxMessageSize = 512 * 1024 // 512 KB
)

// Client represents a WebSocket client connection
type Client struct {
	// User ID
	UserID string

	// WebSocket connection
	conn *websocket.Conn

	// Hub
	hub *Hub

	// Buffered channel of outbound messages
	send chan *Message

	// Logger
	log *logger.Logger

	// Client metadata
	connectedAt time.Time
	lastPingAt  time.Time
}

// NewClient creates a new WebSocket client
func NewClient(userID string, conn *websocket.Conn, hub *Hub, log *logger.Logger) *Client {
	return &Client{
		UserID:      userID,
		conn:        conn,
		hub:         hub,
		send:        make(chan *Message, 256),
		log:         log,
		connectedAt: time.Now(),
		lastPingAt:  time.Now(),
	}
}

// ReadPump reads messages from the WebSocket connection
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.lastPingAt = time.Now()
		return nil
	})

	for {
		var msg Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.log.Error("websocket error", "user_id", c.UserID, "error", err)
			}
			break
		}

		// Set sender
		msg.Sender = c.UserID
		msg.Timestamp = time.Now()

		c.log.Debug("message received", "user_id", c.UserID, "message_type", msg.Type)

		// Route message based on type
		c.handleMessage(&msg)
	}
}

// WritePump writes messages to the WebSocket connection
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(msg); err != nil {
				c.log.Error("failed to write message", "user_id", c.UserID, "error", err)
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.log.Error("failed to send ping", "user_id", c.UserID, "error", err)
				return
			}
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case MessageTypeHeartbeat:
		c.handleHeartbeat(msg)

	case MessageTypeOrderStatusQuery:
		c.handleOrderStatusQuery(msg)

	case MessageTypeAgentAction:
		c.handleAgentAction(msg)

	case MessageTypePaymentUpdate:
		c.handlePaymentUpdate(msg)

	case MessageTypeTaskUpdate:
		c.handleTaskUpdate(msg)

	default:
		c.log.Warn("unknown message type", "user_id", c.UserID, "message_type", msg.Type)
	}
}

// handleHeartbeat handles heartbeat messages
func (c *Client) handleHeartbeat(msg *Message) {
	response := &Message{
		Type:      MessageTypeHeartbeat,
		Sender:    "system",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"connected_at": c.connectedAt,
			"client_time":  msg.Timestamp,
		},
	}

	select {
	case c.send <- response:
	default:
		c.log.Warn("send channel full", "user_id", c.UserID)
	}
}

// handleOrderStatusQuery handles order status queries
func (c *Client) handleOrderStatusQuery(msg *Message) {
	c.log.Debug("received order status query", "user_id", c.UserID, "data", msg.Data)

	// In production, fetch actual order status from database
	// For now, return mock response
	response := &Message{
		Type:      MessageTypeOrderStatusQuery,
		Sender:    "system",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"status":             "delivered",
			"estimated_delivery": time.Now().Add(2 * 24 * time.Hour),
		},
	}

	select {
	case c.send <- response:
	default:
		c.log.Warn("send channel full", "user_id", c.UserID)
	}
}

// handleAgentAction handles agent action messages
func (c *Client) handleAgentAction(msg *Message) {
	c.log.Debug("received agent action", "user_id", c.UserID, "data", msg.Data)

	// Broadcast to relevant clients
	c.hub.BroadcastToUser(c.UserID, msg)
}

// handlePaymentUpdate handles payment update messages
func (c *Client) handlePaymentUpdate(msg *Message) {
	c.log.Debug("received payment update", "user_id", c.UserID, "data", msg.Data)

	select {
	case c.send <- msg:
	default:
		c.log.Warn("send channel full", "user_id", c.UserID)
	}
}

// handleTaskUpdate handles task update messages
func (c *Client) handleTaskUpdate(msg *Message) {
	c.log.Debug("received task update", "user_id", c.UserID, "data", msg.Data)

	select {
	case c.send <- msg:
	default:
		c.log.Warn("send channel full", "user_id", c.UserID)
	}
}

// SendMessage sends a message to the client
func (c *Client) SendMessage(msg *Message) error {
	select {
	case c.send <- msg:
		return nil
	default:
		return fmt.Errorf("send channel full for user %s", c.UserID)
	}
}

// SendJSON sends JSON data to the client
func (c *Client) SendJSON(msgType MessageType, data interface{}) error {
	msg := &Message{
		Type:      msgType,
		Sender:    "system",
		Timestamp: time.Now(),
		Data:      data,
	}
	return c.SendMessage(msg)
}

// GetConnectionDuration returns how long the client has been connected
func (c *Client) GetConnectionDuration() time.Duration {
	return time.Since(c.connectedAt)
}

// GetLastActivity returns the time of last activity
func (c *Client) GetLastActivity() time.Time {
	if c.lastPingAt.After(c.connectedAt) {
		return c.lastPingAt
	}
	return c.connectedAt
}

// IsAlive checks if the client is still connected
func (c *Client) IsAlive() bool {
	return time.Since(c.GetLastActivity()) < pongWait
}

// Close closes the client connection
func (c *Client) Close() error {
	return c.conn.Close()
}

// Message represents a WebSocket message
type Message struct {
	// Message type
	Type MessageType `json:"type"`

	// Sender (user ID or system)
	Sender string `json:"sender"`

	// Recipients (empty = broadcast to all)
	Recipients []string `json:"recipients,omitempty"`

	// Timestamp
	Timestamp time.Time `json:"timestamp"`

	// Message data
	Data interface{} `json:"data"`
}

// MessageType represents the type of WebSocket message
type MessageType string

const (
	// System messages
	MessageTypeHeartbeat MessageType = "heartbeat"

	// Order messages
	MessageTypeOrderStatusQuery  MessageType = "order_status_query"
	MessageTypeOrderStatusUpdate MessageType = "order_status_update"
	MessageTypeOrderCreated      MessageType = "order_created"

	// Payment messages
	MessageTypePaymentUpdate  MessageType = "payment_update"
	MessageTypePaymentSuccess MessageType = "payment_success"
	MessageTypePaymentFailed  MessageType = "payment_failed"

	// Agent messages
	MessageTypeAgentAction  MessageType = "agent_action"
	MessageTypeAgentProduct MessageType = "agent_product_found"

	// Task messages
	MessageTypeTaskUpdate   MessageType = "task_update"
	MessageTypeTaskComplete MessageType = "task_complete"
	MessageTypeTaskError    MessageType = "task_error"
)

// OrderStatusUpdate represents an order status update message
type OrderStatusUpdate struct {
	OrderID           string     `json:"order_id"`
	Status            string     `json:"status"`
	UpdatedAt         time.Time  `json:"updated_at"`
	Message           string     `json:"message,omitempty"`
	EstimatedDelivery *time.Time `json:"estimated_delivery,omitempty"`
}

// PaymentUpdateData represents a payment update message
type PaymentUpdateData struct {
	OrderID    string    `json:"order_id"`
	Amount     float64   `json:"amount"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	RazorpayID string    `json:"razorpay_id,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// AgentActionData represents an agent action message
type AgentActionData struct {
	AgentID string                 `json:"agent_id"`
	Action  string                 `json:"action"`
	Data    map[string]interface{} `json:"data"`
	Status  string                 `json:"status"`
}

// TaskUpdateData represents a task update message
type TaskUpdateData struct {
	TaskID    string                 `json:"task_id"`
	Status    string                 `json:"status"`
	Progress  int                    `json:"progress"`
	Message   string                 `json:"message,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// NewOrderStatusUpdateMessage creates a new order status update message
func NewOrderStatusUpdateMessage(userID string, update *OrderStatusUpdate) *Message {
	return &Message{
		Type:       MessageTypeOrderStatusUpdate,
		Sender:     "system",
		Recipients: []string{userID},
		Timestamp:  time.Now(),
		Data:       update,
	}
}

// NewPaymentUpdateMessage creates a new payment update message
func NewPaymentUpdateMessage(userID string, update *PaymentUpdateData) *Message {
	return &Message{
		Type:       MessageTypePaymentUpdate,
		Sender:     "system",
		Recipients: []string{userID},
		Timestamp:  time.Now(),
		Data:       update,
	}
}

// NewAgentActionMessage creates a new agent action message
func NewAgentActionMessage(userID string, action *AgentActionData) *Message {
	return &Message{
		Type:       MessageTypeAgentAction,
		Sender:     "system",
		Recipients: []string{userID},
		Timestamp:  time.Now(),
		Data:       action,
	}
}

// NewTaskUpdateMessage creates a new task update message
func NewTaskUpdateMessage(userID string, update *TaskUpdateData) *Message {
	return &Message{
		Type:       MessageTypeTaskUpdate,
		Sender:     "system",
		Recipients: []string{userID},
		Timestamp:  time.Now(),
		Data:       update,
	}
}

// MarshalJSON custom JSON marshaling for Message
func (m *Message) MarshalJSON() ([]byte, error) {
	type Alias Message
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}
