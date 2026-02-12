package services

import (
	"context"
	"fmt"
	"strings"

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
	BusinessID string   `json:"business_id" binding:"required,uuid"`
	Name       string   `json:"name" binding:"required"`
	URL        string   `json:"url" binding:"required,url"`
	Events     []string `json:"events" binding:"required,min=1"`
	Secret     string   `json:"secret" binding:"required"`
}

func (s *WebhookService) Create(ctx context.Context, input CreateWebhookInput) (*models.Webhook, error) {
	log := logger.FromContext(ctx).With("service", "webhook", "operation", "create", "business_id", input.BusinessID)
	webhook := &models.Webhook{
		BusinessID: input.BusinessID,
		Name:       input.Name,
		URL:        input.URL,
		Events:     strings.Join(input.Events, ","),
		Secret:     input.Secret,
		IsActive:   true,
	}

	if err := s.repo.Create(ctx, webhook); err != nil {
		log.Error("failed to create webhook", "error", err)
		return nil, fmt.Errorf("failed to create webhook: %w", err)
	}

	log.Info("webhook created", "webhook_id", webhook.ID)
	return webhook, nil
}

func (s *WebhookService) Get(ctx context.Context, id string) (*models.Webhook, error) {
	log := logger.FromContext(ctx).With("service", "webhook", "operation", "get", "webhook_id", id)
	webhook, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to get webhook", "error", err)
		return nil, err
	}
	return webhook, nil
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

func (s *WebhookService) Update(ctx context.Context, id string, input UpdateWebhookInput) (*models.Webhook, error) {
	log := logger.FromContext(ctx).With("service", "webhook", "operation", "update", "webhook_id", id)
	webhook, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to load webhook for update", "error", err)
		return nil, err
	}

	if input.Name != "" {
		webhook.Name = input.Name
	}
	if input.URL != "" {
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
