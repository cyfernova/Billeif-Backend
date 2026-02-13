package handlers

import (
	"net/http"
	"strconv"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// AgentDiscoveryHandler handles agent discovery endpoints
type AgentDiscoveryHandler struct {
	discovery *services.AgentDiscoveryService
	log       *logger.Logger
}

// NewAgentDiscoveryHandler creates a new agent discovery handler
func NewAgentDiscoveryHandler(discovery *services.AgentDiscoveryService, log *logger.Logger) *AgentDiscoveryHandler {
	return &AgentDiscoveryHandler{
		discovery: discovery,
		log:       log,
	}
}

// GetAgentCard handles the well-known URI for agent discovery
// GET /.well-known/agent-card.json
func (h *AgentDiscoveryHandler) GetAgentCard(c *gin.Context) {
	// This would be called for a specific agent's well-known endpoint
	// Extract domain or agent info from request
	// Return the agent card in AP2 format

	// For now, return a placeholder
	c.JSON(http.StatusOK, gin.H{
		"error": "agent card endpoint not configured for this domain",
	})
}

// RegisterAgent registers an agent in the discovery registry
// @Summary Register agent
// @Description Register an agent in the discovery registry.
// @Tags Discovery
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.RegisterAgentRequest true "Agent registration details"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/register [post]
func (h *AgentDiscoveryHandler) RegisterAgent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req services.RegisterAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	registry, err := h.discovery.RegisterAgent(c.Request.Context(), &req)
	if err != nil {
		h.log.Error("failed to register agent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register agent"})
		return
	}

	h.log.Info("agent registered", "user_id", userID, "registry_id", registry.ID.String())
	c.JSON(http.StatusCreated, registry)
}

