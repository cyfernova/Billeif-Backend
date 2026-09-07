package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

const (
	defaultNotificationListLimit = 100
	maxNotificationListLimit     = 200
)

var (
	ErrNotificationScopeRequired = errors.New("notification business and user scope are required")
	ErrNotificationInvalid       = errors.New("notification input is invalid")
)

var supportedNotificationTypes = map[string]struct{}{
	"invoice":  {},
	"payment":  {},
	"order":    {},
	"workflow": {},
	"agent":    {},
	"system":   {},
}

type NotificationInput struct {
	BusinessID     string
	UserID         string
	SourceEventKey string
	Type           string
	Title          string
	Body           string
	ResourceType   string
	ResourceID     string
}

type NotificationServiceOptions struct {
	Now func() time.Time
}

type NotificationService struct {
	repository interfaces.NotificationRepository
	now        func() time.Time
}

func NewNotificationService(repository interfaces.NotificationRepository, options NotificationServiceOptions) *NotificationService {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &NotificationService{repository: repository, now: now}
}

func (s *NotificationService) Ingest(ctx context.Context, input NotificationInput) (*models.Notification, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("notification service is unavailable")
	}
	input.BusinessID = strings.TrimSpace(input.BusinessID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.SourceEventKey = strings.TrimSpace(input.SourceEventKey)
	input.Type = strings.TrimSpace(input.Type)
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	if input.BusinessID == "" || input.UserID == "" {
		return nil, ErrNotificationScopeRequired
	}
	if input.SourceEventKey == "" || input.Title == "" || input.Body == "" {
		return nil, ErrNotificationInvalid
	}
	if _, ok := supportedNotificationTypes[input.Type]; !ok {
		return nil, ErrNotificationInvalid
	}
	if len(input.SourceEventKey) > 255 || len(input.Title) > 255 || len(input.ResourceType) > 64 || len(input.ResourceID) > 255 {
		return nil, ErrNotificationInvalid
	}

	now := s.now().UTC()
	created, err := s.repository.CreateIdempotent(ctx, &models.Notification{
		BusinessID: input.BusinessID, UserID: input.UserID, SourceEventKey: input.SourceEventKey,
		Type: input.Type, Title: input.Title, Body: input.Body,
		ResourceType: input.ResourceType, ResourceID: input.ResourceID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return nil, fmt.Errorf("persist notification: %w", err)
	}
	return created, nil
}

func (s *NotificationService) List(ctx context.Context, businessID, userID string, limit int) ([]*models.Notification, error) {
	businessID, userID, err := validateNotificationScope(businessID, userID)
	if err != nil {
		return nil, err
	}
	if s == nil || s.repository == nil {
		return nil, errors.New("notification service is unavailable")
	}
	if limit <= 0 {
		limit = defaultNotificationListLimit
	}
	if limit > maxNotificationListLimit {
		limit = maxNotificationListLimit
	}
	return s.repository.List(ctx, businessID, userID, limit)
}

func (s *NotificationService) MarkRead(ctx context.Context, id, businessID, userID string) (*models.Notification, error) {
	businessID, userID, err := validateNotificationScope(businessID, userID)
	if err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, interfaces.ErrNotificationNotFound
	}
	if s == nil || s.repository == nil {
		return nil, errors.New("notification service is unavailable")
	}
	notification, err := s.repository.MarkRead(ctx, id, businessID, userID, s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("mark notification read: %w", err)
	}
	return notification, nil
}

func (s *NotificationService) MarkAllRead(ctx context.Context, businessID, userID string) (int64, error) {
	businessID, userID, err := validateNotificationScope(businessID, userID)
	if err != nil {
		return 0, err
	}
	if s == nil || s.repository == nil {
		return 0, errors.New("notification service is unavailable")
	}
	updated, err := s.repository.MarkAllRead(ctx, businessID, userID, s.now().UTC())
	if err != nil {
		return 0, fmt.Errorf("mark notifications read: %w", err)
	}
	return updated, nil
}

func validateNotificationScope(businessID, userID string) (string, string, error) {
	businessID = strings.TrimSpace(businessID)
	userID = strings.TrimSpace(userID)
	if businessID == "" || userID == "" {
		return "", "", ErrNotificationScopeRequired
	}
	return businessID, userID, nil
}
