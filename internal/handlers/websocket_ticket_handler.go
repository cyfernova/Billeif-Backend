package handlers

import (
	"context"
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type websocketTicketIssuer interface {
	Issue(ctx context.Context, subject, businessID string) (*services.WebSocketTicketIssue, error)
}

type WebSocketTicketHandler struct {
	service websocketTicketIssuer
	log     *logger.Logger
}

func NewWebSocketTicketHandler(service *services.WebSocketTicketService, log *logger.Logger) *WebSocketTicketHandler {
	return &WebSocketTicketHandler{service: service, log: log.Named("websocket_tickets")}
}

func (h *WebSocketTicketHandler) Issue(c *gin.Context) {
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "websocket tickets are unavailable"})
		return
	}

	issued, err := h.service.Issue(c.Request.Context(), userID, businessID)
	if err != nil {
		h.log.Error("failed to issue websocket ticket", "user_id", userID, "business_id", businessID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue websocket ticket"})
		return
	}
	c.JSON(http.StatusCreated, issued)
}
