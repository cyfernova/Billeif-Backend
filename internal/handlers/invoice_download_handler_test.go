package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type handlerInvoiceDownloadRepository struct {
	interfaces.CanonicalInvoiceRepository
	invoice *models.Invoice
	err     error
}

func (r *handlerInvoiceDownloadRepository) GetByID(
	_ context.Context,
	id, businessID string,
) (*models.Invoice, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.invoice == nil || r.invoice.ID != id || r.invoice.BusinessID != businessID {
		return nil, interfaces.ErrInvoiceNotFound
	}
	return r.invoice, nil
}

type handlerInvoiceRenderReader struct {
	job *models.DocumentRenderJob
	err error
}

func (r *handlerInvoiceRenderReader) GetInvoiceRenderJob(
	_ context.Context,
	_, _, _ string,
) (*models.DocumentRenderJob, error) {
	return r.job, r.err
}

func (r *handlerInvoiceRenderReader) GetCompletedFinalRenderJob(
	_ context.Context,
	_, _ string,
	_ int,
) (*models.DocumentRenderJob, error) {
	return r.job, r.err
}

type handlerInvoicePDFPresigner struct {
	url string
	err error
}

func (p *handlerInvoicePDFPresigner) GeneratePresignedDownloadURL(
	_ context.Context,
	_, _ string,
	_ int64,
) (string, error) {
	return p.url, p.err
}

func newInvoiceDownloadHandler(
	invoices interfaces.CanonicalInvoiceRepository,
	renders interfaces.InvoiceRenderReadRepository,
	presigner services.InvoicePDFPresigner,
) *InvoiceHandler {
	service := services.NewInvoiceService(
		nil,
		&config.Config{S3: config.S3Config{BucketInvoices: "private-invoices"}},
		invoices,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		logger.New(),
		services.WithInvoiceRenderReadRepository(renders),
		services.WithInvoicePDFPresigner(presigner),
	)
	return NewInvoiceHandler(service, nil, logger.New())
}

func TestInvoiceRenderStatusHandlerReturnsExactSafeJSONFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	version := 3
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	handler := newInvoiceDownloadHandler(
		&handlerInvoiceDownloadRepository{},
		&handlerInvoiceRenderReader{job: &models.DocumentRenderJob{
			ID: jobID, InvoiceID: &invoiceID, BusinessID: businessID, Kind: models.RenderKindPreview,
			SourceInvoiceVersion: &version, Status: models.RenderJobStatusCompleted,
			ObjectKey: "must-not-leak", OutputURL: "must-not-leak", ErrorMessage: "must-not-leak",
			LeaseOwner: models.StringPointer("must-not-leak"), Attempts: 9,
			CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
		}},
		&handlerInvoicePDFPresigner{},
	)
	router := gin.New()
	router.GET("/api/v1/invoices/:id/renders/:render_job_id", func(c *gin.Context) {
		c.Set("business_id", businessID)
		handler.GetRenderStatus(c)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/invoices/"+invoiceID+"/renders/"+jobID,
			nil,
		),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	allowed := map[string]bool{
		"id": true, "invoice_id": true, "kind": true, "source_invoice_version": true,
		"status": true, "created_at": true, "updated_at": true, "completed_at": true,
	}
	if len(payload) != len(allowed) {
		t.Fatalf("response keys = %v, want exact safe allowlist", payload)
	}
	for key := range payload {
		if !allowed[key] {
			t.Fatalf("unsafe response key %q leaked in %s", key, response.Body.String())
		}
	}
}

func TestInvoicePDFHandlerReturnsExactPresignedDownloadEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	businessID, invoiceID := uuid.NewString(), uuid.NewString()
	version := 5
	handler := newInvoiceDownloadHandler(
		&handlerInvoiceDownloadRepository{invoice: &models.Invoice{
			ID: invoiceID, BusinessID: businessID, Version: version, PDFURL: "https://legacy.example/public.pdf",
		}},
		&handlerInvoiceRenderReader{job: &models.DocumentRenderJob{
			ID: uuid.NewString(), InvoiceID: &invoiceID, BusinessID: businessID,
			Kind: models.RenderKindFinal, SourceInvoiceVersion: &version,
			Status: models.RenderJobStatusCompleted, ObjectKey: "private/final.pdf",
		}},
		&handlerInvoicePDFPresigner{url: "https://signed.example/final.pdf"},
	)
	router := gin.New()
	router.GET("/api/v1/invoices/:id/pdf", func(c *gin.Context) {
		c.Set("business_id", businessID)
		handler.GetPDF(c)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/invoices/"+invoiceID+"/pdf", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 2 || payload["download_url"] == nil || payload["expires_at"] == nil {
		t.Fatalf("response = %s, want exact download_url/expires_at", response.Body.String())
	}
	if payload["pdf_url"] != nil {
		t.Fatalf("legacy pdf_url leaked: %s", response.Body.String())
	}
}

