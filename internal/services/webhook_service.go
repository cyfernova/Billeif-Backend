package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type WebhookService struct {
	repo interfaces.WebhookRepository
	log  *logger.Logger
}

func NewWebhookService(repo interfaces.WebhookRepository, log *logger.Logger) *WebhookService {
	return &WebhookService{repo: repo, log: log}
}

type CreateWebhookInput struct {
	BusinessID string   `json:"business_id,omitempty"`
	Name       string   `json:"name" binding:"required"`
	URL        string   `json:"url" binding:"required,url"`
	Events     []string `json:"events" binding:"required,min=1"`
	Secret     string   `json:"secret"`
}

func (s *WebhookService) Create(ctx context.Context, input CreateWebhookInput) (*models.Webhook, error) {
	log := logger.FromContext(ctx).With("service", "webhook", "operation", "create", "business_id", input.BusinessID)

	if err := validateWebhookURL(ctx, input.URL); err != nil {
		return nil, fmt.Errorf("invalid webhook URL: %w", err)
	}

	secret := input.Secret
	if secret == "" {
		secret = generateWebhookSecret()
	}
	webhook := &models.Webhook{
		BusinessID: input.BusinessID,
		Name:       input.Name,
		URL:        input.URL,
		Events:     strings.Join(input.Events, ","),
		Secret:     secret,
		IsActive:   true,
	}

	if err := s.repo.Create(ctx, webhook); err != nil {
		log.Error("failed to create webhook", "error", err)
		return nil, fmt.Errorf("failed to create webhook: %w", err)
	}

	log.Info("webhook created", "webhook_id", webhook.ID)
	return webhook, nil
}

func (s *WebhookService) CreateByBusiness(ctx context.Context, businessID string, input CreateWebhookInput) (*models.Webhook, error) {
	input.BusinessID = businessID
	return s.Create(ctx, input)
}

func (s *WebhookService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Webhook, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

func (s *WebhookService) List(ctx context.Context, businessID string) ([]*models.Webhook, error) {
	log := logger.FromContext(ctx).With("service", "webhook", "operation", "list", "business_id", businessID)
	webhooks, err := s.repo.GetByBusinessID(ctx, businessID)
	if err != nil {
		log.Error("failed to list webhooks", "error", err)
		return nil, err
	}
	log.Debug("listed webhooks", "count", len(webhooks))
	return webhooks, nil
}

type UpdateWebhookInput struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Events   []string `json:"events"`
	Secret   string   `json:"secret"`
	IsActive *bool    `json:"is_active"`
}

func (s *WebhookService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateWebhookInput) (*models.Webhook, error) {
	log := logger.FromContext(ctx).With("service", "webhook", "operation", "update", "webhook_id", id, "business_id", businessID)
	webhook, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load webhook for scoped update", "error", err)
		return nil, err
	}

	if input.Name != "" {
		webhook.Name = input.Name
	}
	if input.URL != "" {
		if err := validateWebhookURL(ctx, input.URL); err != nil {
			return nil, fmt.Errorf("invalid webhook URL: %w", err)
		}
		webhook.URL = input.URL
	}
	if len(input.Events) > 0 {
		webhook.Events = strings.Join(input.Events, ",")
	}
	if input.Secret != "" {
		webhook.Secret = input.Secret
	}
	if input.IsActive != nil {
		webhook.IsActive = *input.IsActive
	}

	if err := s.repo.Update(ctx, webhook); err != nil {
		log.Error("failed to update webhook", "error", err)
		return nil, err
	}
	log.Info("webhook updated", "webhook_id", webhook.ID)
	return webhook, nil
}

func (s *WebhookService) Delete(ctx context.Context, id string) error {
	log := logger.FromContext(ctx).With("service", "webhook", "operation", "delete", "webhook_id", id)
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Error("failed to delete webhook", "error", err)
		return err
	}
	log.Info("webhook deleted", "webhook_id", id)
	return nil
}

func (s *WebhookService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	log := logger.FromContext(ctx).With("service", "webhook", "operation", "delete", "webhook_id", id, "business_id", businessID)
	webhook, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load webhook for scoped delete", "error", err)
		return err
	}
	if err := s.repo.Delete(ctx, webhook.ID); err != nil {
		log.Error("failed to delete webhook", "error", err)
		return err
	}
	log.Info("webhook deleted", "webhook_id", webhook.ID)
	return nil
}

func generateWebhookSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("wh_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
