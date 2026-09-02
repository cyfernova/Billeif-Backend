package models

import (
	"time"
)

type IntentMandate struct {
	ID                    string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserID                string    `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	AgentID               string    `gorm:"not null;index" json:"agent_id" validate:"required,uuid"`
	Constraints           string    `gorm:"type:jsonb;not null" json:"constraints" validate:"required"`
	NaturalLanguageIntent string    `gorm:"type:text;not null" json:"natural_language_intent" validate:"required,max=2000"`
	Signature             string    `gorm:"type:varchar(1000);not null" json:"signature" validate:"required"`
	PublicKey             *string   `gorm:"type:varchar(500)" json:"public_key,omitempty" validate:"omitempty,max=500"`
	ExpiresAt             time.Time `gorm:"not null;index" json:"expires_at" validate:"required"`
	Status                string    `gorm:"size:50;default:active;index" json:"status" validate:"required,oneof=active revoked expired"`
	CreatedAt             time.Time `gorm:"autoCreateTime" json:"created_at"`

	CartMandates []CartMandate `gorm:"foreignKey:IntentMandateID" json:"cart_mandates,omitempty"`
}

func (im *IntentMandate) TableName() string {
	return "intent_mandates"
}

type CartMandate struct {
	ID                         string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID                 string    `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	IntentMandateID            *string   `gorm:"index" json:"intent_mandate_id,omitempty" validate:"omitempty,uuid"`
	UserID                     string    `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	AgentID                    string    `gorm:"not null;index" json:"agent_id" validate:"required,uuid"`
	MerchantID                 *string   `gorm:"index" json:"merchant_id,omitempty" validate:"omitempty,uuid"`
	Items                      string    `gorm:"type:jsonb;not null" json:"items" validate:"required"`
	SubtotalAmount             float64   `gorm:"not null;type:decimal(15,2);default:0" json:"subtotal_amount" validate:"gte=0"`
	TaxAmount                  float64   `gorm:"not null;type:decimal(15,2);default:0" json:"tax_amount" validate:"gte=0"`
	TotalAmount                float64   `gorm:"not null;type:decimal(15,2)" json:"total_amount" validate:"gte=0"`
	Currency                   string    `gorm:"not null;size:3;default:INR" json:"currency" validate:"required,len=3"`
	Signature                  string    `gorm:"type:varchar(1000);not null" json:"signature" validate:"required"`
	SignaturePublicKey         string    `gorm:"type:varchar(500)" json:"-"`
	MerchantSignature          *string   `gorm:"type:varchar(1000)" json:"merchant_signature,omitempty" validate:"omitempty,max=1000"`
	MerchantSignaturePublicKey *string   `gorm:"type:varchar(500)" json:"-"`
	Status                     string    `gorm:"size:50;default:pending;index" json:"status" validate:"required,oneof=pending signed rejected expired"`
	Version                    int64     `gorm:"not null;default:1" json:"version" validate:"gte=1"`
	ExpiresAt                  time.Time `gorm:"not null;index" json:"expires_at" validate:"required"`
	CreatedAt                  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt                  time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	PaymentMandates []PaymentMandate `gorm:"foreignKey:CartMandateID" json:"payment_mandates,omitempty"`
}

func (cm *CartMandate) TableName() string {
	return "cart_mandates"
}

type PaymentMandate struct {
	ID                string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	CartMandateID     string     `gorm:"not null;index" json:"cart_mandate_id" validate:"required,uuid"`
	UserID            string     `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	PaymentMethodID   *string    `gorm:"index" json:"payment_method_id,omitempty" validate:"omitempty,uuid"`
	Amount            float64    `gorm:"not null;type:decimal(15,2)" json:"amount" validate:"required,gt=0"`
	Currency          string     `gorm:"not null;size:3" json:"currency" validate:"required,len=3"`
	Signature         string     `gorm:"type:varchar(1000);not null" json:"signature" validate:"required"`
	RazorpayOrderID   *string    `gorm:"size:100;index" json:"razorpay_order_id,omitempty" validate:"omitempty,max=100"`
	RazorpayPaymentID *string    `gorm:"size:100" json:"razorpay_payment_id,omitempty" validate:"omitempty,max=100"`
	Status            string     `gorm:"size:50;default:pending;index" json:"status" validate:"required,oneof=pending authorized captured failed refunded"`
	ProcessedAt       *time.Time `json:"processed_at,omitempty"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (pm *PaymentMandate) TableName() string {
	return "payment_mandates"
}
