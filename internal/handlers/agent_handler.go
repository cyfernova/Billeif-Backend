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

// allowedAgentConfigKeys defines the only keys that may appear in agent config payloads.
var allowedAgentConfigKeys = map[string]bool{
	"type":                 true,
	"product_ids":          true,
	"volatility":           true,
	"preferred_strategies": true,
	"max_budget":           true,
	"auto_approve":         true,
	"notification_url":     true,
}

// sanitizeAgentConfig strips unknown keys from the config map to prevent mass-assignment.
func sanitizeAgentConfig(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return nil
	}
	clean := make(map[string]interface{}, len(config))
	for k, v := range config {
		if allowedAgentConfigKeys[k] {
			clean[k] = v
		}
	}
	return clean
}

func extractProductIDs(config map[string]interface{}) []string {
	if config == nil {
		return nil
	}
	if productIDs, ok := config["product_ids"].([]interface{}); ok {
		var ids []string
		for _, id := range productIDs {
			if s, ok := id.(string); ok {
				ids = append(ids, s)
			}
		}
		return ids
	}
	return nil
}

type AgentHandler struct {
	svc *services.AgentService
	log *logger.Logger
}

func NewAgentHandler(svc *services.AgentService, log *logger.Logger) *AgentHandler {
	return &AgentHandler{svc: svc, log: log}
}

func (h *AgentHandler) reqLog(c *gin.Context, operation string) *logger.Logger {
	return logger.FromContext(c.Request.Context()).Named("agent_handler").With("operation", operation)
}

type CreateAgentRequest struct {
	Type        string                 `json:"type" binding:"required"`
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Config      map[string]interface{} `json:"config"`
	BusinessID  string                 `json:"business_id"`
}

type UpdateAgentRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	IsActive    *bool                  `json:"is_active"`
	Config      map[string]interface{} `json:"config"`
}

type AddCapabilityRequest struct {
	CapabilityType string                 `json:"capability_type" binding:"required"`
	Description    string                 `json:"description"`
	Config         map[string]interface{} `json:"config"`
}

