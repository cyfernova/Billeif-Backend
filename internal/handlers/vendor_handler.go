package handlers

import (
	"invoice-backend/internal/models"
	"net/http"

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
	log := logger.FromContext(c.Request.Context()).Named("vendor_handler").With("operation", "create")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateVendorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid create vendor payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID

	var vendor *models.Vendor
	vendor, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		log.Error("failed to create vendor", "error", err, "business_id", input.BusinessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("vendor created", "vendor_id", vendor.ID, "business_id", vendor.BusinessID)

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
	log := logger.FromContext(c.Request.Context()).Named("vendor_handler").With("operation", "get")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var vendor *models.Vendor
	vendor, err := h.svc.GetByBusiness(c.Request.Context(), businessID, id)
	if err != nil {
		log.Error("failed to get vendor", "error", err, "vendor_id", id)
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
	log := logger.FromContext(c.Request.Context()).Named("vendor_handler").With("operation", "list")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	page, limit := utils.ParsePagination(c)

	vendors, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		log.Error("failed to list vendors", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("vendors listed", "business_id", businessID, "count", len(vendors), "total", total)

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
	log := logger.FromContext(c.Request.Context()).Named("vendor_handler").With("operation", "update")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var input services.UpdateVendorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid update vendor payload", "error", err, "vendor_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var vendor *models.Vendor
	vendor, err := h.svc.UpdateByBusiness(c.Request.Context(), businessID, id, input)
	if err != nil {
		log.Error("failed to update vendor", "error", err, "vendor_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "vendor not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("vendor updated", "vendor_id", vendor.ID)

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
	log := logger.FromContext(c.Request.Context()).Named("vendor_handler").With("operation", "delete")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	if err := h.svc.DeleteByBusiness(c.Request.Context(), businessID, id); err != nil {
		log.Error("failed to delete vendor", "error", err, "vendor_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "vendor not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("vendor deleted", "vendor_id", id)

	c.JSON(http.StatusNoContent, nil)
}
