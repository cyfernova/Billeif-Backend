package handlers

import (
	"net/http"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
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

type PaymentMethodResponse struct {
	ID                 string     `json:"id"`
	CredentialType     string     `json:"credential_type"`
	RazorpayCustomerID *string    `json:"razorpay_customer_id,omitempty"`
	MaskedCardNumber   *string    `json:"masked_card_number,omitempty"`
	CardBrand          *string    `json:"card_brand,omitempty"`
	IsDefault          bool       `json:"is_default"`
	IsActive           bool       `json:"is_active"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type CredentialTokenResponse struct {
	ID               string    `json:"id"`
	CredentialID     string    `json:"credential_id"`
	PaymentMandateID *string   `json:"payment_mandate_id,omitempty"`
	Token            string    `json:"token,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
	IsUsed           bool      `json:"is_used"`
	CreatedAt        time.Time `json:"created_at"`
}

// AddPaymentMethod adds a new payment method for the user
// @Summary Add payment method
// @Description Adds a new payment method (card) to the user's account
// @Tags Credentials
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body AddPaymentMethodRequest true "Payment method details"
// @Success 201 {object} PaymentMethodResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/credentials/payment-methods [post]
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

	c.JSON(http.StatusCreated, toPaymentMethodResponse(credential))
}

// ListPaymentMethods lists all payment methods for the user
// @Summary List payment methods
// @Description Returns all payment methods associated with the user
// @Tags Credentials
// @Produce json
// @Security BearerAuth
// @Success 200 {array} PaymentMethodResponse
// @Failure 500 {object} map[string]string
// @Router /agents/credentials/payment-methods [get]
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

	response := make([]PaymentMethodResponse, 0, len(credentials))
	for _, credential := range credentials {
		response = append(response, toPaymentMethodResponse(credential))
	}
	c.JSON(http.StatusOK, response)
}

// GetPaymentMethod retrieves a specific payment method by ID
// @Summary Get payment method
// @Description Returns a specific payment method by its ID
// @Tags Credentials
// @Produce json
// @Security BearerAuth
// @Param id path string true "Payment Method ID"
// @Success 200 {object} PaymentMethodResponse
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/credentials/payment-methods/{id} [get]
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

	c.JSON(http.StatusOK, toPaymentMethodResponse(credential))
}

// SetDefaultPaymentMethod sets a payment method as the default
// @Summary Set default payment method
// @Description Sets a payment method as the user's default
// @Tags Credentials
// @Produce json
// @Security BearerAuth
// @Param id path string true "Payment Method ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/credentials/payment-methods/{id} [put]
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

// DeletePaymentMethod deletes a payment method
// @Summary Delete payment method
// @Description Deletes a payment method by ID
// @Tags Credentials
// @Produce json
// @Security BearerAuth
// @Param id path string true "Payment Method ID"
// @Success 204 {string} string
// @Failure 500 {object} map[string]string
// @Router /agents/credentials/payment-methods/{id} [delete]
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

// GenerateTokenRequest represents credential token generation request
type GenerateTokenRequest struct {
	CredentialID     string `json:"credential_id" binding:"required"`
	PaymentMandateID string `json:"payment_mandate_id" binding:"required"`
}

// GenerateToken generates a credential token for payment
// @Summary Generate credential token
// @Description Generates a credential token for payment processing
// @Tags Credentials
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body GenerateTokenRequest true "Token request"
// @Success 201 {object} CredentialTokenResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/credentials/tokens [post]
func (h *CredentialHandler) GenerateToken(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "generate_token")
	userID := c.GetString("user_id")

	var req GenerateTokenRequest
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

	c.JSON(http.StatusCreated, toCredentialTokenResponse(token, true))
}

// GetDefaultPaymentMethod retrieves the user's default payment method
// @Summary Get default payment method
// @Description Returns the user's default payment method
// @Tags Credentials
// @Produce json
// @Security BearerAuth
// @Success 200 {object} PaymentMethodResponse
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/credentials/payment-methods/default [get]
func (h *CredentialHandler) GetDefaultPaymentMethod(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("credential_handler").With("operation", "get_default_payment_method")
	userID := c.GetString("user_id")

	credential, err := h.svc.GetDefaultPaymentMethod(c.Request.Context(), userID)
	if err != nil {
		log.Error("failed to get default payment method", "error", err, "user_id", userID)
		c.JSON(http.StatusNotFound, gin.H{"error": "default payment method not found"})
		return
	}

	c.JSON(http.StatusOK, toPaymentMethodResponse(credential))
}

// ValidateToken validates a credential token
// @Summary Validate credential token
// @Description Validates a credential token
// @Tags Credentials
// @Produce json
// @Param token query string true "Token to validate"
// @Success 200 {object} CredentialTokenResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /agents/credentials/tokens/validate [get]
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

	c.JSON(http.StatusOK, toCredentialTokenResponse(credentialToken, false))
}

func toPaymentMethodResponse(credential *models.PaymentCredential) PaymentMethodResponse {
	return PaymentMethodResponse{
		ID:                 credential.ID,
		CredentialType:     credential.CredentialType,
		RazorpayCustomerID: credential.RazorpayCustomerID,
		MaskedCardNumber:   credential.MaskedCardNumber,
		CardBrand:          credential.CardBrand,
		IsDefault:          credential.IsDefault,
		IsActive:           credential.IsActive,
		ExpiresAt:          credential.ExpiresAt,
		CreatedAt:          credential.CreatedAt,
		UpdatedAt:          credential.UpdatedAt,
	}
}

func toCredentialTokenResponse(token *models.CredentialToken, includeToken bool) CredentialTokenResponse {
	response := CredentialTokenResponse{
		ID:               token.ID,
		CredentialID:     token.CredentialID,
		PaymentMandateID: token.PaymentMandateID,
		ExpiresAt:        token.ExpiresAt,
		IsUsed:           token.IsUsed,
		CreatedAt:        token.CreatedAt,
	}
	if includeToken {
		response.Token = token.Token
	}
	return response
}
