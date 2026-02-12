package handlers

import (
	"net/http"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/gin-gonic/gin"
)

func requireOwnedAgent(c *gin.Context, ap2Repo interfaces.AP2Repository, agentID string) (*models.Agent, bool) {
	userID, ok := requireUserScope(c)
	if !ok {
		return nil, false
	}

	agent, err := ap2Repo.GetAgentByID(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return nil, false
	}

	businessID := middleware.GetEffectiveBusinessID(c)
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return nil, false
	}
	return agent, true
}
