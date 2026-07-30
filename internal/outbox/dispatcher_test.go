package outbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"
)

type dispatcherStoreFake struct {
	events          []*models.OutboxEvent
	oldestPendingAt *time.Time
	oldestErr       error
	claimErr        error
	claimOwner      string
	claimNow        time.Time
	claimUntil      time.Time
	claimLimit      int
	claimEventTypes []string
	completed       []string
	retried         []string
	retryAt         map[string]time.Time
	completeErr     map[string]error
	retryErr        map[string]error
}

func (s *dispatcherStoreFake) OldestPendingOutboxEventCreatedAt(
	_ context.Context,
	_ []string,
) (*time.Time, error) {
	return s.oldestPendingAt, s.oldestErr
}

func (s *dispatcherStoreFake) ClaimOutboxEvents(
	_ context.Context,
	owner string,
	now, leaseUntil time.Time,
	eventTypes []string,
	limit int,
) ([]*models.OutboxEvent, error) {
	s.claimOwner = owner
	s.claimNow = now
	s.claimUntil = leaseUntil
	s.claimLimit = limit
	s.claimEventTypes = append([]string(nil), eventTypes...)
	return s.events, s.claimErr
}

func (s *dispatcherStoreFake) CompleteOutboxEvent(
	_ context.Context,
	eventID, owner string,
	_ time.Time,
) error {
	if err := s.completeErr[eventID]; err != nil {
		return err
	}
	s.completed = append(s.completed, eventID+"@"+owner)
	return nil
}

func (s *dispatcherStoreFake) RetryOutboxEvent(
	_ context.Context,
	eventID, owner string,
	availableAt time.Time,
) error {
	if err := s.retryErr[eventID]; err != nil {
		return err
	}
	s.retried = append(s.retried, eventID+"@"+owner)
	if s.retryAt == nil {
		s.retryAt = make(map[string]time.Time)
	}
	s.retryAt[eventID] = availableAt
	return nil
}

type dispatcherPublisherFake struct {
	published []string
	failures  map[string]error
}

func (p *dispatcherPublisherFake) Publish(_ context.Context, event *models.OutboxEvent) error {
	p.published = append(p.published, event.ID)
	return p.failures[event.ID]
}

