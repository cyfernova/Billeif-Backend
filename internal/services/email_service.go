package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"gorm.io/gorm"
)

type EmailService struct {
	cfg    *config.Config
	client *ses.Client
	s3     *S3Service
	log    *logger.Logger
	db     *gorm.DB
}

func NewEmailService(cfg *config.Config, aws *awsclients.Config, s3 *S3Service, log *logger.Logger) *EmailService {
	return &EmailService{
		cfg:    cfg,
		client: aws.SES,
		s3:     s3,
		log:    log,
	}
}

func (s *EmailService) WithDB(db *gorm.DB) *EmailService {
	s.db = db
	return s
}

type EmailPayload struct {
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	SentAt    time.Time `json:"sent_at"`
	MessageID string    `json:"message_id"`
}

type UpsertEmailAccountInput struct {
	BusinessID       string                 `json:"business_id,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Email            string                 `json:"email,omitempty"`
	SenderName       string                 `json:"sender_name,omitempty"`
	ReplyToEmail     string                 `json:"reply_to_email,omitempty"`
	AccountType      string                 `json:"account_type,omitempty"`
	ConfigurationSet string                 `json:"configuration_set,omitempty"`
	IdentityARN      string                 `json:"identity_arn,omitempty"`
	IsDefault        *bool                  `json:"is_default,omitempty"`
	TrackDeliveries  *bool                  `json:"track_deliveries,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

type SendTestEmailInput struct {
	Recipient string `json:"recipient" binding:"required,email"`
	Subject   string `json:"subject,omitempty"`
	Body      string `json:"body,omitempty"`
}

func (s *EmailService) SendEmail(ctx context.Context, to, subject, body string) error {
	_, err := s.sendRawEmail(ctx, "noreply@invoiceapp.local", "", to, subject, body)
	if err != nil {
		s.log.Warn("SES send failed, storing to S3 fallback", "error", err)
		return s.storeEmailFallback(ctx, to, subject, body, "")
	}

	return nil
}

func (s *EmailService) ListAccounts(ctx context.Context, businessID string) ([]*models.EmailAccount, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var rows []models.EmailAccount
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("is_default DESC, updated_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*models.EmailAccount, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	if err := s.decorateEmailAccountStats(ctx, businessID, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *EmailService) UpsertAccount(ctx context.Context, businessID, accountID string, input UpsertEmailAccountInput) (*models.EmailAccount, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	normalizedEmail := strings.ToLower(strings.TrimSpace(input.Email))
	if accountID == "" && (strings.TrimSpace(input.Name) == "" || normalizedEmail == "") {
		return nil, fmt.Errorf("name and email are required")
	}

	var account models.EmailAccount
	if accountID != "" {
		err := s.db.WithContext(ctx).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", accountID, businessID).
			First(&account).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, err
		}
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("email account not found")
		}
	} else {
		account = models.EmailAccount{
			BusinessID:      businessID,
			Provider:        models.EmailProviderSES,
			AccountType:     firstNonEmpty(input.AccountType, "transactional"),
			Status:          models.EmailAccountStatusPendingVerification,
			TrackDeliveries: boolValueOrDefault(input.TrackDeliveries, true),
		}
	}

	if strings.TrimSpace(input.Name) != "" {
		account.Name = strings.TrimSpace(input.Name)
	}
	if normalizedEmail != "" && normalizedEmail != account.Email {
		account.Email = normalizedEmail
		account.VerifiedAt = nil
		account.Status = models.EmailAccountStatusPendingVerification
	}
	if strings.TrimSpace(input.SenderName) != "" {
		account.SenderName = strings.TrimSpace(input.SenderName)
	}
	if strings.TrimSpace(input.ReplyToEmail) != "" {
		account.ReplyToEmail = strings.TrimSpace(input.ReplyToEmail)
	}
	if strings.TrimSpace(input.AccountType) != "" {
		account.AccountType = strings.TrimSpace(input.AccountType)
	}
	if strings.TrimSpace(input.ConfigurationSet) != "" {
		account.ConfigurationSet = strings.TrimSpace(input.ConfigurationSet)
	}
	if strings.TrimSpace(input.IdentityARN) != "" {
		account.IdentityARN = strings.TrimSpace(input.IdentityARN)
	}
	if input.TrackDeliveries != nil {
		account.TrackDeliveries = *input.TrackDeliveries
	}
	if input.Metadata != nil {
		account.Metadata = mustMarshalMap(input.Metadata)
	}

	isDefault := account.IsDefault
	if input.IsDefault != nil {
		isDefault = *input.IsDefault
	}
	if account.ID == "" {
		var existingCount int64
		if err := s.db.WithContext(ctx).
			Model(&models.EmailAccount{}).
			Where("business_id = ? AND deleted_at IS NULL", businessID).
			Count(&existingCount).Error; err != nil {
			return nil, err
		}
		if existingCount == 0 && input.IsDefault == nil {
			isDefault = true
		}
	}
	account.IsDefault = isDefault

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if account.IsDefault {
			if err := tx.Model(&models.EmailAccount{}).
				Where("business_id = ? AND id <> ? AND deleted_at IS NULL", businessID, account.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		if account.ID == "" {
			return tx.Create(&account).Error
		}
		return tx.Save(&account).Error
	}); err != nil {
		return nil, err
	}

	return s.getAccount(ctx, businessID, account.ID)
}

