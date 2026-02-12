package handlers

import (
	"invoice-backend/internal/models"
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type PaymentHandler struct {
	svc *services.PaymentService
	log *logger.Logger
}

func NewPaymentHandler(svc *services.PaymentService, log *logger.Logger) *PaymentHandler {
	return &PaymentHandler{svc: svc, log: log}
}

// Create creates a new payment record
// @Summary Create payment
// @Description Record a new payment for an invoice.
// @Tags Payments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreatePaymentInput true "Payment details"
// @Success 201 {object} models.Payment
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /payments [post]
func (h *PaymentHandler) Create(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("payment_handler").With("operation", "create")
	var input services.CreatePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid create payment payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var payment *models.Payment
	payment, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		log.Error("failed to create payment", "error", err, "invoice_id", input.InvoiceID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("payment created", "payment_id", payment.ID, "invoice_id", payment.InvoiceID)

	c.JSON(http.StatusCreated, payment)
}

// Get retrieves a payment record by ID
// @Summary Get payment
// @Description Returns the details of a specific payment.
// @Tags Payments
// @Produce json
// @Security BearerAuth
// @Param id path string true "Payment ID"
// @Success 200 {object} models.Payment
// @Failure 404 {object} map[string]string
// @Router /payments/{id} [get]
func (h *PaymentHandler) Get(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("payment_handler").With("operation", "get")
	id := c.Param("id")
	var payment *models.Payment
	payment, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get payment", "error", err, "payment_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "payment not found"})
		return
	}

	c.JSON(http.StatusOK, payment)
}

// List retrieves all payments for an invoice
// @Summary List payments
// @Description Returns a list of payments recorded for a specific invoice.
// @Tags Payments
// @Produce json
// @Security BearerAuth
// @Param invoice_id query string true "Invoice ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /payments [get]
func (h *PaymentHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("payment_handler").With("operation", "list")
	invoiceID := c.Query("invoice_id")
	if invoiceID == "" {
		log.Warn("missing invoice_id query param")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invoice_id is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

	payments, total, err := h.svc.ListByInvoice(c.Request.Context(), invoiceID, page, limit)
	if err != nil {
		log.Error("failed to list payments", "error", err, "invoice_id", invoiceID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("payments listed", "invoice_id", invoiceID, "count", len(payments), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  payments,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Update updates a payment record
// @Summary Update payment
// @Description Update the details of a specific payment.
// @Tags Payments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Payment ID"
// @Param input body services.UpdatePaymentInput true "Payment updates"
// @Success 200 {object} models.Payment
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /payments/{id} [put]
func (h *PaymentHandler) Update(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("payment_handler").With("operation", "update")
	id := c.Param("id")
	var input services.UpdatePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid update payment payload", "error", err, "payment_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var payment *models.Payment
	payment, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		log.Error("failed to update payment", "error", err, "payment_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("payment updated", "payment_id", payment.ID)

	c.JSON(http.StatusOK, payment)
}

// Delete deletes a payment record
// @Summary Delete payment
// @Description Remove a specific payment record.
// @Tags Payments
// @Produce json
// @Security BearerAuth
// @Param id path string true "Payment ID"
// @Success 204 "No Content"
// @Failure 500 {object} map[string]string
// @Router /payments/{id} [delete]
func (h *PaymentHandler) Delete(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("payment_handler").With("operation", "delete")
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		log.Error("failed to delete payment", "error", err, "payment_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("payment deleted", "payment_id", id)

	c.JSON(http.StatusNoContent, nil)
}
