package handlers

import (
	"errors"
	"net/http"

	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/razorpay"

	"github.com/gin-gonic/gin"
)

type CredentialHandler struct {
	svc         *services.CredentialProviderService
	ap2Repo     interfaces.AP2Repository
	razorpaySvc *razorpay.RazorpayService
	log         *logger.Logger
	cfg         *config.Config
}

func NewCredentialHandler(
	svc *services.CredentialProviderService,
	ap2Repo interfaces.AP2Repository,
	razorpaySvc *razorpay.RazorpayService,
	cfg *config.Config,
	log *logger.Logger,
) *CredentialHandler {
	return &CredentialHandler{
		svc:         svc,
		ap2Repo:     ap2Repo,
		razorpaySvc: razorpaySvc,
		log:         log,
		cfg:         cfg,
	}
}

type AddPaymentMethodRequest struct {
	CredentialType     string `json:"credential_type" binding:"required"`
	RazorpayCustomerID string `json:"razorpay_customer_id" binding:"required"`
	MaskedCardNumber   string `json:"masked_card_number" binding:"required"`
	CardBrand          string `json:"card_brand" binding:"required"`
	CardToken          string `json:"card_token" binding:"required"`
	IsDefault          bool   `json:"is_default"`
}

func (h *CredentialHandler) AddPaymentMethod(c *gin.Context) {
	userID := c.GetString("user_id")

	var req AddPaymentMethodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	addReq := &services.AddPaymentMethodRequest{
		UserID:             userID,
		CredentialType:     req.CredentialType,
		RazorpayCustomerID: req.RazorpayCustomerID,
		MaskedCardNumber:   req.MaskedCardNumber,
		CardBrand:          req.CardBrand,
		CardToken:          req.CardToken,
		IsDefault:          req.IsDefault,
	}

	credential, err := h.svc.AddPaymentMethod(c.Request.Context(), addReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, credential)
}

func (h *CredentialHandler) ListPaymentMethods(c *gin.Context) {
	userID := c.GetString("user_id")

	credentials, err := h.svc.GetPaymentMethods(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, credentials)
}

func (h *CredentialHandler) GetPaymentMethod(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	credential, err := h.svc.GetPaymentMethodByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "payment method not found"})
		return
	}

	if credential.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized"})
		return
	}

	c.JSON(http.StatusOK, credential)
}

func (h *CredentialHandler) SetDefaultPaymentMethod(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	if err := h.svc.SetDefaultPaymentMethod(c.Request.Context(), userID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "default payment method updated"})
}