func (s *EmailService) ListDeliveries(ctx context.Context, businessID string, limit int) ([]*models.EmailDelivery, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var rows []models.EmailDelivery
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*models.EmailDelivery, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, nil
}

func (s *EmailService) SendTestEmail(ctx context.Context, businessID, accountID string, input SendTestEmailInput) (*models.EmailDelivery, error) {
	account, err := s.getAccount(ctx, businessID, accountID)
	if err != nil {
		return nil, err
	}
	subject := firstNonEmpty(strings.TrimSpace(input.Subject), fmt.Sprintf("Test email from %s", account.Name))
	body := firstNonEmpty(strings.TrimSpace(input.Body), fmt.Sprintf("This is a test email sent from %s <%s> at %s.", account.Name, account.Email, time.Now().UTC().Format(time.RFC3339)))

	now := time.Now().UTC()
	delivery := &models.EmailDelivery{
		BusinessID:     businessID,
		EmailAccountID: &account.ID,
		Recipient:      strings.TrimSpace(input.Recipient),
		Subject:        subject,
		SourceEmail:    account.Email,
		Status:         models.EmailDeliveryStatusQueued,
		Metadata: mustMarshalMap(map[string]interface{}{
			"kind": "test",
		}),
	}

	messageID, sendErr := s.sendRawEmail(ctx, account.Email, account.ReplyToEmail, delivery.Recipient, subject, body)
	delivery.ProviderMessageID = messageID
	if sendErr != nil {
		delivery.Status = models.EmailDeliveryStatusFailed
		delivery.ErrorMessage = sendErr.Error()
		delivery.FailedAt = &now
		_ = s.storeEmailFallback(ctx, delivery.Recipient, subject, body, messageID)
	} else {
		delivery.Status = models.EmailDeliveryStatusSent
		delivery.SentAt = &now
		account.LastTestedAt = &now
		account.Status = models.EmailAccountStatusConnected
		if account.VerifiedAt == nil {
			account.VerifiedAt = &now
		}
	}

	if s.db == nil {
		return delivery, sendErr
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(delivery).Error; err != nil {
			return err
		}
		return tx.Save(account).Error
	}); err != nil {
		return nil, err
	}
	return delivery, sendErr
}

