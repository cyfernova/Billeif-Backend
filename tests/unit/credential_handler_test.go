package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock CredentialProviderService
// =============================================================================

type MockCredentialService struct {
	mock.Mock
}

func (m *MockCredentialService) AddPaymentMethod(ctx context.Context, req *AddPaymentMethodServiceRequest) (*models.PaymentCredential, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PaymentCredential), args.Error(1)
}

func (m *MockCredentialService) GetPaymentMethods(ctx context.Context, userID string) ([]*models.PaymentCredential, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.PaymentCredential), args.Error(1)
}

func (m *MockCredentialService) GetDefaultPaymentMethod(ctx context.Context, userID string) (*models.PaymentCredential, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PaymentCredential), args.Error(1)
}

func (m *MockCredentialService) GetPaymentMethodByID(ctx context.Context, credentialID string) (*models.PaymentCredential, error) {
	args := m.Called(ctx, credentialID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PaymentCredential), args.Error(1)
}

func (m *MockCredentialService) SetDefaultPaymentMethod(ctx context.Context, userID, credentialID string) error {
	args := m.Called(ctx, userID, credentialID)
	return args.Error(0)
}

func (m *MockCredentialService) DeletePaymentMethod(ctx context.Context, userID, credentialID string) error {
	args := m.Called(ctx, userID, credentialID)
	return args.Error(0)
}

func (m *MockCredentialService) GenerateCredentialToken(ctx context.Context, req *GenerateTokenServiceRequest) (*models.CredentialToken, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CredentialToken), args.Error(1)
}

func (m *MockCredentialService) ValidateToken(ctx context.Context, token string) (*models.CredentialToken, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CredentialToken), args.Error(1)
}

// Service-level request types (for mocking)
type AddPaymentMethodServiceRequest struct {
	UserID             string
	CredentialType     string
	RazorpayCustomerID string
	MaskedCardNumber   string
	CardBrand          string
	CardToken          string
	IsDefault          bool
}

type GenerateTokenServiceRequest struct {
	CredentialID     string
	PaymentMandateID string
	UserID           string
}

// =============================================================================
// Testable wrapper
// =============================================================================

type CredentialHandlerTestable struct {
	svc *MockCredentialService
	log *logger.Logger
}

func NewCredentialHandlerTestable(svc *MockCredentialService, log *logger.Logger) *CredentialHandlerTestable {
	return &CredentialHandlerTestable{
		svc: svc,
		log: log,
	}
}

