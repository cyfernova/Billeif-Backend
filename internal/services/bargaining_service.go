package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
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
	llm          *LLMService
	log          *logger.Logger
}

func NewBargainingService(ap2Repo interfaces.AP2Repository, a2aClient *a2a.A2AClient, agentService *AgentService, mentee *MenteeService, llm *LLMService, log *logger.Logger) *BargainingService {
	return &BargainingService{
		ap2Repo:      ap2Repo,
		a2aClient:    a2aClient,
		agentService: agentService,
		mentee:       mentee,
		llm:          llm,
		log:          log,
	}
}

type CreateNegotiationRequest struct {
	BuyerAgentID       string                 `json:"buyer_agent_id" validate:"required,uuid"`
	SellerAgentID      string                 `json:"seller_agent_id" validate:"required,uuid"`
	UserID             string                 `json:"user_id" validate:"required,uuid"`
	InitialAmount      float64                `json:"initial_amount" validate:"required,gt=0"`
	ReferencePrice     float64                `json:"reference_price" validate:"omitempty,gt=0"`
	MarketplaceOrderID *string                `json:"marketplace_order_id,omitempty"`
	MaxRounds          int                    `json:"max_rounds" validate:"omitempty,gte=1,lte=20"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
	SessionID          *string                `json:"session_id,omitempty"`
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

	if NormalizeMarketplaceAgentType(buyerAgent.Type) != "shopping" {
		return nil, fmt.Errorf("buyer agent must be a shopping agent")
	}
	if NormalizeMarketplaceAgentType(sellerAgent.Type) != "merchant" {
		return nil, fmt.Errorf("seller agent must be a merchant agent")
	}

	buyerVolatility := getAgentVolatilityInternal(buyerAgent)
	sellerVolatility := getAgentVolatilityInternal(sellerAgent)

	maxRounds := req.MaxRounds
	if maxRounds == 0 {
		maxRounds = 5
	}

	// Use ReferencePrice if provided, otherwise fall back to seller's listed price from agent config
	referencePrice := req.ReferencePrice
	if referencePrice == 0 && sellerAgent.Price != nil && *sellerAgent.Price > 0 {
		referencePrice = *sellerAgent.Price
	}
	if referencePrice == 0 {
		referencePrice = req.InitialAmount
	}

	// Store reference_price in metadata
	metadata := req.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["reference_price"] = referencePrice

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
		Metadata:           s.marshalMetadata(metadata),
		SessionID:          req.SessionID,
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

	buyerAgent, sellerAgent, err := s.resolveNegotiationAgents(ctx, negotiation)
	if err != nil {
		return nil, nil, err
	}

	var agentType string
	var volatility float64
	var agent *models.Agent

	if req.AgentID == negotiation.BuyerAgentID {
		agentType = "buyer"
		volatility = negotiation.BuyerVolatility
		agent = buyerAgent
	} else if req.AgentID == negotiation.SellerAgentID {
		agentType = "seller"
		volatility = negotiation.SellerVolatility
		agent = sellerAgent
	} else {
		return nil, nil, errors.New("agent is not part of this negotiation")
	}

	receiverAgent := s.getCounterpartyAgent(negotiation, agent, buyerAgent, sellerAgent)

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

		if !s.skipA2ANotifications(negotiation) {
			go s.notifyAgentNegotiationComplete(ctx, negotiation, agent, receiverAgent, "accepted")
		}

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

		if !s.skipA2ANotifications(negotiation) {
			go s.notifyAgentNegotiationComplete(ctx, negotiation, agent, receiverAgent, "rejected")
		}

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

		if !s.skipA2ANotifications(negotiation) {
			go s.notifyAgentCounterOffer(ctx, negotiation, agent, receiverAgent, round)
		}

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

func (s *BargainingService) skipA2ANotifications(negotiation *models.BargainingNegotiation) bool {
	if negotiation == nil || strings.TrimSpace(negotiation.Metadata) == "" {
		return false
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(negotiation.Metadata), &metadata); err != nil {
		return false
	}

	value, ok := metadata["procurement_managed_a2a"].(bool)
	return ok && value
}

func (s *BargainingService) resolveNegotiationAgents(ctx context.Context, negotiation *models.BargainingNegotiation) (*models.Agent, *models.Agent, error) {
	buyerAgent := negotiation.BuyerAgent
	if buyerAgent == nil || buyerAgent.ID == "" {
		var err error
		buyerAgent, err = s.agentService.GetAgentByID(ctx, negotiation.BuyerAgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("buyer agent not found: %w", err)
		}
	}

	sellerAgent := negotiation.SellerAgent
	if sellerAgent == nil || sellerAgent.ID == "" {
		var err error
		sellerAgent, err = s.agentService.GetAgentByID(ctx, negotiation.SellerAgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("seller agent not found: %w", err)
		}
	}

	return buyerAgent, sellerAgent, nil
}

func (s *BargainingService) getCounterpartyAgent(negotiation *models.BargainingNegotiation, agent, buyerAgent, sellerAgent *models.Agent) *models.Agent {
	if agent == nil {
		return nil
	}
	if agent.ID == negotiation.BuyerAgentID {
		return sellerAgent
	}
	return buyerAgent
}

func (s *BargainingService) notifyAgentCounterOffer(ctx context.Context, negotiation *models.BargainingNegotiation, agent, receiverAgent *models.Agent, round *models.BargainingRound) {
	if receiverAgent == nil {
		s.log.Warn("receiver agent missing for counteroffer notification", "negotiation_id", negotiation.ID)
		return
	}

	receiverID := receiverAgent.ID
	if receiverAgent.A2AEndpoint == nil {
		s.log.Warn("receiver agent has no A2A endpoint", "receiver_id", receiverID)
		return
	}

	req := &a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: uuid.NewString(),
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{
				{Text: fmt.Sprintf("Counteroffer in negotiation %s.", negotiation.ID)},
			},
			Metadata: map[string]interface{}{
				"taskType": "bargaining.notification",
			},
		},
		Metadata: map[string]interface{}{
			"taskType":         "bargaining.notification",
			"type":             "counter_offer",
			"negotiationId":    negotiation.ID,
			"roundNumber":      round.RoundNumber,
			"proposedAmount":   round.ProposedAmount,
			"agentType":        round.AgentType,
			"volatilityFactor": round.VolatilityFactor,
			"reason":           round.Reason,
			"senderAgentId":    agent.ID,
			"receiverAgentId":  receiverID,
		},
	}

	if _, err := s.a2aClient.SendMessage(ctx, *receiverAgent.A2AEndpoint, req); err != nil {
		s.log.Error("failed to send counteroffer notification", "error", err, "receiver_id", receiverID)
	}
}

func (s *BargainingService) notifyAgentNegotiationComplete(ctx context.Context, negotiation *models.BargainingNegotiation, agent, receiverAgent *models.Agent, outcome string) {
	if receiverAgent == nil {
		s.log.Warn("receiver agent missing for completion notification", "negotiation_id", negotiation.ID)
		return
	}

	receiverID := receiverAgent.ID
	if receiverAgent.A2AEndpoint == nil {
		s.log.Warn("receiver agent has no A2A endpoint", "receiver_id", receiverID)
		return
	}

	req := &a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: uuid.NewString(),
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{
				{Text: fmt.Sprintf("Negotiation %s completed with status %s.", negotiation.ID, outcome)},
			},
			Metadata: map[string]interface{}{
				"taskType": "bargaining.notification",
			},
		},
		Metadata: map[string]interface{}{
			"taskType":        "bargaining.notification",
			"type":            "negotiation_complete",
			"negotiationId":   negotiation.ID,
			"status":          outcome,
			"finalAmount":     negotiation.CurrentAmount,
			"rounds":          negotiation.Rounds,
			"senderAgentId":   agent.ID,
			"receiverAgentId": receiverID,
		},
	}

	if _, err := s.a2aClient.SendMessage(ctx, *receiverAgent.A2AEndpoint, req); err != nil {
		s.log.Error("failed to send negotiation complete notification", "error", err, "receiver_id", receiverID)
	}
}

type LLMBargainingRequest struct {
	AgentID       string  `json:"agent_id"`
	AgentType     string  `json:"agent_type"`
	NegotiationID string  `json:"negotiation_id"`
	CurrentAmount float64 `json:"current_amount"`
	InitialAmount float64 `json:"initial_amount"`
	Round         int     `json:"round"`
	MaxRounds     int     `json:"max_rounds"`
	OpponentID    string  `json:"opponent_id"`
	HistoryRounds int     `json:"history_rounds"`
}

type LLMBargainingResponse struct {
	Action         string  `json:"action"`
	ProposedAmount float64 `json:"proposed_amount"`
	Reason         string  `json:"reason"`
	Confidence     float64 `json:"confidence"`
}

func (s *BargainingService) GetLLMBargainingDecision(ctx context.Context, agentID, agentType, negotiationID string) (*LLMBargainingResponse, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("LLM service not available")
	}

	negotiation, err := s.GetNegotiation(ctx, negotiationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get negotiation: %w", err)
	}

	rounds, err := s.ap2Repo.GetBargainingRounds(ctx, negotiationID)
	if err != nil {
		s.log.Warn("failed to get negotiation rounds", "error", err)
		rounds = []*models.BargainingRound{}
	}

	buyerAgent, sellerAgent, err := s.resolveNegotiationAgents(ctx, negotiation)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve negotiation agents: %w", err)
	}

	var agent *models.Agent

	switch agentID {
	case negotiation.BuyerAgentID:
		agent = buyerAgent
	case negotiation.SellerAgentID:
		agent = sellerAgent
	default:
		return nil, errors.New("agent is not part of this negotiation")
	}

	agentConfig := make(map[string]interface{})
	if agent.Config != "" {
		if err := json.Unmarshal([]byte(agent.Config), &agentConfig); err != nil {
			s.log.Warn("failed to parse agent config", "error", err, "agent_id", agent.ID)
		}
	}

	// Get reference price from metadata
	referencePrice := getReferencePriceFromMetadata(negotiation)

	contextText := fmt.Sprintf(`You are a bargaining agent.
Role: %s
- BUYER: Your goal is to MINIMIZE the price. You start with initial_offer. Counter offers should be LOWER than current.
- SELLER: Your goal is to MAXIMIZE the price. You start with reference_price (your listed price). Counter offers should be HIGHER than current, approaching reference_price.
Options: counteroffer, accept, reject
Constraints:
  - buyer: proposes <= current_amount (minimum 30%% of reference_price)
  - seller: proposes >= current_amount (maximum 150%% of reference_price)
Output JSON: {"action":"counteroffer|accept|reject","proposed_amount":<number>,"reason":"<short>","confidence":0.0-1.0}

State: round %d/%d
- reference_price: %.2f (seller's listed price)
- initial_offer: %.2f (buyer's first offer)
- current_amount: %.2f (latest negotiated amount)
- buyer_volatility: %.2f
- seller_volatility: %.2f
- previous_rounds: %d
`, agentType, negotiation.Rounds+1, negotiation.MaxRounds, referencePrice, negotiation.InitialAmount, negotiation.CurrentAmount, negotiation.BuyerVolatility, negotiation.SellerVolatility, len(rounds))

	if len(agentConfig) > 0 {
		if volatility, ok := agentConfig["volatility"].(float64); ok {
			contextText += fmt.Sprintf("\n- Agent Volatility: %.2f", volatility)
		}
		if strategies, ok := agentConfig["preferred_strategies"].([]interface{}); ok {
			contextText += "\n- Preferred Strategies: "
			for i, s := range strategies {
				if i > 0 {
					contextText += ", "
				}
				contextText += fmt.Sprintf("%v", s)
			}
		}
	}

	messages := []ChatMessage{
		{Role: "system", Content: contextText},
		{Role: "user", Content: fmt.Sprintf("What should I do next in this negotiation? Current state: round %d/%d, amount %.2f/%.2f", negotiation.Rounds, negotiation.MaxRounds, negotiation.CurrentAmount, negotiation.InitialAmount)},
	}

	response, err := s.llm.Chat(ctx, messages)
	if err != nil {
		s.log.Error("LLM bargaining decision failed", "error", err)
		return nil, fmt.Errorf("failed to get LLM decision: %w", err)
	}

	var result LLMBargainingResponse
	// Strip markdown code fences if LLM wraps JSON in ```json ... ```
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		s.log.Error("failed to parse LLM bargaining response", "error", err, "response", response)
		result.Action = "counteroffer"
		if agentType == "buyer" {
			result.ProposedAmount = s.calculateFallbackCounterOffer(negotiation, agentType)
		} else {
			// Seller fallback: INCREASE price toward reference_price
			referencePrice := getReferencePriceFromMetadata(negotiation)
			// Calculate how close we are to reference and move closer
			volatility := negotiation.SellerVolatility
			if volatility == 0 {
				volatility = 0.5
			}
			markupFactor := volatility * 0.15
			// Move current_amount UP toward reference_price
			distance := referencePrice - negotiation.CurrentAmount
			increase := distance * markupFactor
			result.ProposedAmount = negotiation.CurrentAmount + increase
			if result.ProposedAmount > referencePrice {
				result.ProposedAmount = referencePrice
			}
		}
		result.Reason = "Using fallback calculation (LLM parsing failed)"
		result.Confidence = 0.5
	} else {
		// Validate parsed amount - if invalid, use fallback
		if !s.isValidCounterOffer(negotiation, agentType, result.ProposedAmount) {
			s.log.Warn("LLM proposed invalid amount, using fallback", "agent_type", agentType, "proposed_amount", result.ProposedAmount)
			if agentType == "buyer" {
				result.ProposedAmount = s.calculateFallbackCounterOffer(negotiation, agentType)
			} else {
				// Seller fallback: INCREASE price toward reference_price
				referencePrice := getReferencePriceFromMetadata(negotiation)
				volatility := negotiation.SellerVolatility
				if volatility == 0 {
					volatility = 0.5
				}
				markupFactor := volatility * 0.15
				distance := referencePrice - negotiation.CurrentAmount
				increase := distance * markupFactor
				result.ProposedAmount = negotiation.CurrentAmount + increase
				if result.ProposedAmount > referencePrice {
					result.ProposedAmount = referencePrice
				}
			}
			result.Reason = "Using fallback (LLM proposed invalid amount)"
			result.Confidence = 0.5
		} else if agentType == "seller" {
			// For seller, ALWAYS use fallback calculation to ensure price moves UP toward reference_price
			// LLM tends to give wrong values for seller (moving price down instead of up)
			referencePrice := getReferencePriceFromMetadata(negotiation)
			volatility := negotiation.SellerVolatility
			if volatility == 0 {
				volatility = 0.5
			}
			markupFactor := volatility * 0.15
			// Move current_amount UP toward reference_price
			distance := referencePrice - negotiation.CurrentAmount
			increase := distance * markupFactor
			result.ProposedAmount = negotiation.CurrentAmount + increase
			if result.ProposedAmount > referencePrice {
				result.ProposedAmount = referencePrice
			}
			result.Reason = "Using seller fallback (LLM response overridden for correct direction)"
			s.log.Info("seller LLM response overridden with fallback", "llm_proposed", result.ProposedAmount, "fallback_proposed", result.ProposedAmount)
		}
	}

	s.log.Info("LLM bargaining decision generated",
		"negotiation_id", negotiationID,
		"agent_id", agentID,
		"agent_type", agentType,
		"action", result.Action,
		"proposed_amount", result.ProposedAmount,
		"confidence", result.Confidence)

	return &result, nil
}

func (s *BargainingService) GetLLMNegotiationSummary(ctx context.Context, negotiationID string) (string, error) {
	if s.llm == nil {
		return "", fmt.Errorf("LLM service not available")
	}

	negotiation, err := s.GetNegotiation(ctx, negotiationID)
	if err != nil {
		return "", fmt.Errorf("failed to get negotiation: %w", err)
	}

	rounds, err := s.ap2Repo.GetBargainingRounds(ctx, negotiationID)
	if err != nil {
		s.log.Warn("failed to get negotiation rounds", "error", err)
		rounds = []*models.BargainingRound{}
	}

	buyerAgent, sellerAgent, err := s.resolveNegotiationAgents(ctx, negotiation)
	if err != nil {
		s.log.Warn("failed to resolve negotiation agents for summary", "error", err, "negotiation_id", negotiationID)
		buyerAgent = nil
		sellerAgent = nil
	}

	roundsData := make([]map[string]interface{}, len(rounds))
	for i, r := range rounds {
		roundsData[i] = map[string]interface{}{
			"round":      r.RoundNumber,
			"agent_type": r.AgentType,
			"action":     r.Action,
			"amount":     r.ProposedAmount,
			"volatility": r.VolatilityFactor,
		}
	}

	roundsJSON, _ := json.Marshal(roundsData)

	systemPrompt := fmt.Sprintf(`# Negotiation Summary Generator

Generate a concise, human-readable summary of a bargaining negotiation session.

## Output Format
Provide a clear summary in 2-3 paragraphs covering:
1. The negotiation journey (initial position, key counteroffers)
2. Final outcome and what led to it
3. Any notable patterns or strategies observed

## Context Information

Negotiation Details:
- ID: %s
- Status: %s
- Buyer Agent: %s (Volatility: %.2f)
- Seller Agent: %s (Volatility: %.2f)
- Initial Amount: %.2f
- Final Amount: %.2f
- Rounds: %d / %d
- Created: %s

Rounds History:
%s
`,
		negotiation.ID,
		negotiation.Status,
		safeAgentName(buyerAgent), negotiation.BuyerVolatility,
		safeAgentName(sellerAgent), negotiation.SellerVolatility,
		negotiation.InitialAmount,
		negotiation.CurrentAmount,
		negotiation.Rounds,
		negotiation.MaxRounds,
		negotiation.CreatedAt.Format("2006-01-02 15:04"),
		string(roundsJSON),
	)

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Please summarize this negotiation session in 2-3 paragraphs, highlighting key strategies and final outcome."},
	}

	summary, err := s.llm.Chat(ctx, messages)
	if err != nil {
		s.log.Error("LLM negotiation summary failed", "error", err)
		return "", fmt.Errorf("failed to get LLM summary: %w", err)
	}

	s.log.Info("LLM negotiation summary generated", "negotiation_id", negotiationID, "summary_length", len(summary))

	return summary, nil
}

func safeAgentName(agent *models.Agent) string {
	if agent == nil {
		return "Unknown"
	}
	return agent.Name
}

// AgentServiceInterface defines the AgentService methods used by BargainingService
type AgentServiceInterface interface {
	GetAgentByID(ctx context.Context, id string) (*models.Agent, error)
}

// MenteeServiceInterface defines the MenteeService methods used by BargainingService
type MenteeServiceInterface interface {
	GetBargainingDecision(ctx context.Context, agentID string, agentType string, currentAmount float64, initialAmount float64, round int, maxRounds int, opponentID string) (*BargainingDecision, error)
	RecordNegotiationOutcome(ctx context.Context, outcome *NegotiationOutcome) error
}

// AP2RepositoryBargainingInterface defines the AP2Repository methods used by BargainingService for bargaining
type AP2RepositoryBargainingInterface interface {
	CreateBargainingNegotiation(ctx context.Context, negotiation *models.BargainingNegotiation) error
	GetBargainingNegotiationByID(ctx context.Context, id string) (*models.BargainingNegotiation, error)
	GetNegotiationsByUser(ctx context.Context, userID string, page, limit int) ([]*models.BargainingNegotiation, int64, error)
	UpdateNegotiationStatus(ctx context.Context, id, status string) error
	UpdateNegotiationAmountAndRounds(ctx context.Context, id string, amount float64, rounds int, status string) error
	CompleteNegotiation(ctx context.Context, id, status string, finalAmount float64, completedAt *time.Time) error
	CreateBargainingRound(ctx context.Context, round *models.BargainingRound) error
	GetBargainingRounds(ctx context.Context, negotiationID string) ([]*models.BargainingRound, error)
}

// BargainingServiceTestable is a test-friendly version of BargainingService
type BargainingServiceTestable struct {
	ap2Repo      AP2RepositoryBargainingInterface
	a2aClient    *a2a.A2AClient
	agentService AgentServiceInterface
	mentee       MenteeServiceInterface
	llm          *LLMService
	log          *logger.Logger
}

// NewBargainingServiceForTesting creates a BargainingService with mockable dependencies for testing
func NewBargainingServiceForTesting(
	ap2Repo AP2RepositoryBargainingInterface,
	a2aClient *a2a.A2AClient,
	agentService AgentServiceInterface,
	mentee MenteeServiceInterface,
	llm *LLMService,
	log *logger.Logger,
) *BargainingServiceTestable {
	return &BargainingServiceTestable{
		ap2Repo:      ap2Repo,
		a2aClient:    a2aClient,
		agentService: agentService,
		mentee:       mentee,
		llm:          llm,
		log:          log,
	}
}

// CreateNegotiation creates a new bargaining negotiation
func (s *BargainingServiceTestable) CreateNegotiation(ctx context.Context, req *CreateNegotiationRequest) (*models.BargainingNegotiation, error) {
	buyerAgent, err := s.agentService.GetAgentByID(ctx, req.BuyerAgentID)
	if err != nil {
		return nil, fmt.Errorf("buyer agent not found: %w", err)
	}

	sellerAgent, err := s.agentService.GetAgentByID(ctx, req.SellerAgentID)
	if err != nil {
		return nil, fmt.Errorf("seller agent not found: %w", err)
	}

	if NormalizeMarketplaceAgentType(buyerAgent.Type) != "shopping" {
		return nil, fmt.Errorf("buyer agent must be a shopping agent")
	}
	if NormalizeMarketplaceAgentType(sellerAgent.Type) != "merchant" {
		return nil, fmt.Errorf("seller agent must be a merchant agent")
	}

	buyerVolatility := getAgentVolatilityInternal(buyerAgent)
	sellerVolatility := getAgentVolatilityInternal(sellerAgent)

	maxRounds := req.MaxRounds
	if maxRounds == 0 {
		maxRounds = 5
	}

	// Use ReferencePrice if provided, otherwise fall back to seller's listed price from agent config
	referencePrice := req.ReferencePrice
	if referencePrice == 0 && sellerAgent.Price != nil && *sellerAgent.Price > 0 {
		referencePrice = *sellerAgent.Price
	}
	if referencePrice == 0 {
		referencePrice = req.InitialAmount
	}

	// Store reference_price in metadata
	metadata := req.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["reference_price"] = referencePrice

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
		Metadata:           s.marshalMetadata(metadata),
		SessionID:          req.SessionID,
	}

	if err := s.ap2Repo.CreateBargainingNegotiation(ctx, negotiation); err != nil {
		return nil, fmt.Errorf("failed to create negotiation: %w", err)
	}

	return negotiation, nil
}

// GetNegotiation retrieves a negotiation by ID
func (s *BargainingServiceTestable) GetNegotiation(ctx context.Context, negotiationID string) (*models.BargainingNegotiation, error) {
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

// GetNegotiationsByUser retrieves negotiations for a user
func (s *BargainingServiceTestable) GetNegotiationsByUser(ctx context.Context, userID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	return s.ap2Repo.GetNegotiationsByUser(ctx, userID, page, limit)
}

// GetNegotiationRounds retrieves rounds for a negotiation
func (s *BargainingServiceTestable) GetNegotiationRounds(ctx context.Context, negotiationID string) ([]*models.BargainingRound, error) {
	return s.ap2Repo.GetBargainingRounds(ctx, negotiationID)
}

// SubmitCounterOffer submits a counteroffer
func (s *BargainingServiceTestable) SubmitCounterOffer(ctx context.Context, negotiationID string, req *CounterOfferRequest) (*models.BargainingRound, *models.BargainingNegotiation, error) {
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

	buyerAgent := negotiation.BuyerAgent
	if buyerAgent == nil || buyerAgent.ID == "" {
		buyerAgent, err = s.agentService.GetAgentByID(ctx, negotiation.BuyerAgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("buyer agent not found: %w", err)
		}
	}

	sellerAgent := negotiation.SellerAgent
	if sellerAgent == nil || sellerAgent.ID == "" {
		sellerAgent, err = s.agentService.GetAgentByID(ctx, negotiation.SellerAgentID)
		if err != nil {
			return nil, nil, fmt.Errorf("seller agent not found: %w", err)
		}
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

		return round, negotiation, nil
	}

	if req.Action == "counteroffer" {
		if !isValidCounterOfferInternal(negotiation, agentType, req.ProposedAmount) {
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
			VolatilityFactor: calculateVolatilityFactorInternal(volatility, negotiation.Rounds, negotiation.InitialAmount, req.ProposedAmount),
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

		return round, negotiation, nil
	}

	return nil, nil, errors.New("invalid action")
}

// IsValidCounterOffer validates a counteroffer amount
func (s *BargainingServiceTestable) IsValidCounterOffer(negotiation *models.BargainingNegotiation, agentType string, proposedAmount float64) bool {
	return isValidCounterOfferInternal(negotiation, agentType, proposedAmount)
}

// CalculateVolatilityFactor calculates volatility factor
func (s *BargainingServiceTestable) CalculateVolatilityFactor(volatility float64, rounds int, initialAmount, currentAmount float64) float64 {
	return calculateVolatilityFactorInternal(volatility, rounds, initialAmount, currentAmount)
}

// CalculateFallbackCounterOffer calculates a fallback counteroffer
func (s *BargainingServiceTestable) CalculateFallbackCounterOffer(negotiation *models.BargainingNegotiation, agentType string) float64 {
	return calculateFallbackCounterOfferInternal(negotiation, agentType)
}

// GetAgentVolatility gets volatility from agent config
func (s *BargainingServiceTestable) GetAgentVolatility(agent *models.Agent) float64 {
	return getAgentVolatilityInternal(agent)
}

// Helper functions (package-private for testing)

func getAgentVolatilityInternal(agent *models.Agent) float64 {
	if agent == nil || agent.Config == "" {
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

// getReferencePriceFromMetadata extracts reference_price from negotiation metadata
func getReferencePriceFromMetadata(negotiation *models.BargainingNegotiation) float64 {
	if negotiation == nil || negotiation.Metadata == "" {
		return negotiation.InitialAmount
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(negotiation.Metadata), &metadata); err != nil {
		return negotiation.InitialAmount
	}

	if refPrice, ok := metadata["reference_price"].(float64); ok {
		return refPrice
	}

	return negotiation.InitialAmount
}

func isValidCounterOfferInternal(negotiation *models.BargainingNegotiation, agentType string, proposedAmount float64) bool {
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

func calculateVolatilityFactorInternal(volatility float64, rounds int, initialAmount, currentAmount float64) float64 {
	changePercent := math.Abs((currentAmount - initialAmount) / initialAmount * 100)
	volatilityEffect := volatility * (changePercent / 100)
	roundDecay := 1.0 / math.Pow(1.2, float64(rounds))

	factor := volatilityEffect * roundDecay

	return math.Round(factor*10000) / 10000
}

func calculateFallbackCounterOfferInternal(negotiation *models.BargainingNegotiation, agentType string) float64 {
	var volatility float64
	var currentAmount float64

	if agentType == "buyer" {
		volatility = negotiation.BuyerVolatility
		currentAmount = negotiation.CurrentAmount
	} else {
		volatility = negotiation.SellerVolatility
		currentAmount = negotiation.CurrentAmount
	}

	referencePrice := getReferencePriceFromMetadata(negotiation)

	maxDiscount := volatility * 0.15
	minDiscount := volatility * 0.02

	discountFactor := minDiscount + (maxDiscount-minDiscount)*0.5

	var suggestedAmount float64
	if agentType == "buyer" {
		suggestedAmount = currentAmount * (1 - discountFactor)
	} else {
		// Seller: INCREASE price toward reference_price
		distance := referencePrice - currentAmount
		increase := distance * discountFactor
		suggestedAmount = currentAmount + increase
		if suggestedAmount > referencePrice {
			suggestedAmount = referencePrice
		}
	}

	return math.Round(suggestedAmount*100) / 100
}

func (s *BargainingServiceTestable) marshalMetadata(metadata map[string]interface{}) string {
	data, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(data)
}
