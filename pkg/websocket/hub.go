package websocket

import (
	"sync"

	"invoice-backend/pkg/logger"
)

// Hub maintains active WebSocket client connections
type Hub struct {
	// Registered clients mapped by user ID
	clients map[string]*Client

	// Channel for registering new clients
	register chan *Client

	// Channel for unregistering clients
	unregister chan *Client

	// Channel for broadcasting messages to all clients
	broadcast chan *Message

	// Mutex for thread-safe client access
	mu sync.RWMutex

	// Logger
	log *logger.Logger

	// Stop channel for graceful shutdown
	stop chan struct{}
}

// NewHub creates a new WebSocket hub
func NewHub(log *logger.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
		broadcast:  make(chan *Message, 256),
		log:        log,
		stop:       make(chan struct{}),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	go func() {
		for {
			select {
			case client := <-h.register:
				h.registerClient(client)

			case client := <-h.unregister:
				h.unregisterClient(client)

			case message := <-h.broadcast:
				h.broadcastMessage(message)

			case <-h.stop:
				h.shutdown()
				return
			}
		}
	}()
}

// registerClient registers a new client connection
func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close any existing connection for this user
	if existing, ok := h.clients[client.UserID]; ok {
		h.log.Info("closing existing connection for user", "user_id", client.UserID)
		close(existing.send)
	}

	h.clients[client.UserID] = client
	h.log.Info("client registered", "user_id", client.UserID, "total_clients", len(h.clients))
}

// unregisterClient unregisters a client connection
func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.clients[client.UserID]; ok && existing == client {
		delete(h.clients, client.UserID)
		close(client.send)
		h.log.Info("client unregistered", "user_id", client.UserID, "total_clients", len(h.clients))
	}
}

// broadcastMessage sends a message to specific recipients
func (h *Hub) broadcastMessage(msg *Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(msg.Recipients) == 0 {
		// Broadcast to all clients
		for _, client := range h.clients {
			select {
			case client.send <- msg:
			default:
				h.log.Warn("message queue full for client", "user_id", client.UserID)
			}
		}
		return
	}

	// Send to specific recipients
	for _, userID := range msg.Recipients {
		if client, ok := h.clients[userID]; ok {
			select {
			case client.send <- msg:
			default:
				h.log.Warn("message queue full for client", "user_id", userID)
			}
		}
	}
}

// BroadcastToUser sends a message to a specific user
func (h *Hub) BroadcastToUser(userID string, msg *Message) {
	msg.Recipients = []string{userID}
	select {
	case h.broadcast <- msg:
	default:
		h.log.Warn("broadcast channel full")
	}
}

// BroadcastToAll sends a message to all connected clients
func (h *Hub) BroadcastToAll(msg *Message) {
	msg.Recipients = []string{}
	select {
	case h.broadcast <- msg:
	default:
		h.log.Warn("broadcast channel full")
	}
}

// BroadcastToUsers sends a message to multiple users
func (h *Hub) BroadcastToUsers(userIDs []string, msg *Message) {
	msg.Recipients = userIDs
	select {
	case h.broadcast <- msg:
	default:
		h.log.Warn("broadcast channel full")
	}
}

// GetClientCount returns the number of connected clients
func (h *Hub) GetClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// IsClientConnected checks if a specific user is connected
func (h *Hub) IsClientConnected(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[userID]
	return ok
}

// GetConnectedUsers returns list of connected user IDs
func (h *Hub) GetConnectedUsers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	users := make([]string, 0, len(h.clients))
	for userID := range h.clients {
		users = append(users, userID)
	}
	return users
}

// Shutdown gracefully closes all connections
func (h *Hub) Shutdown() {
	close(h.stop)
}

// shutdown closes all client connections
func (h *Hub) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for userID, client := range h.clients {
		close(client.send)
		h.log.Info("closing client connection during shutdown", "user_id", userID)
	}
	h.clients = make(map[string]*Client)
}

// RegisterClient exposes hub registration for testing
func (h *Hub) RegisterClient(client *Client) {
	h.register <- client
}

// UnregisterClient exposes hub unregistration for testing
func (h *Hub) UnregisterClient(client *Client) {
	h.unregister <- client
}
