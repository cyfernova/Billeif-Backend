package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/sesfeedback"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SESFeedbackRepository struct {
	db *gorm.DB
}

func NewSESFeedbackRepository(db *gorm.DB) *SESFeedbackRepository {
	return &SESFeedbackRepository{db: db}
}

func (r *SESFeedbackRepository) ApplyFeedback(
	ctx context.Context,
	event sesfeedback.FeedbackEvent,
) (sesfeedback.ApplyResult, error) {
	if r == nil || r.db == nil {
		return sesfeedback.ApplyResult{}, errors.New("SES feedback repository is required")
	}
	if _, err := uuid.Parse(event.DeliveryID); err != nil {
		return sesfeedback.ApplyResult{}, fmt.Errorf("%w: delivery id is invalid", sesfeedback.ErrInvalidEvent)
	}
	if _, err := uuid.Parse(event.BusinessID); err != nil {
		return sesfeedback.ApplyResult{}, fmt.Errorf("%w: business id is invalid", sesfeedback.ErrInvalidEvent)
	}
	if strings.TrimSpace(event.ProviderMessageID) == "" || event.OccurredAt.IsZero() {
		return sesfeedback.ApplyResult{}, fmt.Errorf("%w: provider correlation is incomplete", sesfeedback.ErrInvalidEvent)
	}

	var applied sesfeedback.ApplyResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var (
			current     string
			deliveredAt sql.NullTime
			failedAt    sql.NullTime
		)
		row := tx.Raw(`
SELECT status, delivered_at, failed_at
FROM email_deliveries
WHERE id = ?
	AND business_id = ?
	AND provider_message_id = ?
	AND deleted_at IS NULL
FOR UPDATE`,
			event.DeliveryID,
			event.BusinessID,
			event.ProviderMessageID,
		).Row()
		if err := row.Scan(&current, &deliveredAt, &failedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sesfeedback.ErrFeedbackUncorrelated
			}
			return fmt.Errorf("lock correlated email delivery: %w", err)
		}

		next, statusChanged, err := sesfeedback.ResolveTransition(current, event.Type)
		if err != nil {
			return err
		}

		timestampColumn := "failed_at"
		existingTimestamp := failedAt
		if event.Type == sesfeedback.EventTypeDelivery {
			timestampColumn = "delivered_at"
			existingTimestamp = deliveredAt
		}
		timestampChanged := !existingTimestamp.Valid ||
			event.OccurredAt.Before(existingTimestamp.Time)
		applied = sesfeedback.ApplyResult{
			Status:  next,
			Changed: statusChanged || timestampChanged,
		}
		if !applied.Changed {
			return nil
		}
		timestamp := earliestTime(existingTimestamp, event.OccurredAt)

		update := fmt.Sprintf(`
UPDATE email_deliveries
SET status = ?,
	%s = ?,
	updated_at = NOW()
WHERE id = ?
	AND business_id = ?
	AND provider_message_id = ?
	AND deleted_at IS NULL`, timestampColumn)
		result := tx.Exec(
			update,
			next,
			timestamp,
			event.DeliveryID,
			event.BusinessID,
			event.ProviderMessageID,
		)
		if result.Error != nil {
			return fmt.Errorf("apply SES feedback: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return sesfeedback.ErrFeedbackUncorrelated
		}
		return nil
	})
	if err != nil {
		return sesfeedback.ApplyResult{}, err
	}
	return applied, nil
}

func earliestTime(existing sql.NullTime, candidate time.Time) time.Time {
	if existing.Valid && existing.Time.Before(candidate) {
		return existing.Time
	}
	return candidate
}
