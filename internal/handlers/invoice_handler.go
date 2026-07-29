package handlers

import (
	"errors"
	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/models"
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

// GetPDF returns a presigned URL for the invoice PDF
// @Summary Get invoice PDF
// @Description Returns a presigned S3 URL to download the invoice in PDF format.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /invoices/{id}/pdf [get]
func (h *InvoiceHandler) GetPDF(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("invoice_handler").With("operation", "get_pdf")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	url, err := h.svc.GetPDFURLByBusiness(c.Request.Context(), businessID, id)
	if err != nil {
		log.Error("failed to get invoice PDF URL", "error", err, "invoice_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	log.Debug("invoice PDF URL fetched", "invoice_id", id)

	c.JSON(http.StatusOK, gin.H{"pdf_url": url})
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
