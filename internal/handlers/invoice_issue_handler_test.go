package handlers

import (
	"bytes"
	"context"
	"errors"
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

type handlerInvoiceIssueRepository struct {
	interfaces.CanonicalInvoiceRepository
	command interfaces.AtomicInvoiceIssue
}

func TestInvoiceIssueErrorStatusContract(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{err: &invoiceissue.InvalidSeriesError{}, want: http.StatusBadRequest},
		{err: &invoiceissue.InvalidDocumentTypeError{}, want: http.StatusBadRequest},
		{err: &invoiceissue.InvalidLifecycleError{}, want: http.StatusBadRequest},
		{err: &invoiceissue.NotFoundError{}, want: http.StatusNotFound},
		{err: &invoiceissue.StaleVersionError{}, want: http.StatusConflict},
		{err: &invoiceissue.AlreadyIssuedError{}, want: http.StatusConflict},
		{err: &idempotency.ConflictError{}, want: http.StatusConflict},
		{err: &invoiceissue.SequenceExhaustedError{}, want: http.StatusConflict},
		{err: errors.New("database unavailable"), want: http.StatusInternalServerError},
	}
	for _, fixture := range tests {
		if got := invoiceIssueErrorStatus(fixture.err); got != fixture.want {
			t.Fatalf("status(%T) = %d, want %d", fixture.err, got, fixture.want)
		}
	}
}

func (r *handlerInvoiceIssueRepository) IssueDraftAtomic(
	_ context.Context,
	command interfaces.AtomicInvoiceIssue,
) (*interfaces.AtomicInvoiceIssueResult, error) {
	r.command = command
	version := command.ExpectedVersion + 1
	number := "INV/26-27/000001"
	return &interfaces.AtomicInvoiceIssueResult{
		Invoice: &models.Invoice{
			ID: command.InvoiceID, BusinessID: command.BusinessID, Version: version,
			Status: models.InvoiceStatusIssued, InvoiceNo: &number,
		},
		FinalRender: &models.DocumentRenderJob{
			ID: uuid.NewString(), BusinessID: command.BusinessID, Kind: models.RenderKindFinal,
			SourceInvoiceVersion: &version,
		},
	}, nil
}

func TestInvoiceIssueHandlerRequiresHeadersAndUsesAuthenticatedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	businessID := uuid.NewString()
	userID := uuid.NewString()
	invoiceID := uuid.NewString()
	repo := &handlerInvoiceIssueRepository{}
	service := services.NewInvoiceService(nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, logger.New())
	handler := NewInvoiceHandler(service, nil, logger.New())
	router := gin.New()
	router.POST("/api/v1/invoices/:id/issue", func(c *gin.Context) {
		c.Set("business_id", businessID)
		c.Set("user_id", userID)
		c.Set("role", "accountant")
		c.Set(middleware.RequestIDKey, "request-issue")
		handler.Issue(c)
	})

	for _, fixture := range []struct {
		name        string
		idempotency string
		ifMatch     string
		want        int
	}{
		{name: "missing idempotency", ifMatch: `"1"`, want: http.StatusBadRequest},
		{name: "missing if-match", idempotency: uuid.NewString(), want: http.StatusBadRequest},
		{name: "invalid if-match", idempotency: uuid.NewString(), ifMatch: `"wat"`, want: http.StatusBadRequest},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/invoices/"+invoiceID+"/issue",
				bytes.NewBufferString(`{"document_type":"tax_invoice","series":"INV"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", fixture.idempotency)
			request.Header.Set("If-Match", fixture.ifMatch)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != fixture.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, fixture.want, response.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/invoices/"+invoiceID+"/issue",
		bytes.NewBufferString(`{
			"document_type":"tax_invoice",
			"series":"INV",
			"business_id":"attacker-business",
			"actor_id":"attacker-actor"
		}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	request.Header.Set("If-Match", `"1"`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", response.Code, response.Body.String())
	}
	if repo.command.BusinessID != businessID || repo.command.ActorID != userID ||
		repo.command.InvoiceID != invoiceID || repo.command.ExpectedVersion != 1 {
		t.Fatalf("trusted issue command = %#v", repo.command)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"invoice"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"final_render"`)) {
		t.Fatalf("response shape = %s", response.Body.String())
	}
}
