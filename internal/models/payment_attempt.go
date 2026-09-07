package models

import (
	"time"
)

const (
	PaymentAttemptTargetPlan       = "plan"
	PaymentAttemptTargetStoreOrder = "store_order"

	PaymentAttemptStatusCreated = "created"
	PaymentAttemptStatusPending = "pending"
	PaymentAttemptStatusPaid    = "paid"
	PaymentAttemptStatusFailed  = "failed"
)

type PaymentAttempt struct {
	ID                string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserID            string     `gorm:"not null;size:120;index" json:"user_id"`
	BusinessID        string     `gorm:"not null;type:uuid;index" json:"business_id"`
	TargetType        string     `gorm:"not null;size:40;index" json:"target_type"`
	TargetID          string     `gorm:"not null;size:120;index" json:"target_id"`
	AmountPaise       int64      `gorm:"not null" json:"amount_paise"`
	Currency          string     `gorm:"not null;size:3;default:'INR'" json:"currency"`
	RazorpayOrderID   string     `gorm:"size:100;uniqueIndex" json:"-"`
	RazorpayPaymentID string     `gorm:"size:100;index" json:"-"`
	Status            string     `gorm:"not null;size:40;default:'created';index" json:"status"`
	IdempotencyKey    string     `gorm:"not null;size:255" json:"idempotency_key"`
	FailureReason     string     `gorm:"type:text" json:"-"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
}

func (PaymentAttempt) TableName() string {
	return "payment_attempts"
}

type RazorpayWebhookEvent struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	RazorpayEventID    string     `gorm:"not null;size:160;uniqueIndex:ux_razorpay_event_mode" json:"-"`
	ProviderMode       string     `gorm:"not null;size:16;uniqueIndex:ux_razorpay_event_mode" json:"-"`
	EventType          string     `gorm:"not null;size:120;index" json:"event_type"`
	PayloadHash        string     `gorm:"not null;size:64" json:"payload_hash"`
	SignatureVerified  bool       `gorm:"not null;default:false" json:"signature_verified"`
	ReceivedAt         time.Time  `gorm:"not null" json:"received_at"`
	ProviderOccurredAt *time.Time `json:"provider_occurred_at,omitempty"`
	ProcessingStatus   string     `gorm:"not null;size:32;index" json:"processing_status"`
	AttemptCount       int        `gorm:"not null;default:0" json:"attempt_count"`
	SanitizedErrorCode string     `gorm:"size:80" json:"error_code,omitempty"`
	ProcessedAt        *time.Time `json:"processed_at,omitempty"`
	BusinessID         string     `gorm:"type:uuid;index" json:"-"`
	SubscriptionID     string     `gorm:"type:uuid;index" json:"-"`
	ReplayCount        int        `gorm:"not null;default:0" json:"replay_count"`
	LastReplayedAt     *time.Time `json:"last_replayed_at,omitempty"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (RazorpayWebhookEvent) TableName() string {
	return "razorpay_webhook_events"
}
