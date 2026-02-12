package handlers

import (
	"invoice-backend/internal/models"
	"net/http"

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
	log := logger.FromContext(c.Request.Context()).Named("subscription_handler").With("operation", "create")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid create subscription payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID

	var subscription *models.Subscription
	subscription, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		log.Error("failed to create subscription", "error", err, "business_id", input.BusinessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("subscription created", "subscription_id", subscription.ID, "business_id", subscription.BusinessID)

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
	log := logger.FromContext(c.Request.Context()).Named("subscription_handler").With("operation", "get")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	var subscription *models.Subscription
	subscription, err := h.svc.GetByBusinessID(c.Request.Context(), businessID)
	if err != nil {
		log.Error("failed to get subscription", "error", err, "business_id", businessID)
		c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
		return
	}
	log.Debug("subscription fetched", "subscription_id", subscription.ID, "business_id", businessID)

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
	log := logger.FromContext(c.Request.Context()).Named("subscription_handler").With("operation", "update")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	var input services.UpdateSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid update subscription payload", "error", err, "business_id", businessID)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var subscription *models.Subscription
	subscription, err := h.svc.Update(c.Request.Context(), businessID, input)
	if err != nil {
		log.Error("failed to update subscription", "error", err, "business_id", businessID)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("subscription updated", "subscription_id", subscription.ID, "business_id", businessID)

	c.JSON(http.StatusOK, subscription)
}
