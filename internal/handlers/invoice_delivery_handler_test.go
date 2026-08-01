package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type handlerInvoiceDeliveryRepository struct {
	interfaces.CanonicalInvoiceRepository
	command   interfaces.AtomicInvoiceDelivery
	result    *interfaces.AtomicInvoiceDeliveryResult
	status    *models.EmailDelivery
	createErr error
	getErr    error
}

func (r *handlerInvoiceDeliveryRepository) CreateDeliveryAtomic(
	_ context.Context,
	command interfaces.AtomicInvoiceDelivery,
) (*interfaces.AtomicInvoiceDeliveryResult, error) {
	r.command = command
	return r.result, r.createErr
}

func (r *handlerInvoiceDeliveryRepository) GetInvoiceDelivery(
	_ context.Context,
	_, _, _ string,
) (*models.EmailDelivery, error) {
	return r.status, r.getErr
}

func newInvoiceDeliveryHandler(repository *handlerInvoiceDeliveryRepository) *InvoiceHandler {
	service := services.NewInvoiceService(
		nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New(),
		services.WithInvoiceDeliveryRepository(repository),
	)
	return NewInvoiceHandler(service, nil, logger.New())
}

func TestInvoiceDeliveryHandlerCreatesNestedSafeStatusWithAuthenticatedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	businessID, userID, invoiceID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	deliveryID, jobID := uuid.NewString(), uuid.NewString()
	repository := &handlerInvoiceDeliveryRepository{result: &interfaces.AtomicInvoiceDeliveryResult{
		Delivery: &models.EmailDelivery{
			ID: deliveryID, BusinessID: businessID, InvoiceID: &invoiceID, RenderJobID: &jobID,
			Recipient: "buyer@example.com", Status: models.EmailDeliveryStatusWaitingForRender,
			ProviderMessageID: "must-not-leak", ErrorMessage: "must-not-leak",
			LeaseOwner: models.StringPointer("must-not-leak"), Attempts: 3,
			Metadata: `{"secret":"must-not-leak"}`, SourceEmail: "must-not-leak@example.com",
		},
	}}
	handler := newInvoiceDeliveryHandler(repository)
	router := gin.New()
	router.POST("/api/v1/invoices/:id/deliveries", func(c *gin.Context) {
		c.Set("business_id", businessID)
		c.Set("user_id", userID)
		c.Set("role", "accountant")
		c.Set(middleware.RequestIDKey, "request-delivery")
		handler.Deliver(c)
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/invoices/"+invoiceID+"/deliveries",
		bytes.NewBufferString(`{"recipient":" Buyer@Example.COM "}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", response.Code, response.Body.String())
	}
	if repository.command.BusinessID != businessID || repository.command.InvoiceID != invoiceID ||
		repository.command.ActorID != userID || repository.command.Recipient != "buyer@example.com" {
		t.Fatalf("trusted delivery command = %#v", repository.command)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 2 || body["replayed"] != false {
		t.Fatalf("top-level response = %#v", body)
	}
	delivery, ok := body["delivery"].(map[string]interface{})
	if !ok {
		t.Fatalf("delivery response = %#v", body["delivery"])
	}
	for _, forbidden := range []string{
		"provider_message_id", "error_message", "lease_owner", "lease_expires_at",
		"attempts", "metadata", "source_email", "business_id",
	} {
		if _, exists := delivery[forbidden]; exists {
			t.Fatalf("unsafe field %q leaked: %#v", forbidden, delivery)
		}
	}
}

func TestInvoiceDeliveryStatusHandlerReturnsSafeTenantScopedDTO(t *testing.T) {
	gin.SetMode(gin.TestMode)
	businessID, invoiceID, deliveryID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)
	repository := &handlerInvoiceDeliveryRepository{status: &models.EmailDelivery{
		ID: deliveryID, BusinessID: businessID, InvoiceID: &invoiceID, RenderJobID: &jobID,
		Recipient: "buyer@example.com", Status: models.EmailDeliveryStatusQueued,
		ProviderMessageID: "must-not-leak", ErrorMessage: "must-not-leak",
		LeaseOwner: models.StringPointer("must-not-leak"), Attempts: 3,
		Metadata: `{"secret":"must-not-leak"}`, SourceEmail: "must-not-leak@example.com",
		CreatedAt: now, UpdatedAt: now,
	}}
	handler := newInvoiceDeliveryHandler(repository)
	router := gin.New()
	router.GET("/api/v1/invoices/:id/deliveries/:delivery_id", func(c *gin.Context) {
		c.Set("business_id", businessID)
		handler.GetDeliveryStatus(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/invoices/"+invoiceID+"/deliveries/"+deliveryID,
		nil,
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{
		"provider_message_id", "error_message", "lease_owner", "lease_expires_at",
		"attempts", "metadata", "source_email", "business_id",
	} {
		if bytes.Contains(response.Body.Bytes(), []byte(`"`+forbidden+`"`)) {
			t.Fatalf("unsafe field %q leaked: %s", forbidden, response.Body.String())
		}
	}
}

func TestInvoiceDeliveryHandlersReturnStableErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid key", err: &idempotency.InvalidKeyError{}, want: http.StatusBadRequest},
		{name: "invalid payload", err: &idempotency.InvalidPayloadError{}, want: http.StatusBadRequest},
		{name: "not found", err: interfaces.ErrInvoiceNotFound, want: http.StatusNotFound},
		{name: "not deliverable", err: interfaces.ErrInvoiceNotDeliverable, want: http.StatusConflict},
		{name: "idempotency conflict", err: &idempotency.ConflictError{}, want: http.StatusConflict},
		{name: "in progress", err: &idempotency.InProgressError{}, want: http.StatusConflict},
		{name: "database", err: errors.New("postgres password must not leak"), want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, message := invoiceDeliveryErrorResponse(test.err)
			if status != test.want {
				t.Fatalf("status/message = %d/%q, want %d", status, message, test.want)
			}
			if strings.Contains(message, "password") {
				t.Fatalf("sensitive error leaked: %q", message)
			}
		})
	}
}
