package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
)

type EmailService struct {
	cfg    *config.Config
	client *ses.Client
	s3     *S3Service
	log    *logger.Logger
}

func NewEmailService(cfg *config.Config, aws *awsclients.Config, s3 *S3Service, log *logger.Logger) *EmailService {
	return &EmailService{
		cfg:    cfg,
		client: aws.SES,
		s3:     s3,
		log:    log,
	}
}

type EmailPayload struct {
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	SentAt    time.Time `json:"sent_at"`
	MessageID string    `json:"message_id"`
}

func (s *EmailService) SendEmail(ctx context.Context, to, subject, body string) error {
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
		Source: aws.String("noreply@invoiceapp.local"),
	}

	_, err := s.client.SendEmail(ctx, input)
	if err != nil {
		s.log.Warn("SES send failed, storing to S3 fallback", "error", err)
		return s.storeEmailFallback(ctx, to, subject, body, "")
	}

	return nil
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
