package services

import (
	"context"
	"sync"
	"time"

	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"
)

type A2ABargainingService struct {
	a2aClient    *a2a.A2AClient
	bargaining   *BargainingService
	mentee       *MenteeService
	log          *logger.Logger
	sessions     map[string]*A2ASession
	sessionsLock sync.RWMutex
}

type A2ASession struct {
	NegotiationID string
	BuyerAgentID  string
	SellerAgentID string
	InitialAmount float64
	CurrentAmount float64
	Round         int
	MaxRounds     int
	Status        string
	StartTime     time.Time
}

func NewA2ABargainingService(a2aClient *a2a.A2AClient, bargaining *BargainingService, mentee *MenteeService, log *logger.Logger) *A2ABargainingService {
	return &A2ABargainingService{
		a2aClient:  a2aClient,
		bargaining: bargaining,
		mentee:     mentee,
		log:        log,
		sessions:   make(map[string]*A2ASession),
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
	s.sessionsLock.Unlock()

	s.log.Info("A2A negotiation session started", "negotiation_id", negotiationID, "buyer_id", buyerAgentID, "seller_id", sellerAgentID, "initial_amount", initialAmount)

	return session, nil
}

func (s *A2ABargainingService) GetSessionProgress(sessionID string) *A2ASession {
	s.sessionsLock.RLock()
	session, exists := s.sessions[sessionID]
	s.sessionsLock.RUnlock()

	if !exists {
		return &A2ASession{Round: -1}
	}

	return session
}

func (s *A2ABargainingService) StopNegotiation(sessionID string) {
	s.sessionsLock.Lock()
	if session, exists := s.sessions[sessionID]; exists {
		session.Status = "stopped"
		s.log.Info("A2A negotiation session stopped", "negotiation_id", sessionID)
	}
	s.sessionsLock.Unlock()
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