// DiscoverAgents searches for agents
// @Summary Discover agents
// @Description Search for agents using various filters like type, capability, jurisdiction, and currency.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param query query string false "Search query"
// @Param type query string false "Agent type filter"
// @Param capability query string false "Capability filter"
// @Param jurisdiction query string false "Jurisdiction filter"
// @Param currency query string false "Currency filter"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /discovery/agents [get]
func (h *AgentDiscoveryHandler) DiscoverAgents(c *gin.Context) {
	query := c.DefaultQuery("query", "")
	agentType := c.DefaultQuery("type", "")
	capability := c.DefaultQuery("capability", "")
	jurisdiction := c.DefaultQuery("jurisdiction", "")
	currency := c.DefaultQuery("currency", "")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	// Build filter
	filter := &models.AgentDiscoveryFilter{
		ExcludeDeleted: true,
	}

	if agentType != "" {
		filter.AgentTypes = []string{agentType}
	}

	if capability != "" {
		filter.Capabilities = []string{capability}
	}

	if jurisdiction != "" {
		filter.Jurisdictions = []string{jurisdiction}
	}

	if currency != "" {
		filter.Currencies = []string{currency}
	}

	agents, total, err := h.discovery.DiscoverAgents(c.Request.Context(), query, filter, page, limit)
	if err != nil {
		h.log.Error("failed to discover agents", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to discover agents"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"agents": agents,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

// GetPublicAgents retrieves all public agents
// @Summary Get public agents
// @Description Retrieve all publicly available agents with pagination.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/public [get]
func (h *AgentDiscoveryHandler) GetPublicAgents(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	agents, total, err := h.discovery.GetPublicAgents(c.Request.Context(), page, limit)
	if err != nil {
		h.log.Error("failed to get public agents", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get public agents"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"agents": agents,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

// GetVerifiedAgents retrieves all verified agents
// @Summary Get verified agents
// @Description Retrieve all verified agents, optionally filtered by type.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param type query string false "Agent type filter"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/verified [get]
func (h *AgentDiscoveryHandler) GetVerifiedAgents(c *gin.Context) {
	agentType := c.DefaultQuery("type", "")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	agents, total, err := h.discovery.GetVerifiedAgents(c.Request.Context(), agentType, page, limit)
	if err != nil {
		h.log.Error("failed to get verified agents", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get verified agents"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"agents": agents,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

// GetAgentsByCapability retrieves agents with specific capabilities
// @Summary Get agents by capability
// @Description Retrieve agents that have specific capabilities.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param capability query []string true "Capability filter (can specify multiple)"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/by-capability [get]
func (h *AgentDiscoveryHandler) GetAgentsByCapability(c *gin.Context) {
	capabilities := c.QueryArray("capability")

	if len(capabilities) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one capability is required"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	agents, total, err := h.discovery.GetAgentsByCapability(c.Request.Context(), capabilities, page, limit)
	if err != nil {
		h.log.Error("failed to get agents by capability", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get agents"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"agents": agents,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

// GetAgentRegistry retrieves an agent's registry entry
// @Summary Get agent registry entry
// @Description Retrieve a specific agent's registry entry by agent ID.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param agentID path string true "Agent ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /discovery/agents/{agentID} [get]
func (h *AgentDiscoveryHandler) GetAgentRegistry(c *gin.Context) {
	agentID := c.Param("agentID")

	registry, err := h.discovery.GetAgentRegistry(c.Request.Context(), agentID)
	if err != nil {
		h.log.Error("failed to get agent registry", "error", err, "agent_id", agentID)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	c.JSON(http.StatusOK, registry)
}

// VerifyAgent marks an agent as verified (admin only)
// @Summary Verify agent
// @Description Mark an agent as verified. Requires admin role.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param registryID path string true "Registry ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/{registryID}/verify [post]
func (h *AgentDiscoveryHandler) VerifyAgent(c *gin.Context) {
	registryID := c.Param("registryID")

	if err := h.discovery.VerifyAgent(c.Request.Context(), registryID); err != nil {
		h.log.Error("failed to verify agent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent verified"})
}

// UnverifyAgent removes verification from an agent (admin only)
// @Summary Unverify agent
// @Description Remove verification from an agent. Requires admin role.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param registryID path string true "Registry ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/{registryID}/unverify [post]
func (h *AgentDiscoveryHandler) UnverifyAgent(c *gin.Context) {
	registryID := c.Param("registryID")

	if err := h.discovery.UnverifyAgent(c.Request.Context(), registryID); err != nil {
		h.log.Error("failed to unverify agent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unverify agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent unverified"})
}

// DeactivateAgent deactivates an agent
// @Summary Deactivate agent
// @Description Deactivate an agent in the registry. Requires admin role.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param registryID path string true "Registry ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/{registryID}/deactivate [post]
func (h *AgentDiscoveryHandler) DeactivateAgent(c *gin.Context) {
	registryID := c.Param("registryID")

	if err := h.discovery.DeactivateAgent(c.Request.Context(), registryID); err != nil {
		h.log.Error("failed to deactivate agent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deactivate agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent deactivated"})
}

// ActivateAgent activates a deactivated agent
// @Summary Activate agent
// @Description Activate a previously deactivated agent. Requires admin role.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param registryID path string true "Registry ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/{registryID}/activate [post]
func (h *AgentDiscoveryHandler) ActivateAgent(c *gin.Context) {
	registryID := c.Param("registryID")

	if err := h.discovery.ActivateAgent(c.Request.Context(), registryID); err != nil {
		h.log.Error("failed to activate agent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to activate agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent activated"})
}

// HealthCheck performs a health check on an agent
// @Summary Health check agent
// @Description Perform a health check on a registered agent. Requires admin role.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param registryID path string true "Registry ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/{registryID}/health-check [post]
func (h *AgentDiscoveryHandler) HealthCheck(c *gin.Context) {
	registryID := c.Param("registryID")

	if err := h.discovery.PerformHealthCheck(c.Request.Context(), registryID); err != nil {
		h.log.Error("failed to perform health check", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to perform health check"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "health check completed"})
}

// RateAgent rates an agent
// @Summary Rate agent
// @Description Submit a rating and optional review for an agent.
// @Tags Discovery
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param registryID path string true "Registry ID"
// @Param input body object true "Rating details (rating: 1-5, review: optional string)"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/{registryID}/rate [post]
func (h *AgentDiscoveryHandler) RateAgent(c *gin.Context) {
	registryID := c.Param("registryID")

	var req struct {
		Rating float64 `json:"rating" binding:"required,min=1,max=5"`
		Review string  `json:"review"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.discovery.UpdateAgentRating(c.Request.Context(), registryID, req.Rating, req.Review); err != nil {
		h.log.Error("failed to rate agent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rate agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent rated successfully"})
}

// RecordInquiry records an inquiry for an agent
// @Summary Record agent inquiry
// @Description Record an inquiry event for a registered agent.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param registryID path string true "Registry ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/{registryID}/inquiry [post]
func (h *AgentDiscoveryHandler) RecordInquiry(c *gin.Context) {
	registryID := c.Param("registryID")

	if err := h.discovery.RecordAgentInquiry(c.Request.Context(), registryID); err != nil {
		h.log.Error("failed to record inquiry", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record inquiry"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "inquiry recorded"})
}

// RecordIntegration records an integration for an agent
// @Summary Record agent integration
// @Description Record an integration event for a registered agent.
// @Tags Discovery
// @Produce json
// @Security BearerAuth
// @Param registryID path string true "Registry ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /discovery/agents/{registryID}/integration [post]
func (h *AgentDiscoveryHandler) RecordIntegration(c *gin.Context) {
	registryID := c.Param("registryID")

	if err := h.discovery.RecordAgentIntegration(c.Request.Context(), registryID); err != nil {
		h.log.Error("failed to record integration", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record integration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "integration recorded"})
}
