package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// A2APushService handles push notification delivery for A2A tasks
type A2APushService struct {
	db         *gorm.DB
	ap2Repo    interfaces.AP2Repository
	log        *logger.Logger
	httpClient *http.Client
	maxRetries int
}

// PushConfig represents a push notification configuration stored in the database
type PushConfig struct {
	ID             string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	AgentID        string     `gorm:"not null;index" json:"agent_id"`
	WebhookURL     string     `gorm:"column:webhook_url;not null" json:"webhook_url"`
	Secret         string     `gorm:"column:secret" json:"-"`
	Headers        string     `gorm:"type:jsonb;default:'{}'" json:"-"`
	Events         []string   `gorm:"-" json:"events"`
	EventsJSON     string     `gorm:"column:events;type:text[]" json:"-"`
	Authentication string     `gorm:"type:jsonb;default:'{}'" json:"-"`
	IsActive       bool       `gorm:"default:true" json:"is_active"`
	FailureCount   int        `gorm:"default:0" json:"failure_count"`
	LastFailureAt  *time.Time `json:"last_failure_at,omitempty"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName returns the table name for GORM
func (PushConfig) TableName() string {
	return "a2a_push_configs"
}

// PushNotification represents a push notification to be delivered
type PushNotification struct {
	ID        string                 `json:"id"`
	Event     string                 `json:"event"`
	Timestamp time.Time              `json:"timestamp"`
	Data      interface{}            `json:"data"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

var (
	ErrPushConfigNotFound = errors.New("push config not found")
	ErrUnsafeWebhookURL   = errors.New("unsafe webhook URL")
)

// NewA2APushService creates a new push notification service
func NewA2APushService(db *gorm.DB, ap2Repo interfaces.AP2Repository, log *logger.Logger) *A2APushService {
	return &A2APushService{
		db:      db,
		ap2Repo: ap2Repo,
		log:     log,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		maxRetries: 3,
	}
}

// ConfigurePush creates or updates a push notification configuration
func (s *A2APushService) ConfigurePush(ctx context.Context, agentID string, config *a2a.PushNotificationConfig) (*PushConfig, error) {
	// Validate webhook URL
	if config.URL == "" {
		return nil, fmt.Errorf("webhook URL is required")
	}
	if err := validateWebhookURL(ctx, config.URL); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafeWebhookURL, err)
	}

	// Marshal headers and auth
	headersJSON, _ := json.Marshal(config.Headers)
	authJSON, _ := json.Marshal(config.Authentication)

	pushConfig := &PushConfig{
		ID:             uuid.New().String(),
		AgentID:        agentID,
		WebhookURL:     config.URL,
		Headers:        string(headersJSON),
		Authentication: string(authJSON),
		IsActive:       true,
	}

	// Generate secret for webhook signature
	pushConfig.Secret = generateSecret()

	// Save to database
	result := s.db.WithContext(ctx).Create(pushConfig)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to create push config: %w", result.Error)
	}

	s.log.Info("push config created", "config_id", pushConfig.ID, "agent_id", agentID)

	return pushConfig, nil
}

// GetPushConfig retrieves a push configuration
func (s *A2APushService) GetPushConfig(ctx context.Context, configID string) (*PushConfig, error) {
	var config PushConfig
	result := s.db.WithContext(ctx).Where("id = ?", configID).First(&config)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, ErrPushConfigNotFound
		}
		return nil, result.Error
	}
	return &config, nil
}

// GetPushConfigsByAgent retrieves all push configurations for an agent
func (s *A2APushService) GetPushConfigsByAgent(ctx context.Context, agentID string) ([]*PushConfig, error) {
	var configs []*PushConfig
	result := s.db.WithContext(ctx).Where("agent_id = ? AND is_active = ?", agentID, true).Find(&configs)
	if result.Error != nil {
		return nil, result.Error
	}
	return configs, nil
}

// DeletePushConfig deletes a push configuration
func (s *A2APushService) DeletePushConfig(ctx context.Context, configID string) error {
	result := s.db.WithContext(ctx).Delete(&PushConfig{}, "id = ?", configID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrPushConfigNotFound
	}
	s.log.Info("push config deleted", "config_id", configID)
	return nil
}

func (s *A2APushService) ConfigurePushForScope(ctx context.Context, agentID, userID, businessID string, config *a2a.PushNotificationConfig) (*PushConfig, error) {
	if err := s.ensureAgentAccess(ctx, agentID, userID, businessID); err != nil {
		return nil, err
	}
	return s.ConfigurePush(ctx, agentID, config)
}

func (s *A2APushService) GetPushConfigsByAgentForScope(ctx context.Context, agentID, userID, businessID string) ([]*PushConfig, error) {
	if err := s.ensureAgentAccess(ctx, agentID, userID, businessID); err != nil {
		return nil, err
	}
	return s.GetPushConfigsByAgent(ctx, agentID)
}

