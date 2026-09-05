package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InvoiceActorRepository interface {
	GetByCognitoID(context.Context, string) (*models.User, error)
	GetByID(context.Context, string) (*models.User, error)
}

func WithInvoiceActorRepository(repository InvoiceActorRepository) InvoiceServiceOption {
	return func(service *InvoiceService) { service.actorUsers = repository }
}

// Invoice audit rows reference users.id; authorization continues to use the
// verified subject in the original request context.
func (s *InvoiceService) invoiceActor(ctx context.Context, operation string) (ActorContext, error) {
	actor := actorFromContext(ctx)
	subject := strings.TrimSpace(actor.UserID)
	if subject == "" {
		return ActorContext{}, fmt.Errorf("invoice %s actor is required", operation)
	}
	if s.actorUsers != nil {
		user, err := s.actorUsers.GetByCognitoID(ctx, subject)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if rawSubject, ok := rawCognitoSubject(subject); ok {
				user, err = s.actorUsers.GetByCognitoID(ctx, rawSubject)
			}
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if _, parseErr := uuid.Parse(subject); parseErr == nil {
				user, err = s.actorUsers.GetByID(ctx, subject)
			}
		}
		if err != nil {
			return ActorContext{}, fmt.Errorf("resolve invoice audit actor: %w", err)
		}
		if user == nil {
			return ActorContext{}, fmt.Errorf("invoice audit actor not found")
		}
		actor.UserID = user.ID
	}
	id, err := uuid.Parse(strings.TrimSpace(actor.UserID))
	if err != nil {
		return ActorContext{}, fmt.Errorf("invoice %s actor is required", operation)
	}
	actor.UserID = id.String()
	return actor, nil
}
