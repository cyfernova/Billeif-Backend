package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"

	"gorm.io/gorm"
)

type OutboxRepository struct {
	db *gorm.DB
}

func NewOutboxRepository(db *gorm.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

func (r *OutboxRepository) ClaimOutboxEvents(
	ctx context.Context,
	owner string,
	now, leaseUntil time.Time,
	limit int,
) ([]*models.OutboxEvent, error) {
	owner = strings.TrimSpace(owner)
	if r == nil || r.db == nil {
		return nil, errors.New("outbox repository database is required")
	}
	if owner == "" || len(owner) > 255 {
		return nil, errors.New("outbox lease owner is required and must not exceed 255 characters")
	}
	if now.IsZero() || !leaseUntil.After(now) {
		return nil, errors.New("outbox claim times are invalid")
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("outbox claim limit must be between 1 and 100")
	}

	const query = `
WITH ready AS (
	SELECT id
	FROM outbox_events
	WHERE published_at IS NULL
		AND available_at <= ?
		AND (lease_expires_at IS NULL OR lease_expires_at <= ?)
	ORDER BY available_at, created_at, id
	LIMIT ?
	FOR UPDATE SKIP LOCKED
)
UPDATE outbox_events AS events
SET lease_owner = ?,
	lease_expires_at = ?,
	publish_attempts = events.publish_attempts + 1
FROM ready
WHERE events.id = ready.id
RETURNING events.*`

	var events []*models.OutboxEvent
	if err := r.db.WithContext(ctx).
		Raw(query, now, now, limit, owner, leaseUntil).
		Scan(&events).Error; err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	return events, nil
}

func (r *OutboxRepository) CompleteOutboxEvent(
	ctx context.Context,
	eventID, owner string,
	publishedAt time.Time,
) error {
	if err := validateOutboxLeaseMutation(r, eventID, owner, publishedAt); err != nil {
		return err
	}
	result := r.db.WithContext(ctx).
		Model(&models.OutboxEvent{}).
		Where("id = ? AND published_at IS NULL AND lease_owner = ?", eventID, owner).
		Updates(map[string]interface{}{
			"published_at":     publishedAt,
			"lease_owner":      nil,
			"lease_expires_at": nil,
		})
	if result.Error != nil {
		return fmt.Errorf("complete outbox event: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.New("outbox event lease owner did not match")
	}
	return nil
}

func (r *OutboxRepository) RetryOutboxEvent(
	ctx context.Context,
	eventID, owner string,
	availableAt time.Time,
) error {
	if err := validateOutboxLeaseMutation(r, eventID, owner, availableAt); err != nil {
		return err
	}
	result := r.db.WithContext(ctx).
		Model(&models.OutboxEvent{}).
		Where("id = ? AND published_at IS NULL AND lease_owner = ?", eventID, owner).
		Updates(map[string]interface{}{
			"available_at":     availableAt,
			"lease_owner":      nil,
			"lease_expires_at": nil,
		})
	if result.Error != nil {
		return fmt.Errorf("retry outbox event: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.New("outbox event lease owner did not match")
	}
	return nil
}

func validateOutboxLeaseMutation(
	repository *OutboxRepository,
	eventID, owner string,
	at time.Time,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("outbox repository database is required")
	}
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(owner) == "" || at.IsZero() {
		return errors.New("outbox event id, lease owner, and mutation time are required")
	}
	return nil
}

func (r *invoiceRepository) MarkOutboxPublished(
	ctx context.Context,
	eventID string,
	publishedAt time.Time,
) error {
	if eventID == "" || publishedAt.IsZero() {
		return errors.New("outbox event id and publication time are required")
	}
	result := r.db.WithContext(ctx).
		Model(&models.OutboxEvent{}).
		Where(
			"id = ? AND published_at IS NULL AND lease_owner IS NULL AND lease_expires_at IS NULL",
			eventID,
		).
		Updates(map[string]interface{}{
			"published_at":     publishedAt,
			"lease_owner":      nil,
			"lease_expires_at": nil,
		})
	if result.Error != nil {
		return fmt.Errorf("mark outbox published: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.New("outbox event was not pending")
	}
	return nil
}
