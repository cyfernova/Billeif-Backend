package handlers

import (
	"net/http"

	"invoice-backend/internal/config"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type A2ABargainingHandler struct {
	a2aBargaining *services.A2ABargainingService
	agentService  *services.AgentService
	cfg           *config.Config
	log           *logger.Logger
}

func NewA2ABargainingHandler(a2aBargaining *services.A2ABargainingService, agentService *services.AgentService, cfg *config.Config, log *logger.Logger) *A2ABargainingHandler {
	return &A2ABargainingHandler{
		a2aBargaining: a2aBargaining,
		agentService:  agentService,
		cfg:           cfg,
		log:           log,
	}
}

type StartA2ANegotiationRequest struct {
	BuyerAgentID  string  `json:"buyer_agent_id" binding:"required,uuid"`
	SellerAgentID string  `json:"seller_agent_id" binding:"required,uuid"`
	InitialAmount float64 `json:"initial_amount" binding:"required,gt=0"`
	MaxRounds     *int    `json:"max_rounds" binding:"omitempty,gte=1,lte=10"`
}

type StartAutonomousNegotiationRequest struct {
	BuyerAgentID   string  `json:"buyer_agent_id" binding:"required,uuid"`
	SellerAgentID  string  `json:"seller_agent_id" binding:"required,uuid"`
	InitialAmount  float64 `json:"initial_amount" binding:"required,gt=0"`
	ReferencePrice float64 `json:"reference_price" binding:"omitempty,gt=0"`
	MaxRounds      int     `json:"max_rounds" binding:"omitempty,gte=1,lte=20"`
	CallbackURL    string  `json:"callback_url"`
	UserID         string  `json:"user_id" binding:"omitempty,uuid"`
}

type A2ANegotiationSession struct {
	SessionID     string     `json:"session_id"`
	BuyerAgent    *AgentInfo `json:"buyer_agent"`
	SellerAgent   *AgentInfo `json:"seller_agent"`
	InitialAmount float64    `json:"initial_amount"`
	CurrentAmount float64    `json:"current_amount"`
	Round         int        `json:"round"`
	MaxRounds     int        `json:"max_rounds"`
	Status        string     `json:"status"`
	StartTime     string     `json:"start_time"`
}

type AgentInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

func (h *A2ABargainingHandler) defaultA2AMessageEndpoint() string {
	if h.cfg == nil {
		return "/api/v1/a2a/message"
	}
	return h.cfg.Server.A2AMessageEndpoint()
}

// StartNegotiation starts an A2A bargaining negotiation
// @Summary Start A2A negotiation
// @Description Starts an A2A bargaining negotiation between buyer and seller agents
// @Tags A2A Bargaining
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body StartA2ANegotiationRequest true "Negotiation details"
// @Success 201 {object} A2ANegotiationSession
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /a2a-bargaining/start [post]
func (h *A2ABargainingHandler) StartNegotiation(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("a2a_bargaining_handler").With("operation", "start_negotiation")

	var req StartA2ANegotiationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warn("invalid start negotiation request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	buyerAgent, err := h.agentService.GetAgentByID(c.Request.Context(), req.BuyerAgentID)
	if err != nil {
		log.Error("buyer agent not found", "agent_id", req.BuyerAgentID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "buyer agent not found"})
		return
	}

	sellerAgent, err := h.agentService.GetAgentByID(c.Request.Context(), req.SellerAgentID)
	if err != nil {
		log.Error("seller agent not found", "agent_id", req.SellerAgentID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "seller agent not found"})
		return
	}

	if buyerAgent.Type != "shopping" {
		log.Warn("buyer agent must be shopping type", "agent_type", buyerAgent.Type)
		c.JSON(http.StatusBadRequest, gin.H{"error": "buyer agent must be of type 'shopping'"})
		return
	}

	if sellerAgent.Type != "merchant" {
		log.Warn("seller agent must be merchant type", "agent_type", sellerAgent.Type)
		c.JSON(http.StatusBadRequest, gin.H{"error": "seller agent must be of type 'merchant'"})
		return
	}

	session, err := h.a2aBargaining.StartNegotiation(
		c.Request.Context(),
		req.BuyerAgentID,
		req.SellerAgentID,
		req.InitialAmount,
	)

	if err != nil {
		log.Error("failed to start A2A negotiation", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start negotiation"})
		return
	}

	buyerEndpoint := h.defaultA2AMessageEndpoint()
	if buyerAgent.A2AEndpoint != nil {
		buyerEndpoint = *buyerAgent.A2AEndpoint
	}

	sellerEndpoint := h.defaultA2AMessageEndpoint()
	if sellerAgent.A2AEndpoint != nil {
		sellerEndpoint = *sellerAgent.A2AEndpoint
	}

	maxRounds := 5
	if req.MaxRounds != nil {
		maxRounds = *req.MaxRounds
	}

	response := A2ANegotiationSession{
		SessionID: session.NegotiationID,
		BuyerAgent: &AgentInfo{
			ID:       buyerAgent.ID,
			Name:     buyerAgent.Name,
			Type:     buyerAgent.Type,
			Endpoint: buyerEndpoint,
		},
		SellerAgent: &AgentInfo{
			ID:       sellerAgent.ID,
			Name:     sellerAgent.Name,
			Type:     sellerAgent.Type,
			Endpoint: sellerEndpoint,
		},
		InitialAmount: session.InitialAmount,
		CurrentAmount: session.CurrentAmount,
		Round:         session.Round,
		MaxRounds:     maxRounds,
		Status:        "running",
		StartTime:     session.StartTime.Format("2006-01-02T15:04:05Z"),
	}

	log.Info("A2A negotiation started", "session_id", session.NegotiationID, "buyer", req.BuyerAgentID, "seller", req.SellerAgentID)

	c.JSON(http.StatusCreated, response)
}

// StartAutonomousNegotiation starts an AI-driven autonomous negotiation between buyer and seller agents
// @Summary Start autonomous AI negotiation
// @Description Starts a fully autonomous price negotiation driven by LLM decisions between buyer and seller agents. Real-time events are delivered via webhook callbacks.
// @Tags A2A Bargaining
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body StartAutonomousNegotiationRequest true "Autonomous negotiation details"
// @Success 202 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /a2a-bargaining/autonomous/start [post]
func (h *A2ABargainingHandler) StartAutonomousNegotiation(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("a2a_bargaining_handler").With("operation", "start_autonomous_negotiation")

	var req StartAutonomousNegotiationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warn("invalid autonomous negotiation request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	buyerAgent, err := h.agentService.GetAgentByID(c.Request.Context(), req.BuyerAgentID)
	if err != nil {
		log.Error("buyer agent not found", "agent_id", req.BuyerAgentID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "buyer agent not found"})
		return
	}

	sellerAgent, err := h.agentService.GetAgentByID(c.Request.Context(), req.SellerAgentID)
	if err != nil {
		log.Error("seller agent not found", "agent_id", req.SellerAgentID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "seller agent not found"})
		return
	}

	if buyerAgent.Type != "shopping" {
		log.Warn("buyer agent must be shopping type", "agent_type", buyerAgent.Type)
		c.JSON(http.StatusBadRequest, gin.H{"error": "buyer agent must be of type 'shopping'"})
		return
	}

	if sellerAgent.Type != "merchant" {
		log.Warn("seller agent must be merchant type", "agent_type", sellerAgent.Type)
		c.JSON(http.StatusBadRequest, gin.H{"error": "seller agent must be of type 'merchant'"})
		return
	}

	// Ensure max_rounds defaults to 5 if not provided or invalid
	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 5
		log.Info("max_rounds not provided, defaulting to 5", "buyer", req.BuyerAgentID, "seller", req.SellerAgentID)
	}

	autoReq := &services.AutonomousNegotiationRequest{
		BuyerAgentID:   req.BuyerAgentID,
		SellerAgentID:  req.SellerAgentID,
		InitialAmount:  req.InitialAmount,
		ReferencePrice: req.ReferencePrice,
		MaxRounds:      maxRounds,
		CallbackURL:    req.CallbackURL,
		UserID: func() string {
			if req.UserID != "" {
				return req.UserID
			}
			return c.GetString("user_id")
		}(),
	}

	session, err := h.a2aBargaining.StartAutonomousNegotiation(c.Request.Context(), autoReq)
	if err != nil {
		log.Error("failed to start autonomous negotiation", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start negotiation"})
		return
	}

	log.Info("autonomous negotiation session created, enqueuing for processing",
		"session_id", session.NegotiationID,
		"buyer", req.BuyerAgentID,
		"seller", req.SellerAgentID,
		"callback_url", req.CallbackURL)

	// Enqueue first round to SQS worker
	if err := h.a2aBargaining.EnqueueNegotiationRound(session.NegotiationID, session.DBNegotiationID, 0); err != nil {
		log.Error("failed to enqueue negotiation round", "error", err, "session_id", session.NegotiationID)
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":        "autonomous negotiation started",
		"session_id":     session.NegotiationID,
		"negotiation_id": session.DBNegotiationID,
		"status":         "running",
	})
}

