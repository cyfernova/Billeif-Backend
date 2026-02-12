package handlers

import (
	"errors"
	"net/http"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// A2APushHandler handles A2A push notification configuration endpoints
type A2APushHandler struct {
	pushService *services.A2APushService
	log         *logger.Logger
}

// NewA2APushHandler creates a new push notification handler
func NewA2APushHandler(pushService *services.A2APushService, log *logger.Logger) *A2APushHandler {
	return &A2APushHandler{
		pushService: pushService,
		log:         log,
	}
}

// ConfigurePushRequest represents a request to configure push notifications
type ConfigurePushRequest struct {
	AgentID        string            `json:"agentId" binding:"required,uuid"`
	WebhookURL     string            `json:"webhookUrl" binding:"required,url"`
	Headers        map[string]string `json:"headers,omitempty"`
	Events         []string          `json:"events,omitempty"`
	Authentication *a2a.AuthConfig   `json:"authentication,omitempty"`
	ReturnSecret   bool              `json:"returnSecret,omitempty"`
}

// ConfigurePushResponse represents the response to a push configuration request
type ConfigurePushResponse struct {
	ID         string `json:"id"`
	WebhookURL string `json:"webhookUrl"`
	Secret     string `json:"secret,omitempty"`
	IsActive   bool   `json:"isActive"`
	Message    string `json:"message,omitempty"`
}

// ConfigurePush handles POST /a2a/v0.3/push/configure
// Configures a webhook for push notifications
func (h *A2APushHandler) ConfigurePush(c *gin.Context) {
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	businessID := middleware.GetEffectiveBusinessID(c)

	var req ConfigurePushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to parse push config request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format: " + err.Error(),
		})
		return
	}

	// Create push config
	config := &a2a.PushNotificationConfig{
		URL:            req.WebhookURL,
		Headers:        req.Headers,
		Events:         req.Events,
		Authentication: req.Authentication,
	}

	pushConfig, err := h.pushService.ConfigurePushForScope(c.Request.Context(), req.AgentID, userID, businessID, config)
	if err != nil {
		h.log.Error("failed to configure push", "error", err, "agent_id", req.AgentID)
		if errors.Is(err, services.ErrPushConfigNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Agent not found"})
			return
		}
		if errors.Is(err, services.ErrUnsafeWebhookURL) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to configure push notifications: " + err.Error(),
		})
		return
	}

	h.log.Info("push notifications configured", "config_id", pushConfig.ID, "agent_id", req.AgentID)

	secret := ""
	if req.ReturnSecret {
		secret = pushConfig.Secret
	}

	c.JSON(http.StatusOK, ConfigurePushResponse{
		ID:         pushConfig.ID,
		WebhookURL: pushConfig.WebhookURL,
		Secret:     secret,
		IsActive:   pushConfig.IsActive,
		Message:    "Push notifications configured successfully",
	})
}

// GetPushConfig handles GET /a2a/v0.3/push/config
// Returns push notification configurations for the agent
func (h *A2APushHandler) GetPushConfig(c *gin.Context) {
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	businessID := middleware.GetEffectiveBusinessID(c)
	agentID := c.Query("agent_id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "agent_id is required",
		})
		return
	}

	configs, err := h.pushService.GetPushConfigsByAgentForScope(c.Request.Context(), agentID, userID, businessID)
	if err != nil {
		h.log.Error("failed to get push configs", "error", err, "agent_id", agentID)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Push configurations not found",
		})
		return
	}

	// Map to response format (hide secrets)
	response := make([]map[string]interface{}, len(configs))
	for i, cfg := range configs {
		response[i] = map[string]interface{}{
			"id":            cfg.ID,
			"webhookUrl":    cfg.WebhookURL,
			"isActive":      cfg.IsActive,
			"failureCount":  cfg.FailureCount,
			"lastFailureAt": cfg.LastFailureAt,
			"lastSuccessAt": cfg.LastSuccessAt,
			"createdAt":     cfg.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"configs": response,
		"count":   len(configs),
	})
}

// GetPushConfigByID handles GET /a2a/v0.3/push/config/:id
// Returns a specific push notification configuration
func (h *A2APushHandler) GetPushConfigByID(c *gin.Context) {
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	businessID := middleware.GetEffectiveBusinessID(c)
	configID := c.Param("id")
	if configID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Config ID is required",
		})
		return
	}

	config, err := h.pushService.GetPushConfigForScope(c.Request.Context(), configID, userID, businessID)
	if err != nil {
		h.log.Error("failed to get push config", "error", err, "config_id", configID)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Push configuration not found",
		})
		return
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"id":            config.ID,
		"agentId":       config.AgentID,
		"webhookUrl":    config.WebhookURL,
		"isActive":      config.IsActive,
		"failureCount":  config.FailureCount,
		"lastFailureAt": config.LastFailureAt,
		"lastSuccessAt": config.LastSuccessAt,
		"createdAt":     config.CreatedAt,
	})
}

// DeletePushConfig handles DELETE /a2a/v0.3/push/config/:id
// Removes a push notification configuration
func (h *A2APushHandler) DeletePushConfig(c *gin.Context) {
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	businessID := middleware.GetEffectiveBusinessID(c)
	configID := c.Param("id")
	if configID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Config ID is required",
		})
		return
	}

	err := h.pushService.DeletePushConfigForScope(c.Request.Context(), configID, userID, businessID)
	if err != nil {
		h.log.Error("failed to delete push config", "error", err, "config_id", configID)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Push configuration not found",
		})
		return
	}

	h.log.Info("push config deleted", "config_id", configID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Push configuration deleted successfully",
	})
}

// TestPush handles POST /a2a/v0.3/push/test/:id
// Sends a test notification to verify webhook configuration
func (h *A2APushHandler) TestPush(c *gin.Context) {
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	businessID := middleware.GetEffectiveBusinessID(c)
	configID := c.Param("id")
	if configID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Config ID is required",
		})
		return
	}

	config, err := h.pushService.GetPushConfigForScope(c.Request.Context(), configID, userID, businessID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Push configuration not found",
		})
		return
	}

	// Send test notification
	testData := map[string]interface{}{
		"type":    "test",
		"message": "This is a test notification from A2A Push Service",
	}

	pushConfig := &a2a.PushNotificationConfig{
		URL: config.WebhookURL,
	}

	err = h.pushService.SendNotification(c.Request.Context(), pushConfig, "test", testData)
	if err != nil {
		h.log.Error("test push failed", "error", err, "config_id", configID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Test notification failed",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Test notification sent successfully",
	})
}
