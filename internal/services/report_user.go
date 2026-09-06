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

type ReportUserRepository interface {
	GetByCognitoID(context.Context, string) (*models.User, error)
	GetByID(context.Context, string) (*models.User, error)
}

func (s *ReportService) WithUserRepository(users ReportUserRepository) *ReportService {
	s.users = users
	return s
}

// Report foreign keys use users.id. Keep the verified subject unchanged for
// authorization, capability checks, and report scope evaluation.
func (s *ReportService) reportUserID(ctx context.Context, subject string) (string, error) {
	return resolveDatabaseUserID(ctx, s.users, subject)
}

func resolveDatabaseUserID(ctx context.Context, users ReportUserRepository, subject string) (string, error) {
	if users == nil {
		return subject, nil
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "", fmt.Errorf("report user is required")
	}
	user, err := users.GetByCognitoID(ctx, subject)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if raw, ok := rawCognitoSubject(subject); ok {
			user, err = users.GetByCognitoID(ctx, raw)
		}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if _, parseErr := uuid.Parse(subject); parseErr == nil {
			user, err = users.GetByID(ctx, subject)
		}
	}
	if err != nil {
		return "", fmt.Errorf("resolve report user: %w", err)
	}
	if user == nil {
		return "", fmt.Errorf("report user not found")
	}
	id, err := uuid.Parse(user.ID)
	if err != nil {
		return "", fmt.Errorf("invalid report user identity: %w", err)
	}
	return id.String(), nil
}
