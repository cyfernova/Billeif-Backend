package handlers

import (
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type WebhookHandler struct {
	svc *services.WebhookService
	log *logger.Logger
}

func NewWebhookHandler(svc *services.WebhookService, log *logger.Logger) *WebhookHandler {
	return &WebhookHandler{svc: svc, log: log}
}

type webhookResponse struct {
	ID            string     `json:"id"`
	BusinessID    string     `json:"business_id"`
	Name          string     `json:"name"`
	URL           string     `json:"url"`
	Events        []string   `json:"events"`
	IsActive      bool       `json:"is_active"`
	LastTriggered *time.Time `json:"last_triggered,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Create creates a new webhook subscription
// @Summary Create webhook
// @Description Register a new webhook URL for event notifications.
// @Tags Webhooks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateWebhookInput true "Webhook details"
// @Success 201 {object} models.Webhook
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /webhooks [post]
func (h *WebhookHandler) Create(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("webhook_handler").With("operation", "create")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateWebhookInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid create webhook payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID

	webhook, err := h.svc.CreateByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		log.Error("failed to create webhook", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("webhook created", "webhook_id", webhook.ID, "business_id", webhook.BusinessID)

	c.JSON(http.StatusCreated, toWebhookResponse(webhook))
}

// Get retrieves a webhook by ID
// @Summary Get webhook
// @Description Returns the details of a specific webhook subscription.
// @Tags Webhooks
// @Produce json
// @Security BearerAuth
// @Param id path string true "Webhook ID"
// @Success 200 {object} models.Webhook
// @Failure 404 {object} map[string]string
// @Router /webhooks/{id} [get]
func (h *WebhookHandler) Get(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("webhook_handler").With("operation", "get")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	webhook, err := h.svc.GetByBusiness(c.Request.Context(), businessID, id)
	if err != nil {
		log.Error("failed to get webhook", "error", err, "webhook_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	c.JSON(http.StatusOK, toWebhookResponse(webhook))
}

// List retrieves all webhooks for a business
// @Summary List webhooks
// @Description Returns a list of webhook subscriptions for a specific business.
// @Tags Webhooks
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /webhooks [get]
func (h *WebhookHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("webhook_handler").With("operation", "list")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	webhooks, err := h.svc.List(c.Request.Context(), businessID)
	if err != nil {
		log.Error("failed to list webhooks", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("webhooks listed", "business_id", businessID, "count", len(webhooks))

	response := make([]webhookResponse, 0, len(webhooks))
	for _, webhook := range webhooks {
		response = append(response, toWebhookResponse(webhook))
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

// Update updates a webhook subscription
// @Summary Update webhook
// @Description Update the details of a specific webhook subscription.
// @Tags Webhooks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Webhook ID"
// @Param input body services.UpdateWebhookInput true "Webhook updates"
// @Success 200 {object} models.Webhook
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /webhooks/{id} [put]
func (h *WebhookHandler) Update(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("webhook_handler").With("operation", "update")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var input services.UpdateWebhookInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid update webhook payload", "error", err, "webhook_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	webhook, err := h.svc.UpdateByBusiness(c.Request.Context(), businessID, id, input)
	if err != nil {
		log.Error("failed to update webhook", "error", err, "webhook_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("webhook updated", "webhook_id", webhook.ID)

	c.JSON(http.StatusOK, toWebhookResponse(webhook))
}

// Delete removes a webhook subscription
// @Summary Delete webhook
// @Description Remove a specific webhook subscription.
// @Tags Webhooks
// @Produce json
// @Security BearerAuth
// @Param id path string true "Webhook ID"
// @Success 204 "No Content"
// @Failure 500 {object} map[string]string
// @Router /webhooks/{id} [delete]
func (h *WebhookHandler) Delete(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("webhook_handler").With("operation", "delete")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	if err := h.svc.DeleteByBusiness(c.Request.Context(), businessID, id); err != nil {
		log.Error("failed to delete webhook", "error", err, "webhook_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("webhook deleted", "webhook_id", id)

	c.JSON(http.StatusNoContent, nil)
}

func toWebhookResponse(webhook *models.Webhook) webhookResponse {
	events := []string{}
	if webhook.Events != "" {
		events = strings.Split(webhook.Events, ",")
	}
	return webhookResponse{
		ID:            webhook.ID,
		BusinessID:    webhook.BusinessID,
		Name:          webhook.Name,
		URL:           webhook.URL,
		Events:        events,
		IsActive:      webhook.IsActive,
		LastTriggered: webhook.LastTriggered,
		CreatedAt:     webhook.CreatedAt,
		UpdatedAt:     webhook.UpdatedAt,
	}
}