// GetSessionProgress retrieves progress of an A2A negotiation
// @Summary Get negotiation progress
// @Description Returns the progress of an A2A bargaining negotiation
// @Tags A2A Bargaining
// @Produce json
// @Security BearerAuth
// @Param sessionId path string true "Session ID"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /a2a-bargaining/progress/{sessionId} [get]
func (h *A2ABargainingHandler) GetSessionProgress(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("a2a_bargaining_handler").With("operation", "get_progress")

	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	progress := h.a2aBargaining.GetSessionProgress(sessionID)

	if progress == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "negotiation session not found"})
		return
	}

	log.Info("A2A negotiation progress retrieved", "session_id", sessionID, "round", progress.Round, "current_amount", progress.CurrentAmount)

	c.JSON(http.StatusOK, progress)
}

// GetNegotiationProgress retrieves progress of a bargaining negotiation by its UUID
// @Summary Get negotiation progress by negotiation ID
// @Description Returns the progress of a bargaining negotiation using the database UUID
// @Tags A2A Bargaining
// @Produce json
// @Security BearerAuth
// @Param negotiationId path string true "Negotiation ID (UUID)"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /a2a-bargaining/negotiation/{negotiationId}/progress [get]
func (h *A2ABargainingHandler) GetNegotiationProgress(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("a2a_bargaining_handler").With("operation", "get_negotiation_progress")

	negotiationID := c.Param("negotiationId")
	if negotiationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "negotiation_id is required"})
		return
	}

	progress := h.a2aBargaining.GetSessionProgressByNegotiationID(c.Request.Context(), negotiationID)

	if progress == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "negotiation not found"})
		return
	}

	log.Info("A2A negotiation progress retrieved", "negotiation_id", negotiationID, "round", progress.Round, "current_amount", progress.CurrentAmount, "status", progress.Status)

	c.JSON(http.StatusOK, progress)
}

// StopNegotiation stops an A2A bargaining negotiation
// @Summary Stop negotiation
// @Description Stops an active A2A bargaining negotiation
// @Tags A2A Bargaining
// @Produce json
// @Security BearerAuth
// @Param sessionId path string true "Session ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /a2a-bargaining/stop/{sessionId} [post]
func (h *A2ABargainingHandler) StopNegotiation(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("a2a_bargaining_handler").With("operation", "stop_negotiation")

	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	h.a2aBargaining.StopNegotiation(sessionID)

	log.Info("A2A negotiation stopped", "session_id", sessionID)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Negotiation stopped",
		"session_id": sessionID,
		"status":     "stopped",
	})
}
