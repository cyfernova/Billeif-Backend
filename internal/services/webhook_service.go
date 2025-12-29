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
	webhook := &models.Webhook{
		BusinessID: input.BusinessID,
		Name:       input.Name,
		URL:        input.URL,
		Events:     strings.Join(input.Events, ","),
		Secret:     input.Secret,
		IsActive:   true,
	}

	if err := s.repo.Create(ctx, webhook); err != nil {
		return nil, fmt.Errorf("failed to create webhook: %w", err)
	}

	return webhook, nil
}

func (s *WebhookService) Get(ctx context.Context, id string) (*models.Webhook, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *WebhookService) List(ctx context.Context, businessID string) ([]*models.Webhook, error) {
	return s.repo.GetByBusinessID(ctx, businessID)
}

type UpdateWebhookInput struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Events   []string `json:"events"`
	Secret   string   `json:"secret"`
	IsActive *bool    `json:"is_active"`
}

func (s *WebhookService) Update(ctx context.Context, id string, input UpdateWebhookInput) (*models.Webhook, error) {
	webhook, err := s.repo.GetByID(ctx, id)
	if err != nil {
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
		return nil, err
	}

	return webhook, nil
}

func (s *WebhookService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
