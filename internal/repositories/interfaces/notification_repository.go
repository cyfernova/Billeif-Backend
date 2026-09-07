package interfaces

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
)

var ErrNotificationNotFound = errors.New("notification not found")

type NotificationRepository interface {
	CreateIdempotent(ctx context.Context, notification *models.Notification) (*models.Notification, error)
	List(ctx context.Context, businessID, userID string, limit int) ([]*models.Notification, error)
	MarkRead(ctx context.Context, id, businessID, userID string, readAt time.Time) (*models.Notification, error)
	MarkAllRead(ctx context.Context, businessID, userID string, readAt time.Time) (int64, error)
}
