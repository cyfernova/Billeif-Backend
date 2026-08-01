package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type handlerInvoicePreviewRepository struct {
	interfaces.CanonicalInvoiceRepository
	command interfaces.AtomicInvoicePreview
	result  *interfaces.AtomicInvoicePreviewResult
	err     error
}

func (r *handlerInvoicePreviewRepository) RequestPreviewAtomic(
	_ context.Context,
	command interfaces.AtomicInvoicePreview,
) (*interfaces.AtomicInvoicePreviewResult, error) {
	r.command = command
	if r.err != nil {
		return nil, r.err
	}
	return r.result, nil
}

func TestInvoicePreviewHandlerRequiresKeyAndUsesAuthenticatedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	businessID := uuid.NewString()
	userID := uuid.NewString()
	invoiceID := uuid.NewString()
	version := 4
	repository := &handlerInvoicePreviewRepository{
		result: &interfaces.AtomicInvoicePreviewResult{
			RenderJob: &models.DocumentRenderJob{
				ID: uuid.NewString(), BusinessID: businessID, Kind: models.RenderKindPreview,
				SourceInvoiceVersion: &version,
			},
		},
	}
	service := services.NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New())
	handler := NewInvoiceHandler(service, nil, logger.New())
	router := gin.New()
	router.POST("/api/v1/invoices/:id/previews", func(c *gin.Context) {
		c.Set("business_id", businessID)
		c.Set("user_id", userID)
		c.Set("role", "accountant")
		c.Set(middleware.RequestIDKey, "request-preview")
		handler.Preview(c)
	})

	missingKey := httptest.NewRequest(http.MethodPost, "/api/v1/invoices/"+invoiceID+"/previews", nil)
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missingKey)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing key status = %d, want 400", missingResponse.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/invoices/"+invoiceID+"/previews", nil)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", response.Code, response.Body.String())
	}
	if repository.command.BusinessID != businessID || repository.command.InvoiceID != invoiceID ||
		repository.command.ActorID != userID {
		t.Fatalf("trusted command = %#v", repository.command)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"render_job"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"source_invoice_version":4`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"replayed":false`)) {
		t.Fatalf("response = %s, want versioned render job and replay flag", response.Body.String())
	}
}

func TestInvoicePreviewHandlerReturnsSanitizedStableErrors(t *testing.T) {
	fixtures := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{name: "invalid key", err: &idempotency.InvalidKeyError{}, status: 400, message: "a UUID idempotency key is required"},
		{name: "invalid payload", err: &idempotency.InvalidPayloadError{}, status: 400, message: "invalid invoice preview request"},
		{name: "invalid lifecycle", err: &invoiceissue.InvalidLifecycleError{Reason: "database-detail"}, status: 400, message: "invalid invoice preview request"},
		{name: "not found", err: &invoiceissue.NotFoundError{}, status: 404, message: "invoice not found"},
		{name: "conflict", err: &idempotency.ConflictError{}, status: 409, message: "idempotency key conflicts with a different request"},
		{name: "in progress", err: &idempotency.InProgressError{}, status: 409, message: "idempotent request is still in progress"},
		{name: "database", err: errors.New("postgres password leaked"), status: 500, message: "invoice preview unavailable"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			status, message := invoicePreviewErrorResponse(fmt.Errorf("wrapped: %w", fixture.err))
			if status != fixture.status || message != fixture.message {
				t.Fatalf("response = %d/%q, want %d/%q", status, message, fixture.status, fixture.message)
			}
			if bytes.Contains([]byte(message), []byte("database-detail")) ||
				bytes.Contains([]byte(message), []byte("password")) {
				t.Fatalf("sensitive error detail leaked: %q", message)
			}
		})
	}
}
