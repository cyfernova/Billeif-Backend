package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"
)

const (
	webhookTimeout    = 5 * time.Second
	webhookMaxRetries = 2
	interRoundDelay   = 2 * time.Second
)

type A2ABargainingService struct {
	a2aClient     *a2a.A2AClient
	bargaining    *BargainingService
	mentee        *MenteeService
	ap2Repo       interfaces.AP2Repository
	sqs           *sqs.Client
	cfg           *config.Config
	log           *logger.Logger
	webhookClient *http.Client
	sessions      map[string]*A2ASession
	sessionsLock  sync.RWMutex
	sessionLocks  map[string]*sync.Mutex
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
	ProcessingRound  int32 // atomic: 0 = not processing, >0 = round being processed
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
	BuyerAgentID   string  `json:"buyer_agent_id" binding:"required,uuid"`
	SellerAgentID  string  `json:"seller_agent_id" binding:"required,uuid"`
	InitialAmount  float64 `json:"initial_amount" binding:"required,gt=0"`
	ReferencePrice float64 `json:"reference_price"`
	MaxRounds      int     `json:"max_rounds" binding:"omitempty,gte=1,lte=20"`
	CallbackURL    string  `json:"callback_url"`
	UserID         string  `json:"user_id"`
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

func NewA2ABargainingService(a2aClient *a2a.A2AClient, bargaining *BargainingService, mentee *MenteeService, ap2Repo interfaces.AP2Repository, sqsClient *sqs.Client, cfg *config.Config, log *logger.Logger) *A2ABargainingService {
	return &A2ABargainingService{
		a2aClient:     a2aClient,
		bargaining:    bargaining,
		mentee:        mentee,
		ap2Repo:       ap2Repo,
		sqs:           sqsClient,
		cfg:           cfg,
		log:           log,
		webhookClient: newWebhookDeliveryHTTPClient(webhookTimeout),
		sessions:      make(map[string]*A2ASession),
		sessionLocks:  make(map[string]*sync.Mutex),
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
		Round:         0, //
		MaxRounds:     5,
		Status:        "running",
		StartTime:     time.Now(),
	}

	s.sessionsLock.Lock()
	s.sessions[negotiationID] = session
	s.sessionLocks[negotiationID] = &sync.Mutex{}
	s.sessionsLock.Unlock()

	// Persist negotiation to DB so rounds survive cold starts
	negReq := &CreateNegotiationRequest{
		BuyerAgentID:  buyerAgentID,
		SellerAgentID: sellerAgentID,
		InitialAmount: initialAmount,
		MaxRounds:     5,
		SessionID:     &negotiationID,
	}
	negotiation, err := s.bargaining.CreateNegotiation(ctx, negReq)
	if err != nil {
		s.log.Error("failed to persist negotiation", "error", err, "negotiation_id", negotiationID)
	} else {
		session.DBNegotiationID = negotiation.ID
	}

	s.log.Info("A2A negotiation session started", "negotiation_id", negotiationID, "buyer_id", buyerAgentID, "seller_id", sellerAgentID, "initial_amount", initialAmount)

	return session, nil
}

func (s *A2ABargainingService) StartAutonomousNegotiation(ctx context.Context, req *AutonomousNegotiationRequest) (*A2ASession, error) {
	if req != nil && req.CallbackURL != "" {
		if err := validateWebhookURL(ctx, req.CallbackURL); err != nil {
			return nil, fmt.Errorf("invalid callback URL: %w", err)
		}
	}

	negotiationID := generateA2ANegotiationID()

	maxRounds := req.MaxRounds
	if maxRounds == 0 {
		maxRounds = 5
	}

	// Create the database negotiation first so DBNegotiationID is available before enqueuing
	negReq := &CreateNegotiationRequest{
		BuyerAgentID:  req.BuyerAgentID,
		SellerAgentID: req.SellerAgentID,
		UserID:        req.UserID,
		InitialAmount: req.InitialAmount,
		MaxRounds:     maxRounds,
		SessionID:     &negotiationID,
	}

	negotiation, err := s.bargaining.CreateNegotiation(ctx, negReq)
	if err != nil {
		s.log.Error("failed to create negotiation in database", "error", err, "session_id", negotiationID)
		return nil, fmt.Errorf("failed to create negotiation: %w", err)
	}

	session := &A2ASession{
		NegotiationID:    negotiationID,
		DBNegotiationID:  negotiation.ID,
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
		NegotiationReady: make(chan struct{}),
	}

	s.sessionsLock.Lock()
	s.sessions[negotiationID] = session
	s.sessionLocks[negotiationID] = &sync.Mutex{}
	s.sessionsLock.Unlock()

	s.log.Info("A2A autonomous negotiation session created",
		"session_id", negotiationID,
		"db_negotiation_id", negotiation.ID,
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
	if atomic.LoadInt32(&session.ProcessingRound) != 0 {
		lock.Unlock()
		return fmt.Errorf("negotiation already started for session: %s", sessionID)
	}
	atomic.StoreInt32(&session.ProcessingRound, -1) // -1 indicates full negotiation loop running
	lock.Unlock()
	defer atomic.StoreInt32(&session.ProcessingRound, 0) // reset when done

	s.log.Info("starting autonomous negotiation loop", "session_id", sessionID)

	var negotiation *models.BargainingNegotiation

	// If DBNegotiationID is already set, reuse the existing negotiation
	if session.DBNegotiationID != "" {
		negotiation, err = s.bargaining.GetNegotiation(ctx, session.DBNegotiationID)
		if err != nil {
			s.log.Error("failed to get existing negotiation", "error", err, "session_id", sessionID, "db_negotiation_id", session.DBNegotiationID)
			s.sendWebhook(session.CallbackURL, WebhookPayload{
				Event:         "negotiation_error",
				SessionID:     sessionID,
				Status:        "failed",
				BuyerAgentID:  session.BuyerAgentID,
				SellerAgentID: session.SellerAgentID,
			})
			return fmt.Errorf("failed to get existing negotiation: %w", err)
		}
		s.log.Info("reusing existing negotiation", "session_id", sessionID, "db_negotiation_id", session.DBNegotiationID)
	} else {
		// DBNegotiationID not set yet, create a new negotiation
		negReq := &CreateNegotiationRequest{
			BuyerAgentID:  session.BuyerAgentID,
			SellerAgentID: session.SellerAgentID,
			UserID:        session.UserID,
			InitialAmount: session.InitialAmount,
			MaxRounds:     session.MaxRounds,
			SessionID:     &sessionID,
		}

		negotiation, err = s.bargaining.CreateNegotiation(ctx, negReq)
		if err != nil {
			s.log.Error("failed to create negotiation", "error", err, "session_id", sessionID)
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
		s.log.Info("created new negotiation", "session_id", sessionID, "db_negotiation_id", negotiation.ID)
	}

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

func (s *A2ABargainingService) RunAutonomousNegotiationRound(ctx context.Context, sessionID string) error {
	var session *A2ASession
	s.sessionsLock.RLock()
	session, exists := s.sessions[sessionID]
	s.sessionsLock.RUnlock()

	if !exists {
		// Cold start: session not in memory. Try to reload from DB.
		var err error
		session, err = s.reloadSessionFromDB(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("session not found: %s", sessionID)
		}
	}

	if session.DBNegotiationID == "" {
		return fmt.Errorf("no database negotiation ID for session: %s", sessionID)
	}

	// Determine which round to process by checking DB rounds
	// This must be done before claiming to know which round to atomically claim
	rounds, err := s.ap2Repo.GetBargainingRounds(ctx, session.DBNegotiationID)
	if err != nil {
		s.log.Warn("failed to get negotiation rounds", "error", err)
		rounds = []*models.BargainingRound{}
	}

	// Calculate the round to process: if DB has N rounds completed, next is N+1
	// But we need to account for session.Round which might be ahead or behind
	// Use DB state as source of truth for which rounds exist
	dbRoundCount := len(rounds)
	nextRound := dbRoundCount + 1

	if nextRound > session.MaxRounds {
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
		return nil
	}

	// Atomically claim this specific round to prevent concurrent processing
	// If another Lambda already claimed it, CAS returns false
	if !atomic.CompareAndSwapInt32(&session.ProcessingRound, 0, int32(nextRound)) {
		s.log.Info("round already being processed by another Lambda", "session_id", sessionID, "round", nextRound)
		return fmt.Errorf("round %d already being processed", nextRound)
	}
	defer atomic.StoreInt32(&session.ProcessingRound, 0) // release when done

	s.log.Info("running autonomous negotiation round", "session_id", sessionID, "round", nextRound)

	// Sync session state from DB before processing
	// This ensures we use the authoritative DB round count, not stale in-memory state
	latestNeg, err := s.bargaining.GetNegotiation(ctx, session.DBNegotiationID)
	if err != nil {
		return fmt.Errorf("failed to get negotiation: %w", err)
	}
	// Update in-memory session with authoritative DB state
	session.Round = latestNeg.Rounds
	session.Status = latestNeg.Status
	session.CurrentAmount = latestNeg.CurrentAmount

	// Re-calculate nextRound from synced DB state to stay in sync
	// If DB rounds advanced (e.g. concurrent submission), use that
	syncedNextRound := latestNeg.Rounds + 1
	if syncedNextRound != nextRound {
		s.log.Info("round count updated from DB sync", "session_id", sessionID, "old_next_round", nextRound, "new_next_round", syncedNextRound, "db_rounds", latestNeg.Rounds)
		nextRound = syncedNextRound
	}

	// Determine which agent should act based on who went last
	// If no rounds yet, buyer starts. If last round was by buyer, seller goes next.
	var activeAgentID string
	var activeAgentType string
	if latestNeg.Rounds == 0 {
		activeAgentID = session.BuyerAgentID
		activeAgentType = "buyer"
	} else {
		lastRound := rounds[len(rounds)-1]
		if lastRound.AgentID == session.BuyerAgentID {
			activeAgentID = session.SellerAgentID
			activeAgentType = "seller"
		} else {
			activeAgentID = session.BuyerAgentID
			activeAgentType = "buyer"
		}
	}

	if nextRound > session.MaxRounds {
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
		return nil
	}

	decision, err := s.bargaining.GetLLMBargainingDecision(ctx, activeAgentID, activeAgentType, latestNeg.ID)
	if err != nil {
		s.log.Error("LLM decision failed", "error", err, "session_id", sessionID, "round", nextRound, "agent_id", activeAgentID)
		return fmt.Errorf("LLM decision failed: %w", err)
	}

	counterReq := &CounterOfferRequest{
		AgentID:        activeAgentID,
		ProposedAmount: decision.ProposedAmount,
		Reason:         &decision.Reason,
		Action:         decision.Action,
	}

	_, updatedNegotiation, err := s.bargaining.SubmitCounterOffer(ctx, latestNeg.ID, counterReq)
	if err != nil {
		s.log.Error("submit counter offer failed", "error", err, "session_id", sessionID, "round", nextRound)
		return fmt.Errorf("submit counter offer failed: %w", err)
	}

	session.CurrentAmount = updatedNegotiation.CurrentAmount
	session.Round = updatedNegotiation.Rounds

	s.sendWebhook(session.CallbackURL, WebhookPayload{
		Event:          "round_completed",
		SessionID:      sessionID,
		Round:          session.Round,
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

		s.recordLearning(updatedNegotiation, activeAgentID, activeAgentType, decision.Action, session.Round)

		s.sendWebhook(session.CallbackURL, WebhookPayload{
			Event:         "negotiation_completed",
			SessionID:     sessionID,
			Status:        session.Status,
			FinalAmount:   updatedNegotiation.CurrentAmount,
			Rounds:        session.Round,
			BuyerAgentID:  session.BuyerAgentID,
			SellerAgentID: session.SellerAgentID,
		})

		s.log.Info("autonomous negotiation completed",
			"session_id", sessionID,
			"status", session.Status,
			"final_amount", updatedNegotiation.CurrentAmount,
			"total_rounds", session.Round)

		return nil
	}

	s.log.Info("autonomous negotiation round completed, session may continue",
		"session_id", sessionID,
		"round", session.Round,
		"max_rounds", session.MaxRounds,
		"current_amount", session.CurrentAmount)

	// Enqueue next round if not complete
	if session.Round < session.MaxRounds && session.Status == "running" {
		if err := s.EnqueueNegotiationRound(sessionID, session.DBNegotiationID, session.Round); err != nil {
			s.log.Warn("failed to self-enqueue next round", "error", err, "session_id", sessionID, "round", session.Round)
		} else {
			s.log.Info("enqueued next round from service", "session_id", sessionID, "next_round", session.Round+1)
		}
	}

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

func (s *A2ABargainingService) EnqueueNegotiationRound(sessionID, negotiationID string, currentRound int) error {
	if s.sqs == nil || s.cfg == nil {
		err := fmt.Errorf("SQS not configured: sqs=%v, cfg=%v", s.sqs == nil, s.cfg == nil)
		s.log.Error("cannot enqueue negotiation round", "error", err, "session_id", sessionID)
		return err
	}

	if negotiationID == "" {
		err := fmt.Errorf("negotiation ID is empty, cannot enqueue round")
		s.log.Error("cannot enqueue negotiation round", "error", err, "session_id", sessionID)
		return err
	}

	msg := BargainingQueueMessage{
		Type:          "process_round",
		SessionID:     sessionID,
		NegotiationID: negotiationID,
		Round:         currentRound + 1,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal queue message: %w", err)
	}

	_, err = s.sqs.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    aws.String(s.cfg.SQS.BargainingQueue),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("failed to send SQS message: %w", err)
	}

	s.log.Info("enqueued negotiation round", "session_id", sessionID, "negotiation_id", negotiationID, "round", currentRound+1)
	return nil
}

type BargainingQueueMessage struct {
	Type          string `json:"type"`
	SessionID     string `json:"session_id"`
	NegotiationID string `json:"negotiation_id"`
	Round         int    `json:"round"`
}

func (s *A2ABargainingService) sendWebhook(callbackURL string, payload WebhookPayload) {
	if callbackURL == "" {
		return
	}
	if err := validateWebhookURL(context.Background(), callbackURL); err != nil {
		s.log.Warn("unsafe webhook callback rejected", "url", callbackURL, "event", payload.Event, "error", err)
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		s.log.Error("failed to marshal webhook payload", "error", err) //
		return
	}

	var lastErr error
	client := s.webhookClient
	if client == nil {
		client = newWebhookDeliveryHTTPClient(webhookTimeout)
	}
	for attempt := 0; attempt <= webhookMaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, callbackURL, bytes.NewReader(body))
		if err != nil {
			s.log.Error("failed to create webhook request", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

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

	if exists {
		// Sync in-memory session with DB state to catch external modifications
		dbProgress := s.getDBProgressBySessionID(sessionID)
		if dbProgress != nil {
			session.Round = dbProgress.Round
			session.Status = dbProgress.Status
			session.CurrentAmount = dbProgress.CurrentAmount
		}
		// Return pointer to the actual in-memory session's progress (not a copy)
		progress := session.ToProgressResponse()
		return &progress
	}

	// Fall back to DB lookup (handles Lambda cold starts where in-memory session is lost)
	dbProgress := s.getDBProgressBySessionID(sessionID)
	return dbProgress
}

func (s *A2ABargainingService) getDBProgressBySessionID(sessionID string) *A2ASessionProgress {
	neg, err := s.ap2Repo.GetBargainingNegotiationBySessionID(context.Background(), sessionID)
	if err != nil || neg == nil {
		return nil
	}

	sessionIDStr := ""
	if neg.SessionID != nil {
		sessionIDStr = *neg.SessionID
	}

	return &A2ASessionProgress{
		NegotiationID:   sessionIDStr,
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
			// Validate session state against DB to avoid stale in-memory data
			dbProgress := s.getDBProgress(ctx, negotiationID)
			if dbProgress != nil {
				session.Round = dbProgress.Round
				session.Status = dbProgress.Status
				session.CurrentAmount = dbProgress.CurrentAmount
			}
			progress := session.ToProgressResponse()
			return &progress
		}
	}
	s.sessionsLock.RUnlock()

	// Fall back to DB lookup
	dbProgress := s.getDBProgress(ctx, negotiationID)
	return dbProgress
}

func (s *A2ABargainingService) getDBProgress(ctx context.Context, negotiationID string) *A2ASessionProgress {
	neg, err := s.bargaining.GetNegotiation(ctx, negotiationID)
	if err != nil || neg == nil {
		return nil
	}
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

func (s *A2ABargainingService) reloadSessionFromDB(ctx context.Context, sessionID string) (*A2ASession, error) {
	// Try to find negotiation by session_id in DB
	neg, err := s.ap2Repo.GetBargainingNegotiationBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("negotiation not found for session_id: %s", sessionID)
	}

	// Check if negotiation is still active
	if neg.Status != "running" && neg.Status != "initiated" && neg.Status != "in_progress" {
		return nil, fmt.Errorf("negotiation is not active: %s (status: %s)", sessionID, neg.Status)
	}

	// Reload rounds from DB to determine current state
	rounds, err := s.ap2Repo.GetBargainingRounds(ctx, neg.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bargaining rounds: %w", err)
	}

	session := &A2ASession{
		NegotiationID:    sessionID,
		DBNegotiationID:  neg.ID,
		BuyerAgentID:     neg.BuyerAgentID,
		SellerAgentID:    neg.SellerAgentID,
		UserID:           neg.UserID,
		InitialAmount:    neg.InitialAmount,
		CurrentAmount:    neg.CurrentAmount,
		Round:            len(rounds),
		MaxRounds:        neg.MaxRounds,
		Status:           neg.Status,
		StartTime:        neg.CreatedAt,
		NegotiationReady: nil,
	}

	// Re-register session in memory
	s.sessionsLock.Lock()
	s.sessions[sessionID] = session
	s.sessionLocks[sessionID] = &sync.Mutex{}
	s.sessionsLock.Unlock()

	return session, nil
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
