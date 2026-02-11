package handlers

import (
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"
	"net/http"

	"github.com/gin-gonic/gin"
)

type InvoiceHandler struct {
	svc *services.InvoiceService
	log *logger.Logger
}

func NewInvoiceHandler(svc *services.InvoiceService, log *logger.Logger) *InvoiceHandler {
	return &InvoiceHandler{svc: svc, log: log}
}

// Create creates a new invoice
// @Summary Create invoice
// @Description Create a new invoice for a business and customer.
// @Tags Invoices
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateInvoiceInput true "Invoice details"
// @Success 201 {object} models.Invoice
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices [post]
func (h *InvoiceHandler) Create(c *gin.Context) {
	var input services.CreateInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var invoice *models.Invoice
	invoice, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, invoice)
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
	id := c.Param("id")
	var invoice *models.Invoice
	invoice, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
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
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

	invoices, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
	id := c.Param("id")
	var input services.UpdateInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var invoice *models.Invoice
	invoice, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// Send sends an invoice to the customer
// @Summary Send invoice
// @Description Trigger the delivery of an invoice to the customer (e.g., via email).
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param id path string true "Invoice ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/{id}/send [post]
func (h *InvoiceHandler) Send(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Send(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "invoice sent successfully"})
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
	id := c.Param("id")
	url, err := h.svc.GetPDFURL(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"pdf_url": url})
}

// NextNumber returns the next available invoice number
// @Summary Get next invoice number
// @Description Returns the incremented invoice number for the next invoice to be created.
// @Tags Invoices
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /invoices/next-number [get]
func (h *InvoiceHandler) NextNumber(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	number, err := h.svc.GetNextNumber(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"next_number": number})
}
