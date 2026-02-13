package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"
)

var (
	ErrNegotiationNotFound = errors.New("negotiation not found")
	ErrNegotiationExpired  = errors.New("negotiation has expired")
	ErrMaxRoundsExceeded   = errors.New("maximum negotiation rounds exceeded")
	ErrInvalidAmount       = errors.New("invalid amount proposed")
)

type BargainingService struct {
	ap2Repo      interfaces.AP2Repository
	a2aClient    *a2a.A2AClient
	agentService *AgentService
	mentee       *MenteeService
	log          *logger.Logger
}

func NewBargainingService(ap2Repo interfaces.AP2Repository, a2aClient *a2a.A2AClient, agentService *AgentService, mentee *MenteeService, log *logger.Logger) *BargainingService {
	return &BargainingService{
		ap2Repo:      ap2Repo,
		a2aClient:    a2aClient,
		agentService: agentService,
		mentee:       mentee,
		log:          log,
	}
}

type CreateNegotiationRequest struct {
	BuyerAgentID       string  `json:"buyer_agent_id" validate:"required,uuid"`
	SellerAgentID      string  `json:"seller_agent_id" validate:"required,uuid"`
	UserID             string  `json:"user_id" validate:"required,uuid"`
	InitialAmount      float64 `json:"initial_amount" validate:"required,gt=0"`
	MarketplaceOrderID *string `json:"marketplace_order_id,omitempty"`
	MaxRounds          int     `json:"max_rounds" validate:"omitempty,gte=1,lte=10"`
}

type CounterOfferRequest struct {
	AgentID        string  `json:"agent_id" validate:"required,uuid"`
	ProposedAmount float64 `json:"proposed_amount" validate:"required,gte=0"`
	Reason         *string `json:"reason,omitempty" validate:"omitempty,max=500"`
	Action         string  `json:"action" validate:"required,oneof=counteroffer accept reject"`
}

func (s *BargainingService) CreateNegotiation(ctx context.Context, req *CreateNegotiationRequest) (*models.BargainingNegotiation, error) {
	buyerAgent, err := s.agentService.GetAgentByID(ctx, req.BuyerAgentID)
	if err != nil {
		return nil, fmt.Errorf("buyer agent not found: %w", err)
	}

	sellerAgent, err := s.agentService.GetAgentByID(ctx, req.SellerAgentID)
	if err != nil {
		return nil, fmt.Errorf("seller agent not found: %w", err)
	}

	buyerVolatility := s.getAgentVolatility(buyerAgent)
	sellerVolatility := s.getAgentVolatility(sellerAgent)

	maxRounds := req.MaxRounds
	if maxRounds == 0 {
		maxRounds = 5
	}

	negotiation := &models.BargainingNegotiation{
		BuyerAgentID:       req.BuyerAgentID,
		SellerAgentID:      req.SellerAgentID,
		UserID:             req.UserID,
		MarketplaceOrderID: req.MarketplaceOrderID,
		InitialAmount:      req.InitialAmount,
		CurrentAmount:      req.InitialAmount,
		BuyerVolatility:    buyerVolatility,
		SellerVolatility:   sellerVolatility,
		Status:             "initiated",
		Rounds:             0,
		MaxRounds:          maxRounds,
		ExpiresAt:          time.Now().Add(24 * time.Hour),
		Metadata:           s.marshalMetadata(map[string]interface{}{}),
	}

	if err := s.ap2Repo.CreateBargainingNegotiation(ctx, negotiation); err != nil {
		s.log.Error("failed to create negotiation", "error", err, "buyer_id", req.BuyerAgentID, "seller_id", req.SellerAgentID)
		return nil, fmt.Errorf("failed to create negotiation: %w", err)
	}

	s.log.Info("created negotiation", "negotiation_id", negotiation.ID, "buyer_volatility", buyerVolatility, "seller_volatility", sellerVolatility)
	return negotiation, nil
}

func (s *BargainingService) GetNegotiation(ctx context.Context, negotiationID string) (*models.BargainingNegotiation, error) {
	negotiation, err := s.ap2Repo.GetBargainingNegotiationByID(ctx, negotiationID)
	if err != nil {
		return nil, ErrNegotiationNotFound
	}

	if negotiation.ExpiresAt.Before(time.Now()) {
		if negotiation.Status != "completed" && negotiation.Status != "accepted" && negotiation.Status != "rejected" {
			if err := s.ap2Repo.UpdateNegotiationStatus(ctx, negotiationID, "expired"); err == nil {
				negotiation.Status = "expired"
			}
			return negotiation, ErrNegotiationExpired
		}
	}

	return negotiation, nil
}

