package handlers

import (
	"errors"
	"net/http"
	"strings"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
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
// @Param Idempotency-Key header string true "UUID idempotency key"
// @Param input body services.CreatePaymentInput true "Payment details"
// @Success 201 {object} models.Payment
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /payments [post]
func (h *PaymentHandler) Create(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("payment_handler").With("operation", "create")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.CreatePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid create payment payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.IdempotencyKey = idempotencyKey

	var payment *models.Payment
	payment, err := h.svc.CreateByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		log.Error("failed to create payment", "error", err, "invoice_id", input.InvoiceID)
		c.JSON(paymentCreateErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	log.Info("payment created", "payment_id", payment.ID, "invoice_id", payment.InvoiceID)

	c.JSON(http.StatusCreated, payment)
}

func paymentCreateErrorStatus(err error) int {
	var invalidKey *idempotency.InvalidKeyError
	var invalidPayload *idempotency.InvalidPayloadError
	if errors.As(err, &invalidKey) || errors.As(err, &invalidPayload) {
		return http.StatusBadRequest
	}
	var conflict *idempotency.ConflictError
	var inProgress *idempotency.InProgressError
	if errors.As(err, &conflict) || errors.As(err, &inProgress) {
		return http.StatusConflict
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "invoice not found") {
		return http.StatusNotFound
	}
	if strings.Contains(message, "exceeds invoice balance") || strings.Contains(message, "invoice is not payable") {
		return http.StatusConflict
	}
	if strings.Contains(message, "invalid payment") || strings.Contains(message, "withholding") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func paymentMutationErrorStatus(err error) int {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(message, "reason is required") {
		return http.StatusBadRequest
	}
	if strings.Contains(message, "immutable") || strings.Contains(message, "already reversed") ||
		strings.Contains(message, "cannot be reversed") || strings.Contains(message, "would violate") ||
		strings.Contains(message, "journal is not posted") || strings.Contains(message, "must be reversed through") {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
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
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var payment *models.Payment
	payment, err := h.svc.GetByBusiness(c.Request.Context(), businessID, id)
	if err != nil {
		log.Error("failed to get payment", "error", err, "payment_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "payment not found"})
		return
	}

	c.JSON(http.StatusOK, payment)
}

// List retrieves payments for the active business, optionally filtered by invoice.
// @Summary List payments
// @Description Returns a paginated list of payments recorded for the active business. Provide invoice_id to limit results to one invoice.
// @Tags Payments
// @Produce json
// @Security BearerAuth
// @Param invoice_id query string false "Invoice ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /payments [get]
func (h *PaymentHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("payment_handler").With("operation", "list")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)

	invoiceID := c.Query("invoice_id")
	var (
		payments []*models.Payment
		total    int64
		err      error
	)
	if invoiceID == "" {
		payments, total, err = h.svc.ListByBusiness(c.Request.Context(), businessID, page, limit)
	} else {
		payments, total, err = h.svc.ListByInvoiceAndBusiness(c.Request.Context(), businessID, invoiceID, page, limit)
	}
	if err != nil {
		log.Error("failed to list payments", "error", err, "invoice_id", invoiceID)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
			return
		}
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
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var input services.UpdatePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid update payment payload", "error", err, "payment_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var payment *models.Payment
	payment, err := h.svc.UpdateByBusiness(c.Request.Context(), businessID, id, input)
	if err != nil {
		log.Error("failed to update payment", "error", err, "payment_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "payment not found"})
			return
		}
		c.JSON(paymentMutationErrorStatus(err), gin.H{"error": err.Error()})
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
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	if err := h.svc.DeleteByBusiness(c.Request.Context(), businessID, id); err != nil {
		log.Error("failed to delete payment", "error", err, "payment_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "payment not found"})
			return
		}
		c.JSON(paymentMutationErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	log.Info("payment deleted", "payment_id", id)

	c.JSON(http.StatusNoContent, nil)
}

// Reverse creates the compensating accounting entry for a posted payment.
// @Summary Reverse payment
// @Description Reverse a posted payment, restore the invoice balance, and create a compensating journal entry.
// @Tags Payments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Payment ID"
// @Param input body services.ReversePaymentInput true "Payment reversal"
// @Success 200 {object} models.Payment
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /payments/{id}/reverse [post]
func (h *PaymentHandler) Reverse(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("payment_handler").With("operation", "reverse")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.ReversePaymentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid payment reversal payload", "error", err, "payment_id", c.Param("id"))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	payment, err := h.svc.ReverseByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		log.Error("failed to reverse payment", "error", err, "payment_id", c.Param("id"))
		c.JSON(paymentMutationErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	log.Info("payment reversed", "payment_id", payment.ID)
	c.JSON(http.StatusOK, payment)
}
