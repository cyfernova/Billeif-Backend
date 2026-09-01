package handlers

import (
	"errors"
	"net/http"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type DocumentHandler struct {
	svc          *services.DocumentService
	documentType string
	log          *logger.Logger
}

func NewDocumentHandler(svc *services.DocumentService, documentType string, log *logger.Logger) *DocumentHandler {
	return &DocumentHandler{svc: svc, documentType: documentType, log: log}
}

// List returns all documents for a business
// @Summary List documents
// @Description Returns all documents for the business
// @Tags Documents
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /documents [get]
func (h *DocumentHandler) List(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	documents, total, err := h.svc.ListByType(c.Request.Context(), businessID, h.documentType, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": documents, "total": total, "page": page, "limit": limit})
}

// Get retrieves a document by ID
// @Summary Get document
// @Description Returns a document by ID
// @Tags Documents
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /documents/{id} [get]
func (h *DocumentHandler) Get(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	document, err := h.svc.GetByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	if document.DocumentType != h.documentType {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	c.JSON(http.StatusOK, document)
}

// Create creates a new document
// @Summary Create document
// @Description Creates a new document for the business
// @Tags Documents
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string false "Required UUID for sales invoice documents"
// @Param input body services.CreateDocumentInput true "Document details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /documents [post]
func (h *DocumentHandler) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey := ""
	if h.documentType == models.DocumentTypeSalesInvoice {
		var present bool
		idempotencyKey, present = requireIdempotencyKey(c)
		if !present {
			return
		}
	}
	var input services.CreateDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.IdempotencyKey = idempotencyKey
	requestContextWithActor(c)
	document, err := h.svc.CreateByType(c.Request.Context(), businessID, h.documentType, input)
	if err != nil {
		c.JSON(invoiceCreateErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

// Update updates an existing document
// @Summary Update document
// @Description Updates an existing document by ID
// @Tags Documents
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Param input body services.CreateDocumentInput true "Document update details"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /documents/{id} [put]
func (h *DocumentHandler) Update(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	document, err := h.svc.UpdateByType(c.Request.Context(), businessID, c.Param("id"), h.documentType, input)
	if err != nil {
		c.JSON(documentUpdateErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, document)
}

func documentUpdateErrorStatus(err error) int {
	var conflict *models.DocumentDraftConflictError
	switch {
	case errors.As(err, &conflict):
		return http.StatusConflict
	case isNotFoundErr(err):
		return http.StatusNotFound
	case err.Error() == "only draft documents can be updated" || err.Error() == "document type mismatch":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// Delete deletes a document
// @Summary Delete document
// @Description Deletes a document by ID
// @Tags Documents
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Success 204 {string} string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /documents/{id} [delete]
func (h *DocumentHandler) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	requestContextWithActor(c)
	if err := h.svc.DeleteByType(c.Request.Context(), businessID, c.Param("id"), h.documentType); err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only draft documents can be deleted" || err.Error() == "document type mismatch" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// CancelRequest represents document cancellation request
type CancelRequest struct {
	Reason string `json:"reason"`
}

// Cancel cancels a document
// @Summary Cancel document
// @Description Cancels a document by ID
// @Tags Documents
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Param input body CancelRequest false "Cancellation reason"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /documents/{id}/cancel [post]
func (h *DocumentHandler) Cancel(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var body CancelRequest
	_ = c.ShouldBindJSON(&body)
	requestContextWithActor(c)
	document, err := h.svc.CancelByType(c.Request.Context(), businessID, c.Param("id"), h.documentType, body.Reason)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, document)
}

// GetPDF retrieves the PDF URL for a document
// @Summary Get document PDF
// @Description Returns the PDF URL for a document
// @Tags Documents
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /documents/{id}/pdf [get]
func (h *DocumentHandler) GetPDF(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	url, err := h.svc.GetPDFURLByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusNotFound
		if !isNotFoundErr(err) && err.Error() != "PDF not yet generated" {
			statusCode = http.StatusInternalServerError
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pdf_url": url})
}

type DocumentUtilityHandler struct {
	svc *services.DocumentService
	tax *services.TaxComplianceService
	log *logger.Logger
}

func NewDocumentUtilityHandler(svc *services.DocumentService, tax *services.TaxComplianceService, log *logger.Logger) *DocumentUtilityHandler {
	return &DocumentUtilityHandler{svc: svc, tax: tax, log: log}
}

func (h *DocumentUtilityHandler) Convert(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.ConvertDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	document, err := h.svc.ConvertByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "no convertible line items found" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *DocumentUtilityHandler) Merge(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.MergeDocumentsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestContextWithActor(c)
	document, err := h.svc.MergeByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "documents are not merge-compatible" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *DocumentUtilityHandler) Duplicate(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	document, err := h.svc.DuplicateByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *DocumentUtilityHandler) History(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	history, err := h.svc.GetHistory(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, history)
}

func (h *DocumentUtilityHandler) Render(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.RenderDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.svc.RequestRenderByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandler) GetPDF(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	url, err := h.svc.GetPDFURLByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusNotFound
		if !isNotFoundErr(err) && err.Error() != "PDF not yet generated" {
			statusCode = http.StatusInternalServerError
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pdf_url": url})
}

func (h *DocumentUtilityHandler) GetComplianceStatus(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	status, err := h.tax.GetComplianceStatus(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

func (h *DocumentUtilityHandler) GenerateEInvoice(c *gin.Context) {
	requestContextWithActor(c)
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
	job, err := h.tax.GenerateEInvoiceByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		if writeSubscriptionControlError(c, err) {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandler) GetEInvoice(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	record, err := h.tax.GetEInvoiceByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "e-invoice not found"})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *DocumentUtilityHandler) CancelEInvoice(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.CancelEInvoiceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.tax.CancelEInvoiceByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		if writeSubscriptionControlError(c, err) {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandler) GenerateEWayBill(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.GenerateEWayBillInput
	_ = c.ShouldBindJSON(&input)
	job, err := h.tax.GenerateEWayBillByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		if writeSubscriptionControlError(c, err) {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandler) GetEWayBill(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	record, err := h.tax.GetEWayBillByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "e-way bill not found"})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *DocumentUtilityHandler) GetEWayBillPDF(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	pdfURL, err := h.tax.GetEWayBillPDFByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pdf_url": pdfURL})
}

func (h *DocumentUtilityHandler) UpdateEWayPartB(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.UpdateEWayPartBInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.tax.UpdateEWayPartBByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		if writeSubscriptionControlError(c, err) {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *DocumentUtilityHandler) InitiateMultiVehicle(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.MultiVehicleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.tax.InitiateMultiVehicleByDocument(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		if writeSubscriptionControlError(c, err) {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}