func (s *BargainingService) GetNegotiationsByUser(ctx context.Context, userID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	return s.ap2Repo.GetNegotiationsByUser(ctx, userID, page, limit)
}

func (s *BargainingService) SubmitCounterOffer(ctx context.Context, negotiationID string, req *CounterOfferRequest) (*models.BargainingRound, *models.BargainingNegotiation, error) {
	negotiation, err := s.GetNegotiation(ctx, negotiationID)
	if err != nil {
		return nil, nil, err
	}

	if negotiation.Status == "expired" {
		return nil, nil, ErrNegotiationExpired
	}

	if negotiation.Status == "accepted" || negotiation.Status == "rejected" {
		return nil, nil, errors.New("negotiation already completed")
	}

	if negotiation.Rounds >= negotiation.MaxRounds {
		if err := s.ap2Repo.UpdateNegotiationStatus(ctx, negotiationID, "expired"); err == nil {
			negotiation.Status = "expired"
		}
		return nil, nil, ErrMaxRoundsExceeded
	}

	agent, err := s.agentService.GetAgentByID(ctx, req.AgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("agent not found: %w", err)
	}

	var agentType string
	var volatility float64

	if req.AgentID == negotiation.BuyerAgentID {
		agentType = "buyer"
		volatility = negotiation.BuyerVolatility
	} else if req.AgentID == negotiation.SellerAgentID {
		agentType = "seller"
		volatility = negotiation.SellerVolatility
	} else {
		return nil, nil, errors.New("agent is not part of this negotiation")
	}

	if req.Action == "accept" {
		round := &models.BargainingRound{
			NegotiationID:    negotiationID,
			AgentID:          req.AgentID,
			RoundNumber:      negotiation.Rounds + 1,
			ProposedAmount:   negotiation.CurrentAmount,
			PreviousAmount:   negotiation.CurrentAmount,
			AgentType:        agentType,
			Action:           "accept",
			VolatilityFactor: 0,
			Metadata:         s.marshalMetadata(map[string]interface{}{}),
		}

		if err := s.ap2Repo.CreateBargainingRound(ctx, round); err != nil {
			return nil, nil, fmt.Errorf("failed to create round: %w", err)
		}

		now := time.Now()
		if err := s.ap2Repo.CompleteNegotiation(ctx, negotiationID, "accepted", req.ProposedAmount, &now); err != nil {
			return nil, nil, fmt.Errorf("failed to complete negotiation: %w", err)
		}

		negotiation.Status = "accepted"
		negotiation.CurrentAmount = req.ProposedAmount
		negotiation.Rounds++
		negotiation.CompletedAt = &now

		s.log.Info("negotiation accepted", "negotiation_id", negotiationID, "agent_type", agentType, "amount", req.ProposedAmount)

		go s.notifyAgentNegotiationComplete(ctx, negotiation, agent, "accepted")

		return round, negotiation, nil
	}

	if req.Action == "reject" {
		round := &models.BargainingRound{
			NegotiationID:    negotiationID,
			AgentID:          req.AgentID,
			RoundNumber:      negotiation.Rounds + 1,
			ProposedAmount:   req.ProposedAmount,
			PreviousAmount:   negotiation.CurrentAmount,
			AgentType:        agentType,
			Action:           "reject",
			Reason:           req.Reason,
			VolatilityFactor: 0,
			Metadata:         s.marshalMetadata(map[string]interface{}{}),
		}

		if err := s.ap2Repo.CreateBargainingRound(ctx, round); err != nil {
			return nil, nil, fmt.Errorf("failed to create round: %w", err)
		}

		if err := s.ap2Repo.UpdateNegotiationStatus(ctx, negotiationID, "rejected"); err != nil {
			return nil, nil, fmt.Errorf("failed to update negotiation: %w", err)
		}

		negotiation.Status = "rejected"
		negotiation.Rounds++

		s.log.Info("negotiation rejected", "negotiation_id", negotiationID, "agent_type", agentType)

		go s.notifyAgentNegotiationComplete(ctx, negotiation, agent, "rejected")

		return round, negotiation, nil
	}

	if req.Action == "counteroffer" {
		if !s.isValidCounterOffer(negotiation, agentType, req.ProposedAmount) {
			return nil, nil, ErrInvalidAmount
		}

		round := &models.BargainingRound{
			NegotiationID:    negotiationID,
			AgentID:          req.AgentID,
			RoundNumber:      negotiation.Rounds + 1,
			ProposedAmount:   req.ProposedAmount,
			PreviousAmount:   negotiation.CurrentAmount,
			AgentType:        agentType,
			Action:           "counteroffer",
			Reason:           req.Reason,
			VolatilityFactor: s.calculateVolatilityFactor(volatility, negotiation.Rounds, negotiation.InitialAmount, req.ProposedAmount),
			Metadata:         s.marshalMetadata(map[string]interface{}{}),
		}

		if err := s.ap2Repo.CreateBargainingRound(ctx, round); err != nil {
			return nil, nil, fmt.Errorf("failed to create round: %w", err)
		}

		if err := s.ap2Repo.UpdateNegotiationAmountAndRounds(ctx, negotiationID, req.ProposedAmount, negotiation.Rounds+1, "in_progress"); err != nil {
			return nil, nil, fmt.Errorf("failed to update negotiation: %w", err)
		}

		negotiation.CurrentAmount = req.ProposedAmount
		negotiation.Rounds++

		s.log.Info("counteroffer submitted", "negotiation_id", negotiationID, "agent_type", agentType, "amount", req.ProposedAmount, "round", negotiation.Rounds)

		go s.notifyAgentCounterOffer(ctx, negotiation, agent, round)

		return round, negotiation, nil
	}

	return nil, nil, errors.New("invalid action")
}