func (h *CredentialHandler) DeletePaymentMethod(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	if err := h.svc.DeletePaymentMethod(c.Request.Context(), userID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *CredentialHandler) GenerateToken(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		CredentialID     string `json:"credential_id" binding:"required"`
		PaymentMandateID string `json:"payment_mandate_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokenReq := &services.GenerateTokenRequest{
		CredentialID:     req.CredentialID,
		PaymentMandateID: req.PaymentMandateID,
		UserID:           userID,
	}

	token, err := h.svc.GenerateCredentialToken(c.Request.Context(), tokenReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, token)
}

func (h *CredentialHandler) GetDefaultPaymentMethod(c *gin.Context) {
	userID := c.GetString("user_id")

	credential, err := h.svc.GetDefaultPaymentMethod(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "default payment method not found"})
		return
	}

	c.JSON(http.StatusOK, credential)
}

func (h *CredentialHandler) ValidateToken(c *gin.Context) {
	token := c.Query("token")

	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token parameter is required"})
		return
	}

	credentialToken, err := h.svc.ValidateToken(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	c.JSON(http.StatusOK, credentialToken)
}

func (h *CredentialHandler) HandleRazorpayWebhook(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	signature := c.GetHeader("X-Razorpay-Signature")
	if signature == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "signature missing"})
		return
	}

	if !h.razorpaySvc.VerifyWebhookSignature(c.Request.Context(), &razorpay.VerifyWebhookRequest{
		RawBody:   body,
		Signature: signature,
	}) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	var event map[string]interface{}
	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	eventType, ok := event["event"].(string)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event type"})
		return
	}

	switch eventType {
	case "payment.captured":
		if err := h.handlePaymentCaptured(c, event); err != nil {
			h.log.Error("failed to handle payment.captured", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process webhook"})
			return
		}
	case "payment.failed":
		if err := h.handlePaymentFailed(c, event); err != nil {
			h.log.Error("failed to handle payment.failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process webhook"})
			return
		}
	case "payment.refunded":
		if err := h.handlePaymentRefunded(c, event); err != nil {
			h.log.Error("failed to handle payment.refunded", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process webhook"})
			return
		}
	case "order.paid":
		if err := h.handleOrderPaid(c, event); err != nil {
			h.log.Error("failed to handle order.paid", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process webhook"})
			return
		}
	default:
		h.log.Info("unhandled webhook event", "event_type", eventType)
	}

	c.JSON(http.StatusOK, gin.H{"message": "webhook processed successfully"})
}

func (h *CredentialHandler) handlePaymentCaptured(c *gin.Context, event map[string]interface{}) error {
	payload, ok := event["payload"].(map[string]interface{})
	if !ok {
		return errors.New("invalid payload structure")
	}

	paymentEntity, ok := payload["payment"].(map[string]interface{})
	if !ok {
		return errors.New("invalid payment entity")
	}

	paymentID, _ := paymentEntity["id"].(string)
	amount, _ := paymentEntity["amount"].(float64)
	currency, _ := paymentEntity["currency"].(string)
	status, _ := paymentEntity["status"].(string)
	orderID, _ := paymentEntity["order_id"].(string)

	h.log.Info("payment captured",
		"payment_id", paymentID,
		"order_id", orderID,
		"amount", amount,
		"currency", currency,
		"status", status,
	)

	order, err := h.ap2Repo.GetOrderByID(c.Request.Context(), orderID)
	if err != nil {
		h.log.Error("order not found", "order_id", orderID, "error", err)
		return err
	}

	if status == "captured" {
		order.Status = "paid"
		if err := h.ap2Repo.UpdateOrder(c.Request.Context(), order); err != nil {
			return err
		}

		cartMandate, err := h.ap2Repo.GetCartMandateByID(c.Request.Context(), order.CartMandateID)
		if err == nil {
			cartMandate.Status = "completed"
			_ = h.ap2Repo.UpdateCartMandate(c.Request.Context(), cartMandate)
		}
	}

	return nil
}

func (h *CredentialHandler) handlePaymentFailed(c *gin.Context, event map[string]interface{}) error {
	payload, ok := event["payload"].(map[string]interface{})
	if !ok {
		return errors.New("invalid payload structure")
	}

	paymentEntity, ok := payload["payment"].(map[string]interface{})
	if !ok {
		return errors.New("invalid payment entity")
	}

	paymentID, _ := paymentEntity["id"].(string)
	orderID, _ := paymentEntity["order_id"].(string)
	errorCode, _ := paymentEntity["error_code"].(string)
	errorDescription, _ := paymentEntity["error_description"].(string)

	h.log.Error("payment failed",
		"payment_id", paymentID,
		"order_id", orderID,
		"error_code", errorCode,
		"error_description", errorDescription,
	)

	order, err := h.ap2Repo.GetOrderByID(c.Request.Context(), orderID)
	if err == nil {
		order.Status = "failed"
		_ = h.ap2Repo.UpdateOrder(c.Request.Context(), order)
	}

	return nil
}

func (h *CredentialHandler) handlePaymentRefunded(c *gin.Context, event map[string]interface{}) error {
	payload, ok := event["payload"].(map[string]interface{})
	if !ok {
		return errors.New("invalid payload structure")
	}

	paymentEntity, ok := payload["payment"].(map[string]interface{})
	if !ok {
		return errors.New("invalid payment entity")
	}

	paymentID, _ := paymentEntity["id"].(string)
	amount, _ := paymentEntity["amount_refunded"].(float64)

	h.log.Info("payment refunded",
		"payment_id", paymentID,
		"amount", amount,
	)

	return nil
}

func (h *CredentialHandler) handleOrderPaid(c *gin.Context, event map[string]interface{}) error {
	payload, ok := event["payload"].(map[string]interface{})
	if !ok {
		return errors.New("invalid payload structure")
	}

	orderEntity, ok := payload["order"].(map[string]interface{})
	if !ok {
		return errors.New("invalid order entity")
	}

	orderID, _ := orderEntity["id"].(string)
	status, _ := orderEntity["status"].(string)

	if status == "paid" {
		order, err := h.ap2Repo.GetOrderByID(c.Request.Context(), orderID)
		if err != nil {
			return err
		}
		order.Status = "paid"
		return h.ap2Repo.UpdateOrder(c.Request.Context(), order)
	}

	return nil
}
