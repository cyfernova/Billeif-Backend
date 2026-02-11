package handlers

import (
	"invoice-backend/internal/models"
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

// Create creates a new subscription for a business
// @Summary Create subscription
// @Description Create a new subscription plan for a business.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateSubscriptionInput true "Subscription details"
// @Success 201 {object} models.Subscription
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /subscriptions [post]
func (h *SubscriptionHandler) Create(c *gin.Context) {
	var input services.CreateSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var subscription *models.Subscription
	subscription, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, subscription)
}

// Get retrieves the current subscription for a business
// @Summary Get subscription
// @Description Returns the subscription details for the authenticated business.
// @Tags Subscriptions
// @Produce json
// @Security BearerAuth
// @Param business_id query string false "Business ID (if not in token)"
// @Success 200 {object} models.Subscription
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /subscriptions [get]
func (h *SubscriptionHandler) Get(c *gin.Context) {
	businessID := middleware.GetBusinessID(c)
	if businessID == "" {
		businessID = c.Query("business_id")
	}
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	var subscription *models.Subscription
	subscription, err := h.svc.GetByBusinessID(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
		return
	}

	c.JSON(http.StatusOK, subscription)
}

// Update updates the subscription for a business
// @Summary Update subscription
// @Description Update the subscription plan or status for a business.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param business_id query string false "Business ID (if not in token)"
// @Param input body services.UpdateSubscriptionInput true "Subscription updates"
// @Success 200 {object} models.Subscription
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /subscriptions [put]
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

	var subscription *models.Subscription
	subscription, err := h.svc.Update(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, subscription)
}
