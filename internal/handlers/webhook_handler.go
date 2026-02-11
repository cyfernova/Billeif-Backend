package handlers

import (
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"
	"net/http"

	"github.com/gin-gonic/gin"
)

type WebhookHandler struct {
	svc *services.WebhookService
	log *logger.Logger
}

func NewWebhookHandler(svc *services.WebhookService, log *logger.Logger) *WebhookHandler {
	return &WebhookHandler{svc: svc, log: log}
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
	var input services.CreateWebhookInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var webhook *models.Webhook
	webhook, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, webhook)
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
	id := c.Param("id")
	var webhook *models.Webhook
	webhook, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	c.JSON(http.StatusOK, webhook)
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
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	webhooks, err := h.svc.List(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": webhooks})
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
	id := c.Param("id")
	var input services.UpdateWebhookInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var webhook *models.Webhook
	webhook, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, webhook)
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
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
