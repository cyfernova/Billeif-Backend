package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	EmailProviderSES = "ses"
)

const (
	EmailAccountStatusPendingVerification = "pending_verification"
	EmailAccountStatusConnected           = "connected"
	EmailAccountStatusFailed              = "failed"
)

const (
	EmailDeliveryStatusQueued    = "queued"
	EmailDeliveryStatusSent      = "sent"
	EmailDeliveryStatusDelivered = "delivered"
	EmailDeliveryStatusFailed    = "failed"
)

type EmailAccount struct {
	ID               string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID       string         `gorm:"not null;index" json:"business_id"`
	Name             string         `gorm:"not null;size:160" json:"name"`
	Email            string         `gorm:"not null;size:255" json:"email"`
	SenderName       string         `gorm:"size:160" json:"sender_name,omitempty"`
	ReplyToEmail     string         `gorm:"size:255" json:"reply_to_email,omitempty"`
	Provider         string         `gorm:"not null;size:40;default:'ses'" json:"provider"`
	AccountType      string         `gorm:"not null;size:40;default:'transactional'" json:"account_type"`
	Status           string         `gorm:"not null;size:40;default:'pending_verification'" json:"status"`
	IdentityARN      string         `gorm:"size:255" json:"identity_arn,omitempty"`
	ConfigurationSet string         `gorm:"size:255" json:"configuration_set,omitempty"`
	IsDefault        bool           `gorm:"not null;default:false" json:"is_default"`
	TrackDeliveries  bool           `gorm:"not null;default:true" json:"track_deliveries"`
	LastTestedAt     *time.Time     `json:"last_tested_at,omitempty"`
	VerifiedAt       *time.Time     `json:"verified_at,omitempty"`
	Metadata         string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt        time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
	SentToday        int64          `gorm:"-" json:"sent_today,omitempty"`
	FailedToday      int64          `gorm:"-" json:"failed_today,omitempty"`
}

func (EmailAccount) TableName() string {
	return "email_accounts"
}

type EmailDelivery struct {
	ID                string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string         `gorm:"not null;index" json:"business_id"`
	EmailAccountID    *string        `gorm:"index" json:"email_account_id,omitempty"`
	Recipient         string         `gorm:"size:255;index" json:"recipient,omitempty"`
	Subject           string         `gorm:"size:255" json:"subject,omitempty"`
	SourceEmail       string         `gorm:"size:255;index" json:"source_email,omitempty"`
	Status            string         `gorm:"not null;size:40;default:'queued';index" json:"status"`
	ProviderMessageID string         `gorm:"size:255" json:"provider_message_id,omitempty"`
	ErrorMessage      string         `gorm:"type:text" json:"error_message,omitempty"`
	Metadata          string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	SentAt            *time.Time     `json:"sent_at,omitempty"`
	DeliveredAt       *time.Time     `json:"delivered_at,omitempty"`
	FailedAt          *time.Time     `json:"failed_at,omitempty"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (EmailDelivery) TableName() string {
	return "email_deliveries"
}
