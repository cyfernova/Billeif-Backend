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
