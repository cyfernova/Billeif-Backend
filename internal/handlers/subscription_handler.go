package handlers

import (
	"errors"
	"invoice-backend/internal/models"
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type SubscriptionHandler struct {
	svc       *services.SubscriptionService
	lifecycle *services.SubscriptionLifecycleService
	log       *logger.Logger
}

func NewSubscriptionHandler(svc *services.SubscriptionService, log *logger.Logger, lifecycle ...*services.SubscriptionLifecycleService) *SubscriptionHandler {
	h := &SubscriptionHandler{svc: svc, log: log}
	if len(lifecycle) > 0 {
		h.lifecycle = lifecycle[0]
	}
	return h
}

type SubscriptionAPIError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func subscriptionAPIError(code, message string) SubscriptionAPIError {
	var response SubscriptionAPIError
	response.Error.Code, response.Error.Message = code, message
	return response
}

// Checkout starts a genuine renewable Razorpay subscription without granting paid access.
// @Summary Start renewable subscription checkout
// @Tags Subscriptions
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param input body services.StartRenewableSubscriptionInput true "Subscription checkout"
// @Success 200 {object} services.SubscriptionCheckoutResponse
// @Failure 400 {object} SubscriptionAPIError
// @Failure 409 {object} SubscriptionAPIError
// @Failure 422 {object} SubscriptionAPIError
// @Failure 503 {object} SubscriptionAPIError
// @Router /subscriptions/checkout [post]
func (h *SubscriptionHandler) Checkout(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.StartRenewableSubscriptionInput
	if h.lifecycle == nil || c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, subscriptionAPIError("subscription_invalid_request", "invalid subscription request"))
		return
	}
	response, err := h.lifecycle.StartRenewable(c.Request.Context(), businessID, userID, input)
	if err != nil {
		writeSubscriptionLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// ChangePlan schedules a no-proration plan change at the next verified billing boundary.
// @Summary Schedule subscription plan change
// @Tags Subscriptions
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param input body services.ChangeSubscriptionPlanInput true "Scheduled plan change"
// @Success 200 {object} services.SubscriptionMutationResponse
// @Failure 400 {object} SubscriptionAPIError
// @Failure 409 {object} SubscriptionAPIError
// @Failure 422 {object} SubscriptionAPIError
// @Failure 503 {object} SubscriptionAPIError
// @Router /subscriptions/plan-change [post]
func (h *SubscriptionHandler) ChangePlan(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ChangeSubscriptionPlanInput
	if h.lifecycle == nil || c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, subscriptionAPIError("subscription_invalid_request", "invalid subscription request"))
		return
	}
	response, err := h.lifecycle.SchedulePlanChange(c.Request.Context(), businessID, userID, input)
	if err != nil {
		writeSubscriptionLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// Cancel schedules cancellation at the verified end of the current paid period.
// @Summary Schedule subscription cancellation
// @Tags Subscriptions
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param input body services.ScheduleSubscriptionCancellationInput true "Scheduled cancellation"
// @Success 200 {object} services.SubscriptionMutationResponse
// @Failure 400 {object} SubscriptionAPIError
// @Failure 409 {object} SubscriptionAPIError
// @Failure 422 {object} SubscriptionAPIError
// @Failure 503 {object} SubscriptionAPIError
// @Router /subscriptions/cancellation [post]
func (h *SubscriptionHandler) Cancel(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ScheduleSubscriptionCancellationInput
	if h.lifecycle == nil || c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, subscriptionAPIError("subscription_invalid_request", "invalid subscription request"))
		return
	}
	response, err := h.lifecycle.ScheduleCancellation(c.Request.Context(), businessID, userID, input)
	if err != nil {
		writeSubscriptionLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// BillingHistory lists tenant-scoped immutable provider-verified billing receipts.
// @Summary List subscription billing history
// @Tags Subscriptions
// @Security BearerAuth
// @Produce json
// @Param limit query int false "Maximum records" default(50) minimum(1) maximum(100)
// @Success 200 {object} services.SubscriptionBillingHistoryResponse
// @Failure 400 {object} SubscriptionAPIError
// @Failure 500 {object} SubscriptionAPIError
// @Failure 503 {object} SubscriptionAPIError
// @Router /subscriptions/billing-history [get]
func (h *SubscriptionHandler) BillingHistory(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if h.lifecycle == nil {
		c.JSON(http.StatusServiceUnavailable, subscriptionAPIError("subscription_unavailable", "subscription service is temporarily unavailable"))
		return
	}
	limit, ok := subscriptionHistoryLimit(c.Query("limit"))
	if !ok {
		c.JSON(http.StatusBadRequest, subscriptionAPIError("subscription_invalid_limit", "subscription history limit must be between 1 and 100"))
		return
	}
	response, err := h.lifecycle.BillingHistory(c.Request.Context(), businessID, limit)
	if err != nil {
		writeSubscriptionLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// AuditHistory lists tenant-scoped sanitized lifecycle decisions.
// @Summary List subscription audit history
// @Tags Subscriptions
// @Security BearerAuth
// @Produce json
// @Param limit query int false "Maximum records" default(50) minimum(1) maximum(100)
// @Success 200 {object} services.SubscriptionAuditHistoryResponse
// @Failure 400 {object} SubscriptionAPIError
// @Failure 500 {object} SubscriptionAPIError
// @Failure 503 {object} SubscriptionAPIError
// @Router /subscriptions/audit [get]
func (h *SubscriptionHandler) AuditHistory(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if h.lifecycle == nil {
		c.JSON(http.StatusServiceUnavailable, subscriptionAPIError("subscription_unavailable", "subscription service is temporarily unavailable"))
		return
	}
	limit, ok := subscriptionHistoryLimit(c.Query("limit"))
	if !ok {
		c.JSON(http.StatusBadRequest, subscriptionAPIError("subscription_invalid_limit", "subscription history limit must be between 1 and 100"))
		return
	}
	response, err := h.lifecycle.AuditHistory(c.Request.Context(), businessID, limit)
	if err != nil {
		writeSubscriptionLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func subscriptionHistoryLimit(raw string) (int, bool) {
	if raw == "" {
		return 50, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		return 0, false
	}
	return limit, true
}

func writeSubscriptionLifecycleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrSubscriptionIdempotencyConflict), errors.Is(err, services.ErrSubscriptionLifecycleConflict):
		c.JSON(http.StatusConflict, subscriptionAPIError("subscription_conflict", "subscription request conflicts with current lifecycle state"))
	case errors.Is(err, services.ErrSubscriptionProviderRejected):
		c.JSON(http.StatusUnprocessableEntity, subscriptionAPIError("subscription_provider_rejected", "subscription provider rejected the request"))
	case errors.Is(err, services.ErrSubscriptionProviderUnknown):
		c.JSON(http.StatusServiceUnavailable, subscriptionAPIError("subscription_reconciliation_required", "subscription outcome requires reconciliation"))
	case errors.Is(err, services.ErrSubscriptionUnavailable):
		c.JSON(http.StatusServiceUnavailable, subscriptionAPIError("subscription_unavailable", "subscription service is temporarily unavailable"))
	case errors.Is(err, services.ErrSubscriptionInternal):
		c.JSON(http.StatusInternalServerError, subscriptionAPIError("subscription_internal_error", "subscription history could not be loaded"))
	default:
		c.JSON(http.StatusBadRequest, subscriptionAPIError("subscription_invalid_request", "subscription request could not be processed"))
	}
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
		log.Error("failed to get subscription", "code", "subscription_read_failed", "business_id", businessID)
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