func (s *A2APushService) GetPushConfigForScope(ctx context.Context, configID, userID, businessID string) (*PushConfig, error) {
	config, err := s.GetPushConfig(ctx, configID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureAgentAccess(ctx, config.AgentID, userID, businessID); err != nil {
		return nil, err
	}
	return config, nil
}

func (s *A2APushService) DeletePushConfigForScope(ctx context.Context, configID, userID, businessID string) error {
	config, err := s.GetPushConfig(ctx, configID)
	if err != nil {
		return err
	}
	if err := s.ensureAgentAccess(ctx, config.AgentID, userID, businessID); err != nil {
		return err
	}
	return s.DeletePushConfig(ctx, configID)
}

// SendNotification sends a push notification to a webhook
func (s *A2APushService) SendNotification(ctx context.Context, config *a2a.PushNotificationConfig, event string, data interface{}) error {
	if err := validateWebhookURL(ctx, config.URL); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeWebhookURL, err)
	}

	notification := PushNotification{
		ID:        uuid.New().String(),
		Event:     event,
		Timestamp: time.Now(),
		Data:      data,
	}

	// Check if this event type should be sent
	if len(config.Events) > 0 {
		shouldSend := false
		for _, e := range config.Events {
			if e == event || e == "*" {
				shouldSend = true
				break
			}
		}
		if !shouldSend {
			return nil
		}
	}

	// Serialize notification
	body, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-A2A-Event", event)
	req.Header.Set("X-A2A-Notification-ID", notification.ID)
	req.Header.Set("X-A2A-Timestamp", notification.Timestamp.Format(time.RFC3339))

	// Add custom headers
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}

	// Add authentication
	if config.Authentication != nil {
		s.addAuthentication(req, config.Authentication)
	}

	// Send with retry
	var lastErr error
	for i := 0; i < s.maxRetries; i++ {
		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err
			s.log.Warn("push notification failed, retrying", "attempt", i+1, "error", err)
			time.Sleep(time.Duration(1<<i) * time.Second) // Exponential backoff
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			s.log.Info("push notification sent successfully", "notification_id", notification.ID, "event", event)
			return nil
		}

		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		s.log.Warn("push notification returned error status", "attempt", i+1, "status", resp.StatusCode)
		time.Sleep(time.Duration(1<<i) * time.Second)
	}

	return fmt.Errorf("push notification failed after %d attempts: %w", s.maxRetries, lastErr)
}

// SendNotificationToAgent sends notifications to all configured webhooks for an agent
func (s *A2APushService) SendNotificationToAgent(ctx context.Context, agentID, event string, data interface{}) error {
	configs, err := s.GetPushConfigsByAgent(ctx, agentID)
	if err != nil {
		return err
	}

	for _, config := range configs {
		// Parse headers
		var headers map[string]string
		if err := json.Unmarshal([]byte(config.Headers), &headers); err != nil {
			s.log.Warn("failed to parse push headers", "config_id", config.ID, "error", err)
			continue
		}

		// Parse authentication
		var auth a2a.AuthConfig
		if err := json.Unmarshal([]byte(config.Authentication), &auth); err != nil {
			s.log.Warn("failed to parse push authentication", "config_id", config.ID, "error", err)
			continue
		}

		pushConfig := &a2a.PushNotificationConfig{
			URL:            config.WebhookURL,
			Headers:        headers,
			Authentication: &auth,
		}

		go func(cfg *a2a.PushNotificationConfig, configID string) {
			err := s.SendNotification(ctx, cfg, event, data)
			if err != nil {
				s.log.Error("failed to send push notification", "config_id", configID, "error", err)
				s.recordFailure(ctx, configID)
			} else {
				s.recordSuccess(ctx, configID)
			}
		}(pushConfig, config.ID)
	}

	return nil
}

// addAuthentication adds authentication to a request
func (s *A2APushService) addAuthentication(req *http.Request, auth *a2a.AuthConfig) {
	switch auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "api_key":
		header := auth.Header
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, auth.Token)
	case "basic":
		if username, ok := auth.Credentials["username"]; ok {
			if password, ok := auth.Credentials["password"]; ok {
				req.SetBasicAuth(username, password)
			}
		}
	}
}

// recordFailure records a failed notification attempt
func (s *A2APushService) recordFailure(ctx context.Context, configID string) {
	now := time.Now()
	s.db.WithContext(ctx).Model(&PushConfig{}).
		Where("id = ?", configID).
		Updates(map[string]interface{}{
			"failure_count":   gorm.Expr("failure_count + 1"),
			"last_failure_at": now,
		})
}

// recordSuccess records a successful notification
func (s *A2APushService) recordSuccess(ctx context.Context, configID string) {
	now := time.Now()
	s.db.WithContext(ctx).Model(&PushConfig{}).
		Where("id = ?", configID).
		Updates(map[string]interface{}{
			"failure_count":   0,
			"last_success_at": now,
		})
}

// generateSecret generates a random secret for webhook signatures
func generateSecret() string {
	id := uuid.New()
	hash := sha256.Sum256([]byte(id.String() + time.Now().String()))
	return hex.EncodeToString(hash[:])
}

// SignPayload signs a payload with HMAC-SHA256
func SignPayload(payload []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// VerifySignature verifies an HMAC-SHA256 signature
func VerifySignature(payload []byte, signature, secret string) bool {
	expected := SignPayload(payload, secret)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (s *A2APushService) ensureAgentAccess(ctx context.Context, agentID, userID, businessID string) error {
	agent, err := s.ap2Repo.GetAgentByID(ctx, agentID)
	if err != nil {
		return ErrPushConfigNotFound
	}
	if agent.OwnerID == userID {
		return nil
	}
	if businessID != "" && agent.BusinessID == businessID {
		return nil
	}
	return ErrPushConfigNotFound
}

func validateWebhookURL(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("webhook URL must use https")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("webhook host is required")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("local or internal hosts are not allowed")
	}

	if ip := net.ParseIP(host); ip != nil {
		if isDeniedIP(ip) {
			return fmt.Errorf("private or local IP addresses are not allowed")
		}
		return nil
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("failed to resolve webhook host")
	}
	for _, ip := range ips {
		if isDeniedIP(ip) {
			return fmt.Errorf("private or local IP addresses are not allowed")
		}
	}
	return nil
}

func isDeniedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}
