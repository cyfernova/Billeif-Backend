package interfaces

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
)

var ErrWebSocketTicketInvalid = errors.New("websocket ticket is invalid or expired")

type WebSocketTicketRepository interface {
	Create(ctx context.Context, ticket *models.WebSocketTicket) error
	ConsumeByDigest(ctx context.Context, digest string, consumedAt time.Time) (*models.WebSocketTicket, error)
}
