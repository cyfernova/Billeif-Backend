package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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

var (
	ErrA2ANegotiationNotFound = errors.New("negotiation not found")
	ErrA2ANegotiationTerminal = errors.New("negotiation is already terminal")
)

type A2ANegotiationScope struct {
	UserID     string
	BusinessID string
}

func (s A2ANegotiationScope) valid() bool {
	return strings.TrimSpace(s.UserID) != "" && strings.TrimSpace(s.BusinessID) != ""
}

func a2aScopedLookupError(operation string, err error) error {
	if errors.Is(err, interfaces.ErrA2ANegotiationScopeNotFound) {
		return ErrA2ANegotiationNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

type A2ABargainingService struct {
	a2aClient                   *a2a.A2AClient
	bargaining                  *BargainingService
	mentee                      *MenteeService
	ap2Repo                     interfaces.AP2Repository
	sqs                         *sqs.Client
	cfg                         *config.Config
	log                         *logger.Logger
	webhookClient               *http.Client
	sessions                    map[string]*A2ASession
	sessionsLock                sync.RWMutex
	sessionLocks                map[string]*sync.Mutex
	ungovernedExecutionDisabled bool
}

func (s *A2ABargainingService) DisableUngovernedExecution() *A2ABargainingService {
	if s != nil {
		s.ungovernedExecutionDisabled = true
	}
	return s
}

type A2ASession struct {
	stateMu          sync.RWMutex
	NegotiationID    string
	DBNegotiationID  string
	BuyerAgentID     string
	SellerAgentID    string
	UserID           string
	BusinessID       string
	BuyerAgent       *models.Agent
	SellerAgent      *models.Agent
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
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.toProgressResponseLocked()
}

func (s *A2ASession) toProgressResponseLocked() A2ASessionProgress {
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

func (s *A2ASession) applyProgress(progress *A2ASessionProgress) {
	if progress == nil {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if isTerminalNegotiationStatus(s.Status) && progress.Status != s.Status {
		return
	}
	s.Round = progress.Round
	s.Status = progress.Status
	s.CurrentAmount = progress.CurrentAmount
}

func (s *A2ASession) setStatus(status string) {
	s.stateMu.Lock()
	s.Status = status
	s.stateMu.Unlock()
}

func (s *A2ASession) setStatusIfActive(status string) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if isTerminalNegotiationStatus(s.Status) {
		return false
	}
	s.Status = status
	return true
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
		a2aClient:                   a2aClient,
		bargaining:                  bargaining,
		mentee:                      mentee,
		ap2Repo:                     ap2Repo,
		sqs:                         sqsClient,
		cfg:                         cfg,
		log:                         log,
		webhookClient:               newWebhookDeliveryHTTPClient(webhookTimeout),
		sessions:                    make(map[string]*A2ASession),
		sessionLocks:                make(map[string]*sync.Mutex),
		ungovernedExecutionDisabled: true,
	}
}

func (s *A2ABargainingService) StartNegotiation(
	ctx context.Context,
	scope A2ANegotiationScope,
	buyerAgentID,
	sellerAgentID string,
	initialAmount float64,
) (*A2ASession, error) {
	if s.ungovernedExecutionDisabled {
		return nil, ErrA2AGovernanceRequired
	}
	return s.startNegotiation(ctx, scope, &AutonomousNegotiationRequest{
		BuyerAgentID: buyerAgentID, SellerAgentID: sellerAgentID,
		InitialAmount: initialAmount, MaxRounds: 5,
	})
}

func (s *A2ABargainingService) StartAutonomousNegotiation(
	ctx context.Context,
	scope A2ANegotiationScope,
	req *AutonomousNegotiationRequest,
) (*A2ASession, error) {
	if s.ungovernedExecutionDisabled {
		return nil, ErrA2AGovernanceRequired
	}
	return s.startNegotiation(ctx, scope, req)
}

func (s *A2ABargainingService) startNegotiation(
	ctx context.Context,
	scope A2ANegotiationScope,
	req *AutonomousNegotiationRequest,
) (*A2ASession, error) {
	if req == nil || !scope.valid() {
		return nil, ErrA2ANegotiationNotFound
	}

	buyerAgent, err := s.ap2Repo.GetAgentByIDForOwnerAndBusiness(
		ctx,
		req.BuyerAgentID,
		scope.UserID,
		scope.BusinessID,
	)
	if err != nil {
		return nil, a2aScopedLookupError("authorize buyer agent", err)
	}
	if buyerAgent == nil || NormalizeMarketplaceAgentType(buyerAgent.Type) != "shopping" {
		return nil, ErrA2ANegotiationNotFound
	}
	sellerAgent, err := s.ap2Repo.GetAgentByIDForOwnerAndBusiness(
		ctx,
		req.SellerAgentID,
		scope.BusinessID,
		scope.BusinessID,
	)
	if err != nil {
		return nil, a2aScopedLookupError("authorize seller agent", err)
	}
	if sellerAgent == nil || NormalizeMarketplaceAgentType(sellerAgent.Type) != "merchant" {
		return nil, ErrA2ANegotiationNotFound
	}

	if req.CallbackURL != "" {
		if err := validateWebhookURL(ctx, req.CallbackURL); err != nil {
			return nil, fmt.Errorf("invalid callback URL: %w", err)
		}
	}

	maxRounds := req.MaxRounds
	if maxRounds == 0 {
		maxRounds = 5
	}
	negotiationID := generateA2ANegotiationID()
	negotiation, err := s.bargaining.CreateNegotiation(ctx, &CreateNegotiationRequest{
		BuyerAgentID:   req.BuyerAgentID,
		SellerAgentID:  req.SellerAgentID,
		UserID:         scope.UserID,
		BusinessID:     scope.BusinessID,
		InitialAmount:  req.InitialAmount,
		ReferencePrice: req.ReferencePrice,
		MaxRounds:      maxRounds,
		SessionID:      &negotiationID,
	})
	if err != nil {
		s.log.Error("failed to persist A2A negotiation", "error", err, "session_id", negotiationID)
		return nil, fmt.Errorf("persist negotiation: %w", err)
	}

	session := &A2ASession{
		NegotiationID:    negotiationID,
		DBNegotiationID:  negotiation.ID,
		BuyerAgentID:     req.BuyerAgentID,
		SellerAgentID:    req.SellerAgentID,
		UserID:           scope.UserID,
		BusinessID:       scope.BusinessID,
		BuyerAgent:       buyerAgent,
		SellerAgent:      sellerAgent,
		InitialAmount:    req.InitialAmount,
		CurrentAmount:    req.InitialAmount,
		Round:            0,
		MaxRounds:        maxRounds,
		Status:           "running",
		StartTime:        time.Now().UTC(),
		CallbackURL:      req.CallbackURL,
		NegotiationReady: make(chan struct{}),
	}

	s.sessionsLock.Lock()
	s.sessions[negotiationID] = session
	s.sessionLocks[negotiationID] = &sync.Mutex{}
	s.sessionsLock.Unlock()

	s.log.Info("A2A negotiation session created",
		"session_id", negotiationID,
		"db_negotiation_id", negotiation.ID,
		"buyer_id", req.BuyerAgentID,
		"seller_id", req.SellerAgentID,
		"user_id", scope.UserID,
		"business_id", scope.BusinessID)
	return session, nil
}

func (s *A2ABargainingService) RunAutonomousNegotiation(ctx context.Context, sessionID string) (err error) {
	if s.ungovernedExecutionDisabled {
		return ErrA2AGovernanceRequired
	}
	var session *A2ASession
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in RunAutonomousNegotiation: %v", r)
			s.log.Error("panic in autonomous negotiation goroutine", "error", r, "session_id", sessionID)
		}
		if err != nil && session != nil {
			session.setStatusIfActive("failed")
		}
	}()

	s.sessionsLock.RLock()
	session, exists := s.sessions[sessionID]
	lock, lockExists := s.sessionLocks[sessionID]
	s.sessionsLock.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if !lockExists {
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

	negotiationID := session.DBNegotiationID
	if negotiationID == "" {
		return fmt.Errorf("no durable negotiation for session: %s", sessionID)
	}
	negotiation, err := s.ap2Repo.GetBargainingNegotiationBySessionAndID(ctx, sessionID, negotiationID)
	if err != nil {
		return a2aScopedLookupError("load autonomous negotiation", err)
	}
	if negotiation == nil {
		return ErrA2ANegotiationNotFound
	}
	if isTerminalNegotiationStatus(negotiation.Status) {
		session.applyProgress(bargainingNegotiationProgress(negotiation))
		return nil
	}

	activeAgentID := session.BuyerAgentID
	activeAgentType := "buyer"
	maxRounds := session.ToProgressResponse().MaxRounds

	for round := 1; round <= maxRounds; round++ {
		select {
		case <-ctx.Done():
			s.log.Info("autonomous negotiation cancelled", "session_id", sessionID, "round", round)
			return ctx.Err()
		default:
		}

		if round > 1 {
			timer := time.NewTimer(interRoundDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		progress, progressErr := s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
		if progressErr != nil {
			return progressErr
		}
		if isTerminalNegotiationStatus(progress.Status) {
			return nil
		}

		decision, err := s.bargaining.GetLLMBargainingDecision(ctx, systemBargainingActorScope(), activeAgentID, activeAgentType, negotiation.ID)
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
		progress, progressErr = s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
		if progressErr != nil {
			return progressErr
		}
		if isTerminalNegotiationStatus(progress.Status) {
			return nil
		}

		counterReq := &CounterOfferRequest{
			AgentID:        activeAgentID,
			ProposedAmount: decision.ProposedAmount,
			Reason:         &decision.Reason,
			Action:         decision.Action,
		}

		_, updatedNegotiation, err := s.bargaining.SubmitCounterOffer(ctx, negotiationID, counterReq)
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

		updatedProgress := bargainingNegotiationProgress(updatedNegotiation)
		updatedProgress.NegotiationID = sessionID
		session.applyProgress(updatedProgress)

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
			status := updatedNegotiation.Status
			if status == "" {
				status = decision.Action
			}
			session.setStatus(status)

			s.recordLearning(updatedNegotiation, activeAgentID, activeAgentType, decision.Action, round)

			s.sendWebhook(session.CallbackURL, WebhookPayload{
				Event:         "negotiation_completed",
				SessionID:     sessionID,
				Status:        status,
				FinalAmount:   updatedNegotiation.CurrentAmount,
				Rounds:        round,
				BuyerAgentID:  session.BuyerAgentID,
				SellerAgentID: session.SellerAgentID,
			})

			s.log.Info("autonomous negotiation completed",
				"session_id", sessionID,
				"status", status,
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

	if err := s.ap2Repo.UpdateNegotiationStatus(ctx, negotiationID, "expired"); err != nil {
		progress, progressErr := s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
		if progressErr == nil && isTerminalNegotiationStatus(progress.Status) {
			return nil
		}
		return fmt.Errorf("expire autonomous negotiation: %w", err)
	}
	session.setStatusIfActive("expired")
	sessionProgress := session.ToProgressResponse()
	s.sendWebhook(session.CallbackURL, WebhookPayload{
		Event:         "negotiation_completed",
		SessionID:     sessionID,
		Status:        "expired",
		FinalAmount:   sessionProgress.CurrentAmount,
		Rounds:        sessionProgress.MaxRounds,
		BuyerAgentID:  session.BuyerAgentID,
		SellerAgentID: session.SellerAgentID,
	})

	s.log.Info("autonomous negotiation expired (max rounds)", "session_id", sessionID, "max_rounds", sessionProgress.MaxRounds)
	return nil
}

func (s *A2ABargainingService) RunAutonomousNegotiationRound(
	ctx context.Context,
	sessionID,
	negotiationID string,
	expectedRound int,
) error {
	if s.ungovernedExecutionDisabled {
		return ErrA2AGovernanceRequired
	}
	if sessionID == "" || negotiationID == "" || expectedRound <= 0 {
		return fmt.Errorf("invalid expected negotiation round: %d", expectedRound)
	}
	progress, err := s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
	if err != nil {
		return err
	}
	if isTerminalNegotiationStatus(progress.Status) {
		return nil
	}
	var session *A2ASession
	s.sessionsLock.RLock()
	session, exists := s.sessions[sessionID]
	s.sessionsLock.RUnlock()

	if !exists {
		// Cold start: session not in memory. Try to reload from DB.
		session, err = s.reloadSessionFromDB(ctx, sessionID, negotiationID)
		if err != nil {
			return err
		}
	}

	if session.DBNegotiationID != negotiationID {
		return ErrA2ANegotiationNotFound
	}

	// Determine which round to process by checking DB rounds
	// This must be done before claiming to know which round to atomically claim
	rounds, err := s.ap2Repo.GetBargainingRounds(ctx, negotiationID)
	if err != nil {
		s.log.Warn("failed to get negotiation rounds", "error", err)
		rounds = []*models.BargainingRound{}
	}

	// Calculate the round to process: if DB has N rounds completed, next is N+1
	// But we need to account for session.Round which might be ahead or behind
	// Use DB state as source of truth for which rounds exist
	dbRoundCount := len(rounds)
	nextRound := dbRoundCount + 1
	if nextRound > expectedRound {
		s.log.Info(
			"negotiation round already applied",
			"session_id", sessionID,
			"expected_round", expectedRound,
			"db_next_round", nextRound,
		)
		return nil
	}
	if nextRound < expectedRound {
		return fmt.Errorf(
			"negotiation round out of order: expected=%d db_next=%d",
			expectedRound,
			nextRound,
		)
	}

	sessionProgress := session.ToProgressResponse()
	if nextRound > sessionProgress.MaxRounds {
		return s.expireAutonomousNegotiation(ctx, session, sessionID, negotiationID)
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
	latestProgress, err := s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
	if err != nil {
		return fmt.Errorf("failed to get negotiation: %w", err)
	}
	if isTerminalNegotiationStatus(latestProgress.Status) {
		return nil
	}
	latestNeg, err := s.ap2Repo.GetBargainingNegotiationBySessionAndID(ctx, sessionID, negotiationID)
	if err != nil {
		return a2aScopedLookupError("reload autonomous negotiation", err)
	}
	if latestNeg == nil {
		return ErrA2ANegotiationNotFound
	}
	session.applyProgress(latestProgress)

	// Re-calculate nextRound from synced DB state to stay in sync
	// If DB rounds advanced (e.g. concurrent submission), use that
	syncedNextRound := latestNeg.Rounds + 1
	if syncedNextRound != nextRound {
		s.log.Info("round count updated from DB sync", "session_id", sessionID, "old_next_round", nextRound, "new_next_round", syncedNextRound, "db_rounds", latestNeg.Rounds)
		nextRound = syncedNextRound
	}
	if nextRound > expectedRound {
		s.log.Info(
			"negotiation round applied during claim",
			"session_id", sessionID,
			"expected_round", expectedRound,
			"db_next_round", nextRound,
		)
		return nil
	}
	if nextRound < expectedRound {
		return fmt.Errorf(
			"negotiation round changed out of order: expected=%d db_next=%d",
			expectedRound,
			nextRound,
		)
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

	sessionProgress = session.ToProgressResponse()
	if nextRound > sessionProgress.MaxRounds {
		return s.expireAutonomousNegotiation(ctx, session, sessionID, negotiationID)
	}

	decision, err := s.bargaining.GetLLMBargainingDecision(ctx, systemBargainingActorScope(), activeAgentID, activeAgentType, latestNeg.ID)
	if err != nil {
		s.log.Error("LLM decision failed", "error", err, "session_id", sessionID, "round", nextRound, "agent_id", activeAgentID)
		return fmt.Errorf("LLM decision failed: %w", err)
	}
	latestProgress, err = s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
	if err != nil {
		return err
	}
	if isTerminalNegotiationStatus(latestProgress.Status) {
		return nil
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

	updatedProgress := bargainingNegotiationProgress(updatedNegotiation)
	updatedProgress.NegotiationID = sessionID
	session.applyProgress(updatedProgress)
	sessionProgress = session.ToProgressResponse()

	s.sendWebhook(session.CallbackURL, WebhookPayload{
		Event:          "round_completed",
		SessionID:      sessionID,
		Round:          sessionProgress.Round,
		AgentID:        activeAgentID,
		AgentType:      activeAgentType,
		Action:         decision.Action,
		ProposedAmount: decision.ProposedAmount,
		CurrentAmount:  updatedNegotiation.CurrentAmount,
		Status:         updatedNegotiation.Status,
	})

	if decision.Action == "accept" || decision.Action == "reject" || updatedNegotiation.Status == "accepted" || updatedNegotiation.Status == "rejected" {
		status := updatedNegotiation.Status
		if status == "" {
			status = decision.Action
		}
		session.setStatus(status)
		sessionProgress = session.ToProgressResponse()

		s.recordLearning(updatedNegotiation, activeAgentID, activeAgentType, decision.Action, sessionProgress.Round)

		s.sendWebhook(session.CallbackURL, WebhookPayload{
			Event:         "negotiation_completed",
			SessionID:     sessionID,
			Status:        sessionProgress.Status,
			FinalAmount:   updatedNegotiation.CurrentAmount,
			Rounds:        sessionProgress.Round,
			BuyerAgentID:  session.BuyerAgentID,
			SellerAgentID: session.SellerAgentID,
		})

		s.log.Info("autonomous negotiation completed",
			"session_id", sessionID,
			"status", sessionProgress.Status,
			"final_amount", updatedNegotiation.CurrentAmount,
			"total_rounds", sessionProgress.Round)

		return nil
	}

	s.log.Info("autonomous negotiation round completed, session may continue",
		"session_id", sessionID,
		"round", sessionProgress.Round,
		"max_rounds", sessionProgress.MaxRounds,
		"current_amount", sessionProgress.CurrentAmount)

	// Enqueue next round if not complete
	if sessionProgress.Round < sessionProgress.MaxRounds && !isTerminalNegotiationStatus(sessionProgress.Status) {
		if err := s.EnqueueNegotiationRound(ctx, sessionID, negotiationID, sessionProgress.Round); err != nil {
			return fmt.Errorf(
				"enqueue successor after bargaining round %d: %w",
				sessionProgress.Round,
				err,
			)
		}
		s.log.Info("enqueued next round from service", "session_id", sessionID, "next_round", sessionProgress.Round+1)
	}

	return nil
}

func (s *A2ABargainingService) expireAutonomousNegotiation(
	ctx context.Context,
	session *A2ASession,
	sessionID,
	negotiationID string,
) error {
	if err := s.ap2Repo.UpdateNegotiationStatus(ctx, negotiationID, "expired"); err != nil {
		progress, progressErr := s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
		if progressErr == nil && isTerminalNegotiationStatus(progress.Status) {
			return nil
		}
		return fmt.Errorf("expire autonomous negotiation: %w", err)
	}
	session.setStatusIfActive("expired")
	progress := session.ToProgressResponse()
	s.sendWebhook(session.CallbackURL, WebhookPayload{
		Event:         "negotiation_completed",
		SessionID:     sessionID,
		Status:        "expired",
		FinalAmount:   progress.CurrentAmount,
		Rounds:        progress.MaxRounds,
		BuyerAgentID:  session.BuyerAgentID,
		SellerAgentID: session.SellerAgentID,
	})
	return nil
}

func (s *A2ABargainingService) EnsureAutonomousNegotiationSuccessor(
	ctx context.Context,
	sessionID,
	negotiationID string,
	completedRound int,
) error {
	if sessionID == "" || negotiationID == "" || completedRound < 1 {
		return fmt.Errorf("invalid bargaining successor recovery")
	}
	progress, err := s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
	if err != nil {
		return fmt.Errorf(
			"load bargaining successor session_id=%s negotiation_id=%s: %w",
			sessionID,
			negotiationID,
			err,
		)
	}
	if progress.Round > completedRound {
		return nil
	}
	if progress.Round < completedRound {
		return fmt.Errorf(
			"cannot recover future bargaining successor: completed_round=%d db_round=%d",
			completedRound,
			progress.Round,
		)
	}
	if progress.Round >= progress.MaxRounds || isTerminalNegotiationStatus(progress.Status) {
		return nil
	}
	if !isActiveNegotiationStatus(progress.Status) {
		return fmt.Errorf(
			"cannot recover bargaining successor from status %q",
			progress.Status,
		)
	}
	if err := s.EnqueueNegotiationRound(ctx, sessionID, negotiationID, completedRound); err != nil {
		return fmt.Errorf("enqueue bargaining successor: %w", err)
	}
	return nil
}

func (s *A2ABargainingService) ClaimAutonomousNegotiationRound(
	ctx context.Context,
	sessionID,
	negotiationID string,
	roundNumber int,
	leaseOwner string,
	now time.Time,
	leaseExpiresAt time.Time,
) (bool, error) {
	if sessionID == "" || negotiationID == "" || roundNumber < 1 || leaseOwner == "" || !leaseExpiresAt.After(now) {
		return false, fmt.Errorf("invalid bargaining round claim")
	}
	return s.ap2Repo.ClaimBargainingRound(ctx, sessionID, negotiationID, roundNumber, leaseOwner, now, leaseExpiresAt)
}

func (s *A2ABargainingService) CompleteAutonomousNegotiationRound(
	ctx context.Context,
	sessionID,
	negotiationID string,
	roundNumber int,
	leaseOwner string,
	completedAt time.Time,
) (bool, error) {
	if sessionID == "" || negotiationID == "" || roundNumber < 1 || leaseOwner == "" {
		return false, fmt.Errorf("invalid bargaining round completion")
	}
	return s.ap2Repo.CompleteBargainingRoundClaim(ctx, sessionID, negotiationID, roundNumber, leaseOwner, completedAt)
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

	if err := s.mentee.RecordNegotiationOutcome(context.Background(), &outcome); err != nil {
		s.log.Warn("failed to record negotiation outcome", "negotiation_id", negotiation.ID, "error", err)
		return
	}

	s.log.Info("learning recorded",
		"negotiation_id", negotiation.ID,
		"final_amount", negotiation.CurrentAmount,
		"status", status,
		"rounds", rounds)
}

func (s *A2ABargainingService) EnqueueNegotiationRound(
	ctx context.Context,
	sessionID,
	negotiationID string,
	currentRound int,
) error {
	progress, err := s.GetAutonomousNegotiationProgress(ctx, sessionID, negotiationID)
	if err != nil {
		return err
	}
	if isTerminalNegotiationStatus(progress.Status) {
		return nil
	}
	if progress.Round != currentRound {
		return fmt.Errorf(
			"negotiation round changed before queueing: expected=%d actual=%d",
			currentRound,
			progress.Round,
		)
	}
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

	_, err = s.sqs.SendMessage(ctx, &sqs.SendMessageInput{
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

func (s *A2ABargainingService) GetSessionProgress(
	ctx context.Context,
	scope A2ANegotiationScope,
	sessionID string,
) (*A2ASessionProgress, error) {
	if !scope.valid() || strings.TrimSpace(sessionID) == "" {
		return nil, ErrA2ANegotiationNotFound
	}
	negotiation, err := s.ap2Repo.GetBargainingNegotiationBySessionIDForScope(
		ctx,
		sessionID,
		scope.UserID,
		scope.BusinessID,
	)
	if err != nil {
		return nil, a2aScopedLookupError("load negotiation progress by session", err)
	}
	if negotiation == nil {
		return nil, ErrA2ANegotiationNotFound
	}
	progress := bargainingNegotiationProgress(negotiation)
	s.syncCachedProgress(progress)
	return progress, nil
}

func (s *A2ABargainingService) GetAutonomousNegotiationProgress(
	ctx context.Context,
	sessionID,
	negotiationID string,
) (*A2ASessionProgress, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(negotiationID) == "" {
		return nil, ErrA2ANegotiationNotFound
	}
	negotiation, err := s.ap2Repo.GetBargainingNegotiationBySessionAndID(ctx, sessionID, negotiationID)
	if err != nil {
		return nil, a2aScopedLookupError("load autonomous negotiation progress", err)
	}
	if negotiation == nil {
		return nil, ErrA2ANegotiationNotFound
	}
	progress := bargainingNegotiationProgress(negotiation)
	s.syncCachedProgress(progress)
	return progress, nil
}

func (s *A2ABargainingService) GetSessionProgressByNegotiationID(
	ctx context.Context,
	scope A2ANegotiationScope,
	negotiationID string,
) (*A2ASessionProgress, error) {
	if !scope.valid() || strings.TrimSpace(negotiationID) == "" {
		return nil, ErrA2ANegotiationNotFound
	}
	negotiation, err := s.ap2Repo.GetBargainingNegotiationByIDForScope(
		ctx,
		negotiationID,
		scope.UserID,
		scope.BusinessID,
	)
	if err != nil {
		return nil, a2aScopedLookupError("load negotiation progress by ID", err)
	}
	if negotiation == nil {
		return nil, ErrA2ANegotiationNotFound
	}
	progress := bargainingNegotiationProgress(negotiation)
	s.syncCachedProgress(progress)
	return progress, nil
}

func (s *A2ABargainingService) StopNegotiation(
	ctx context.Context,
	scope A2ANegotiationScope,
	sessionID string,
) error {
	if !scope.valid() || strings.TrimSpace(sessionID) == "" {
		return ErrA2ANegotiationNotFound
	}
	negotiation, err := s.ap2Repo.GetBargainingNegotiationBySessionIDForScope(
		ctx,
		sessionID,
		scope.UserID,
		scope.BusinessID,
	)
	if err != nil {
		return a2aScopedLookupError("load negotiation before stop", err)
	}
	if negotiation == nil {
		return ErrA2ANegotiationNotFound
	}
	if negotiation.Status == "stopped" {
		s.setCachedStatus(sessionID, negotiation.ID, "stopped")
		return nil
	}
	if isTerminalNegotiationStatus(negotiation.Status) {
		return ErrA2ANegotiationTerminal
	}

	stopped, err := s.ap2Repo.StopBargainingNegotiationForScope(
		ctx,
		negotiation.ID,
		scope.UserID,
		scope.BusinessID,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("stop negotiation: %w", err)
	}
	if !stopped {
		latest, latestErr := s.ap2Repo.GetBargainingNegotiationBySessionIDForScope(
			ctx,
			sessionID,
			scope.UserID,
			scope.BusinessID,
		)
		if latestErr != nil {
			return a2aScopedLookupError("reload negotiation after stop race", latestErr)
		}
		if latest == nil {
			return ErrA2ANegotiationNotFound
		}
		if latest.Status != "stopped" {
			return ErrA2ANegotiationTerminal
		}
	}
	s.setCachedStatus(sessionID, negotiation.ID, "stopped")
	s.log.Info("A2A negotiation session stopped", "session_id", sessionID, "negotiation_id", negotiation.ID)
	return nil
}

func (s *A2ABargainingService) StopAutonomousNegotiation(
	ctx context.Context,
	sessionID,
	negotiationID string,
) error {
	negotiation, err := s.ap2Repo.GetBargainingNegotiationBySessionAndID(ctx, sessionID, negotiationID)
	if err != nil {
		return a2aScopedLookupError("load autonomous negotiation before stop", err)
	}
	if negotiation == nil {
		return ErrA2ANegotiationNotFound
	}
	if negotiation.Status == "stopped" {
		s.setCachedStatus(sessionID, negotiationID, "stopped")
		return nil
	}
	if isTerminalNegotiationStatus(negotiation.Status) {
		return nil
	}
	stopped, err := s.ap2Repo.StopBargainingNegotiationBySessionAndID(
		ctx,
		sessionID,
		negotiationID,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("stop autonomous negotiation: %w", err)
	}
	if !stopped {
		latest, latestErr := s.ap2Repo.GetBargainingNegotiationBySessionAndID(ctx, sessionID, negotiationID)
		if latestErr != nil {
			return a2aScopedLookupError("reload autonomous negotiation after stop race", latestErr)
		}
		if latest == nil {
			return ErrA2ANegotiationNotFound
		}
		if !isTerminalNegotiationStatus(latest.Status) {
			return fmt.Errorf("stop autonomous negotiation: concurrent state change")
		}
		s.setCachedStatus(sessionID, negotiationID, latest.Status)
		return nil
	}
	s.setCachedStatus(sessionID, negotiationID, "stopped")
	return nil
}

func bargainingNegotiationProgress(negotiation *models.BargainingNegotiation) *A2ASessionProgress {
	sessionID := ""
	if negotiation.SessionID != nil {
		sessionID = *negotiation.SessionID
	}
	return &A2ASessionProgress{
		NegotiationID:   sessionID,
		NegotiationUUID: negotiation.ID,
		BuyerAgentID:    negotiation.BuyerAgentID,
		SellerAgentID:   negotiation.SellerAgentID,
		UserID:          negotiation.UserID,
		InitialAmount:   negotiation.InitialAmount,
		CurrentAmount:   negotiation.CurrentAmount,
		Round:           negotiation.Rounds,
		MaxRounds:       negotiation.MaxRounds,
		Status:          negotiation.Status,
	}
}

func (s *A2ABargainingService) syncCachedProgress(progress *A2ASessionProgress) {
	if progress == nil {
		return
	}
	s.sessionsLock.RLock()
	session := s.sessions[progress.NegotiationID]
	s.sessionsLock.RUnlock()
	if session != nil && session.DBNegotiationID == progress.NegotiationUUID {
		session.applyProgress(progress)
	}
}

func (s *A2ABargainingService) setCachedStatus(sessionID, negotiationID, status string) {
	s.sessionsLock.RLock()
	session := s.sessions[sessionID]
	s.sessionsLock.RUnlock()
	if session != nil && session.DBNegotiationID == negotiationID {
		session.setStatus(status)
	}
}

func (s *A2ABargainingService) reloadSessionFromDB(ctx context.Context, sessionID, negotiationID string) (*A2ASession, error) {
	neg, err := s.ap2Repo.GetBargainingNegotiationBySessionAndID(ctx, sessionID, negotiationID)
	if err != nil {
		return nil, a2aScopedLookupError("reload autonomous negotiation session", err)
	}
	if neg == nil {
		return nil, ErrA2ANegotiationNotFound
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
		BusinessID:       neg.BusinessID,
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
