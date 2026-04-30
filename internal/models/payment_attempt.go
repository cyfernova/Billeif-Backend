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
	RazorpayOrderID   string     `gorm:"size:100;uniqueIndex" json:"razorpay_order_id,omitempty"`
	RazorpayPaymentID string     `gorm:"size:100;index" json:"razorpay_payment_id,omitempty"`
	Status            string     `gorm:"not null;size:40;default:'created';index" json:"status"`
	IdempotencyKey    string     `gorm:"not null;size:255" json:"idempotency_key"`
	FailureReason     string     `gorm:"type:text" json:"failure_reason,omitempty"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
}

func (PaymentAttempt) TableName() string {
	return "payment_attempts"
}

type RazorpayWebhookEvent struct {
	ID              string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	RazorpayEventID string     `gorm:"not null;size:160;uniqueIndex" json:"razorpay_event_id"`
	EventType       string     `gorm:"not null;size:120;index" json:"event_type"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
	CreatedAt       time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (RazorpayWebhookEvent) TableName() string {
	return "razorpay_webhook_events"
}
