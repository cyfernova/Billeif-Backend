package handlers

import (
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"
	"net/http"

	"github.com/gin-gonic/gin"
)

type BargainingHandler struct {
	svc *services.BargainingService
	log *logger.Logger
}

func NewBargainingHandler(svc *services.BargainingService, log *logger.Logger) *BargainingHandler {
	return &BargainingHandler{svc: svc, log: log}
}

type CreateNegotiationRequest struct {
	BuyerAgentID       string  `json:"buyer_agent_id" binding:"required,uuid"`
	SellerAgentID      string  `json:"seller_agent_id" binding:"required,uuid"`
	InitialAmount      float64 `json:"initial_amount" binding:"required,gt=0"`
	MarketplaceOrderID *string `json:"marketplace_order_id,omitempty" binding:"omitempty,uuid"`
	MaxRounds          int     `json:"max_rounds" binding:"omitempty,gte=1,lte=10"`
}

type CounterOfferRequest struct {
	AgentID        string  `json:"agent_id" binding:"required,uuid"`
	ProposedAmount float64 `json:"proposed_amount" binding:"required,gte=0"`
	Reason         *string `json:"reason" binding:"omitempty,max=500"`
	Action         string  `json:"action" binding:"required,oneof=counteroffer accept reject"`
}

// CreateNegotiation creates a new negotiation
// @Summary Create negotiation
// @Description Create a new bargaining negotiation between buyer and seller agents.
// @Tags Bargaining
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body CreateNegotiationRequest true "Negotiation details"
// @Success 201 {object} models.BargainingNegotiation
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /bargaining/negotiations [post]
func (h *BargainingHandler) CreateNegotiation(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("bargaining_handler").With("operation", "create_negotiation")
	userID := c.GetString("user_id")

	var req CreateNegotiationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warn("invalid create negotiation payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	createReq := &services.CreateNegotiationRequest{
		BuyerAgentID:       req.BuyerAgentID,
		SellerAgentID:      req.SellerAgentID,
		UserID:             userID,
		InitialAmount:      req.InitialAmount,
		MarketplaceOrderID: req.MarketplaceOrderID,
		MaxRounds:          req.MaxRounds,
	}

	var negotiation *models.BargainingNegotiation
	negotiation, err := h.svc.CreateNegotiation(c.Request.Context(), createReq)
	if err != nil {
		log.Error("failed to create negotiation", "error", err, "buyer_agent_id", req.BuyerAgentID, "seller_agent_id", req.SellerAgentID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("negotiation created", "negotiation_id", negotiation.ID)

	c.JSON(http.StatusCreated, negotiation)
}

// GetNegotiation retrieves a negotiation by ID
// @Summary Get negotiation
// @Description Returns the details of a specific negotiation.
// @Tags Bargaining
// @Produce json
// @Security BearerAuth
// @Param id path string true "Negotiation ID"
// @Success 200 {object} models.BargainingNegotiation
// @Failure 404 {object} map[string]string
// @Router /bargaining/negotiations/{id} [get]
func (h *BargainingHandler) GetNegotiation(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("bargaining_handler").With("operation", "get_negotiation")
	id := c.Param("id")

	var negotiation *models.BargainingNegotiation
	negotiation, err := h.svc.GetNegotiation(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get negotiation", "error", err, "negotiation_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "negotiation not found"})
		return
	}

	c.JSON(http.StatusOK, negotiation)
}

