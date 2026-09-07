package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type notificationReader interface {
	List(ctx context.Context, businessID, userID string, limit int) ([]*models.Notification, error)
	MarkRead(ctx context.Context, id, businessID, userID string) (*models.Notification, error)
	MarkAllRead(ctx context.Context, businessID, userID string) (int64, error)
}

type NotificationHandler struct {
	service notificationReader
	log     *logger.Logger
}

func NewNotificationHandler(service *services.NotificationService, log *logger.Logger) *NotificationHandler {
	return &NotificationHandler{service: service, log: log.Named("notifications")}
}

func (h *NotificationHandler) List(c *gin.Context) {
	businessID, userID, ok := notificationRequestScope(c)
	if !ok {
		return
	}
	limit := 0
	if value := c.Query("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notification limit"})
			return
		}
		limit = parsed
	}
	notifications, err := h.service.List(c.Request.Context(), businessID, userID, limit)
	if err != nil {
		h.log.Error("failed to list notifications", "business_id", businessID, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list notifications"})
		return
	}
	c.JSON(http.StatusOK, notifications)
}

func (h *NotificationHandler) MarkRead(c *gin.Context) {
	businessID, userID, ok := notificationRequestScope(c)
	if !ok {
		return
	}
	notification, err := h.service.MarkRead(c.Request.Context(), c.Param("id"), businessID, userID)
	if err != nil {
		if errors.Is(err, interfaces.ErrNotificationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		h.log.Error("failed to mark notification read", "business_id", businessID, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark notification read"})
		return
	}
	c.JSON(http.StatusOK, notification)
}

func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	businessID, userID, ok := notificationRequestScope(c)
	if !ok {
		return
	}
	updated, err := h.service.MarkAllRead(c.Request.Context(), businessID, userID)
	if err != nil {
		h.log.Error("failed to mark all notifications read", "business_id", businessID, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark notifications read"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

func notificationRequestScope(c *gin.Context) (string, string, bool) {
	userID, ok := requireUserScope(c)
	if !ok {
		return "", "", false
	}
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return "", "", false
	}
	return businessID, userID, true
}
