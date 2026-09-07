package handlers

import (
	"errors"
	"invoice-backend/internal/services"
	"net/http"

	"github.com/gin-gonic/gin"
)

type AgentGovernanceHandler struct {
	service *services.GovernanceManagementService
}

func NewAgentGovernanceHandler(service *services.GovernanceManagementService) *AgentGovernanceHandler {
	return &AgentGovernanceHandler{service: service}
}

// Overview godoc
// @Summary Get agent execution controls and recent runs
// @Tags Agent Governance
// @Produce json
// @Security BearerAuth
// @Success 200 {object} services.GovernanceOverview
// @Failure 403 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /agent-governance [get]
func (h *AgentGovernanceHandler) Overview(c *gin.Context) {
	scope, ok := a2aNegotiationScope(c)
	if !ok {
		return
	}
	result, err := h.service.Overview(c.Request.Context(), scope.UserID, scope.BusinessID)
	if err != nil {
		governanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

type UpdateGovernanceRequest struct {
	ExecutionEnabled *bool `json:"execution_enabled" binding:"required"`
}

// Update godoc
// @Summary Pause or resume governed execution for the current business
// @Tags Agent Governance
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body UpdateGovernanceRequest true "Business execution control"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /agent-governance [put]
func (h *AgentGovernanceHandler) Update(c *gin.Context) {
	scope, ok := a2aNegotiationScope(c)
	if !ok {
		return
	}
	var request UpdateGovernanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "execution_enabled must be true or false"})
		return
	}
	if err := h.service.SetBusinessEnabled(c.Request.Context(), scope.UserID, scope.BusinessID, *request.ExecutionEnabled); err != nil {
		governanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"business_enabled": *request.ExecutionEnabled})
}

func governanceError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrAgentToolDenied) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to manage agent governance"})
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Agent governance could not be loaded. Please try again."})
}
