package handlers

import (
	"net/http"

	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type CredentialHandler struct {
	svc     *services.CredentialProviderService
	ap2Repo interfaces.AP2Repository
	log     *logger.Logger
	cfg     *config.Config
}

func NewCredentialHandler(
	svc *services.CredentialProviderService,
	ap2Repo interfaces.AP2Repository,
	cfg *config.Config,
	log *logger.Logger,
) *CredentialHandler {
	return &CredentialHandler{
		svc:     svc,
		ap2Repo: ap2Repo,
		log:     log,
		cfg:     cfg,
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
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "add_payment_method")
	userID := c.GetString("user_id")

	var req AddPaymentMethodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warn("invalid add payment method payload", "error", err)
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
		log.Error("failed to add payment method", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("payment method added", "credential_id", credential.ID)

	c.JSON(http.StatusCreated, credential)
}

func (h *CredentialHandler) ListPaymentMethods(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "list_payment_methods")
	userID := c.GetString("user_id")

	credentials, err := h.svc.GetPaymentMethods(c.Request.Context(), userID)
	if err != nil {
		log.Error("failed to list payment methods", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("payment methods listed", "count", len(credentials))

	c.JSON(http.StatusOK, credentials)
}

func (h *CredentialHandler) GetPaymentMethod(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "get_payment_method")
	id := c.Param("id")
	userID := c.GetString("user_id")

	credential, err := h.svc.GetPaymentMethodByID(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get payment method", "error", err, "credential_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "payment method not found"})
		return
	}

	if credential.UserID != userID {
		log.Warn("payment method access denied", "credential_id", id, "user_id", userID)
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized"})
		return
	}

	c.JSON(http.StatusOK, credential)
}

func (h *CredentialHandler) SetDefaultPaymentMethod(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "set_default_payment_method")
	id := c.Param("id")
	userID := c.GetString("user_id")

	if err := h.svc.SetDefaultPaymentMethod(c.Request.Context(), userID, id); err != nil {
		log.Error("failed to set default payment method", "error", err, "credential_id", id, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("default payment method updated", "credential_id", id, "user_id", userID)

	c.JSON(http.StatusOK, gin.H{"message": "default payment method updated"})
}

func (h *CredentialHandler) DeletePaymentMethod(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "delete_payment_method")
	id := c.Param("id")
	userID := c.GetString("user_id")

	if err := h.svc.DeletePaymentMethod(c.Request.Context(), userID, id); err != nil {
		log.Error("failed to delete payment method", "error", err, "credential_id", id, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("payment method deleted", "credential_id", id, "user_id", userID)

	c.JSON(http.StatusNoContent, nil)
}

func (h *CredentialHandler) GenerateToken(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "generate_token")
	userID := c.GetString("user_id")

	var req struct {
		CredentialID     string `json:"credential_id" binding:"required"`
		PaymentMandateID string `json:"payment_mandate_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warn("invalid credential token request payload", "error", err)
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
		log.Error("failed to generate credential token", "error", err, "user_id", userID, "credential_id", req.CredentialID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("credential token generated", "token_id", token.ID)

	c.JSON(http.StatusCreated, token)
}

func (h *CredentialHandler) GetDefaultPaymentMethod(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "get_default_payment_method")
	userID := c.GetString("user_id")

	credential, err := h.svc.GetDefaultPaymentMethod(c.Request.Context(), userID)
	if err != nil {
		log.Error("failed to get default payment method", "error", err, "user_id", userID)
		c.JSON(http.StatusNotFound, gin.H{"error": "default payment method not found"})
		return
	}

	c.JSON(http.StatusOK, credential)
}

func (h *CredentialHandler) ValidateToken(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "validate_token")
	token := c.Query("token")

	if token == "" {
		log.Warn("missing token query param")
		c.JSON(http.StatusBadRequest, gin.H{"error": "token parameter is required"})
		return
	}

	credentialToken, err := h.svc.ValidateToken(c.Request.Context(), token)
	if err != nil {
		log.Warn("credential token validation failed", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}
	log.Debug("credential token validated", "token_id", credentialToken.ID)

	c.JSON(http.StatusOK, credentialToken)
}
