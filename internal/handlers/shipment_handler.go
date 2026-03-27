package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type ShipmentHandler struct {
	shipping  *services.ShippingService
	documents *services.DocumentService
	log       *logger.Logger
}

func NewShipmentHandler(shipping *services.ShippingService, documents *services.DocumentService, log *logger.Logger) *ShipmentHandler {
	return &ShipmentHandler{shipping: shipping, documents: documents, log: log}
}

func (h *ShipmentHandler) GetByDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	shipment, err := h.shipping.GetShipmentByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}
	c.JSON(http.StatusOK, shipment)
}

func (h *ShipmentHandler) UpsertByDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	document, err := h.documents.GetByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	var input services.ShippingRequestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	shipment, err := h.shipping.CreateOrUpdateShipmentForDocument(c.Request.Context(), document, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, shipment)
}

func (h *ShipmentHandler) GetLabelByDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	label, err := h.shipping.GetShippingLabelByDocument(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipping label not found"})
		return
	}
	c.JSON(http.StatusOK, label)
}

func (h *ShipmentHandler) GenerateLabelByDocument(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	document, err := h.documents.GetByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	var input services.ShippingRequestInput
	if err := c.ShouldBindJSON(&input); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.shipping.CreateLabelForDocument(c.Request.Context(), document, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
