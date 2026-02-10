package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

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

func (h *BargainingHandler) CreateNegotiation(c *gin.Context) {
	userID := c.GetString("user_id")

	var req CreateNegotiationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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

	negotiation, err := h.svc.CreateNegotiation(c.Request.Context(), createReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, negotiation)
}

func (h *BargainingHandler) GetNegotiation(c *gin.Context) {
	id := c.Param("id")

	negotiation, err := h.svc.GetNegotiation(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "negotiation not found"})
		return
	}

	c.JSON(http.StatusOK, negotiation)
}

func (h *BargainingHandler) ListNegotiations(c *gin.Context) {
	userID := c.GetString("user_id")
	page, limit := utils.ParsePagination(c)

	negotiations, total, err := h.svc.GetNegotiationsByUser(c.Request.Context(), userID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  negotiations,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *BargainingHandler) SubmitCounterOffer(c *gin.Context) {
	negotiationID := c.Param("id")

	var req CounterOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	counterOfferReq := &services.CounterOfferRequest{
		AgentID:        req.AgentID,
		ProposedAmount: req.ProposedAmount,
		Reason:         req.Reason,
		Action:         req.Action,
	}

	round, negotiation, err := h.svc.SubmitCounterOffer(c.Request.Context(), negotiationID, counterOfferReq)
	if err != nil {
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

	c.JSON(http.StatusOK, gin.H{
		"round":       round,
		"negotiation": negotiation,
	})
}

func (h *BargainingHandler) GetNegotiationRounds(c *gin.Context) {
	negotiationID := c.Param("id")

	rounds, err := h.svc.GetNegotiationRounds(c.Request.Context(), negotiationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"rounds": rounds,
	})
}

func (h *BargainingHandler) GetSuggestedCounterOffer(c *gin.Context) {
	negotiationID := c.Param("id")
	agentType := c.Query("agent_type")

	if agentType != "buyer" && agentType != "seller" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_type must be 'buyer' or 'seller'"})
		return
	}

	negotiation, err := h.svc.GetNegotiation(c.Request.Context(), negotiationID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "negotiation not found"})
		return
	}

	suggestedAmount := h.svc.CalculateSuggestedCounterOffer(negotiation, agentType)

	c.JSON(http.StatusOK, gin.H{
		"suggested_amount": suggestedAmount,
		"agent_type":       agentType,
	})
}
