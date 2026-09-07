package postgres

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"gorm.io/gorm"
)

type notificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) interfaces.NotificationRepository {
	return &notificationRepository{db: db}
}

func (r *notificationRepository) CreateIdempotent(ctx context.Context, notification *models.Notification) (*models.Notification, error) {
	if notification == nil {
		return nil, errors.New("notification is required")
	}
	var persisted models.Notification
	result := r.db.WithContext(ctx).Raw(`
		INSERT INTO notifications (
			business_id, user_id, source_event_key, type, title, body,
			resource_type, resource_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (business_id, user_id, source_event_key)
		DO UPDATE SET source_event_key = EXCLUDED.source_event_key
		RETURNING id, business_id, user_id, source_event_key, type, title, body,
			resource_type, resource_id, read_at, created_at, updated_at
	`, notification.BusinessID, notification.UserID, notification.SourceEventKey,
		notification.Type, notification.Title, notification.Body, notification.ResourceType,
		notification.ResourceID, notification.CreatedAt, notification.UpdatedAt,
	).Scan(&persisted)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, errors.New("notification insert returned no record")
	}
	return &persisted, nil
}

func (r *notificationRepository) List(ctx context.Context, businessID, userID string, limit int) ([]*models.Notification, error) {
	notifications := make([]*models.Notification, 0)
	err := r.db.WithContext(ctx).
		Where("business_id = ? AND user_id = ?", businessID, userID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&notifications).Error
	return notifications, err
}

func (r *notificationRepository) MarkRead(
	ctx context.Context,
	id, businessID, userID string,
	readAt time.Time,
) (*models.Notification, error) {
	var notification models.Notification
	result := r.db.WithContext(ctx).Raw(`
		UPDATE notifications
		SET read_at = ?, updated_at = ?
		WHERE id = ? AND business_id = ? AND user_id = ?
		RETURNING id, business_id, user_id, source_event_key, type, title, body,
			resource_type, resource_id, read_at, created_at, updated_at
	`, readAt, readAt, id, businessID, userID).Scan(&notification)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, interfaces.ErrNotificationNotFound
	}
	return &notification, nil
}

func (r *notificationRepository) MarkAllRead(
	ctx context.Context,
	businessID, userID string,
	readAt time.Time,
) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE notifications
		SET read_at = ?, updated_at = ?
		WHERE business_id = ? AND user_id = ? AND read_at IS NULL
	`, readAt, readAt, businessID, userID)
	return result.RowsAffected, result.Error
}