// ListNegotiations retrieves all negotiations for a user
// @Summary List negotiations
// @Description Returns a list of negotiations belonging to the authenticated user.
// @Tags Bargaining
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /bargaining/negotiations [get]
func (h *BargainingHandler) ListNegotiations(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("bargaining_handler").With("operation", "list_negotiations")
	userID := c.GetString("user_id")
	page, limit := utils.ParsePagination(c)

	negotiations, total, err := h.svc.GetNegotiationsByUser(c.Request.Context(), userID, page, limit)
	if err != nil {
		log.Error("failed to list negotiations", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("negotiations listed", "count", len(negotiations), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  negotiations,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// SubmitCounterOffer submits a counter offer for a negotiation
// @Summary Submit counter offer
// @Description Submit a counter offer, accept, or reject a negotiation round.
// @Tags Bargaining
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Negotiation ID"
// @Param input body CounterOfferRequest true "Counter offer details"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 410 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /bargaining/negotiations/{id}/counteroffer [post]
func (h *BargainingHandler) SubmitCounterOffer(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("bargaining_handler").With("operation", "submit_counter_offer")
	negotiationID := c.Param("id")

	var req CounterOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warn("invalid counter offer payload", "error", err, "negotiation_id", negotiationID)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	counterOfferReq := &services.CounterOfferRequest{
		AgentID:        req.AgentID,
		ProposedAmount: req.ProposedAmount,
		Reason:         req.Reason,
		Action:         req.Action,
	}

	var negotiation *models.BargainingNegotiation
	round, negotiation, err := h.svc.SubmitCounterOffer(c.Request.Context(), negotiationID, counterOfferReq)
	if err != nil {
		log.Error("failed to submit counter offer", "error", err, "negotiation_id", negotiationID, "action", req.Action)
		statusCode := http.StatusInternalServerError
		if err == services.ErrNegotiationExpired {
			statusCode = http.StatusGone
		} else if err == services.ErrMaxRoundsExceeded {
			statusCode = http.StatusBadRequest
		} else if err == services.ErrInvalidAmount {
			statusCode = http.StatusBadRequest
		}

		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	log.Info("counter offer processed", "negotiation_id", negotiationID, "round_number", round.RoundNumber, "action", req.Action)

	c.JSON(http.StatusOK, gin.H{
		"round":       round,
		"negotiation": negotiation,
	})
}

// GetNegotiationRounds retrieves all rounds for a negotiation
// @Summary Get negotiation rounds
// @Description Returns all rounds for a specific negotiation.
// @Tags Bargaining
// @Produce json
// @Security BearerAuth
// @Param id path string true "Negotiation ID"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /bargaining/negotiations/{id}/rounds [get]
func (h *BargainingHandler) GetNegotiationRounds(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("bargaining_handler").With("operation", "get_negotiation_rounds")
	negotiationID := c.Param("id")

	rounds, err := h.svc.GetNegotiationRounds(c.Request.Context(), negotiationID)
	if err != nil {
		log.Error("failed to get negotiation rounds", "error", err, "negotiation_id", negotiationID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("negotiation rounds fetched", "negotiation_id", negotiationID, "count", len(rounds))

	c.JSON(http.StatusOK, gin.H{
		"rounds": rounds,
	})
}

// GetSuggestedCounterOffer retrieves a suggested counter offer amount
// @Summary Get suggested counter offer
// @Description Returns a suggested counter offer amount based on negotiation state and agent type.
// @Tags Bargaining
// @Produce json
// @Security BearerAuth
// @Param id path string true "Negotiation ID"
// @Param agent_type query string true "Agent type (buyer or seller)" Enums(buyer, seller)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /bargaining/negotiations/{id}/suggest [get]
func (h *BargainingHandler) GetSuggestedCounterOffer(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("bargaining_handler").With("operation", "get_suggested_counter_offer")
	negotiationID := c.Param("id")
	agentType := c.Query("agent_type")

	if agentType != "buyer" && agentType != "seller" {
		log.Warn("invalid agent_type for suggestion", "agent_type", agentType)
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_type must be 'buyer' or 'seller'"})
		return
	}

	var negotiation *models.BargainingNegotiation
	negotiation, err := h.svc.GetNegotiation(c.Request.Context(), negotiationID)
	if err != nil {
		log.Error("failed to load negotiation for suggestion", "error", err, "negotiation_id", negotiationID)
		c.JSON(http.StatusNotFound, gin.H{"error": "negotiation not found"})
		return
	}

	suggestedAmount := h.svc.CalculateSuggestedCounterOffer(negotiation, agentType)

	c.JSON(http.StatusOK, gin.H{
		"suggested_amount": suggestedAmount,
		"agent_type":       agentType,
	})
	log.Debug("counter offer suggestion generated", "negotiation_id", negotiationID, "agent_type", agentType)
}

// GetLLMBargainingDecision uses LLM to generate strategic bargaining recommendations
// @Summary Get LLM-powered bargaining decision
// @Tags Bargaining
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param negotiation_id query string true "Negotiation ID"
// @Param agent_id query string true "Agent ID requesting the decision"
// @Param agent_type query string true "Agent type (buyer or seller)" Enums(buyer, seller)
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /bargaining/negotiations/llm-decision [get]
func (h *BargainingHandler) GetLLMBargainingDecision(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("bargaining_handler").With("operation", "llm_bargaining_decision")

	negotiationID := c.Query("negotiation_id")
	agentID := c.Query("agent_id")
	agentType := c.Query("agent_type")

	if negotiationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "negotiation_id is required"})
		return
	}

	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id is required"})
		return
	}

	if agentType != "buyer" && agentType != "seller" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_type must be 'buyer' or 'seller'"})
		return
	}

	decision, err := h.svc.GetLLMBargainingDecision(c.Request.Context(), agentID, agentType, negotiationID)
	if err != nil {
		log.Error("failed to get LLM bargaining decision", "error", err, "negotiation_id", negotiationID, "agent_id", agentID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Info("LLM bargaining decision retrieved", "negotiation_id", negotiationID, "agent_id", agentID, "agent_type", agentType, "action", decision.Action)

	c.JSON(http.StatusOK, gin.H{
		"decision":        decision.Action,
		"proposed_amount": decision.ProposedAmount,
		"reason":          decision.Reason,
		"confidence":      decision.Confidence,
		"negotiation_id":  negotiationID,
		"agent_id":        agentID,
		"agent_type":      agentType,
	})
}

// GetLLMNegotiationSummary generates AI summary of negotiation session
// @Summary Get LLM-powered negotiation summary
// @Tags Bargaining
// @Produce json
// @Security BearerAuth
// @Param negotiation_id query string true "Negotiation ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /bargaining/negotiations/llm-summary [get]
func (h *BargainingHandler) GetLLMNegotiationSummary(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("bargaining_handler").With("operation", "llm_negotiation_summary")

	negotiationID := c.Query("negotiation_id")

	if negotiationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "negotiation_id is required"})
		return
	}

	summary, err := h.svc.GetLLMNegotiationSummary(c.Request.Context(), negotiationID)
	if err != nil {
		log.Error("failed to get LLM negotiation summary", "error", err, "negotiation_id", negotiationID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Info("LLM negotiation summary retrieved", "negotiation_id", negotiationID, "summary_length", len(summary))

	c.JSON(http.StatusOK, gin.H{
		"negotiation_id": negotiationID,
		"summary":        summary,
	})
}
