package handlers

import (
	"errors"
	"net/http"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type BusinessHandler struct {
	svc *services.BusinessService
	log *logger.Logger
}

func NewBusinessHandler(svc *services.BusinessService, log *logger.Logger) *BusinessHandler {
	return &BusinessHandler{svc: svc, log: log}
}

// Create creates a new business profile
// @Summary Create business
// @Description Create a new business profile for the authenticated user.
// @Tags Businesses
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateBusinessInput true "Business details"
// @Success 201 {object} models.BusinessProfile
// @Failure 401 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /business-profiles [post]
func (h *BusinessHandler) Create(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("business_handler").With("operation", "create")
	userID, ok := requireUserScope(c)
	if !ok {
		log.Warn("unauthorized create business request")
		return
	}

	var input services.CreateBusinessInput
	if err := c.ShouldBindBodyWithJSON(&input); err != nil {
		log.Warn("invalid create business payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var business *models.BusinessProfile
	business, err := h.svc.Create(c.Request.Context(), userID, input)
	if err != nil {
		log.Error("failed to create business", "error", err, "owner_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("business created", "business_id", business.ID, "owner_id", userID)

	c.JSON(http.StatusCreated, business)
}

// Get retrieves a business profile by ID
// @Summary Get business
// @Description Returns the profile information of a specific business.
// @Tags Businesses
// @Produce json
// @Security BearerAuth
// @Param id path string true "Business ID"
// @Success 200 {object} models.BusinessProfile
// @Failure 404 {object} map[string]string
// @Router /business-profiles/{id} [get]
func (h *BusinessHandler) Get(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("business_handler").With("operation", "get")
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var business *models.BusinessProfile
	business, err := h.svc.GetByOwner(c.Request.Context(), userID, id)
	if err != nil {
		log.Error("failed to get business", "error", err, "business_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "business not found"})
		return
	}

	c.JSON(http.StatusOK, business)
}

// List retrieves all business profiles for the user
// @Summary List businesses
// @Description Returns a list of business profiles belonging to the authenticated user.
// @Tags Businesses
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /business-profiles [get]
func (h *BusinessHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("business_handler").With("operation", "list")
	userID, ok := requireUserScope(c)
	if !ok {
		log.Warn("unauthorized list businesses request")
		return
	}

	page, limit := utils.ParsePagination(c)

	businesses, total, err := h.svc.List(c.Request.Context(), userID, page, limit)
	if err != nil {
		log.Error("failed to list businesses", "error", err, "owner_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("businesses listed", "owner_id", userID, "count", len(businesses), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  businesses,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Update updates a business profile
// @Summary Update business
// @Description Update the profile information of a specific business.
// @Tags Businesses
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Business ID"
// @Param input body services.UpdateBusinessInput true "Business updates"
// @Success 200 {object} models.BusinessProfile
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /business-profiles/{id} [put]
func (h *BusinessHandler) Update(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("business_handler").With("operation", "update")
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var input services.UpdateBusinessInput
	if err := c.ShouldBindBodyWithJSON(&input); err != nil {
		log.Warn("invalid update business payload", "error", err, "business_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var business *models.BusinessProfile
	business, err := h.svc.UpdateByOwner(c.Request.Context(), userID, id, input)
	if err != nil {
		log.Error("failed to update business", "error", err, "business_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "business not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("business updated", "business_id", business.ID)

	c.JSON(http.StatusOK, business)
}

// Delete deletes a business profile
// @Summary Delete business
// @Description Remove a specific business profile.
// @Tags Businesses
// @Produce json
// @Security BearerAuth
// @Param id path string true "Business ID"
// @Success 204 "No Content"
// @Failure 500 {object} map[string]string
// @Router /business-profiles/{id} [delete]
func (h *BusinessHandler) Delete(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("business_handler").With("operation", "delete")
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	if err := h.svc.DeleteByOwner(c.Request.Context(), userID, id); err != nil {
		log.Error("failed to delete business", "error", err, "business_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "business not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("business deleted", "business_id", id)

	c.JSON(http.StatusNoContent, nil)
}

// UploadLogo creates a tenant-bound pending logo upload.
// @Summary Create business logo upload
// @Description Creates an opaque pending upload whose checksum, metadata, MIME type, and size are bound into the signed request. JPEG, PNG, and WebP are supported up to 5 MiB.
// @Tags Businesses
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Business ID"
// @Param input body services.BusinessLogoUploadInput true "Exact upload metadata"
// @Success 201 {object} services.PendingUploadCreated
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /business-profiles/{id}/logo [post]
func (h *BusinessHandler) UploadLogo(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("business_handler").With("operation", "upload_logo")
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	if middleware.GetBusinessID(c) != id {
		c.JSON(http.StatusNotFound, gin.H{"error": "business not found"})
		return
	}
	var input services.BusinessLogoUploadInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid business logo upload"})
		return
	}

	upload, err := h.svc.CreateLogoUpload(c.Request.Context(), userID, id, input)
	if err != nil {
		log.Error("failed to generate business logo upload URL", "error", err, "business_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "business not found"})
			return
		}
		if errors.Is(err, services.ErrBusinessLogoInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid business logo upload"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("business logo upload URL generated", "business_id", id)

	c.JSON(http.StatusCreated, upload)
}

// CompleteLogo verifies and attaches a pending business logo.
// @Summary Complete business logo upload
// @Description Verifies the tenant- and uploader-bound object metadata, atomically attaches it once, and cleans up the replaced logo after commit.
// @Tags Businesses
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Business ID"
// @Param input body services.BusinessLogoCompletionInput true "Pending upload identity"
// @Success 200 {object} services.BusinessLogoCompletion
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /business-profiles/{id}/logo/complete [post]
func (h *BusinessHandler) CompleteLogo(c *gin.Context) {
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	businessID := c.Param("id")
	if middleware.GetBusinessID(c) != businessID {
		c.JSON(http.StatusNotFound, gin.H{"error": "business not found"})
		return
	}
	var input services.BusinessLogoCompletionInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid business logo completion"})
		return
	}
	completed, err := h.svc.FinalizeLogoUpload(c.Request.Context(), userID, businessID, input.UploadID)
	if err != nil {
		if isNotFoundErr(err) || errors.Is(err, services.ErrPendingUploadNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "business logo upload not found"})
			return
		}
		if errors.Is(err, services.ErrBusinessLogoInvalid) {
			c.JSON(http.StatusConflict, gin.H{"error": "business logo upload could not be completed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "business logo upload could not be completed"})
		return
	}
	c.JSON(http.StatusOK, completed)
}