func (s *BargainingService) GetNegotiationRounds(ctx context.Context, negotiationID string) ([]*models.BargainingRound, error) {
	return s.ap2Repo.GetBargainingRounds(ctx, negotiationID)
}

func (s *BargainingService) CalculateSuggestedCounterOffer(negotiation *models.BargainingNegotiation, agentType string) float64 {
	var agentID string
	var opponentID string

	if agentType == "buyer" {
		agentID = negotiation.BuyerAgentID
		opponentID = negotiation.SellerAgentID
	} else {
		agentID = negotiation.SellerAgentID
		opponentID = negotiation.BuyerAgentID
	}

	decision, err := s.mentee.GetBargainingDecision(
		context.Background(),
		agentID,
		agentType,
		negotiation.CurrentAmount,
		negotiation.InitialAmount,
		negotiation.Rounds,
		negotiation.MaxRounds,
		opponentID,
	)

	if err != nil {
		s.log.Warn("mentee decision failed, using fallback", "error", err)
		return s.calculateFallbackCounterOffer(negotiation, agentType)
	}

	s.log.Info("mentee decision generated", "action", decision.Action, "confidence", decision.Confidence, "reason", decision.Reason)

	return decision.ProposedAmount
}

func (s *BargainingService) GetMenteeRecommendation(negotiation *models.BargainingNegotiation, agentType string) (*BargainingDecision, error) {
	var agentID string
	var opponentID string

	if agentType == "buyer" {
		agentID = negotiation.BuyerAgentID
		opponentID = negotiation.SellerAgentID
	} else {
		agentID = negotiation.SellerAgentID
		opponentID = negotiation.BuyerAgentID
	}

	return s.mentee.GetBargainingDecision(
		context.Background(),
		agentID,
		agentType,
		negotiation.CurrentAmount,
		negotiation.InitialAmount,
		negotiation.Rounds,
		negotiation.MaxRounds,
		opponentID,
	)
}

func (s *BargainingService) RecordNegotiationOutcome(negotiation *models.BargainingNegotiation, round *models.BargainingRound) error {
	agentType := round.AgentType
	var opponentID string

	if agentType == "buyer" {
		opponentID = negotiation.SellerAgentID
	} else {
		opponentID = negotiation.BuyerAgentID
	}

	outcome := &NegotiationOutcome{
		NegotiationID:    negotiation.ID,
		InitialAmount:    negotiation.InitialAmount,
		FinalAmount:      negotiation.CurrentAmount,
		Status:           negotiation.Status,
		Rounds:           negotiation.Rounds,
		OpponentID:       opponentID,
		OpponentType:     agentType,
		Timestamp:        time.Now(),
		Strategy:         round.Action,
		VolatilityFactor: round.VolatilityFactor,
	}

	return s.mentee.RecordNegotiationOutcome(context.Background(), outcome)
}

func (s *BargainingService) calculateFallbackCounterOffer(negotiation *models.BargainingNegotiation, agentType string) float64 {
	var volatility float64
	var currentAmount float64

	if agentType == "buyer" {
		volatility = negotiation.BuyerVolatility
		currentAmount = negotiation.CurrentAmount
	} else {
		volatility = negotiation.SellerVolatility
		currentAmount = negotiation.CurrentAmount
	}

	maxDiscount := volatility * 0.15
	minDiscount := volatility * 0.02

	discountFactor := minDiscount + (maxDiscount-minDiscount)*0.5
	suggestedAmount := currentAmount * (1 - discountFactor)

	return math.Round(suggestedAmount*100) / 100
}

