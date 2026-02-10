package handlers

import (
	"net/http"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type AgentConfigHandler struct {
	configService *services.AgentConfigService
	agentService  *services.AgentService
	bargaining    *services.BargainingService
	mentee        *services.MenteeService
	log           *logger.Logger
}

func NewAgentConfigHandler(
	configService *services.AgentConfigService,
	agentService *services.AgentService,
	bargaining *services.BargainingService,
	mentee *services.MenteeService,
	log *logger.Logger,
) *AgentConfigHandler {
	return &AgentConfigHandler{
		configService: configService,
		agentService:  agentService,
		bargaining:    bargaining,
		mentee:        mentee,
		log:           log,
	}
}

type CreateAgentConfigRequest struct {
	AgentID string              `json:"agent_id" validate:"required,uuid"`
	Config  *models.AgentConfig `json:"config" validate:"required"`
}

type UpdateAgentConfigRequest struct {
	BuyerConfig  *services.UpdateBuyerConfigRequest  `json:"buyer_config,omitempty"`
	SellerConfig *services.UpdateSellerConfigRequest `json:"seller_config,omitempty"`
	Volatility   *float64                            `json:"volatility,omitempty"`
}

func (h *AgentConfigHandler) CreateAgentConfig(c *gin.Context) {
	var req CreateAgentConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent, err := h.agentService.GetAgentByID(c.Request.Context(), req.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	config, err := h.configService.SaveAgentConfig(c.Request.Context(), agent, req.Config)
	if err != nil {
		h.log.Error("failed to save agent config", "error", err, "agent_id", req.AgentID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save agent config"})
		return
	}

	c.JSON(http.StatusCreated, config)
}

func (h *AgentConfigHandler) GetAgentConfig(c *gin.Context) {
	agentID := c.Param("agent_id")

	config, err := h.configService.GetAgentConfig(c.Request.Context(), agentID)
	if err != nil {
		if err == services.ErrAgentConfigNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent config not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get agent config"})
		return
	}

	c.JSON(http.StatusOK, config)
}

func (h *AgentConfigHandler) UpdateAgentConfig(c *gin.Context) {
	agentID := c.Param("agent_id")

	var req UpdateAgentConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	config, err := h.configService.GetAgentConfig(c.Request.Context(), agentID)
	if err != nil {
		if err == services.ErrAgentConfigNotFound {
			agent, agentErr := h.agentService.GetAgentByID(c.Request.Context(), agentID)
			if agentErr != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
				return
			}

			var defaultConfig *models.AgentConfig
			if agent.Type == "buyer" {
				defaultConfig = h.configService.CreateDefaultBuyerConfig()
			} else {
				defaultConfig = h.configService.CreateDefaultSellerConfig()
			}

			config, err = h.configService.SaveAgentConfig(c.Request.Context(), agent, defaultConfig)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create default config"})
				return
			}
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get agent config"})
			return
		}
	}

	if req.BuyerConfig != nil && config.Config.Type == models.AgentTypeBuyer {
		if config.Config.BuyerConfig == nil {
			config.Config.BuyerConfig = &models.BuyerConfig{}
		}
		if req.BuyerConfig.MaxDiscountPercent != 0 {
			config.Config.BuyerConfig.MaxDiscountPercent = req.BuyerConfig.MaxDiscountPercent
		}
		if req.BuyerConfig.MinDiscountPercent != 0 {
			config.Config.BuyerConfig.MinDiscountPercent = req.BuyerConfig.MinDiscountPercent
		}
		if req.BuyerConfig.TargetDiscount != 0 {
			config.Config.BuyerConfig.TargetDiscount = req.BuyerConfig.TargetDiscount
		}
		if req.BuyerConfig.RiskTolerance != 0 {
			config.Config.BuyerConfig.RiskTolerance = req.BuyerConfig.RiskTolerance
		}
		if req.BuyerConfig.PatienceLevel != 0 {
			config.Config.BuyerConfig.PatienceLevel = req.BuyerConfig.PatienceLevel
		}
		if req.BuyerConfig.MaxRounds != 0 {
			config.Config.BuyerConfig.MaxRounds = req.BuyerConfig.MaxRounds
		}
		if req.BuyerConfig.PreferredProducts != nil {
			config.Config.BuyerConfig.PreferredProducts = req.BuyerConfig.PreferredProducts
		}
		if req.BuyerConfig.BlacklistedVendors != nil {
			config.Config.BuyerConfig.BlacklistedVendors = req.BuyerConfig.BlacklistedVendors
		}
		if req.BuyerConfig.BudgetLimit != nil {
			config.Config.BuyerConfig.BudgetLimit = req.BuyerConfig.BudgetLimit
		}
		if req.BuyerConfig.PaymentTerms != nil {
			config.Config.BuyerConfig.PaymentTerms = req.BuyerConfig.PaymentTerms
		}
		if req.BuyerConfig.AcceptanceThreshold != 0 {
			config.Config.BuyerConfig.AcceptanceThreshold = req.BuyerConfig.AcceptanceThreshold
		}
	}

	if req.SellerConfig != nil && config.Config.Type == models.AgentTypeSeller {
		if config.Config.SellerConfig == nil {
			config.Config.SellerConfig = &models.SellerConfig{}
		}
		if req.SellerConfig.MinAcceptablePrice != 0 {
			config.Config.SellerConfig.MinAcceptablePrice = req.SellerConfig.MinAcceptablePrice
		}
		if req.SellerConfig.MaxMarkupPercent != 0 {
			config.Config.SellerConfig.MaxMarkupPercent = req.SellerConfig.MaxMarkupPercent
		}
		if req.SellerConfig.InventoryPressure != 0 {
			config.Config.SellerConfig.InventoryPressure = req.SellerConfig.InventoryPressure
		}
		if req.SellerConfig.SalesVolumeGoal != nil {
			config.Config.SellerConfig.SalesVolumeGoal = req.SellerConfig.SalesVolumeGoal
		}
		if req.SellerConfig.CustomerLoyaltyFactor != 0 {
			config.Config.SellerConfig.CustomerLoyaltyFactor = req.SellerConfig.CustomerLoyaltyFactor
		}
		if req.SellerConfig.MaxRounds != 0 {
			config.Config.SellerConfig.MaxRounds = req.SellerConfig.MaxRounds
		}
		if req.SellerConfig.PreferredCustomers != nil {
			config.Config.SellerConfig.PreferredCustomers = req.SellerConfig.PreferredCustomers
		}
		if req.SellerConfig.PaymentTerms != nil {
			config.Config.SellerConfig.PaymentTerms = req.SellerConfig.PaymentTerms
		}
		if req.SellerConfig.AcceptanceThreshold != 0 {
			config.Config.SellerConfig.AcceptanceThreshold = req.SellerConfig.AcceptanceThreshold
		}
	}

	if req.Volatility != nil {
		config.Config.Volatility = *req.Volatility
	}

	agent, err := h.agentService.GetAgentByID(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	config, err = h.configService.SaveAgentConfig(c.Request.Context(), agent, config.Config)
	if err != nil {
		h.log.Error("failed to update agent config", "error", err, "agent_id", agentID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update agent config"})
		return
	}

	c.JSON(http.StatusOK, config)
}

func (h *AgentConfigHandler) DeleteAgentConfig(c *gin.Context) {
	agentID := c.Param("agent_id")

	err := h.configService.DeleteAgentConfig(c.Request.Context(), agentID)
	if err != nil {
		if err == services.ErrAgentConfigNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent config not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete agent config"})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *AgentConfigHandler) GetAllAgentConfigs(c *gin.Context) {
	configs, err := h.configService.GetAllAgentConfigs(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get agent configs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"configs": configs})
}

type CreateDefaultConfigRequest struct {
	AgentID string `json:"agent_id" validate:"required,uuid"`
	Type    string `json:"type" validate:"required,oneof=buyer seller"`
}

func (h *AgentConfigHandler) CreateDefaultConfig(c *gin.Context) {
	var req CreateDefaultConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent, err := h.agentService.GetAgentByID(c.Request.Context(), req.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var defaultConfig *models.AgentConfig
	if req.Type == "buyer" {
		defaultConfig = h.configService.CreateDefaultBuyerConfig()
	} else {
		defaultConfig = h.configService.CreateDefaultSellerConfig()
	}

	config, err := h.configService.SaveAgentConfig(c.Request.Context(), agent, defaultConfig)
	if err != nil {
		h.log.Error("failed to create default config", "error", err, "agent_id", req.AgentID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create default config"})
		return
	}

	c.JSON(http.StatusCreated, config)
}

func (h *AgentConfigHandler) GetMenteeRecommendation(c *gin.Context) {
	negotiationID := c.Param("negotiation_id")
	agentType := c.Query("agent_type")

	negotiation, err := h.bargaining.GetNegotiation(c.Request.Context(), negotiationID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "negotiation not found"})
		return
	}

	if agentType != "buyer" && agentType != "seller" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_type must be 'buyer' or 'seller'"})
		return
	}

	decision, err := h.bargaining.GetMenteeRecommendation(negotiation, agentType)
	if err != nil {
		h.log.Error("failed to get mentee recommendation", "error", err, "negotiation_id", negotiationID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get mentee recommendation"})
		return
	}

	c.JSON(http.StatusOK, decision)
}

func (h *AgentConfigHandler) GetMenteeLearningData(c *gin.Context) {
	agentID := c.Param("agent_id")

	data, err := h.mentee.GetAgentLearningData(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get learning data"})
		return
	}

	c.JSON(http.StatusOK, data)
}

func (h *AgentConfigHandler) ResetMenteeLearning(c *gin.Context) {
	agentID := c.Param("agent_id")

	err := h.mentee.ResetAgentLearning(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset learning data"})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *AgentConfigHandler) ExportMenteeData(c *gin.Context) {
	data, err := h.mentee.ExportLearningData(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to export learning data"})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=mentee_data.json")
	c.Data(http.StatusOK, "application/json", data)
}

func (h *AgentConfigHandler) ImportMenteeData(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	if err := h.mentee.ImportLearningData(c.Request.Context(), data); err != nil {
		h.log.Error("failed to import learning data", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to import learning data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "learning data imported successfully"})
}
