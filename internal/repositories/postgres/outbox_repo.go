package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"invoice-backend/internal/models"
)

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
		Where("id = ? AND published_at IS NULL", eventID).
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
