package services

import (
	"bytes"
	"context"
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

type A2APushService struct {
	db         *gorm.DB
	log        *logger.Logger
	httpClient *http.Client
}

type TaskPushConfigRecord struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	TaskID             string     `gorm:"column:task_id;type:uuid;not null;index:idx_a2a_task_push_configs_task_id,priority:1"`
	UserID             string     `gorm:"column:user_id;type:varchar(255);not null;index"`
	BusinessID         string     `gorm:"column:business_id;type:varchar(255);index"`
	URL                string     `gorm:"column:url;type:varchar(500);not null"`
	Token              string     `gorm:"column:token;type:varchar(255)"`
	AuthenticationJSON string     `gorm:"column:authentication;type:jsonb;default:'{}'"`
	CreatedAt          time.Time  `gorm:"autoCreateTime"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime"`
	DeletedAt          *time.Time `gorm:"index"`
}

func (TaskPushConfigRecord) TableName() string {
	return "a2a_task_push_configs"
}

var (
	ErrTaskPushConfigNotFound = errors.New("task push config not found")
	ErrUnsafeWebhookURL       = errors.New("unsafe webhook URL")
)

func NewA2APushService(db *gorm.DB, _ interfaces.AP2Repository, log *logger.Logger) *A2APushService {
	return &A2APushService{
		db:  db,
		log: log,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *A2APushService) CreateTaskPushConfig(ctx context.Context, taskID, userID, businessID, configID string, config *a2a.PushNotificationConfig) (*a2a.TaskPushNotificationConfig, error) {
	if strings.TrimSpace(taskID) == "" || config == nil {
		return nil, a2a.ErrPushConfigInvalid
	}
	if err := validateWebhookURL(ctx, config.URL); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafeWebhookURL, err)
	}
	if configID == "" {
		configID = uuid.NewString()
	}
	config.ID = configID

	authJSON := "{}"
	if config.Authentication != nil {
		payload, err := json.Marshal(config.Authentication)
		if err != nil {
			return nil, fmt.Errorf("marshal push authentication: %w", err)
		}
		authJSON = string(payload)
	}

	record := &TaskPushConfigRecord{
		ID:                 configID,
		TaskID:             taskID,
		UserID:             userID,
		BusinessID:         businessID,
		URL:                config.URL,
		Token:              config.Token,
		AuthenticationJSON: authJSON,
	}
	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return nil, fmt.Errorf("create task push config: %w", err)
	}

	return &a2a.TaskPushNotificationConfig{
		Name:                   a2a.TaskPushConfigName(taskID, configID),
		PushNotificationConfig: *config,
	}, nil
}

func (s *A2APushService) GetTaskPushConfig(ctx context.Context, taskID, configID, userID, businessID string) (*a2a.TaskPushNotificationConfig, error) {
	record, err := s.getRecord(ctx, taskID, configID, userID, businessID)
	if err != nil {
		return nil, err
	}
	return s.toAPI(record)
}

