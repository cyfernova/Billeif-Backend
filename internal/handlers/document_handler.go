package handlers

import (
	"net/http"

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

func (h *DocumentHandler) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateDocumentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	document, err := h.svc.CreateByType(c.Request.Context(), businessID, h.documentType, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

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
	document, err := h.svc.UpdateByType(c.Request.Context(), businessID, c.Param("id"), h.documentType, input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only draft documents can be updated" || err.Error() == "document type mismatch" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, document)
}

func (h *DocumentHandler) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
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

func (h *DocumentHandler) Cancel(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
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
	log *logger.Logger
}

func NewDocumentUtilityHandler(svc *services.DocumentService, log *logger.Logger) *DocumentUtilityHandler {
	return &DocumentUtilityHandler{svc: svc, log: log}
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
