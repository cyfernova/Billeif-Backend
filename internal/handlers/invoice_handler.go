package handlers

import (
	"errors"
	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/invoiceresolution"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type InvoiceHandler struct {
	svc        *services.InvoiceService
	compliance *services.TaxComplianceService
	log        *logger.Logger
}

func NewInvoiceHandler(svc *services.InvoiceService, compliance *services.TaxComplianceService, log *logger.Logger) *InvoiceHandler {
	return &InvoiceHandler{svc: svc, compliance: compliance, log: log}
}

// Create creates a new invoice
// @Summary Create invoice
// @Description Create a new invoice for a business and customer.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string true "UUID idempotency key"
// @Param input body services.CreateInvoiceInput true "Invoice details"
// @Success 201 {object} models.Invoice
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /invoices [post]
func (h *InvoiceHandler) Create(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "create")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.CreateInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid create invoice payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	input.IdempotencyKey = idempotencyKey
	requestContextWithActor(c)

	var invoice *models.Invoice
	invoice, err := h.svc.CreateByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		log.Error("failed to create invoice", "error", err, "business_id", input.BusinessID, "customer_id", input.CustomerID)
		c.JSON(invoiceCreateErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	log.Info("invoice created", "invoice_id", invoice.ID, "business_id", invoice.BusinessID, "invoice_no", invoice.InvoiceNo)

	c.JSON(http.StatusCreated, invoice)
}

func invoiceCreateErrorStatus(err error) int {
	var invalidPayload *idempotency.InvalidPayloadError
	if errors.As(err, &invalidPayload) {
		return http.StatusBadRequest
	}
	var invalidKey *idempotency.InvalidKeyError
	if errors.As(err, &invalidKey) {
		return http.StatusBadRequest
	}
	var conflict *idempotency.ConflictError
	if errors.As(err, &conflict) {
		return http.StatusConflict
	}
	var inProgress *idempotency.InProgressError
	if errors.As(err, &inProgress) {
		return http.StatusConflict
	}
	var resolverUnavailable *invoiceresolution.UnavailableError
	if errors.As(err, &resolverUnavailable) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

// Issue freezes and numbers a canonical draft invoice.
// @Summary Issue invoice
// @Description Atomically assigns the legal invoice number, freezes the draft, and queues the final private render.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Param Idempotency-Key header string true "UUID idempotency key"
// @Param If-Match header string true "Expected invoice version"
// @Param input body services.IssueInvoiceInput true "Issue details"
// @Success 202 {object} services.IssueInvoiceResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{id}/issue [post]
func (h *InvoiceHandler) Issue(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	expectedVersion, ok := requireInvoiceIfMatch(c)
	if !ok {
		return
	}
	var input services.IssueInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.IdempotencyKey = idempotencyKey
	input.ExpectedVersion = expectedVersion
	requestContextWithActor(c)
	result, err := h.svc.IssueByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		c.JSON(invoiceIssueErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func requireInvoiceIfMatch(c *gin.Context) (int, bool) {
	value := strings.TrimSpace(c.GetHeader("If-Match"))
	value = strings.TrimSpace(strings.Trim(value, `"`))
	version, err := strconv.Atoi(value)
	if err != nil || version < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "If-Match expected invoice version is required"})
		return 0, false
	}
	return version, true
}