func TestDispatcherClaimsBoundedLeaseAndCompletesPublishedEvents(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	store := &dispatcherStoreFake{
		events: []*models.OutboxEvent{{
			ID:              "event-1",
			PublishAttempts: 1,
			CreatedAt:       now.Add(-7 * time.Minute),
		}},
		completeErr: map[string]error{},
		retryErr:    map[string]error{},
	}
	oldestPendingAt := now.Add(-20 * time.Minute)
	store.oldestPendingAt = &oldestPendingAt
	publisher := &dispatcherPublisherFake{failures: map[string]error{}}
	dispatcher, err := NewDispatcher(store, publisher, DispatcherOptions{
		BatchSize:     25,
		LeaseDuration: 2 * time.Minute,
		EventTypes:    InvoiceRenderEventTypes(),
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	result, err := dispatcher.Dispatch(context.Background(), "request-123")

	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Claimed != 1 || result.Published != 1 || result.Retried != 0 ||
		result.OldestPendingAgeSeconds != 1200 {
		t.Fatalf("result = %#v", result)
	}
	if store.claimOwner != "request-123" || !store.claimNow.Equal(now) ||
		!store.claimUntil.Equal(now.Add(2*time.Minute)) || store.claimLimit != 25 ||
		len(store.claimEventTypes) != 2 ||
		store.claimEventTypes[0] != invoicePreviewRequestedEvent ||
		store.claimEventTypes[1] != invoiceIssuedEvent {
		t.Fatalf(
			"claim owner/now/until/limit = %q/%v/%v/%d",
			store.claimOwner,
			store.claimNow,
			store.claimUntil,
			store.claimLimit,
		)
	}
	if len(store.completed) != 1 || store.completed[0] != "event-1@request-123" ||
		len(store.retried) != 0 {
		t.Fatalf("completed/retried = %#v/%#v", store.completed, store.retried)
	}
}

func TestDispatcherReportsOldestClaimedEventAgeWithoutGoingNegative(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	store := &dispatcherStoreFake{
		events: []*models.OutboxEvent{
			{ID: "newer", CreatedAt: now.Add(-30 * time.Second)},
			{ID: "oldest", CreatedAt: now.Add(-10 * time.Minute)},
			{ID: "future-clock-skew", CreatedAt: now.Add(time.Minute)},
		},
		completeErr: map[string]error{},
		retryErr:    map[string]error{},
	}
	oldestPendingAt := now.Add(-10 * time.Minute)
	store.oldestPendingAt = &oldestPendingAt
	dispatcher, err := NewDispatcher(
		store,
		&dispatcherPublisherFake{failures: map[string]error{}},
		DispatcherOptions{
			EventTypes: InvoiceRenderEventTypes(),
			Now:        func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	result, err := dispatcher.Dispatch(context.Background(), "request-age")

	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.OldestPendingAgeSeconds != 600 {
		t.Fatalf("oldest pending age = %d, want 600", result.OldestPendingAgeSeconds)
	}
}

func TestDispatcherReportsDelayedPendingEventWhenNothingIsClaimable(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	oldestPendingAt := now.Add(-45 * time.Minute)
	store := &dispatcherStoreFake{
		oldestPendingAt: &oldestPendingAt,
		completeErr:     map[string]error{},
		retryErr:        map[string]error{},
	}
	dispatcher, err := NewDispatcher(
		store,
		&dispatcherPublisherFake{failures: map[string]error{}},
		DispatcherOptions{
			EventTypes: InvoiceRenderEventTypes(),
			Now:        func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	result, err := dispatcher.Dispatch(context.Background(), "request-delayed")

	if err != nil {
		t.Fatalf("dispatch delayed event observation: %v", err)
	}
	if result.Claimed != 0 || result.OldestPendingAgeSeconds != 2700 {
		t.Fatalf("delayed pending result = %#v", result)
	}
}

func TestDispatcherContinuesAfterPublishFailureAndSchedulesDeterministicBackoff(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	store := &dispatcherStoreFake{
		events: []*models.OutboxEvent{
			{ID: "failed", PublishAttempts: 3},
			{ID: "published", PublishAttempts: 1},
		},
		completeErr: map[string]error{},
		retryErr:    map[string]error{},
	}
	publisher := &dispatcherPublisherFake{failures: map[string]error{
		"failed": errors.New("SQS unavailable"),
	}}
	dispatcher, err := NewDispatcher(store, publisher, DispatcherOptions{
		BatchSize:     10,
		LeaseDuration: time.Minute,
		EventTypes:    InvoiceRenderEventTypes(),
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	result, err := dispatcher.Dispatch(context.Background(), "request-456")

	if err == nil || !strings.Contains(err.Error(), "publish outbox event") {
		t.Fatalf("dispatch error = %v", err)
	}
	if result.Claimed != 2 || result.Published != 1 || result.Retried != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.published) != 2 || publisher.published[1] != "published" {
		t.Fatalf("published attempts = %#v", publisher.published)
	}
	if got := store.retryAt["failed"]; !got.Equal(now.Add(4 * time.Minute)) {
		t.Fatalf("retry at = %v, want %v", got, now.Add(4*time.Minute))
	}
	if len(store.completed) != 1 || store.completed[0] != "published@request-456" {
		t.Fatalf("completed = %#v", store.completed)
	}
}

func TestDispatcherCapsBackoffAtOneHourAndReportsRetryCASFailure(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	store := &dispatcherStoreFake{
		events:      []*models.OutboxEvent{{ID: "failed", PublishAttempts: 20}},
		completeErr: map[string]error{},
		retryErr:    map[string]error{"failed": errors.New("stale lease owner")},
	}
	publisher := &dispatcherPublisherFake{failures: map[string]error{
		"failed": errors.New("SQS unavailable"),
	}}
	dispatcher, err := NewDispatcher(store, publisher, DispatcherOptions{
		Now: func() time.Time { return now }, EventTypes: InvoiceRenderEventTypes(),
	})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	result, err := dispatcher.Dispatch(context.Background(), "request-789")

	if err == nil || !strings.Contains(err.Error(), "retry outbox event") {
		t.Fatalf("dispatch error = %v", err)
	}
	if result.Retried != 0 {
		t.Fatalf("retried = %d, want zero after CAS failure", result.Retried)
	}
	if len(store.retried) != 0 {
		t.Fatalf("retry mutation recorded despite failure: %#v", store.retried)
	}
}

func TestDispatcherRejectsInvalidDependenciesAndOwner(t *testing.T) {
	store := &dispatcherStoreFake{}
	publisher := &dispatcherPublisherFake{}
	options := DispatcherOptions{EventTypes: InvoiceRenderEventTypes()}
	if _, err := NewDispatcher(nil, publisher, options); err == nil {
		t.Fatal("nil store accepted")
	}
	if _, err := NewDispatcher(store, nil, options); err == nil {
		t.Fatal("nil publisher accepted")
	}
	if _, err := NewDispatcher(store, publisher, DispatcherOptions{}); err == nil {
		t.Fatal("empty event family accepted")
	}
	dispatcher, err := NewDispatcher(store, publisher, options)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	if _, err := dispatcher.Dispatch(context.Background(), " "); err == nil {
		t.Fatal("blank owner accepted")
	}
}
