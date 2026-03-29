package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type BarcodeHandler struct {
	svc *services.BarcodeService
	log *logger.Logger
}

func NewBarcodeHandler(svc *services.BarcodeService, log *logger.Logger) *BarcodeHandler {
	return &BarcodeHandler{svc: svc, log: log}
}

func (h *BarcodeHandler) Generate(c *gin.Context) {
	var body struct {
		Prefix string `json:"prefix"`
		Seed   string `json:"seed"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"value": h.svc.GenerateValue(body.Prefix, body.Seed)})
}

func (h *BarcodeHandler) Assign(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var body struct {
		ProductID string `json:"product_id" binding:"required,uuid"`
		VariantID string `json:"variant_id,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	value, err := h.svc.EnsureBarcode(c.Request.Context(), businessID, body.ProductID, body.VariantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"value": value})
}

func (h *BarcodeHandler) Lookup(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	value := c.Query("value")
	result, err := h.svc.Lookup(c.Request.Context(), businessID, value)
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *BarcodeHandler) RenderPNG(c *gin.Context) {
	var input services.BarcodeRenderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := h.svc.RenderPNG(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "image/png", data)
}

func (h *BarcodeHandler) RenderSVG(c *gin.Context) {
	var input services.BarcodeRenderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := h.svc.RenderSVG(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "image/svg+xml", data)
}

func (h *BarcodeHandler) RenderPDF(c *gin.Context) {
	var input services.BarcodeRenderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := h.svc.RenderPDF(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/pdf", data)
}
