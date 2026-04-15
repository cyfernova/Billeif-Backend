package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type RenderProfileHandler struct {
	svc *services.DocumentService
	log *logger.Logger
}

func NewRenderProfileHandler(svc *services.DocumentService, log *logger.Logger) *RenderProfileHandler {
	return &RenderProfileHandler{svc: svc, log: log}
}

// List returns all render profiles for a business
// @Summary List render profiles
// @Description Returns all render profiles for the business
// @Tags Render Profiles
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /render-profiles [get]
func (h *RenderProfileHandler) List(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	profiles, total, err := h.svc.ListRenderProfilesByBusiness(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": profiles, "total": total, "page": page, "limit": limit})
}

// Get retrieves a render profile by ID
// @Summary Get render profile
// @Description Returns a render profile by ID
// @Tags Render Profiles
// @Produce json
// @Security BearerAuth
// @Param id path string true "Render Profile ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /render-profiles/{id} [get]
func (h *RenderProfileHandler) Get(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	profile, err := h.svc.GetRenderProfileByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "render profile not found"})
		return
	}
	c.JSON(http.StatusOK, profile)
}

// GetDefault retrieves the default render profile for a business
// @Summary Get default render profile
// @Description Returns the default render profile for the business
// @Tags Render Profiles
// @Produce json
// @Security BearerAuth
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /render-profiles/default [get]
func (h *RenderProfileHandler) GetDefault(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	profile, err := h.svc.GetDefaultRenderProfileByBusiness(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "render profile not found"})
		return
	}
	c.JSON(http.StatusOK, profile)
}

// Create creates a new render profile
// @Summary Create render profile
// @Description Creates a new render profile for the business
// @Tags Render Profiles
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateRenderProfileInput true "Render profile details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /render-profiles [post]
func (h *RenderProfileHandler) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateRenderProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	profile, err := h.svc.CreateRenderProfileByBusiness(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, profile)
}

// Update updates an existing render profile
// @Summary Update render profile
// @Description Updates an existing render profile by ID
// @Tags Render Profiles
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Render Profile ID"
// @Param input body services.UpdateRenderProfileInput true "Render profile update details"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /render-profiles/{id} [put]
func (h *RenderProfileHandler) Update(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpdateRenderProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	profile, err := h.svc.UpdateRenderProfileByBusiness(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, profile)
}

// SetDefault marks a render profile as default for the business.
// @Summary Set default render profile
// @Description Marks the specified render profile as default for the business
// @Tags Render Profiles
// @Produce json
// @Security BearerAuth
// @Param id path string true "Render Profile ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /render-profiles/{id}/default [post]
func (h *RenderProfileHandler) SetDefault(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	value := true
	profile, err := h.svc.UpdateRenderProfileByBusiness(c.Request.Context(), businessID, c.Param("id"), services.UpdateRenderProfileInput{
		IsDefault: &value,
	})
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, profile)
}

// Delete deletes a render profile
// @Summary Delete render profile
// @Description Deletes a render profile by ID
// @Tags Render Profiles
// @Produce json
// @Security BearerAuth
// @Param id path string true "Render Profile ID"
// @Success 204 {string} string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /render-profiles/{id} [delete]
func (h *RenderProfileHandler) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteRenderProfileByBusiness(c.Request.Context(), businessID, c.Param("id")); err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}