// CreateAgent creates a new agent
// @Summary Create agent
// @Description Creates a new shopping or merchant agent
// @Tags Agents
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body CreateAgentRequest true "Agent details"
// @Success 201 {object} models.Agent
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents [post]
func (h *AgentHandler) CreateAgent(c *gin.Context) {
	log := h.reqLog(c, "create_agent")
	businessID := c.GetString("business_id")
	userID := c.GetString("user_id")

	var req CreateAgentRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		log.Warn("invalid create agent payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.BusinessID != "" {
		businessID = req.BusinessID
	}

	if businessID == "" {
		log.Warn("business_id is required")
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	req.Config = sanitizeAgentConfig(req.Config)
	req.Type = services.NormalizeMarketplaceAgentType(req.Type)

	var agent *models.Agent
	var err error

	switch req.Type {
	case "shopping":
		personalReq := &services.CreatePersonalAgentRequest{
			UserID:      userID,
			BusinessID:  businessID,
			Name:        req.Name,
			Description: req.Description,
			Config:      req.Config,
		}
		agent, err = h.svc.CreatePersonalAgent(c.Request.Context(), personalReq)
	case "merchant":
		agent, err = h.svc.CreateMerchantAgent(c.Request.Context(), &services.CreateMerchantAgentRequest{
			BusinessID:  businessID,
			Name:        req.Name,
			Description: req.Description,
			ProductIDs:  extractProductIDs(req.Config),
		})
	default:
		log.Warn("invalid agent type", "type", req.Type)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent type"})
		return
	}

	if err != nil {
		log.Error("failed to create agent", "error", err, "type", req.Type)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("agent created", "agent_id", agent.ID, "type", req.Type)

	c.JSON(http.StatusCreated, agent)
}

// ListAgents lists all agents for the user or business
// @Summary List agents
// @Description Returns all agents owned by the user or business
// @Tags Agents
// @Produce json
// @Security BearerAuth
// @Param business_id query string false "Filter by business ID"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /agents [get]
func (h *AgentHandler) ListAgents(c *gin.Context) {
	log := h.reqLog(c, "list_agents")
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)

	var agents []*models.Agent
	var total int64
	var err error
	selectedBusinessID := ""

	if c.Query("business_id") != "" {
		businessID, ok := requireBusinessScope(c)
		if !ok {
			return
		}
		selectedBusinessID = businessID
		agents, total, err = h.svc.GetAgentsByBusiness(c.Request.Context(), businessID, page, limit)
	} else {
		agents, total, err = h.svc.GetAgentsByUser(c.Request.Context(), userID, page, limit)
	}

	if err != nil {
		log.Error("failed to list agents", "error", err, "business_id", selectedBusinessID, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("agents listed", "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  agents,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetAgent retrieves an agent by ID
// @Summary Get agent
// @Description Returns a specific agent by ID
// @Tags Agents
// @Produce json
// @Security BearerAuth
// @Param id path string true "Agent ID"
// @Success 200 {object} models.Agent
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/{id} [get]
func (h *AgentHandler) GetAgent(c *gin.Context) {
	log := h.reqLog(c, "get_agent")
	id := c.Param("id")
	agent, ok := h.loadOwnedAgent(c, log, id)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, agent)
}

// UpdateAgent updates an existing agent
// @Summary Update agent
// @Description Updates an existing agent by ID
// @Tags Agents
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Agent ID"
// @Param input body UpdateAgentRequest true "Agent update details"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/{id} [put]
func (h *AgentHandler) UpdateAgent(c *gin.Context) {
	log := h.reqLog(c, "update_agent")
	id := c.Param("id")
	if _, ok := h.loadOwnedAgent(c, log, id); !ok {
		return
	}

	var req UpdateAgentRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		log.Warn("invalid update agent payload", "error", err, "agent_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if req.Config != nil {
		updates["config"] = sanitizeAgentConfig(req.Config)
	}

	if err := h.svc.UpdateAgent(c.Request.Context(), id, updates); err != nil {
		log.Error("failed to update agent", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("agent updated", "agent_id", id)

	c.JSON(http.StatusOK, gin.H{"message": "agent updated successfully"})
}

// DeleteAgent deletes an agent
// @Summary Delete agent
// @Description Deletes an agent by ID
// @Tags Agents
// @Produce json
// @Security BearerAuth
// @Param id path string true "Agent ID"
// @Success 204 {string} string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/{id} [delete]
func (h *AgentHandler) DeleteAgent(c *gin.Context) {
	log := h.reqLog(c, "delete_agent")
	id := c.Param("id")
	if _, ok := h.loadOwnedAgent(c, log, id); !ok {
		return
	}
	if err := h.svc.DeleteAgent(c.Request.Context(), id); err != nil {
		log.Error("failed to delete agent", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("agent deleted", "agent_id", id)

	c.JSON(http.StatusNoContent, nil)
}

// AddCapability adds a capability to an agent
// @Summary Add agent capability
// @Description Adds a new capability to an existing agent
// @Tags Agents
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Agent ID"
// @Param input body AddCapabilityRequest true "Capability details"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/{id}/capabilities [post]
func (h *AgentHandler) AddCapability(c *gin.Context) {
	log := h.reqLog(c, "add_capability")
	id := c.Param("id")
	if _, ok := h.loadOwnedAgent(c, log, id); !ok {
		return
	}

	var req AddCapabilityRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		log.Warn("invalid add capability payload", "error", err, "agent_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.AddCapability(c.Request.Context(), id, req.CapabilityType, req.Description, req.Config); err != nil {
		log.Error("failed to add capability", "error", err, "agent_id", id, "capability_type", req.CapabilityType)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("capability added", "agent_id", id, "capability_type", req.CapabilityType)

	c.JSON(http.StatusCreated, gin.H{"message": "capability added successfully"})
}

func (h *AgentHandler) ListCapabilities(c *gin.Context) {
	log := h.reqLog(c, "list_capabilities")
	id := c.Param("id")
	if _, ok := h.loadOwnedAgent(c, log, id); !ok {
		return
	}
	capabilities, err := h.svc.GetAgentCapabilities(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to list capabilities", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("capabilities listed", "agent_id", id, "count", len(capabilities))

	c.JSON(http.StatusOK, capabilities)
}

func (h *AgentHandler) RemoveCapability(c *gin.Context) {
	log := h.reqLog(c, "remove_capability")
	id := c.Param("id")
	if _, ok := h.loadOwnedAgent(c, log, id); !ok {
		return
	}
	capabilityID := c.Param("capability_id")

	if err := h.svc.RemoveCapability(c.Request.Context(), capabilityID); err != nil {
		log.Error("failed to remove capability", "error", err, "capability_id", capabilityID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("capability removed", "capability_id", capabilityID)

	c.JSON(http.StatusOK, gin.H{"message": "capability removed successfully"})
}

// GetAgentCapabilities retrieves capabilities for an agent
// @Summary Get agent capabilities
// @Description Returns all capabilities for a specific agent
// @Tags Agents
// @Produce json
// @Security BearerAuth
// @Param id path string true "Agent ID"
// @Success 200 {array} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/{id}/capabilities [get]
func (h *AgentHandler) GetAgentCapabilities(c *gin.Context) {
	log := h.reqLog(c, "get_agent_capabilities")
	id := c.Param("id")
	if _, ok := h.loadOwnedAgent(c, log, id); !ok {
		return
	}
	capabilities, err := h.svc.GetAgentCapabilities(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get agent capabilities", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("agent capabilities fetched", "agent_id", id, "count", len(capabilities))

	c.JSON(http.StatusOK, capabilities)
}

// ValidateAgentPermissions validates if the agent has permissions
// @Summary Validate agent permissions
// @Description Validates if the agent has required permissions
// @Tags Agents
// @Produce json
// @Security BearerAuth
// @Param id path string true "Agent ID"
// @Success 200 {object} map[string]bool
// @Router /agents/validate-permissions/{id} [post]
func (h *AgentHandler) ValidateAgentPermissions(c *gin.Context) {
	log := h.reqLog(c, "validate_agent_permissions")
	agentID := c.Param("id")
	if _, ok := h.loadOwnedAgent(c, log, agentID); !ok {
		c.JSON(http.StatusOK, gin.H{"has_permission": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"has_permission": true})
}

// GetActiveAgents retrieves all active agents for a business
// @Summary Get active agents
// @Description Returns all active agents of a specific type for the business
// @Tags Agents
// @Produce json
// @Security BearerAuth
// @Param type query string true "Agent type (shopping, merchant)"
// @Success 200 {array} models.Agent
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/active [get]
func (h *AgentHandler) GetActiveAgents(c *gin.Context) {
	log := h.reqLog(c, "get_active_agents")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	agentType := services.NormalizeMarketplaceAgentType(c.Query("type"))
	if agentType == "" {
		log.Warn("missing type query param for active agents")
		c.JSON(http.StatusBadRequest, gin.H{"error": "type parameter is required"})
		return
	}

	agents, _, err := h.svc.GetAgentsByBusiness(c.Request.Context(), businessID, 1, 1000)
	if err != nil {
		log.Error("failed to get active agents", "error", err, "type", agentType)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	filtered := make([]*models.Agent, 0, len(agents))
	for _, agent := range agents {
		if agent.IsActive && agent.Type == agentType {
			filtered = append(filtered, agent)
		}
	}
	services.ApplyMarketplaceRoleToAgents(filtered)
	log.Debug("active agents fetched", "type", agentType, "count", len(filtered))
	c.JSON(http.StatusOK, filtered)
}

func (h *AgentHandler) CreateCredentialProviderAgent(c *gin.Context) {
	log := h.reqLog(c, "create_credential_provider_agent")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		log.Warn("invalid credential provider agent payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent, err := h.svc.CreateCredentialProviderAgent(c.Request.Context(), businessID, req.Name, req.Description)
	if err != nil {
		log.Error("failed to create credential provider agent", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("credential provider agent created", "agent_id", agent.ID)

	c.JSON(http.StatusCreated, agent)
}

func (h *AgentHandler) CreatePaymentProcessorAgent(c *gin.Context) {
	log := h.reqLog(c, "create_payment_processor_agent")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		log.Warn("invalid payment processor agent payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent, err := h.svc.CreatePaymentProcessorAgent(c.Request.Context(), businessID, req.Name, req.Description)
	if err != nil {
		log.Error("failed to create payment processor agent", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("payment processor agent created", "agent_id", agent.ID)

	c.JSON(http.StatusCreated, agent)
}

func (h *AgentHandler) UpdateAgentStatus(c *gin.Context) {
	log := h.reqLog(c, "update_agent_status")
	id := c.Param("id")
	if _, ok := h.loadOwnedAgent(c, log, id); !ok {
		return
	}

	var req struct {
		IsActive bool `json:"is_active" binding:"required"`
	}
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		log.Warn("invalid update agent status payload", "error", err, "agent_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{"is_active": req.IsActive}
	if err := h.svc.UpdateAgent(c.Request.Context(), id, updates); err != nil {
		log.Error("failed to update agent status", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("agent status updated", "agent_id", id, "is_active", req.IsActive)

	c.JSON(http.StatusOK, gin.H{"message": "agent status updated successfully"})
}

// GetAgentByType retrieves agents by type
// @Summary Get agents by type
// @Description Returns agents filtered by type for the user or business
// @Tags Agents
// @Produce json
// @Security BearerAuth
// @Param type path string true "Agent type (shopping, merchant)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/type/{type} [get]
func (h *AgentHandler) GetAgentByType(c *gin.Context) {
	log := h.reqLog(c, "get_agent_by_type")
	agentType := services.NormalizeMarketplaceAgentType(c.Param("type"))
	page, limit := utils.ParsePagination(c)

	userID, ok := requireUserScope(c)
	if !ok {
		return
	}

	var agents []*models.Agent
	var total int64
	var err error

	if agentType == "shopping" {
		agents, _, err = h.svc.GetAgentsByUser(c.Request.Context(), userID, page, limit)
		filtered := make([]*models.Agent, 0, len(agents))
		for _, agent := range agents {
			if agent.Type == "shopping" {
				filtered = append(filtered, agent)
			}
		}
		agents = filtered
		total = int64(len(filtered))
	} else if agentType == "merchant" {
		businessID := middleware.GetEffectiveBusinessID(c)
		if businessID == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
			return
		}
		agents, _, err = h.svc.GetAgentsByBusiness(c.Request.Context(), businessID, page, limit)
		filtered := make([]*models.Agent, 0, len(agents))
		for _, agent := range agents {
			if agent.Type == "merchant" {
				filtered = append(filtered, agent)
			}
		}
		agents = filtered
		total = int64(len(filtered))
	} else {
		log.Warn("invalid agent type", "type", agentType)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent type"})
		return
	}
	services.ApplyMarketplaceRoleToAgents(agents)

	if err != nil {
		log.Error("failed to get agents by type", "error", err, "type", agentType, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("agents by type fetched", "type", agentType, "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  agents,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *AgentHandler) loadOwnedAgent(c *gin.Context, log *logger.Logger, agentID string) (*models.Agent, bool) {
	userID, ok := requireUserScope(c)
	if !ok {
		return nil, false
	}
	businessID := middleware.GetEffectiveBusinessID(c)
	agent, err := h.svc.GetAgentByID(c.Request.Context(), agentID)
	if err != nil {
		log.Error("failed to get agent", "error", err, "agent_id", agentID)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return nil, false
	}
	if agent.OwnerID != userID && (businessID == "" || agent.BusinessID != businessID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return nil, false
	}
	return agent, true
}