func TestInvoiceDownloadHandlersReturnStableStatusCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	version := 5

	fixtures := []struct {
		name     string
		path     string
		handler  *InvoiceHandler
		register func(*gin.Engine, *InvoiceHandler)
		want     int
	}{
		{
			name: "invalid render UUID",
			path: "/api/v1/invoices/" + invoiceID + "/renders/not-a-uuid",
			handler: newInvoiceDownloadHandler(
				&handlerInvoiceDownloadRepository{},
				&handlerInvoiceRenderReader{},
				&handlerInvoicePDFPresigner{},
			),
			register: func(router *gin.Engine, handler *InvoiceHandler) {
				router.GET("/api/v1/invoices/:id/renders/:render_job_id", handler.GetRenderStatus)
			},
			want: http.StatusBadRequest,
		},
		{
			name: "render mismatch",
			path: "/api/v1/invoices/" + invoiceID + "/renders/" + jobID,
			handler: newInvoiceDownloadHandler(
				&handlerInvoiceDownloadRepository{},
				&handlerInvoiceRenderReader{err: interfaces.ErrInvoiceRenderNotFound},
				&handlerInvoicePDFPresigner{},
			),
			register: func(router *gin.Engine, handler *InvoiceHandler) {
				router.GET("/api/v1/invoices/:id/renders/:render_job_id", handler.GetRenderStatus)
			},
			want: http.StatusNotFound,
		},
		{
			name: "invoice absent",
			path: "/api/v1/invoices/" + invoiceID + "/pdf",
			handler: newInvoiceDownloadHandler(
				&handlerInvoiceDownloadRepository{err: interfaces.ErrInvoiceNotFound},
				&handlerInvoiceRenderReader{},
				&handlerInvoicePDFPresigner{},
			),
			register: func(router *gin.Engine, handler *InvoiceHandler) {
				router.GET("/api/v1/invoices/:id/pdf", handler.GetPDF)
			},
			want: http.StatusNotFound,
		},
		{
			name: "final not ready",
			path: "/api/v1/invoices/" + invoiceID + "/pdf",
			handler: newInvoiceDownloadHandler(
				&handlerInvoiceDownloadRepository{invoice: &models.Invoice{
					ID: invoiceID, BusinessID: businessID, Version: version,
				}},
				&handlerInvoiceRenderReader{err: interfaces.ErrInvoiceRenderNotFound},
				&handlerInvoicePDFPresigner{},
			),
			register: func(router *gin.Engine, handler *InvoiceHandler) {
				router.GET("/api/v1/invoices/:id/pdf", handler.GetPDF)
			},
			want: http.StatusConflict,
		},
		{
			name: "render storage error containing not found",
			path: "/api/v1/invoices/" + invoiceID + "/renders/" + jobID,
			handler: newInvoiceDownloadHandler(
				&handlerInvoiceDownloadRepository{},
				&handlerInvoiceRenderReader{err: errors.New(`relation "render_jobs_not_found" does not exist`)},
				&handlerInvoicePDFPresigner{},
			),
			register: func(router *gin.Engine, handler *InvoiceHandler) {
				router.GET("/api/v1/invoices/:id/renders/:render_job_id", handler.GetRenderStatus)
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "invoice storage error containing not found",
			path: "/api/v1/invoices/" + invoiceID + "/pdf",
			handler: newInvoiceDownloadHandler(
				&handlerInvoiceDownloadRepository{err: errors.New(`relation "invoice_not_found_archive" does not exist`)},
				&handlerInvoiceRenderReader{},
				&handlerInvoicePDFPresigner{},
			),
			register: func(router *gin.Engine, handler *InvoiceHandler) {
				router.GET("/api/v1/invoices/:id/pdf", handler.GetPDF)
			},
			want: http.StatusInternalServerError,
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("business_id", businessID)
				c.Next()
			})
			fixture.register(router, fixture.handler)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, fixture.path, nil))
			if response.Code != fixture.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, fixture.want, response.Body.String())
			}
		})
	}
}
