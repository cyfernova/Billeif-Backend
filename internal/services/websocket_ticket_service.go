package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

const (
	WebSocketTicketEntropyBytes = 32
	MaxWebSocketTicketTTL       = 60 * time.Second
)

var ErrWebSocketTicketScopeRequired = errors.New("websocket ticket subject and business scope are required")

type WebSocketTicketServiceOptions struct {
	TTL    time.Duration
	Now    func() time.Time
	Random io.Reader
}

type WebSocketTicketIssue struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expires_at"`
}

type WebSocketTicketService struct {
	repository interfaces.WebSocketTicketRepository
	ttl        time.Duration
	now        func() time.Time
	random     io.Reader
}

func NewWebSocketTicketService(
	repository interfaces.WebSocketTicketRepository,
	options WebSocketTicketServiceOptions,
) *WebSocketTicketService {
	ttl := options.TTL
	if ttl <= 0 || ttl > MaxWebSocketTicketTTL {
		ttl = MaxWebSocketTicketTTL
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	random := options.Random
	if random == nil {
		random = rand.Reader
	}
	return &WebSocketTicketService{
		repository: repository,
		ttl:        ttl,
		now:        now,
		random:     random,
	}
}

func (s *WebSocketTicketService) Issue(
	ctx context.Context,
	subject string,
	businessID string,
) (*WebSocketTicketIssue, error) {
	subject = strings.TrimSpace(subject)
	businessID = strings.TrimSpace(businessID)
	if subject == "" || businessID == "" {
		return nil, ErrWebSocketTicketScopeRequired
	}
	if s == nil || s.repository == nil {
		return nil, errors.New("websocket ticket service is unavailable")
	}

	raw := make([]byte, WebSocketTicketEntropyBytes)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return nil, fmt.Errorf("generate websocket ticket: %w", err)
	}
	ticketValue := base64.RawURLEncoding.EncodeToString(raw)
	digest := digestWebSocketTicket(ticketValue)
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)
	ticket := &models.WebSocketTicket{
		TicketDigest: digest,
		Subject:      subject,
		BusinessID:   businessID,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repository.Create(ctx, ticket); err != nil {
		return nil, fmt.Errorf("store websocket ticket: %w", err)
	}
	return &WebSocketTicketIssue{Ticket: ticketValue, ExpiresAt: expiresAt}, nil
}

func (s *WebSocketTicketService) Consume(
	ctx context.Context,
	ticketValue string,
) (*models.WebSocketTicket, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("websocket ticket service is unavailable")
	}
	ticketValue = strings.TrimSpace(ticketValue)
	if ticketValue == "" {
		return nil, interfaces.ErrWebSocketTicketInvalid
	}
	ticket, err := s.repository.ConsumeByDigest(ctx, digestWebSocketTicket(ticketValue), s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("consume websocket ticket: %w", err)
	}
	return ticket, nil
}

func digestWebSocketTicket(ticketValue string) string {
	digest := sha256.Sum256([]byte(ticketValue))
	return hex.EncodeToString(digest[:])
}
