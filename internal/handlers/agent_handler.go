package handlers

import (
	"net/http"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

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

func (h *AgentHandler) CreateAgent(c *gin.Context) {
	log := h.reqLog(c, "create_agent")
	userID := c.GetString("user_id")

	var req CreateAgentRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		log.Warn("invalid create agent payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Resolve business_id: JWT claim → BusinessAuth middleware → request body
	businessID := c.GetString("business_id")
	if businessID == "" {
		businessID = c.GetString("validated_business_id")
	}
	if businessID == "" {
		businessID = req.BusinessID
	}
	if businessID == "" {
		log.Warn("missing business_id for agent creation")
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

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
		var merchantReq services.CreateMerchantAgentRequest
		if err := c.ShouldBindBodyWithJSON(&merchantReq); err != nil {
			log.Warn("invalid create merchant agent payload", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		merchantReq.Name = req.Name
		merchantReq.Description = req.Description
		agent, err = h.svc.CreateMerchantAgent(c.Request.Context(), &merchantReq)
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

func (h *AgentHandler) ListAgents(c *gin.Context) {
	log := h.reqLog(c, "list_agents")
	businessID := c.Query("business_id")
	userID := c.GetString("user_id")
	page, limit := utils.ParsePagination(c)

	var agents interface{}
	var total int64
	var err error

	if businessID != "" {
		agents, total, err = h.svc.GetAgentsByBusiness(c.Request.Context(), businessID, page, limit)
	} else {
		if userID == "" {
			log.Warn("list agents unauthorized: missing user_id")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			return
		}
		agents, total, err = h.svc.GetAgentsByUser(c.Request.Context(), userID, page, limit)
	}

	if err != nil {
		log.Error("failed to list agents", "error", err, "business_id", businessID, "user_id", userID)
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

func (h *AgentHandler) GetAgent(c *gin.Context) {
	log := h.reqLog(c, "get_agent")
	id := c.Param("id")
	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get agent", "error", err, "agent_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	c.JSON(http.StatusOK, agent)
}

func (h *AgentHandler) UpdateAgent(c *gin.Context) {
	log := h.reqLog(c, "update_agent")
	id := c.Param("id")

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
		updates["config"] = req.Config
	}

	if err := h.svc.UpdateAgent(c.Request.Context(), id, updates); err != nil {
		log.Error("failed to update agent", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("agent updated", "agent_id", id)

	c.JSON(http.StatusOK, gin.H{"message": "agent updated successfully"})
}

func (h *AgentHandler) DeleteAgent(c *gin.Context) {
	log := h.reqLog(c, "delete_agent")
	id := c.Param("id")
	if err := h.svc.DeleteAgent(c.Request.Context(), id); err != nil {
		log.Error("failed to delete agent", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("agent deleted", "agent_id", id)

	c.JSON(http.StatusNoContent, nil)
}

func (h *AgentHandler) AddCapability(c *gin.Context) {
	log := h.reqLog(c, "add_capability")
	id := c.Param("id")

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
	capabilityID := c.Param("capability_id")

	if err := h.svc.RemoveCapability(c.Request.Context(), capabilityID); err != nil {
		log.Error("failed to remove capability", "error", err, "capability_id", capabilityID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("capability removed", "capability_id", capabilityID)

	c.JSON(http.StatusOK, gin.H{"message": "capability removed successfully"})
}

func (h *AgentHandler) GetAgentCapabilities(c *gin.Context) {
	log := h.reqLog(c, "get_agent_capabilities")
	id := c.Param("id")
	capabilities, err := h.svc.GetAgentCapabilities(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get agent capabilities", "error", err, "agent_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("agent capabilities fetched", "agent_id", id, "count", len(capabilities))

	c.JSON(http.StatusOK, capabilities)
}

func (h *AgentHandler) ValidateAgentPermissions(c *gin.Context) {
	log := h.reqLog(c, "validate_agent_permissions")
	userID := c.GetString("user_id")
	if userID == "" {
		log.Warn("permission validation unauthorized: missing user_id")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	agentID := c.Param("id")

	hasPermission, err := h.svc.ValidateAgentPermission(userID, agentID)
	if err != nil {
		log.Error("failed to validate agent permission", "error", err, "agent_id", agentID, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("agent permission validated", "agent_id", agentID, "user_id", userID, "has_permission", hasPermission)

	c.JSON(http.StatusOK, gin.H{"has_permission": hasPermission})
}

func (h *AgentHandler) GetActiveAgents(c *gin.Context) {
	log := h.reqLog(c, "get_active_agents")
	agentType := c.Query("type")
	if agentType == "" {
		log.Warn("missing type query param for active agents")
		c.JSON(http.StatusBadRequest, gin.H{"error": "type parameter is required"})
		return
	}

	agents, err := h.svc.GetActiveAgentsByType(c.Request.Context(), agentType)
	if err != nil {
		log.Error("failed to get active agents", "error", err, "type", agentType)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("active agents fetched", "type", agentType, "count", len(agents))

	c.JSON(http.StatusOK, agents)
}

func (h *AgentHandler) CreateCredentialProviderAgent(c *gin.Context) {
	log := h.reqLog(c, "create_credential_provider_agent")
	businessID := c.GetString("business_id")

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
	businessID := c.GetString("business_id")

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

func (h *AgentHandler) GetAgentByType(c *gin.Context) {
	log := h.reqLog(c, "get_agent_by_type")
	agentType := c.Param("type")
	page, limit := utils.ParsePagination(c)

	userID := c.GetString("user_id")
	if userID == "" {
		log.Warn("get agent by type unauthorized: missing user_id")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	var agents interface{}
	var total int64
	var err error

	if agentType == "shopping" || agentType == "merchant" {
		agents, total, err = h.svc.GetAgentsByUser(c.Request.Context(), userID, page, limit)
	} else {
		log.Warn("invalid agent type", "type", agentType)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent type"})
		return
	}

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
