package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"
)

const (
	webhookTimeout    = 5 * time.Second
	webhookMaxRetries = 2
	interRoundDelay   = 2 * time.Second
)

type A2ABargainingService struct {
	a2aClient    *a2a.A2AClient
	bargaining   *BargainingService
	mentee       *MenteeService
	log          *logger.Logger
	sessions     map[string]*A2ASession
	sessionsLock sync.RWMutex
	sessionLocks map[string]*sync.Mutex
}

type A2ASession struct {
	NegotiationID    string
	DBNegotiationID  string
	BuyerAgentID     string
	SellerAgentID    string
	UserID           string
	InitialAmount    float64
	CurrentAmount    float64
	Round            int
	MaxRounds        int
	Status           string
	StartTime        time.Time
	CallbackURL      string
	RunStarted       bool
	NegotiationReady chan struct{}
}

type A2ASessionProgress struct {
	NegotiationID   string  `json:"session_id"`
	NegotiationUUID string  `json:"negotiation_id"`
	BuyerAgentID    string  `json:"buyer_agent_id"`
	SellerAgentID   string  `json:"seller_agent_id"`
	UserID          string  `json:"user_id,omitempty"`
	InitialAmount   float64 `json:"initial_amount"`
	CurrentAmount   float64 `json:"current_amount"`
	Round           int     `json:"round"`
	MaxRounds       int     `json:"max_rounds"`
	Status          string  `json:"status"`
}

func (s *A2ASession) ToProgressResponse() A2ASessionProgress {
	return A2ASessionProgress{
		NegotiationID:   s.NegotiationID,
		NegotiationUUID: s.DBNegotiationID,
		BuyerAgentID:    s.BuyerAgentID,
		SellerAgentID:   s.SellerAgentID,
		UserID:          s.UserID,
		InitialAmount:   s.InitialAmount,
		CurrentAmount:   s.CurrentAmount,
		Round:           s.Round,
		MaxRounds:       s.MaxRounds,
		Status:          s.Status,
	}
}

type AutonomousNegotiationRequest struct {
	BuyerAgentID  string  `json:"buyer_agent_id" binding:"required,uuid"`
	SellerAgentID string  `json:"seller_agent_id" binding:"required,uuid"`
	InitialAmount float64 `json:"initial_amount" binding:"required,gt=0"`
	MaxRounds     int     `json:"max_rounds" binding:"omitempty,gte=1,lte=20"`
	CallbackURL   string  `json:"callback_url"`
	UserID        string  `json:"user_id"`
}

type WebhookPayload struct {
	Event          string  `json:"event"`
	SessionID      string  `json:"session_id"`
	Round          int     `json:"round,omitempty"`
	AgentID        string  `json:"agent_id,omitempty"`
	AgentType      string  `json:"agent_type,omitempty"`
	Action         string  `json:"action,omitempty"`
	ProposedAmount float64 `json:"proposed_amount,omitempty"`
	CurrentAmount  float64 `json:"current_amount,omitempty"`
	Status         string  `json:"status,omitempty"`
	FinalAmount    float64 `json:"final_amount,omitempty"`
	Rounds         int     `json:"rounds,omitempty"`
	BuyerAgentID   string  `json:"buyer_agent_id,omitempty"`
	SellerAgentID  string  `json:"seller_agent_id,omitempty"`
}

func NewA2ABargainingService(a2aClient *a2a.A2AClient, bargaining *BargainingService, mentee *MenteeService, log *logger.Logger) *A2ABargainingService {
	return &A2ABargainingService{
		a2aClient:    a2aClient,
		bargaining:   bargaining,
		mentee:       mentee,
		log:          log,
		sessions:     make(map[string]*A2ASession),
		sessionLocks: make(map[string]*sync.Mutex),
	}
}

