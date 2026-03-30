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

// GenerateRequest represents barcode generation request
type GenerateRequest struct {
	Prefix string `json:"prefix"`
	Seed   string `json:"seed"`
}

// Generate generates a new barcode value
// @Summary Generate barcode
// @Description Generates a new barcode value
// @Tags Barcodes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body GenerateRequest true "Barcode generation details"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /barcodes/generate [post]
func (h *BarcodeHandler) Generate(c *gin.Context) {
	var body GenerateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"value": h.svc.GenerateValue(body.Prefix, body.Seed)})
}

// AssignRequest represents barcode assignment request
type AssignRequest struct {
	ProductID string `json:"product_id" binding:"required,uuid"`
	VariantID string `json:"variant_id,omitempty"`
}

// Assign assigns a barcode to a product
// @Summary Assign barcode
// @Description Assigns a barcode to a product
// @Tags Barcodes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body AssignRequest true "Assignment details"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /barcodes/assign [post]
func (h *BarcodeHandler) Assign(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var body AssignRequest
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

// Lookup looks up a barcode value
// @Summary Lookup barcode
// @Description Looks up a barcode and returns product information
// @Tags Barcodes
// @Produce json
// @Security BearerAuth
// @Param value query string true "Barcode value"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /barcodes/lookup [get]
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

// RenderPNG renders a barcode as PNG image
// @Summary Render barcode as PNG
// @Description Renders a barcode as a PNG image
// @Tags Barcodes
// @Accept json
// @Produce image/png
// @Security BearerAuth
// @Param input body services.BarcodeRenderInput true "Barcode render details"
// @Success 200 {file} binary "PNG image"
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /barcodes/render/png [post]
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

// RenderSVG renders a barcode as SVG image
// @Summary Render barcode as SVG
// @Description Renders a barcode as an SVG image
// @Tags Barcodes
// @Accept json
// @Produce image/svg+xml
// @Security BearerAuth
// @Param input body services.BarcodeRenderInput true "Barcode render details"
// @Success 200 {file} binary "SVG image"
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /barcodes/render/svg [post]
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

// RenderPDF renders a barcode as PDF document
// @Summary Render barcode as PDF
// @Description Renders a barcode as a PDF document
// @Tags Barcodes
// @Accept json
// @Produce application/pdf
// @Security BearerAuth
// @Param input body services.BarcodeRenderInput true "Barcode render details"
// @Success 200 {file} binary "PDF document"
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /barcodes/render/pdf [post]
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
