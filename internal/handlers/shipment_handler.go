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

// GetByDocument retrieves shipment by document ID
// @Summary Get shipment by document
// @Description Returns shipment information for a document
// @Tags Shipments
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /shipments/documents/{id} [get]
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

// UpsertByDocument creates or updates shipment for a document
// @Summary Upsert shipment by document
// @Description Creates or updates shipment for a document
// @Tags Shipments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Param input body services.ShippingRequestInput true "Shipment details"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /shipments/documents/{id} [post]
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

// GetLabelByDocument retrieves shipping label for a document
// @Summary Get shipping label
// @Description Returns shipping label for a document
// @Tags Shipments
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /shipments/documents/{id}/label [get]
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

// GenerateLabelByDocument generates shipping label for a document
// @Summary Generate shipping label
// @Description Generates and returns shipping label for a document
// @Tags Shipments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Document ID"
// @Param input body services.ShippingRequestInput false "Label generation details"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /shipments/documents/{id}/label [post]
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