func (s *A2ABargainingService) StartNegotiation(ctx context.Context, buyerAgentID, sellerAgentID string, initialAmount float64) (*A2ASession, error) {
	negotiationID := generateA2ANegotiationID()

	session := &A2ASession{
		NegotiationID: negotiationID,
		BuyerAgentID:  buyerAgentID,
		SellerAgentID: sellerAgentID,
		InitialAmount: initialAmount,
		CurrentAmount: initialAmount,
		Round:         0,
		MaxRounds:     5,
		Status:        "running",
		StartTime:     time.Now(),
	}

	s.sessionsLock.Lock()
	s.sessions[negotiationID] = session
	s.sessionLocks[negotiationID] = &sync.Mutex{}
	s.sessionsLock.Unlock()

	s.log.Info("A2A negotiation session started", "negotiation_id", negotiationID, "buyer_id", buyerAgentID, "seller_id", sellerAgentID, "initial_amount", initialAmount)

	return session, nil
}

func (s *A2ABargainingService) StartAutonomousNegotiation(ctx context.Context, req *AutonomousNegotiationRequest) (*A2ASession, error) {
	negotiationID := generateA2ANegotiationID()

	maxRounds := req.MaxRounds
	if maxRounds == 0 {
		maxRounds = 5
	}

	session := &A2ASession{
		NegotiationID:    negotiationID,
		BuyerAgentID:     req.BuyerAgentID,
		SellerAgentID:    req.SellerAgentID,
		UserID:           req.UserID,
		InitialAmount:    req.InitialAmount,
		CurrentAmount:    req.InitialAmount,
		Round:            0,
		MaxRounds:        maxRounds,
		Status:           "running",
		StartTime:        time.Now(),
		CallbackURL:      req.CallbackURL,
		RunStarted:       false,
		NegotiationReady: make(chan struct{}),
	}

	s.sessionsLock.Lock()
	s.sessions[negotiationID] = session
	s.sessionLocks[negotiationID] = &sync.Mutex{}
	s.sessionsLock.Unlock()

	s.log.Info("A2A autonomous negotiation session created",
		"session_id", negotiationID,
		"buyer_id", req.BuyerAgentID,
		"seller_id", req.SellerAgentID,
		"initial_amount", req.InitialAmount,
		"max_rounds", maxRounds,
		"callback_url", req.CallbackURL)

	return session, nil
}

