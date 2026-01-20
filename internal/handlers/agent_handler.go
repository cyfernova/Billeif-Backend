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

type CreateAgentRequest struct {
	Type        string                 `json:"type" binding:"required"`
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Config      map[string]interface{} `json:"config"`
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
	businessID := c.GetString("business_id")
	userID := c.GetString("user_id")

	var req CreateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		if err := c.ShouldBindJSON(&merchantReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		merchantReq.Name = req.Name
		merchantReq.Description = req.Description
		agent, err = h.svc.CreateMerchantAgent(c.Request.Context(), &merchantReq)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent type"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, agent)
}

func (h *AgentHandler) ListAgents(c *gin.Context) {
	businessID := c.Query("business_id")
	userID := c.GetString("user_id")
	page, limit := utils.ParsePagination(c)

	var agents interface{}
	var total int64
	var err error

	if businessID != "" {
		agents, total, err = h.svc.GetAgentsByBusiness(c.Request.Context(), businessID, page, limit)
	} else {
		agents, total, err = h.svc.GetAgentsByUser(c.Request.Context(), userID, page, limit)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  agents,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *AgentHandler) GetAgent(c *gin.Context) {
	id := c.Param("id")
	agent, err := h.svc.GetAgentByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	c.JSON(http.StatusOK, agent)
}

func (h *AgentHandler) UpdateAgent(c *gin.Context) {
	id := c.Param("id")

	var req UpdateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent updated successfully"})
}

func (h *AgentHandler) DeleteAgent(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.DeleteAgent(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *AgentHandler) AddCapability(c *gin.Context) {
	id := c.Param("id")

	var req AddCapabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.AddCapability(c.Request.Context(), id, req.CapabilityType, req.Description, req.Config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "capability added successfully"})
}

func (h *AgentHandler) ListCapabilities(c *gin.Context) {
	id := c.Param("id")
	capabilities, err := h.svc.GetAgentCapabilities(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, capabilities)
}

func (h *AgentHandler) RemoveCapability(c *gin.Context) {
	capabilityID := c.Param("capability_id")

	if err := h.svc.RemoveCapability(c.Request.Context(), capabilityID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "capability removed successfully"})
}

func (h *AgentHandler) GetAgentCapabilities(c *gin.Context) {
	id := c.Param("id")
	capabilities, err := h.svc.GetAgentCapabilities(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, capabilities)
}

func (h *AgentHandler) ValidateAgentPermissions(c *gin.Context) {
	userID := c.GetString("user_id")
	agentID := c.Param("id")

	hasPermission, err := h.svc.ValidateAgentPermission(userID, agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"has_permission": hasPermission})
}

func (h *AgentHandler) GetActiveAgents(c *gin.Context) {
	agentType := c.Query("type")
	if agentType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type parameter is required"})
		return
	}

	agents, err := h.svc.GetActiveAgentsByType(c.Request.Context(), agentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, agents)
}

func (h *AgentHandler) CreateCredentialProviderAgent(c *gin.Context) {
	businessID := c.GetString("business_id")

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent, err := h.svc.CreateCredentialProviderAgent(c.Request.Context(), businessID, req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, agent)
}

func (h *AgentHandler) CreatePaymentProcessorAgent(c *gin.Context) {
	businessID := c.GetString("business_id")

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent, err := h.svc.CreatePaymentProcessorAgent(c.Request.Context(), businessID, req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, agent)
}

func (h *AgentHandler) UpdateAgentStatus(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		IsActive bool `json:"is_active" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{"is_active": req.IsActive}
	if err := h.svc.UpdateAgent(c.Request.Context(), id, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent status updated successfully"})
}

func (h *AgentHandler) GetAgentByType(c *gin.Context) {
	agentType := c.Param("type")
	page, limit := utils.ParsePagination(c)

	userID := c.GetString("user_id")
	var agents interface{}
	var total int64
	var err error

	if agentType == "shopping" || agentType == "merchant" {
		agents, total, err = h.svc.GetAgentsByUser(c.Request.Context(), userID, page, limit)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent type"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  agents,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
