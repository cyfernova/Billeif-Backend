package handlers

import (
	"net/http"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type SubscriptionHandler struct {
	svc *services.SubscriptionService
	log *logger.Logger
}

func NewSubscriptionHandler(svc *services.SubscriptionService, log *logger.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{svc: svc, log: log}
}

func (h *SubscriptionHandler) Create(c *gin.Context) {
	var input services.CreateSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	subscription, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, subscription)
}

func (h *SubscriptionHandler) Get(c *gin.Context) {
	businessID := middleware.GetBusinessID(c)
	if businessID == "" {
		businessID = c.Query("business_id")
	}
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	subscription, err := h.svc.GetByBusinessID(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
		return
	}

	c.JSON(http.StatusOK, subscription)
}

func (h *SubscriptionHandler) Update(c *gin.Context) {
	businessID := middleware.GetBusinessID(c)
	if businessID == "" {
		businessID = c.Query("business_id")
	}
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	var input services.UpdateSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	subscription, err := h.svc.Update(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, subscription)
}