func (s *A2ABargainingService) RunAutonomousNegotiation(ctx context.Context, sessionID string) (err error) {
	var session *A2ASession
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in RunAutonomousNegotiation: %v", r)
			s.log.Error("panic in autonomous negotiation goroutine", "error", r, "session_id", sessionID)
		}
		if err != nil && session != nil {
			session.Status = "failed"
		}
	}()

	s.sessionsLock.RLock()
	session, exists := s.sessions[sessionID]
	s.sessionsLock.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	lock, ok := s.sessionLocks[sessionID]
	if !ok {
		return fmt.Errorf("session lock not found: %s", sessionID)
	}
	lock.Lock()
	if session.RunStarted {
		lock.Unlock()
		return fmt.Errorf("negotiation already started for session: %s", sessionID)
	}
	session.RunStarted = true
	lock.Unlock()

	s.log.Info("starting autonomous negotiation loop", "session_id", sessionID)

	negReq := &CreateNegotiationRequest{
		BuyerAgentID:  session.BuyerAgentID,
		SellerAgentID: session.SellerAgentID,
		UserID:        session.UserID,
		InitialAmount: session.InitialAmount,
		MaxRounds:     session.MaxRounds,
		SessionID:     &sessionID,
	}

	negotiation, err := s.bargaining.CreateNegotiation(ctx, negReq)
	if err != nil {
		s.log.Error("failed to create negotiation", "error", err, "session_id", sessionID)
		close(session.NegotiationReady)
		s.sendWebhook(session.CallbackURL, WebhookPayload{
			Event:         "negotiation_error",
			SessionID:     sessionID,
			Status:        "failed",
			BuyerAgentID:  session.BuyerAgentID,
			SellerAgentID: session.SellerAgentID,
		})
		return err
	}

	session.DBNegotiationID = negotiation.ID
	close(session.NegotiationReady)

	activeAgentID := session.BuyerAgentID
	activeAgentType := "buyer"

	for round := 1; round <= session.MaxRounds; round++ {
		select {
		case <-ctx.Done():
			s.log.Info("autonomous negotiation cancelled", "session_id", sessionID, "round", round)
			return ctx.Err()
		default:
		}

		if round > 1 {
			time.Sleep(interRoundDelay)
		}

		decision, err := s.bargaining.GetLLMBargainingDecision(ctx, activeAgentID, activeAgentType, negotiation.ID)
		if err != nil {
			s.log.Error("LLM decision failed", "error", err, "session_id", sessionID, "round", round, "agent_id", activeAgentID)
			s.sendWebhook(session.CallbackURL, WebhookPayload{
				Event:     "negotiation_error",
				SessionID: sessionID,
				Round:     round,
				AgentID:   activeAgentID,
				AgentType: activeAgentType,
				Status:    "llm_failed",
			})
			return fmt.Errorf("LLM decision failed: %w", err)
		}

		counterReq := &CounterOfferRequest{
			AgentID:        activeAgentID,
			ProposedAmount: decision.ProposedAmount,
			Reason:         &decision.Reason,
			Action:         decision.Action,
		}

		_, updatedNegotiation, err := s.bargaining.SubmitCounterOffer(ctx, negotiation.ID, counterReq)
		if err != nil {
			s.log.Error("submit counter offer failed", "error", err, "session_id", sessionID, "round", round)
			s.sendWebhook(session.CallbackURL, WebhookPayload{
				Event:     "negotiation_error",
				SessionID: sessionID,
				Round:     round,
				AgentID:   activeAgentID,
				AgentType: activeAgentType,
				Status:    "submit_failed",
			})
			return fmt.Errorf("submit counter offer failed: %w", err)
		}

		session.CurrentAmount = updatedNegotiation.CurrentAmount
		session.Round = round

		s.sendWebhook(session.CallbackURL, WebhookPayload{
			Event:          "round_completed",
			SessionID:      sessionID,
			Round:          round,
			AgentID:        activeAgentID,
			AgentType:      activeAgentType,
			Action:         decision.Action,
			ProposedAmount: decision.ProposedAmount,
			CurrentAmount:  updatedNegotiation.CurrentAmount,
			Status:         updatedNegotiation.Status,
		})

		if decision.Action == "accept" || decision.Action == "reject" || updatedNegotiation.Status == "accepted" || updatedNegotiation.Status == "rejected" {
			session.Status = updatedNegotiation.Status
			if session.Status == "" {
				session.Status = decision.Action
			}

			s.recordLearning(updatedNegotiation, activeAgentID, activeAgentType, decision.Action, round)

			s.sendWebhook(session.CallbackURL, WebhookPayload{
				Event:         "negotiation_completed",
				SessionID:     sessionID,
				Status:        session.Status,
				FinalAmount:   updatedNegotiation.CurrentAmount,
				Rounds:        round,
				BuyerAgentID:  session.BuyerAgentID,
				SellerAgentID: session.SellerAgentID,
			})

			s.log.Info("autonomous negotiation completed",
				"session_id", sessionID,
				"status", session.Status,
				"final_amount", updatedNegotiation.CurrentAmount,
				"total_rounds", round)

			return nil
		}

		if activeAgentID == session.BuyerAgentID {
			activeAgentID = session.SellerAgentID
			activeAgentType = "seller"
		} else {
			activeAgentID = session.BuyerAgentID
			activeAgentType = "buyer"
		}
	}

	session.Status = "expired"
	s.sendWebhook(session.CallbackURL, WebhookPayload{
		Event:         "negotiation_completed",
		SessionID:     sessionID,
		Status:        "expired",
		FinalAmount:   session.CurrentAmount,
		Rounds:        session.MaxRounds,
		BuyerAgentID:  session.BuyerAgentID,
		SellerAgentID: session.SellerAgentID,
	})

	s.log.Info("autonomous negotiation expired (max rounds)", "session_id", sessionID, "max_rounds", session.MaxRounds)
	return nil
}

