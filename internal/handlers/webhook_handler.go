package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type WebhookHandler struct {
	svc *services.WebhookService
	log *logger.Logger
}

func NewWebhookHandler(svc *services.WebhookService, log *logger.Logger) *WebhookHandler {
	return &WebhookHandler{svc: svc, log: log}
}

func (h *WebhookHandler) Create(c *gin.Context) {
	var input services.CreateWebhookInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	webhook, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, webhook)
}

func (h *WebhookHandler) Get(c *gin.Context) {
	id := c.Param("id")
	webhook, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	c.JSON(http.StatusOK, webhook)
}

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

func (h *WebhookHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdateWebhookInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	webhook, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, webhook)
}

func (h *WebhookHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
