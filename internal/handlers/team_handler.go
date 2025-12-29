package handlers

import (
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type TeamHandler struct {
	svc *services.TeamService
	log *logger.Logger
}

func NewTeamHandler(svc *services.TeamService, log *logger.Logger) *TeamHandler {
	return &TeamHandler{svc: svc, log: log}
}

func (h *TeamHandler) Create(c *gin.Context) {
	var input services.CreateTeamMemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	member, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, member)
}

func (h *TeamHandler) Get(c *gin.Context) {
	id := c.Param("id")
	member, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "team member not found"})
		return
	}

	c.JSON(http.StatusOK, member)
}

func (h *TeamHandler) List(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	members, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  members,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *TeamHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdateTeamMemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	member, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, member)
}

func (h *TeamHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