func (s *A2ABargainingService) recordLearning(negotiation *models.BargainingNegotiation, lastAgentID, lastAgentType, action string, rounds int) {
	if s.mentee == nil {
		return
	}

	status := negotiation.Status
	if status == "" {
		if action == "accept" {
			status = "accepted"
		} else if action == "reject" {
			status = "rejected"
		}
	}

	outcome := NegotiationOutcome{
		NegotiationID: negotiation.ID,
		InitialAmount: negotiation.InitialAmount,
		FinalAmount:   negotiation.CurrentAmount,
		Status:        status,
		Rounds:        rounds,
		OpponentID: func() string {
			if lastAgentType == "buyer" {
				return negotiation.SellerAgentID
			}
			return negotiation.BuyerAgentID
		}(),
		OpponentType: func() string {
			if lastAgentType == "buyer" {
				return "seller"
			}
			return "buyer"
		}(),
		Timestamp: time.Now(),
		Strategy:  action,
	}

	s.mentee.RecordNegotiationOutcome(context.Background(), &outcome)

	s.log.Info("learning recorded",
		"negotiation_id", negotiation.ID,
		"final_amount", negotiation.CurrentAmount,
		"status", status,
		"rounds", rounds)
}

func (s *A2ABargainingService) sendWebhook(callbackURL string, payload WebhookPayload) {
	if callbackURL == "" {
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		s.log.Error("failed to marshal webhook payload", "error", err)
		return
	}

	var lastErr error
	for attempt := 0; attempt <= webhookMaxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, callbackURL, bytes.NewReader(body))
		if err != nil {
			s.log.Error("failed to create webhook request", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: webhookTimeout}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				s.log.Debug("webhook delivered", "url", callbackURL, "event", payload.Event, "status", resp.StatusCode)
				return
			}
			lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		} else {
			lastErr = err
		}

		if attempt < webhookMaxRetries {
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		}
	}

	s.log.Warn("webhook delivery failed after retries", "url", callbackURL, "event", payload.Event, "error", lastErr)
}

func (s *A2ABargainingService) GetSessionProgress(sessionID string) *A2ASessionProgress {
	s.sessionsLock.RLock()
	session, exists := s.sessions[sessionID]
	s.sessionsLock.RUnlock()

	if !exists {
		return nil
	}

	progress := session.ToProgressResponse()
	return &progress
}

func (s *A2ABargainingService) StopNegotiation(sessionID string) {
	s.sessionsLock.Lock()
	if session, exists := s.sessions[sessionID]; exists {
		session.Status = "stopped"
		s.log.Info("A2A negotiation session stopped", "session_id", sessionID)
	}
	s.sessionsLock.Unlock()
}

func (s *A2ABargainingService) GetSessionProgressByNegotiationID(ctx context.Context, negotiationID string) *A2ASessionProgress {
	// Try to find in-memory session by DB negotiation ID
	s.sessionsLock.RLock()
	for _, session := range s.sessions {
		if session.DBNegotiationID == negotiationID {
			s.sessionsLock.RUnlock()
			progress := session.ToProgressResponse()
			return &progress
		}
	}
	s.sessionsLock.RUnlock()

	// Fall back to DB lookup
	neg, err := s.bargaining.GetNegotiation(ctx, negotiationID)
	if err != nil || neg == nil {
		return nil
	}

	// Return DB-backed session state
	return &A2ASessionProgress{
		NegotiationID:   "",
		NegotiationUUID: neg.ID,
		BuyerAgentID:    neg.BuyerAgentID,
		SellerAgentID:   neg.SellerAgentID,
		UserID:          neg.UserID,
		InitialAmount:   neg.InitialAmount,
		CurrentAmount:   neg.CurrentAmount,
		Round:           neg.Rounds,
		MaxRounds:       neg.MaxRounds,
		Status:          neg.Status,
	}
}

func generateA2ANegotiationID() string {
	return "a2a_" + time.Now().Format("20060102150405") + "_" + randomString(8)
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
