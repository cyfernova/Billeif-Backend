package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type WebhookService struct {
	repo       interfaces.WebhookRepository
	log        *logger.Logger
	httpClient *http.Client
}

var webhookSecretReader io.Reader = rand.Reader

func NewWebhookService(repo interfaces.WebhookRepository, log *logger.Logger) *WebhookService {
	return &WebhookService{
		repo:       repo,
		log:        log,
		httpClient: newWebhookDeliveryHTTPClient(10 * time.Second),
	}
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
		generated, err := generateWebhookSecret()
		if err != nil {
			return nil, fmt.Errorf("generate webhook secret: %w", err)
		}
		secret = generated
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

func (s *WebhookService) EmitEvent(ctx context.Context, businessID, event string, payload map[string]interface{}) error {
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(event) == "" {
		return nil
	}
	webhooks, err := s.repo.GetByBusinessID(ctx, businessID)
	if err != nil {
		return err
	}

	envelope := map[string]interface{}{
		"event":       event,
		"business_id": businessID,
		"sent_at":     time.Now().UTC().Format(time.RFC3339Nano),
		"data":        payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}

	client := s.httpClient
	if client == nil {
		client = newWebhookDeliveryHTTPClient(10 * time.Second)
	}
	for _, webhook := range webhooks {
		if webhook == nil || !webhook.IsActive || !webhookSubscribedToEvent(webhook.Events, event) {
			continue
		}
		if err := validateWebhookURL(ctx, webhook.URL); err != nil {
			s.log.Warn("unsafe stored webhook URL rejected", "webhook_id", webhook.ID, "event", event, "error", err)
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, strings.NewReader(string(body)))
		if err != nil {
			s.log.Warn("failed to build webhook request", "webhook_id", webhook.ID, "event", event, "error", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Cyfernova-Event", event)
		req.Header.Set("X-Cyfernova-Signature", signWebhookPayload(webhook.Secret, body))

		resp, err := client.Do(req)
		if err != nil {
			s.log.Warn("failed to deliver webhook", "webhook_id", webhook.ID, "event", event, "error", err)
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			now := time.Now().UTC()
			webhook.LastTriggered = &now
			if updateErr := s.repo.Update(ctx, webhook); updateErr != nil {
				s.log.Warn("failed to update webhook delivery timestamp", "webhook_id", webhook.ID, "event", event, "error", updateErr)
			}
			continue
		}
		s.log.Warn("webhook delivery returned non-success", "webhook_id", webhook.ID, "event", event, "status_code", resp.StatusCode)
	}

	return nil
}

func webhookSubscribedToEvent(events, event string) bool {
	if strings.TrimSpace(events) == "" {
		return false
	}
	for _, candidate := range strings.Split(events, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.EqualFold(candidate, event) {
			return true
		}
	}
	return false
}

func signWebhookPayload(secret string, payload []byte) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func generateWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(webhookSecretReader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