func (s *BargainingService) getAgentVolatility(agent *models.Agent) float64 {
	if agent.Config == "" {
		return 0.5
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(agent.Config), &config); err != nil {
		return 0.5
	}

	if volatility, ok := config["volatility"].(float64); ok {
		if volatility >= 0 && volatility <= 1 {
			return volatility
		}
	}

	return 0.5
}

func (s *BargainingService) isValidCounterOffer(negotiation *models.BargainingNegotiation, agentType string, proposedAmount float64) bool {
	if proposedAmount <= 0 {
		return false
	}

	if agentType == "buyer" {
		if proposedAmount > negotiation.CurrentAmount {
			return false
		}
		if proposedAmount < negotiation.InitialAmount*0.3 {
			return false
		}
	} else {
		if proposedAmount < negotiation.CurrentAmount {
			return false
		}
		if proposedAmount > negotiation.InitialAmount*1.5 {
			return false
		}
	}

	return true
}

func (s *BargainingService) calculateVolatilityFactor(volatility float64, rounds int, initialAmount, currentAmount float64) float64 {
	changePercent := math.Abs((currentAmount - initialAmount) / initialAmount * 100)
	volatilityEffect := volatility * (changePercent / 100)
	roundDecay := 1.0 / math.Pow(1.2, float64(rounds))

	factor := volatilityEffect * roundDecay

	return math.Round(factor*10000) / 10000
}

func (s *BargainingService) marshalMetadata(metadata map[string]interface{}) string {
	data, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *BargainingService) notifyAgentCounterOffer(ctx context.Context, negotiation *models.BargainingNegotiation, agent *models.Agent, round *models.BargainingRound) {
	var receiverID string
	if agent.ID == negotiation.BuyerAgentID {
		receiverID = negotiation.SellerAgentID
	} else {
		receiverID = negotiation.BuyerAgentID
	}

	receiverAgent, err := s.agentService.GetAgentByID(ctx, receiverID)
	if err != nil {
		s.log.Error("failed to get receiver agent for notification", "error", err, "receiver_id", receiverID)
		return
	}

	if receiverAgent.A2AEndpoint == nil {
		s.log.Warn("receiver agent has no A2A endpoint", "receiver_id", receiverID)
		return
	}

	payload := map[string]interface{}{
		"type":              "counter_offer",
		"negotiation_id":    negotiation.ID,
		"round_number":      round.RoundNumber,
		"proposed_amount":   round.ProposedAmount,
		"agent_type":        round.AgentType,
		"volatility_factor": round.VolatilityFactor,
		"reason":            round.Reason,
	}

	taskPayload := &a2a.TaskStartPayload{
		TaskName:    "bargaining_notification",
		Description: fmt.Sprintf("Counteroffer in negotiation %s", negotiation.ID),
		Parameters:  payload,
	}

	if _, err := s.a2aClient.StartTask(ctx, *receiverAgent.A2AEndpoint, agent.ID, receiverID, taskPayload); err != nil {
		s.log.Error("failed to send counteroffer notification", "error", err, "receiver_id", receiverID)
	}
}

func (s *BargainingService) notifyAgentNegotiationComplete(ctx context.Context, negotiation *models.BargainingNegotiation, agent *models.Agent, outcome string) {
	var receiverID string
	if agent.ID == negotiation.BuyerAgentID {
		receiverID = negotiation.SellerAgentID
	} else {
		receiverID = negotiation.BuyerAgentID
	}

	receiverAgent, err := s.agentService.GetAgentByID(ctx, receiverID)
	if err != nil {
		s.log.Error("failed to get receiver agent for notification", "error", err, "receiver_id", receiverID)
		return
	}

	if receiverAgent.A2AEndpoint == nil {
		s.log.Warn("receiver agent has no A2A endpoint", "receiver_id", receiverID)
		return
	}

	payload := map[string]interface{}{
		"type":           "negotiation_complete",
		"negotiation_id": negotiation.ID,
		"status":         outcome,
		"final_amount":   negotiation.CurrentAmount,
		"rounds":         negotiation.Rounds,
	}

	taskPayload := &a2a.TaskStartPayload{
		TaskName:    "bargaining_notification",
		Description: fmt.Sprintf("Negotiation %s completed with status %s", negotiation.ID, outcome),
		Parameters:  payload,
	}

	if _, err := s.a2aClient.StartTask(ctx, *receiverAgent.A2AEndpoint, agent.ID, receiverID, taskPayload); err != nil {
		s.log.Error("failed to send negotiation complete notification", "error", err, "receiver_id", receiverID)
	}
}
