package handlers

import (
	"net/http"
	"invoice-backend/internal/models"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type VendorHandler struct {
	svc *services.VendorService
	log *logger.Logger
}

func NewVendorHandler(svc *services.VendorService, log *logger.Logger) *VendorHandler {
	return &VendorHandler{svc: svc, log: log}
}

// Create creates a new vendor
// @Summary Create vendor
// @Description Create a new vendor for a business.
// @Tags Vendors
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateVendorInput true "Vendor details"
// @Success 201 {object} models.Vendor
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /vendors [post]
func (h *VendorHandler) Create(c *gin.Context) {
	var input services.CreateVendorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	vendor, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, vendor)
}

// Get retrieves a vendor by ID
// @Summary Get vendor
// @Description Returns the details of a specific vendor.
// @Tags Vendors
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vendor ID"
// @Success 200 {object} models.Vendor
// @Failure 404 {object} map[string]string
// @Router /vendors/{id} [get]
func (h *VendorHandler) Get(c *gin.Context) {
	id := c.Param("id")
	vendor, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "vendor not found"})
		return
	}

	c.JSON(http.StatusOK, vendor)
}

// List retrieves all vendors for a business
// @Summary List vendors
// @Description Returns a list of vendors belonging to a specific business.
// @Tags Vendors
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /vendors [get]
func (h *VendorHandler) List(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

	vendors, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  vendors,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Update updates a vendor's information
// @Summary Update vendor
// @Description Update the details of a specific vendor.
// @Tags Vendors
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vendor ID"
// @Param input body services.UpdateVendorInput true "Vendor updates"
// @Success 200 {object} models.Vendor
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /vendors/{id} [put]
func (h *VendorHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdateVendorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	vendor, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, vendor)
}

// Delete deletes a vendor
// @Summary Delete vendor
// @Description Remove a specific vendor.
// @Tags Vendors
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vendor ID"
// @Success 204 "No Content"
// @Failure 500 {object} map[string]string
// @Router /vendors/{id} [delete]
func (h *VendorHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