func invoiceIssueErrorStatus(err error) int {
	var invalidKey *idempotency.InvalidKeyError
	var invalidPayload *idempotency.InvalidPayloadError
	var invalidSeries *invoiceissue.InvalidSeriesError
	var invalidDocumentType *invoiceissue.InvalidDocumentTypeError
	var invalidLifecycle *invoiceissue.InvalidLifecycleError
	var invalidTimezone *invoiceissue.InvalidTimezoneError
	if errors.As(err, &invalidKey) ||
		errors.As(err, &invalidPayload) ||
		errors.As(err, &invalidSeries) ||
		errors.As(err, &invalidDocumentType) ||
		errors.As(err, &invalidLifecycle) ||
		errors.As(err, &invalidTimezone) {
		return http.StatusBadRequest
	}
	var notFound *invoiceissue.NotFoundError
	if errors.As(err, &notFound) {
		return http.StatusNotFound
	}
	var stale *invoiceissue.StaleVersionError
	var alreadyIssued *invoiceissue.AlreadyIssuedError
	var conflict *idempotency.ConflictError
	var inProgress *idempotency.InProgressError
	var exhausted *invoiceissue.SequenceExhaustedError
	if errors.As(err, &stale) ||
		errors.As(err, &alreadyIssued) ||
		errors.As(err, &conflict) ||
		errors.As(err, &inProgress) ||
		errors.As(err, &exhausted) {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

// Preview queues a private PDF render for the current draft invoice version.
// @Summary Preview invoice
// @Description Atomically queues a private PDF preview for the current draft invoice version.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Param Idempotency-Key header string true "UUID idempotency key"
// @Success 202 {object} services.PreviewInvoiceResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{id}/previews [post]
func (h *InvoiceHandler) Preview(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	requestContextWithActor(c)
	result, err := h.svc.PreviewByBusiness(
		c.Request.Context(),
		businessID,
		c.Param("id"),
		services.PreviewInvoiceInput{IdempotencyKey: idempotencyKey},
	)
	if err != nil {
		status, message := invoicePreviewErrorResponse(err)
		h.log.Error(
			"failed to request invoice preview",
			"error", err,
			"business_id", businessID,
			"invoice_id", c.Param("id"),
		)
		c.JSON(status, gin.H{"error": message})
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func invoicePreviewErrorResponse(err error) (int, string) {
	var invalidKey *idempotency.InvalidKeyError
	if errors.As(err, &invalidKey) {
		return http.StatusBadRequest, "a UUID idempotency key is required"
	}
	var invalidPayload *idempotency.InvalidPayloadError
	var invalidLifecycle *invoiceissue.InvalidLifecycleError
	if errors.As(err, &invalidPayload) || errors.As(err, &invalidLifecycle) {
		return http.StatusBadRequest, "invalid invoice preview request"
	}
	var notFound *invoiceissue.NotFoundError
	if errors.As(err, &notFound) {
		return http.StatusNotFound, "invoice not found"
	}
	var conflict *idempotency.ConflictError
	if errors.As(err, &conflict) {
		return http.StatusConflict, "idempotency key conflicts with a different request"
	}
	var inProgress *idempotency.InProgressError
	if errors.As(err, &inProgress) {
		return http.StatusConflict, "idempotent request is still in progress"
	}
	return http.StatusInternalServerError, "invoice preview unavailable"
}

// Get retrieves an invoice by ID
// @Summary Get invoice
// @Description Returns the details of a specific invoice.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} models.Invoice
// @Failure 404 {object} map[string]string
// @Router /invoices/{id} [get]
func (h *InvoiceHandler) Get(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "get")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var invoice *models.Invoice
	invoice, err := h.svc.GetByBusiness(c.Request.Context(), businessID, id)
	if err != nil {
		log.Error("failed to get invoice", "error", err, "invoice_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
		return
	}

	c.JSON(http.StatusOK, invoice)
}

// List retrieves all invoices for a business
// @Summary List invoices
// @Description Returns a list of invoices belonging to a specific business.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices [get]
func (h *InvoiceHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "list")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	page, limit := utils.ParsePagination(c)

	invoices, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		log.Error("failed to list invoices", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("invoices listed", "business_id", businessID, "count", len(invoices), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  invoices,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Update updates an invoice
// @Summary Update invoice
// @Description Update the details of a specific invoice.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Param input body services.UpdateInvoiceInput true "Invoice updates"
// @Success 200 {object} models.Invoice
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{id} [put]
func (h *InvoiceHandler) Update(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "update")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var input services.UpdateInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid update invoice payload", "error", err, "invoice_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)

	var invoice *models.Invoice
	invoice, err := h.svc.UpdateByBusiness(c.Request.Context(), businessID, id, input)
	if err != nil {
		log.Error("failed to update invoice", "error", err, "invoice_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("invoice updated", "invoice_id", invoice.ID)

	c.JSON(http.StatusOK, invoice)
}

// UpdateDraft updates the mutable canonical state for a draft invoice.
// @Summary Update draft invoice
// @Description Replaces draft customer snapshot, items, details, template, payment display, and compliance draft JSON transactionally.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Param input body services.UpdateInvoiceDraftInput true "Draft invoice updates"
// @Success 200 {object} models.Invoice
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{id}/draft [patch]
func (h *InvoiceHandler) UpdateDraft(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "update_draft")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var input services.UpdateInvoiceDraftInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid update draft invoice payload", "error", err, "invoice_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)

	invoice, err := h.svc.UpdateDraftByBusiness(c.Request.Context(), businessID, id, input)
	if err != nil {
		log.Error("failed to update draft invoice", "error", err, "invoice_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
			return
		}
		if err.Error() == "invoice version conflict" {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if err.Error() == "only draft invoices can be updated through draft endpoint" || err.Error() == "signed invoices are immutable" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("draft invoice updated", "invoice_id", invoice.ID, "version", invoice.Version)

	c.JSON(http.StatusOK, invoice)
}

// Delete deletes an invoice
// @Summary Delete invoice
// @Description Remove a specific invoice.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Success 204 "No Content"
// @Failure 500 {object} map[string]string
// @Router /invoices/{id} [delete]
func (h *InvoiceHandler) Delete(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "delete")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	requestContextWithActor(c)
	if err := h.svc.DeleteByBusiness(c.Request.Context(), businessID, id); err != nil {
		log.Error("failed to delete invoice", "error", err, "invoice_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("invoice deleted", "invoice_id", id)

	c.JSON(http.StatusNoContent, nil)
}

// GetRenderStatus returns the status of one invoice render job.
// @Summary Get invoice render status
// @Description Returns a safe status projection for an invoice render job.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param invoice_id path string true "Invoice ID"
// @Param render_job_id path string true "Render job ID"
// @Success 200 {object} services.InvoiceRenderStatus
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{invoice_id}/renders/{render_job_id} [get]
func (h *InvoiceHandler) GetRenderStatus(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "get_render_status")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	invoiceID := c.Param("id")
	renderJobID := c.Param("render_job_id")
	status, err := h.svc.GetRenderStatusByBusiness(
		c.Request.Context(),
		businessID,
		invoiceID,
		renderJobID,
	)
	if err != nil {
		log.Error("failed to get invoice render status", "error", err, "invoice_id", invoiceID, "render_job_id", renderJobID)
		var invalidPayload *idempotency.InvalidPayloadError
		switch {
		case errors.As(err, &invalidPayload):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid invoice render request"})
		case errors.Is(err, interfaces.ErrInvoiceRenderNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "render job not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invoice render status unavailable"})
		}
		return
	}
	c.JSON(http.StatusOK, status)
}

// GetPDF returns a presigned URL for the invoice PDF
// @Summary Get invoice PDF
// @Description Returns a presigned S3 URL to download the invoice in PDF format.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} services.InvoicePDFDownload
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{id}/pdf [get]
func (h *InvoiceHandler) GetPDF(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "get_pdf")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	download, err := h.svc.GetPDFDownloadByBusiness(c.Request.Context(), businessID, id)
	if err != nil {
		log.Error("failed to get invoice PDF URL", "error", err, "invoice_id", id)
		var invalidPayload *idempotency.InvalidPayloadError
		switch {
		case errors.As(err, &invalidPayload):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid invoice request"})
		case errors.Is(err, services.ErrInvoicePDFNotReady):
			c.JSON(http.StatusConflict, gin.H{"error": "invoice PDF is not ready"})
		case errors.Is(err, interfaces.ErrInvoiceNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invoice PDF unavailable"})
		}
		return
	}
	log.Debug("invoice PDF URL fetched", "invoice_id", id)

	c.JSON(http.StatusOK, download)
}

// Deliver queues canonical invoice email delivery after its final render is ready.
// @Summary Deliver invoice
// @Description Creates an idempotent invoice email delivery request.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Param Idempotency-Key header string true "UUID idempotency key"
// @Param input body services.DeliverInvoiceInput true "Delivery recipient"
// @Success 202 {object} services.DeliverInvoiceResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{id}/deliveries [post]
func (h *InvoiceHandler) Deliver(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.DeliverInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid invoice delivery request"})
		return
	}
	input.IdempotencyKey = idempotencyKey
	requestContextWithActor(c)
	result, err := h.svc.DeliverByBusiness(
		c.Request.Context(), businessID, c.Param("id"), input,
	)
	if err != nil {
		status, message := invoiceDeliveryErrorResponse(err)
		c.JSON(status, gin.H{"error": message})
		return
	}
	c.JSON(http.StatusAccepted, result)
}

// GetDeliveryStatus returns a safe projection of an invoice delivery attempt.
// @Summary Get invoice delivery status
// @Description Returns tenant-scoped invoice delivery status without provider or lease details.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Param delivery_id path string true "Delivery ID"
// @Success 200 {object} services.InvoiceDeliveryStatus
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{id}/deliveries/{delivery_id} [get]
func (h *InvoiceHandler) GetDeliveryStatus(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	status, err := h.svc.GetDeliveryStatusByBusiness(
		c.Request.Context(), businessID, c.Param("id"), c.Param("delivery_id"),
	)
	if err != nil {
		var invalidPayload *idempotency.InvalidPayloadError
		switch {
		case errors.As(err, &invalidPayload):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid invoice delivery request"})
		case errors.Is(err, interfaces.ErrInvoiceDeliveryNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "invoice delivery not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invoice delivery status unavailable"})
		}
		return
	}
	c.JSON(http.StatusOK, status)
}

func invoiceDeliveryErrorResponse(err error) (int, string) {
	var invalidKey *idempotency.InvalidKeyError
	var invalidPayload *idempotency.InvalidPayloadError
	if errors.As(err, &invalidKey) || errors.As(err, &invalidPayload) {
		return http.StatusBadRequest, "invalid invoice delivery request"
	}
	if errors.Is(err, interfaces.ErrInvoiceNotFound) {
		return http.StatusNotFound, "invoice not found"
	}
	var conflict *idempotency.ConflictError
	var inProgress *idempotency.InProgressError
	if errors.Is(err, interfaces.ErrInvoiceNotDeliverable) ||
		errors.As(err, &conflict) || errors.As(err, &inProgress) {
		return http.StatusConflict, "invoice delivery conflicts with current state"
	}
	return http.StatusInternalServerError, "invoice delivery unavailable"
}

func (h *InvoiceHandler) GenerateEInvoice(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.GenerateEInvoiceInput
	_ = c.ShouldBindJSON(&input)
	job, err := h.compliance.GenerateEInvoiceByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}
