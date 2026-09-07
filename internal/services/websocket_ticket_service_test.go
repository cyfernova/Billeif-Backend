package services

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

type memoryWebSocketTicketRepository struct {
	mu      sync.Mutex
	records map[string]models.WebSocketTicket
}

func newMemoryWebSocketTicketRepository() *memoryWebSocketTicketRepository {
	return &memoryWebSocketTicketRepository{records: make(map[string]models.WebSocketTicket)}
}

func (r *memoryWebSocketTicketRepository) Create(_ context.Context, ticket *models.WebSocketTicket) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.records[ticket.TicketDigest]; exists {
		return errors.New("duplicate digest")
	}
	r.records[ticket.TicketDigest] = *ticket
	return nil
}

func (r *memoryWebSocketTicketRepository) ConsumeByDigest(_ context.Context, digest string, consumedAt time.Time) (*models.WebSocketTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, exists := r.records[digest]
	if !exists || ticket.ConsumedAt != nil || !ticket.ExpiresAt.After(consumedAt) {
		return nil, interfaces.ErrWebSocketTicketInvalid
	}
	ticket.ConsumedAt = &consumedAt
	r.records[digest] = ticket
	return &ticket, nil
}

func TestWebSocketTicketIssueStoresOnlyDigestAndBoundScope(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	repository := newMemoryWebSocketTicketRepository()
	service := NewWebSocketTicketService(repository, WebSocketTicketServiceOptions{
		Now:    func() time.Time { return now },
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, WebSocketTicketEntropyBytes)),
	})

	issued, err := service.Issue(context.Background(), "subject-1", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if issued.Ticket == "" || issued.ExpiresAt != now.Add(MaxWebSocketTicketTTL) {
		t.Fatalf("issued ticket = %#v", issued)
	}
	if len(repository.records) != 1 {
		t.Fatalf("stored records = %d, want 1", len(repository.records))
	}
	for digest, stored := range repository.records {
		if digest == issued.Ticket || stored.TicketDigest == issued.Ticket {
			t.Fatal("raw ticket was persisted")
		}
		if len(digest) != 64 {
			t.Fatalf("digest length = %d, want 64", len(digest))
		}
		if stored.Subject != "subject-1" || stored.BusinessID != "10000000-0000-0000-0000-000000000001" {
			t.Fatalf("stored scope = %#v", stored)
		}
	}

	consumed, err := service.Consume(context.Background(), issued.Ticket)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if consumed.Subject != "subject-1" || consumed.BusinessID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("consumed scope = %#v", consumed)
	}
}

func TestWebSocketTicketCannotBeReusedOrConsumedAfterExpiry(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	repository := newMemoryWebSocketTicketRepository()
	service := NewWebSocketTicketService(repository, WebSocketTicketServiceOptions{
		Now: func() time.Time { return now },
		TTL: 30 * time.Second,
		Random: bytes.NewReader(append(
			bytes.Repeat([]byte{0x24}, WebSocketTicketEntropyBytes),
			bytes.Repeat([]byte{0x25}, WebSocketTicketEntropyBytes)...,
		)),
	})

	first, err := service.Issue(context.Background(), "subject-1", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("issue first ticket: %v", err)
	}
	if _, err := service.Consume(context.Background(), first.Ticket); err != nil {
		t.Fatalf("consume first ticket: %v", err)
	}
	if _, err := service.Consume(context.Background(), first.Ticket); !errors.Is(err, interfaces.ErrWebSocketTicketInvalid) {
		t.Fatalf("reuse error = %v, want ErrWebSocketTicketInvalid", err)
	}

	second, err := service.Issue(context.Background(), "subject-1", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("issue second ticket: %v", err)
	}
	now = now.Add(31 * time.Second)
	if _, err := service.Consume(context.Background(), second.Ticket); !errors.Is(err, interfaces.ErrWebSocketTicketInvalid) {
		t.Fatalf("expired error = %v, want ErrWebSocketTicketInvalid", err)
	}
}

func TestConcurrentWebSocketTicketConsumeHasExactlyOneWinner(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	repository := newMemoryWebSocketTicketRepository()
	service := NewWebSocketTicketService(repository, WebSocketTicketServiceOptions{
		Now:    func() time.Time { return now },
		Random: bytes.NewReader(bytes.Repeat([]byte{0x18}, WebSocketTicketEntropyBytes)),
	})
	issued, err := service.Issue(context.Background(), "subject-1", "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	const attempts = 16
	var wait sync.WaitGroup
	wait.Add(attempts)
	results := make(chan error, attempts)
	for range attempts {
		go func() {
			defer wait.Done()
			_, consumeErr := service.Consume(context.Background(), issued.Ticket)
			results <- consumeErr
		}()
	}
	wait.Wait()
	close(results)

	winners := 0
	for consumeErr := range results {
		if consumeErr == nil {
			winners++
			continue
		}
		if !errors.Is(consumeErr, interfaces.ErrWebSocketTicketInvalid) {
			t.Fatalf("unexpected consume error: %v", consumeErr)
		}
	}
	if winners != 1 {
		t.Fatalf("successful consumes = %d, want 1", winners)
	}
}

func TestWebSocketTicketRejectsMissingScope(t *testing.T) {
	service := NewWebSocketTicketService(newMemoryWebSocketTicketRepository(), WebSocketTicketServiceOptions{})
	if _, err := service.Issue(context.Background(), "", "business-1"); !errors.Is(err, ErrWebSocketTicketScopeRequired) {
		t.Fatalf("missing subject error = %v", err)
	}
	if _, err := service.Issue(context.Background(), "subject-1", ""); !errors.Is(err, ErrWebSocketTicketScopeRequired) {
		t.Fatalf("missing business error = %v", err)
	}
}
