package handlers

import (
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
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var input services.CreateBusinessInput
	if err := c.ShouldBindBodyWithJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var business *models.BusinessProfile
	business, err := h.svc.Create(c.Request.Context(), userID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
	id := c.Param("id")
	var business *models.BusinessProfile
	business, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
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
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	page, limit := utils.ParsePagination(c)

	businesses, total, err := h.svc.List(c.Request.Context(), userID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
	id := c.Param("id")
	var input services.UpdateBusinessInput
	if err := c.ShouldBindBodyWithJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var business *models.BusinessProfile
	business, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// UploadLogo generates a presigned URL for logo upload
// @Summary Upload business logo
// @Description Returns a presigned S3 URL to upload a business logo.
// @Tags Businesses
// @Produce json
// @Security BearerAuth
// @Param id path string true "Business ID"
// @Param Content-Type header string false "MIME type (default: image/png)"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /business-profiles/{id}/logo [post]
func (h *BusinessHandler) UploadLogo(c *gin.Context) {
	id := c.Param("id")
	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}

	url, err := h.svc.GetLogoUploadURL(c.Request.Context(), id, contentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"upload_url": url})
}
