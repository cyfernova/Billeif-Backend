package outbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
)

const (
	defaultBatchSize     = 25
	defaultLeaseDuration = 2 * time.Minute
	maxBatchSize         = 100
	maxRetryDelay        = time.Hour
)

type DispatchStore interface {
	ClaimOutboxEvents(
		ctx context.Context,
		owner string,
		now, leaseUntil time.Time,
		eventTypes []string,
		limit int,
	) ([]*models.OutboxEvent, error)
	CompleteOutboxEvent(ctx context.Context, eventID, owner string, publishedAt time.Time) error
	RetryOutboxEvent(ctx context.Context, eventID, owner string, availableAt time.Time) error
}

type EventPublisher interface {
	Publish(ctx context.Context, event *models.OutboxEvent) error
}

type DispatcherOptions struct {
	BatchSize     int
	LeaseDuration time.Duration
	EventTypes    []string
	Now           func() time.Time
}

type DispatchResult struct {
	Claimed                 int   `json:"claimed"`
	Published               int   `json:"published"`
	Retried                 int   `json:"retried"`
	OldestPendingAgeSeconds int64 `json:"oldest_pending_age_seconds"`
}

type Dispatcher struct {
	store         DispatchStore
	publisher     EventPublisher
	batchSize     int
	leaseDuration time.Duration
	eventTypes    []string
	now           func() time.Time
}

func NewDispatcher(
	store DispatchStore,
	publisher EventPublisher,
	options DispatcherOptions,
) (*Dispatcher, error) {
	if store == nil {
		return nil, errors.New("outbox dispatch store is required")
	}
	if publisher == nil {
		return nil, errors.New("outbox event publisher is required")
	}
	batchSize := options.BatchSize
	if batchSize == 0 {
		batchSize = defaultBatchSize
	}
	if batchSize < 1 || batchSize > maxBatchSize {
		return nil, fmt.Errorf("outbox batch size must be between 1 and %d", maxBatchSize)
	}
	leaseDuration := options.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = defaultLeaseDuration
	}
	if leaseDuration <= 0 {
		return nil, errors.New("outbox lease duration must be positive")
	}
	eventTypes, err := validateEventTypes(options.EventTypes)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Dispatcher{
		store:         store,
		publisher:     publisher,
		batchSize:     batchSize,
		leaseDuration: leaseDuration,
		eventTypes:    eventTypes,
		now:           now,
	}, nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, owner string) (DispatchResult, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return DispatchResult{}, errors.New("outbox lease owner is required")
	}
	now := d.now().UTC()
	events, err := d.store.ClaimOutboxEvents(
		ctx,
		owner,
		now,
		now.Add(d.leaseDuration),
		d.eventTypes,
		d.batchSize,
	)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("claim outbox events: %w", err)
	}

	result := DispatchResult{
		Claimed:                 len(events),
		OldestPendingAgeSeconds: oldestPendingAgeSeconds(events, now),
	}
	var dispatchErrors []error
	for _, event := range events {
		if event == nil {
			dispatchErrors = append(dispatchErrors, errors.New("publish outbox event: claimed event is nil"))
			continue
		}
		if err := d.publisher.Publish(ctx, event); err != nil {
			availableAt := now.Add(outboxRetryDelay(event.PublishAttempts))
			if retryErr := d.store.RetryOutboxEvent(ctx, event.ID, owner, availableAt); retryErr != nil {
				dispatchErrors = append(
					dispatchErrors,
					fmt.Errorf("retry outbox event %q: %w", event.ID, retryErr),
				)
			} else {
				result.Retried++
			}
			dispatchErrors = append(
				dispatchErrors,
				fmt.Errorf("publish outbox event %q: %w", event.ID, err),
			)
			continue
		}
		if err := d.store.CompleteOutboxEvent(ctx, event.ID, owner, d.now().UTC()); err != nil {
			dispatchErrors = append(
				dispatchErrors,
				fmt.Errorf("complete outbox event %q: %w", event.ID, err),
			)
			continue
		}
		result.Published++
	}
	return result, errors.Join(dispatchErrors...)
}

func oldestPendingAgeSeconds(events []*models.OutboxEvent, now time.Time) int64 {
	var oldestAge time.Duration
	for _, event := range events {
		if event == nil || event.CreatedAt.IsZero() {
			continue
		}
		age := now.Sub(event.CreatedAt.UTC())
		if age > oldestAge {
			oldestAge = age
		}
	}
	return int64(oldestAge / time.Second)
}

func InvoiceRenderEventTypes() []string {
	return []string{invoicePreviewRequestedEvent, invoiceIssuedEvent}
}

func InvoiceDispatchEventTypes() []string {
	return []string{
		invoicePreviewRequestedEvent,
		invoiceIssuedEvent,
		invoiceDeliveryRequestedEvent,
	}
}

func validateEventTypes(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("outbox event types are required")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("outbox event type must not be blank")
		}
		if _, exists := seen[value]; exists {
			return nil, errors.New("outbox event types must be unique")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func outboxRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Minute
	for attempt := 1; attempt < attempts; attempt++ {
		if delay >= maxRetryDelay/2 {
			return maxRetryDelay
		}
		delay *= 2
	}
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}