func (s *EmailService) getAccount(ctx context.Context, businessID, accountID string) (*models.EmailAccount, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var account models.EmailAccount
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", accountID, businessID).
		First(&account).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("email account not found")
		}
		return nil, err
	}
	result := &account
	if err := s.decorateEmailAccountStats(ctx, businessID, []*models.EmailAccount{result}); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *EmailService) decorateEmailAccountStats(ctx context.Context, businessID string, accounts []*models.EmailAccount) error {
	if len(accounts) == 0 || s.db == nil {
		return nil
	}
	accountIDs := make([]string, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	startOfDay := time.Now().UTC().Truncate(24 * time.Hour)
	type statRow struct {
		EmailAccountID string
		SentToday      int64
		FailedToday    int64
	}
	var stats []statRow
	statsSQL := `
		SELECT
			email_account_id,
			SUM(CASE WHEN status IN ('sent', 'delivered') AND COALESCE(sent_at, created_at) >= ? THEN 1 ELSE 0 END) AS sent_today,
			SUM(CASE WHEN status = 'failed' AND COALESCE(failed_at, created_at) >= ? THEN 1 ELSE 0 END) AS failed_today
		FROM email_deliveries
		WHERE business_id = ? AND email_account_id IN ? AND deleted_at IS NULL
		GROUP BY email_account_id
	`
	if err := s.db.WithContext(ctx).Raw(statsSQL, startOfDay, startOfDay, businessID, accountIDs).Scan(&stats).Error; err != nil {
		return err
	}
	statsByAccount := map[string]statRow{}
	for _, row := range stats {
		statsByAccount[row.EmailAccountID] = row
	}
	for _, account := range accounts {
		row := statsByAccount[account.ID]
		account.SentToday = row.SentToday
		account.FailedToday = row.FailedToday
	}
	return nil
}

func (s *EmailService) sendRawEmail(ctx context.Context, source, replyTo, to, subject, body string) (string, error) {
	input := &ses.SendEmailInput{
		Destination: &types.Destination{
			ToAddresses: []string{to},
		},
		Message: &types.Message{
			Subject: &types.Content{
				Charset: aws.String("UTF-8"),
				Data:    aws.String(subject),
			},
			Body: &types.Body{
				Text: &types.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(body),
				},
			},
		},
		Source: aws.String(source),
	}
	if strings.TrimSpace(replyTo) != "" {
		input.ReplyToAddresses = []string{replyTo}
	}

	output, err := s.client.SendEmail(ctx, input)
	if err != nil {
		return "", err
	}
	if output.MessageId == nil {
		return "", nil
	}
	return aws.ToString(output.MessageId), nil
}

func (s *EmailService) storeEmailFallback(ctx context.Context, to, subject, body, messageID string) error {
	payload := EmailPayload{
		To:        to,
		Subject:   subject,
		Body:      body,
		SentAt:    time.Now(),
		MessageID: messageID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("emails/%s/%d.json", to, time.Now().UnixNano())
	return s.s3.Upload(ctx, s.cfg.S3.BucketEmailSink, key, data, "application/json")
}

func (s *EmailService) GetCapturedEmails(ctx context.Context, email string) ([]EmailPayload, error) {
	prefix := fmt.Sprintf("emails/%s/", email)
	keys, err := s.s3.ListObjects(ctx, s.cfg.S3.BucketEmailSink, prefix)
	if err != nil {
		return nil, err
	}

	var emails []EmailPayload
	for _, key := range keys {
		data, err := s.s3.Download(ctx, s.cfg.S3.BucketEmailSink, key)
		if err != nil {
			continue
		}

		var payload EmailPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		emails = append(emails, payload)
	}

	return emails, nil
}

func (s *EmailService) ListAllCapturedEmails(ctx context.Context) ([]EmailPayload, error) {
	keys, err := s.s3.ListObjects(ctx, s.cfg.S3.BucketEmailSink, "emails/")
	if err != nil {
		return nil, err
	}

	var emails []EmailPayload
	for _, key := range keys {
		data, err := s.s3.Download(ctx, s.cfg.S3.BucketEmailSink, key)
		if err != nil {
			continue
		}

		var payload EmailPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		emails = append(emails, payload)
	}

	return emails, nil
}
