package handlers

import (
	"net/http"
	"invoice-backend/internal/models"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
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

// Create adds a new team member
// @Summary Add team member
// @Description Add a new team member to a business.
// @Tags Team
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateTeamMemberInput true "Team member details"
// @Success 201 {object} models.TeamMember
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /teams [post]
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

// Get retrieves a team member by ID
// @Summary Get team member
// @Description Returns the details of a specific team member.
// @Tags Team
// @Produce json
// @Security BearerAuth
// @Param id path string true "Team Member ID"
// @Success 200 {object} models.TeamMember
// @Failure 404 {object} map[string]string
// @Router /teams/{id} [get]
func (h *TeamHandler) Get(c *gin.Context) {
	id := c.Param("id")
	member, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "team member not found"})
		return
	}

	c.JSON(http.StatusOK, member)
}

// List retrieves all team members for a business
// @Summary List team members
// @Description Returns a list of team members belonging to a specific business.
// @Tags Team
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /teams [get]
func (h *TeamHandler) List(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

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

// Update updates a team member's information
// @Summary Update team member
// @Description Update the details of a specific team member.
// @Tags Team
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Team Member ID"
// @Param input body services.UpdateTeamMemberInput true "Team member updates"
// @Success 200 {object} models.TeamMember
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /teams/{id} [put]
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

// Delete removes a team member
// @Summary Delete team member
// @Description Remove a specific team member from a business.
// @Tags Team
// @Produce json
// @Security BearerAuth
// @Param id path string true "Team Member ID"
// @Success 204 "No Content"
// @Failure 500 {object} map[string]string
// @Router /teams/{id} [delete]
func (h *TeamHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