func (h *CredentialHandlerTestable) AddPaymentMethod(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		CredentialType     string `json:"credential_type" binding:"required"`
		RazorpayCustomerID string `json:"razorpay_customer_id" binding:"required"`
		MaskedCardNumber   string `json:"masked_card_number" binding:"required"`
		CardBrand          string `json:"card_brand" binding:"required"`
		CardToken          string `json:"card_token" binding:"required"`
		IsDefault          bool   `json:"is_default"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("invalid add payment method payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	serviceReq := &AddPaymentMethodServiceRequest{
		UserID:             userID,
		CredentialType:     req.CredentialType,
		RazorpayCustomerID: req.RazorpayCustomerID,
		MaskedCardNumber:   req.MaskedCardNumber,
		CardBrand:          req.CardBrand,
		CardToken:          req.CardToken,
		IsDefault:          req.IsDefault,
	}

	credential, err := h.svc.AddPaymentMethod(c.Request.Context(), serviceReq)
	if err != nil {
		h.log.Error("failed to add payment method", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, toPaymentMethodResponse(credential))
}

func (h *CredentialHandlerTestable) ListPaymentMethods(c *gin.Context) {
	userID := c.GetString("user_id")

	credentials, err := h.svc.GetPaymentMethods(c.Request.Context(), userID)
	if err != nil {
		h.log.Error("failed to list payment methods", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := make([]map[string]interface{}, 0, len(credentials))
	for _, credential := range credentials {
		response = append(response, toPaymentMethodResponseMap(credential))
	}
	c.JSON(http.StatusOK, response)
}

func (h *CredentialHandlerTestable) GetPaymentMethod(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	credential, err := h.svc.GetPaymentMethodByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("failed to get payment method", "error", err, "credential_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "payment method not found"})
		return
	}

	if credential.UserID != userID {
		h.log.Warn("payment method access denied", "credential_id", id, "user_id", userID)
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized"})
		return
	}

	c.JSON(http.StatusOK, toPaymentMethodResponseMap(credential))
}

func (h *CredentialHandlerTestable) SetDefaultPaymentMethod(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	if err := h.svc.SetDefaultPaymentMethod(c.Request.Context(), userID, id); err != nil {
		h.log.Error("failed to set default payment method", "error", err, "credential_id", id, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "default payment method updated"})
}

func (h *CredentialHandlerTestable) DeletePaymentMethod(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")

	if err := h.svc.DeletePaymentMethod(c.Request.Context(), userID, id); err != nil {
		h.log.Error("failed to delete payment method", "error", err, "credential_id", id, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *CredentialHandlerTestable) GenerateToken(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		CredentialID     string `json:"credential_id" binding:"required"`
		PaymentMandateID string `json:"payment_mandate_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Warn("invalid credential token request payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	serviceReq := &GenerateTokenServiceRequest{
		CredentialID:     req.CredentialID,
		PaymentMandateID: req.PaymentMandateID,
		UserID:           userID,
	}

	token, err := h.svc.GenerateCredentialToken(c.Request.Context(), serviceReq)
	if err != nil {
		h.log.Error("failed to generate credential token", "error", err, "user_id", userID, "credential_id", req.CredentialID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, toCredentialTokenResponseMap(token, true))
}

func (h *CredentialHandlerTestable) GetDefaultPaymentMethod(c *gin.Context) {
	userID := c.GetString("user_id")

	credential, err := h.svc.GetDefaultPaymentMethod(c.Request.Context(), userID)
	if err != nil {
		h.log.Error("failed to get default payment method", "error", err, "user_id", userID)
		c.JSON(http.StatusNotFound, gin.H{"error": "default payment method not found"})
		return
	}

	c.JSON(http.StatusOK, toPaymentMethodResponseMap(credential))
}

func (h *CredentialHandlerTestable) ValidateToken(c *gin.Context) {
	token := c.Query("token")

	if token == "" {
		h.log.Warn("missing token query param")
		c.JSON(http.StatusBadRequest, gin.H{"error": "token parameter is required"})
		return
	}

	credentialToken, err := h.svc.ValidateToken(c.Request.Context(), token)
	if err != nil {
		h.log.Warn("credential token validation failed", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	c.JSON(http.StatusOK, toCredentialTokenResponseMap(credentialToken, false))
}

func toPaymentMethodResponse(credential *models.PaymentCredential) map[string]interface{} {
	resp := map[string]interface{}{
		"id":              credential.ID,
		"credential_type": credential.CredentialType,
		"is_default":      credential.IsDefault,
		"is_active":       credential.IsActive,
		"created_at":      credential.CreatedAt,
		"updated_at":      credential.UpdatedAt,
	}
	if credential.RazorpayCustomerID != nil {
		resp["razorpay_customer_id"] = *credential.RazorpayCustomerID
	}
	if credential.MaskedCardNumber != nil {
		resp["masked_card_number"] = *credential.MaskedCardNumber
	}
	if credential.CardBrand != nil {
		resp["card_brand"] = *credential.CardBrand
	}
	if credential.ExpiresAt != nil {
		resp["expires_at"] = *credential.ExpiresAt
	}
	return resp
}

func toPaymentMethodResponseMap(credential *models.PaymentCredential) map[string]interface{} {
	return toPaymentMethodResponse(credential)
}

func toCredentialTokenResponseMap(token *models.CredentialToken, includeToken bool) map[string]interface{} {
	resp := map[string]interface{}{
		"id":            token.ID,
		"credential_id": token.CredentialID,
		"expires_at":    token.ExpiresAt,
		"is_used":       token.IsUsed,
		"created_at":    token.CreatedAt,
	}
	if token.PaymentMandateID != nil {
		resp["payment_mandate_id"] = *token.PaymentMandateID
	}
	if includeToken {
		resp["token"] = token.Token
	}
	return resp
}

// =============================================================================
// AddPaymentMethod Tests
// =============================================================================

func TestAddPaymentMethod_Success(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	now := time.Now()
	credential := &models.PaymentCredential{
		ID:                 "cred-123",
		UserID:             "user-123",
		CredentialType:     "razorpay_card",
		RazorpayCustomerID: credentialStrPtr("cust-123"),
		MaskedCardNumber:   credentialStrPtr("****1234"),
		CardBrand:          credentialStrPtr("Visa"),
		IsDefault:          true,
		IsActive:           true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	mockSvc.On("AddPaymentMethod", mock.Anything, mock.Anything).Return(credential, nil)

	router := gin.New()
	router.POST("/agents/credentials/payment-methods", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddPaymentMethod(c)
	})

	reqBody := map[string]interface{}{
		"credential_type":      "razorpay_card",
		"razorpay_customer_id": "cust-123",
		"masked_card_number":   "****1234",
		"card_brand":           "Visa",
		"card_token":           "tok-123",
		"is_default":           true,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/payment-methods", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestAddPaymentMethod_BadRequest_MissingCredentialType(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/credentials/payment-methods", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddPaymentMethod(c)
	})

	reqBody := map[string]interface{}{
		"razorpay_customer_id": "cust-123",
		"masked_card_number":   "****1234",
		"card_brand":           "Visa",
		"card_token":           "tok-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/payment-methods", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestAddPaymentMethod_BadRequest_MissingRazorpayCustomerID(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/credentials/payment-methods", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddPaymentMethod(c)
	})

	reqBody := map[string]interface{}{
		"credential_type":    "razorpay_card",
		"masked_card_number": "****1234",
		"card_brand":         "Visa",
		"card_token":         "tok-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/payment-methods", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestAddPaymentMethod_InternalError(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("AddPaymentMethod", mock.Anything, mock.Anything).Return(nil, errors.New("encryption failed"))

	router := gin.New()
	router.POST("/agents/credentials/payment-methods", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.AddPaymentMethod(c)
	})

	reqBody := map[string]interface{}{
		"credential_type":      "razorpay_card",
		"razorpay_customer_id": "cust-123",
		"masked_card_number":   "****1234",
		"card_brand":           "Visa",
		"card_token":           "tok-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/payment-methods", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListPaymentMethods Tests
// =============================================================================

func TestListPaymentMethods_Success(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	now := time.Now()
	credentials := []*models.PaymentCredential{
		{
			ID:                 "cred-1",
			UserID:             "user-123",
			CredentialType:     "razorpay_card",
			RazorpayCustomerID: credentialStrPtr("cust-1"),
			MaskedCardNumber:   credentialStrPtr("****1111"),
			CardBrand:          credentialStrPtr("Visa"),
			IsDefault:          true,
			IsActive:           true,
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		{
			ID:                 "cred-2",
			UserID:             "user-123",
			CredentialType:     "razorpay_upi",
			RazorpayCustomerID: credentialStrPtr("cust-2"),
			MaskedCardNumber:   nil,
			CardBrand:          nil,
			IsDefault:          false,
			IsActive:           true,
			CreatedAt:          now,
			UpdatedAt:          now,
		},
	}

	mockSvc.On("GetPaymentMethods", mock.Anything, "user-123").Return(credentials, nil)

	router := gin.New()
	router.GET("/agents/credentials/payment-methods", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListPaymentMethods(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/payment-methods", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListPaymentMethods_Empty(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("GetPaymentMethods", mock.Anything, "user-123").Return([]*models.PaymentCredential{}, nil)

	router := gin.New()
	router.GET("/agents/credentials/payment-methods", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListPaymentMethods(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/payment-methods", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestListPaymentMethods_Error(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("GetPaymentMethods", mock.Anything, "user-123").Return(nil, errors.New("database error"))

	router := gin.New()
	router.GET("/agents/credentials/payment-methods", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.ListPaymentMethods(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/payment-methods", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetPaymentMethod Tests
// =============================================================================

func TestGetPaymentMethod_Success(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	now := time.Now()
	credential := &models.PaymentCredential{
		ID:                 "cred-123",
		UserID:             "user-123",
		CredentialType:     "razorpay_card",
		RazorpayCustomerID: credentialStrPtr("cust-123"),
		MaskedCardNumber:   credentialStrPtr("****1234"),
		CardBrand:          credentialStrPtr("Visa"),
		IsDefault:          true,
		IsActive:           true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	mockSvc.On("GetPaymentMethodByID", mock.Anything, "cred-123").Return(credential, nil)

	router := gin.New()
	router.GET("/agents/credentials/payment-methods/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetPaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/payment-methods/cred-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetPaymentMethod_NotFound(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("GetPaymentMethodByID", mock.Anything, "nonexistent").Return(nil, errors.New("credential not found"))

	router := gin.New()
	router.GET("/agents/credentials/payment-methods/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetPaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/payment-methods/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestGetPaymentMethod_Forbidden(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	now := time.Now()
	credential := &models.PaymentCredential{
		ID:             "cred-123",
		UserID:         "other-user",
		CredentialType: "razorpay_card",
		IsDefault:      false,
		IsActive:       true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	mockSvc.On("GetPaymentMethodByID", mock.Anything, "cred-123").Return(credential, nil)

	router := gin.New()
	router.GET("/agents/credentials/payment-methods/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetPaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/payment-methods/cred-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// SetDefaultPaymentMethod Tests
// =============================================================================

func TestSetDefaultPaymentMethod_Success(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("SetDefaultPaymentMethod", mock.Anything, "user-123", "cred-123").Return(nil)

	router := gin.New()
	router.PUT("/agents/credentials/payment-methods/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.SetDefaultPaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/agents/credentials/payment-methods/cred-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestSetDefaultPaymentMethod_InternalError(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("SetDefaultPaymentMethod", mock.Anything, "user-123", "cred-123").Return(errors.New("failed to set default"))

	router := gin.New()
	router.PUT("/agents/credentials/payment-methods/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.SetDefaultPaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/agents/credentials/payment-methods/cred-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DeletePaymentMethod Tests
// =============================================================================

func TestDeletePaymentMethod_Success(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("DeletePaymentMethod", mock.Anything, "user-123", "cred-123").Return(nil)

	router := gin.New()
	router.DELETE("/agents/credentials/payment-methods/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.DeletePaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/agents/credentials/payment-methods/cred-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDeletePaymentMethod_NotFound(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("DeletePaymentMethod", mock.Anything, "user-123", "nonexistent").Return(errors.New("credential not found"))

	router := gin.New()
	router.DELETE("/agents/credentials/payment-methods/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.DeletePaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/agents/credentials/payment-methods/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDeletePaymentMethod_Forbidden(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("DeletePaymentMethod", mock.Anything, "user-123", "cred-other").Return(errors.New("unauthorized: credential does not belong to user"))

	router := gin.New()
	router.DELETE("/agents/credentials/payment-methods/:id", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.DeletePaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/agents/credentials/payment-methods/cred-other", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GenerateToken Tests
// =============================================================================

func TestGenerateToken_Success(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	now := time.Now()
	token := &models.CredentialToken{
		ID:               "token-123",
		CredentialID:     "cred-123",
		PaymentMandateID: credentialStrPtr("mandate-123"),
		Token:            "raw-token-abc",
		ExpiresAt:        now.Add(30 * time.Minute),
		IsUsed:           false,
		CreatedAt:        now,
	}

	mockSvc.On("GenerateCredentialToken", mock.Anything, mock.Anything).Return(token, nil)

	router := gin.New()
	router.POST("/agents/credentials/tokens", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GenerateToken(c)
	})

	reqBody := map[string]interface{}{
		"credential_id":      "cred-123",
		"payment_mandate_id": "mandate-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/tokens", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGenerateToken_BadRequest_MissingCredentialID(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/credentials/tokens", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GenerateToken(c)
	})

	reqBody := map[string]interface{}{
		"payment_mandate_id": "mandate-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/tokens", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestGenerateToken_BadRequest_MissingPaymentMandateID(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/agents/credentials/tokens", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GenerateToken(c)
	})

	reqBody := map[string]interface{}{
		"credential_id": "cred-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/tokens", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestGenerateToken_CredentialNotFound(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("GenerateCredentialToken", mock.Anything, mock.Anything).Return(nil, errors.New("credential not found"))

	router := gin.New()
	router.POST("/agents/credentials/tokens", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GenerateToken(c)
	})

	reqBody := map[string]interface{}{
		"credential_id":      "nonexistent",
		"payment_mandate_id": "mandate-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/tokens", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestGenerateToken_CredentialNotActive(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("GenerateCredentialToken", mock.Anything, mock.Anything).Return(nil, errors.New("credential is not active"))

	router := gin.New()
	router.POST("/agents/credentials/tokens", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GenerateToken(c)
	})

	reqBody := map[string]interface{}{
		"credential_id":      "cred-inactive",
		"payment_mandate_id": "mandate-123",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agents/credentials/tokens", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetDefaultPaymentMethod Tests
// =============================================================================

func TestGetDefaultPaymentMethod_Success(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	now := time.Now()
	credential := &models.PaymentCredential{
		ID:                 "cred-123",
		UserID:             "user-123",
		CredentialType:     "razorpay_card",
		RazorpayCustomerID: credentialStrPtr("cust-123"),
		MaskedCardNumber:   credentialStrPtr("****1234"),
		CardBrand:          credentialStrPtr("Visa"),
		IsDefault:          true,
		IsActive:           true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	mockSvc.On("GetDefaultPaymentMethod", mock.Anything, "user-123").Return(credential, nil)

	router := gin.New()
	router.GET("/agents/credentials/payment-methods/default", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetDefaultPaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/payment-methods/default", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetDefaultPaymentMethod_NotFound(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("GetDefaultPaymentMethod", mock.Anything, "user-123").Return(nil, errors.New("credential not found"))

	router := gin.New()
	router.GET("/agents/credentials/payment-methods/default", func(c *gin.Context) {
		createTestContext(c, "user-123", "")
		handler.GetDefaultPaymentMethod(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/payment-methods/default", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ValidateToken Tests
// =============================================================================

func TestValidateToken_Success(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	now := time.Now()
	token := &models.CredentialToken{
		ID:               "token-123",
		CredentialID:     "cred-123",
		PaymentMandateID: credentialStrPtr("mandate-123"),
		ExpiresAt:        now.Add(30 * time.Minute),
		IsUsed:           false,
		CreatedAt:        now,
	}

	mockSvc.On("ValidateToken", mock.Anything, "valid-token-abc").Return(token, nil)

	router := gin.New()
	router.GET("/agents/credentials/tokens/validate", func(c *gin.Context) {
		handler.ValidateToken(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/tokens/validate?token=valid-token-abc", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestValidateToken_BadRequest_MissingToken(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/agents/credentials/tokens/validate", func(c *gin.Context) {
		handler.ValidateToken(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/tokens/validate", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestValidateToken_Unauthorized_InvalidToken(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("ValidateToken", mock.Anything, "invalid-token").Return(nil, errors.New("invalid or expired token"))

	router := gin.New()
	router.GET("/agents/credentials/tokens/validate", func(c *gin.Context) {
		handler.ValidateToken(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/tokens/validate?token=invalid-token", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestValidateToken_Unauthorized_TokenAlreadyUsed(t *testing.T) {
	mockSvc := new(MockCredentialService)
	log := logger.New()
	handler := NewCredentialHandlerTestable(mockSvc, log)

	mockSvc.On("ValidateToken", mock.Anything, "used-token").Return(nil, errors.New("token already used"))

	router := gin.New()
	router.GET("/agents/credentials/tokens/validate", func(c *gin.Context) {
		handler.ValidateToken(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/agents/credentials/tokens/validate?token=used-token", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func credentialStrPtr(s string) *string {
	return &s
}
