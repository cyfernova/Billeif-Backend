package postgres

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"gorm.io/gorm"
)

type websocketTicketRepository struct {
	db *gorm.DB
}

func NewWebSocketTicketRepository(db *gorm.DB) interfaces.WebSocketTicketRepository {
	return &websocketTicketRepository{db: db}
}

func (r *websocketTicketRepository) Create(ctx context.Context, ticket *models.WebSocketTicket) error {
	if ticket == nil {
		return errors.New("websocket ticket is required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", ticket.CreatedAt).Delete(&models.WebSocketTicket{}).Error; err != nil {
			return err
		}
		return tx.Create(ticket).Error
	})
}

func (r *websocketTicketRepository) ConsumeByDigest(
	ctx context.Context,
	digest string,
	consumedAt time.Time,
) (*models.WebSocketTicket, error) {
	var ticket models.WebSocketTicket
	result := r.db.WithContext(ctx).Raw(`
		UPDATE websocket_tickets
		SET consumed_at = ?, updated_at = ?
		WHERE ticket_digest = ?
		  AND consumed_at IS NULL
		  AND expires_at > ?
		RETURNING id, ticket_digest, subject, business_id, expires_at, consumed_at, created_at, updated_at
	`, consumedAt, consumedAt, digest, consumedAt).Scan(&ticket)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, interfaces.ErrWebSocketTicketInvalid
	}
	return &ticket, nil
}
