package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

const razorpayWebhookMaxBytes int64 = 1 << 20

type RazorpayPaymentHandler struct {
	svc *services.RazorpayPaymentService
	log *logger.Logger
}

func NewRazorpayPaymentHandler(svc *services.RazorpayPaymentService, log *logger.Logger) *RazorpayPaymentHandler {
	if log == nil {
		log = logger.Global()
	}
	return &RazorpayPaymentHandler{svc: svc, log: log.Named("razorpay_payments")}
}

// CreateOrder godoc
// @Summary Create Razorpay payment order
// @Description Creates a server-calculated Razorpay order for a plan or authenticated store order
// @Tags Payments
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body services.RazorpayCreateOrderInput true "Payment target"
// @Success 200 {object} services.RazorpayCreateOrderResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} CapabilityMutationError
// @Failure 409 {object} map[string]string
// @Failure 422 {object} CapabilityMutationError
// @Failure 429 {object} CapabilityMutationError
// @Failure 503 {object} CapabilityMutationError
// @Router /payments/razorpay/order [post]
func (h *RazorpayPaymentHandler) CreateOrder(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}

	var input services.RazorpayCreateOrderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	resp, err := h.svc.CreateOrder(c.Request.Context(), businessID, userID, input)
	if err != nil {
		if writeSubscriptionControlError(c, err) {
			return
		}
		status, message := paymentErrorResponse(err)
		h.log.Warn("create Razorpay order failed", "business_id", businessID, "user_id", userID, "target_type", input.TargetType, "status", status, "error", err)
		c.JSON(status, gin.H{"error": message})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// VerifyPayment godoc
// @Summary Verify Razorpay checkout payment
// @Description Verifies the Razorpay checkout signature and trusted provider payment status
// @Tags Payments
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body services.RazorpayVerifyPaymentInput true "Checkout verification payload"
// @Success 200 {object} services.RazorpayVerifyPaymentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 429 {object} map[string]string
// @Router /payments/razorpay/verify [post]
func (h *RazorpayPaymentHandler) VerifyPayment(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}

	var input services.RazorpayVerifyPaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	resp, err := h.svc.VerifyPayment(c.Request.Context(), businessID, userID, input)
	if err != nil {
		status, message := paymentErrorResponse(err)
		h.log.Warn("verify Razorpay payment failed", "business_id", businessID, "user_id", userID, "payment_attempt_id", input.PaymentAttemptID, "status", status, "error", err)
		c.JSON(status, gin.H{"error": message})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Webhook godoc
// @Summary Razorpay webhook
// @Description Verifies and processes Razorpay webhooks using raw body HMAC
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param X-Razorpay-Signature header string true "Razorpay signature"
// @Param x-razorpay-event-id header string true "Razorpay event id"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 429 {object} map[string]string
// @Router /webhooks/razorpay [post]
func (h *RazorpayPaymentHandler) Webhook(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, razorpayWebhookMaxBytes)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook body"})
		return
	}

	duplicate, err := h.svc.HandleWebhook(
		c.Request.Context(),
		c.GetHeader("X-Razorpay-Signature"),
		firstNonEmptyHeader(c, "x-razorpay-event-id", "X-Razorpay-Event-Id"),
		rawBody,
	)
	if err != nil {
		status, message := webhookErrorResponse(err)
		h.log.Warn("Razorpay webhook rejected", "status", status, "error", err)
		c.JSON(status, gin.H{"error": message})
		return
	}
	if duplicate {
		c.JSON(http.StatusOK, gin.H{"status": "duplicate"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "processed"})
}

func paymentErrorResponse(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "already paid"), strings.Contains(msg, "already used"):
		return http.StatusConflict, "payment request conflicts with an existing attempt"
	case strings.Contains(msg, "not configured"):
		return http.StatusServiceUnavailable, "payment provider is not configured"
	case strings.Contains(msg, "razorpay request failed"), strings.Contains(msg, "razorpay api returned"):
		return http.StatusBadGateway, "payment provider request failed"
	case strings.Contains(msg, "invalid payment signature"):
		return http.StatusBadRequest, "invalid payment verification"
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound, "payment target not found"
	default:
		return http.StatusBadRequest, "payment request could not be processed"
	}
}

func webhookErrorResponse(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid webhook signature"):
		return http.StatusUnauthorized, "invalid webhook signature"
	case strings.Contains(msg, "missing x-razorpay-event-id"):
		return http.StatusBadRequest, "missing webhook event id"
	case errors.Is(err, http.ErrBodyReadAfterClose):
		return http.StatusBadRequest, "invalid webhook body"
	default:
		return http.StatusBadRequest, "webhook could not be processed"
	}
}

func firstNonEmptyHeader(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.GetHeader(name)); value != "" {
			return value
		}
	}
	return ""
}