func (s *A2APushService) ListTaskPushConfigs(ctx context.Context, taskID, userID, businessID string, pageSize int, pageToken string) (*a2a.ListTaskPushNotificationConfigResponse, error) {
	if pageSize <= 0 {
		pageSize = a2a.DefaultPushListPageSize
	}
	if pageSize > a2a.MaxPushListPageSize {
		pageSize = a2a.MaxPushListPageSize
	}

	query := s.db.WithContext(ctx).
		Model(&TaskPushConfigRecord{}).
		Where("task_id = ? AND user_id = ? AND deleted_at IS NULL", taskID, userID)
	if businessID != "" {
		query = query.Where("business_id = ?", businessID)
	}

	cursorTime, cursorID, err := a2a.DecodePageToken(pageToken)
	if err != nil {
		return nil, a2a.ErrInvalidPageToken
	}
	if !cursorTime.IsZero() && cursorID != "" {
		query = query.Where("(created_at > ?) OR (created_at = ? AND id > ?)", cursorTime, cursorTime, cursorID)
	}

	var records []TaskPushConfigRecord
	if err := query.Order("created_at ASC, id ASC").Limit(pageSize + 1).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list task push configs: %w", err)
	}

	nextPageToken := ""
	if len(records) > pageSize {
		last := records[pageSize-1]
		nextPageToken = a2a.EncodePageToken(last.CreatedAt, last.ID)
		records = records[:pageSize]
	}

	configs := make([]a2a.TaskPushNotificationConfig, 0, len(records))
	for i := range records {
		cfg, convErr := s.toAPI(&records[i])
		if convErr != nil {
			return nil, convErr
		}
		configs = append(configs, *cfg)
	}

	return &a2a.ListTaskPushNotificationConfigResponse{
		Configs:       configs,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *A2APushService) DeleteTaskPushConfig(ctx context.Context, taskID, configID, userID, businessID string) error {
	record, err := s.getRecord(ctx, taskID, configID, userID, businessID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.db.WithContext(ctx).
		Model(&TaskPushConfigRecord{}).
		Where("id = ?", record.ID).
		Update("deleted_at", now).Error
}

func (s *A2APushService) NotifyTaskEvent(ctx context.Context, task *a2a.Task, event a2a.StreamEvent) {
	if task == nil {
		return
	}

	var records []TaskPushConfigRecord
	query := s.db.WithContext(ctx).
		Where("task_id = ? AND user_id = ? AND deleted_at IS NULL", task.ID, task.UserID)
	if task.BusinessID != "" {
		query = query.Where("business_id = ?", task.BusinessID)
	}
	if err := query.Find(&records).Error; err != nil {
		s.log.Warn("failed to load task push configs", "task_id", task.ID, "error", err)
		return
	}

	for i := range records {
		record := records[i]
		go func() {
			cfg, convErr := s.toAPI(&record)
			if convErr != nil {
				s.log.Warn("failed to decode task push config", "config_id", record.ID, "error", convErr)
				return
			}
			if err := s.sendNotification(ctx, cfg, event.Data); err != nil {
				s.log.Warn("failed to deliver task push notification", "task_id", task.ID, "config_id", record.ID, "error", err)
			}
		}()
	}
}

func (s *A2APushService) getRecord(ctx context.Context, taskID, configID, userID, businessID string) (*TaskPushConfigRecord, error) {
	var record TaskPushConfigRecord
	query := s.db.WithContext(ctx).
		Where("id = ? AND task_id = ? AND user_id = ? AND deleted_at IS NULL", configID, taskID, userID)
	if businessID != "" {
		query = query.Where("business_id = ?", businessID)
	}
	if err := query.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskPushConfigNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (s *A2APushService) toAPI(record *TaskPushConfigRecord) (*a2a.TaskPushNotificationConfig, error) {
	cfg := a2a.PushNotificationConfig{
		ID:    record.ID,
		URL:   record.URL,
		Token: record.Token,
	}
	if strings.TrimSpace(record.AuthenticationJSON) != "" && record.AuthenticationJSON != "{}" {
		var auth a2a.AuthenticationInfo
		if err := json.Unmarshal([]byte(record.AuthenticationJSON), &auth); err != nil {
			return nil, fmt.Errorf("decode push authentication: %w", err)
		}
		cfg.Authentication = &auth
	}
	return &a2a.TaskPushNotificationConfig{
		Name:                   a2a.TaskPushConfigName(record.TaskID, record.ID),
		PushNotificationConfig: cfg,
	}, nil
}

func (s *A2APushService) sendNotification(ctx context.Context, config *a2a.TaskPushNotificationConfig, payload []byte) error {
	if err := validateWebhookURL(ctx, config.PushNotificationConfig.URL); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeWebhookURL, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.PushNotificationConfig.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build push notification request: %w", err)
	}
	req.Header.Set("Content-Type", a2a.ContentTypeA2AJSON)
	if config.PushNotificationConfig.Token != "" {
		req.Header.Set(a2a.HeaderNotificationToken, config.PushNotificationConfig.Token)
	}
	if auth := config.PushNotificationConfig.Authentication; auth != nil && len(auth.Schemes) > 0 && auth.Credentials != "" {
		req.Header.Set("Authorization", auth.Schemes[0]+" "+auth.Credentials)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deliver push notification: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("push notification failed with status %d", resp.StatusCode)
	}
	return nil
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
