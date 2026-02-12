package models

import (
	"time"
)

type PaymentCredential struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserID             string     `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	CredentialType     string     `gorm:"not null;size:50;index" json:"credential_type" validate:"required,oneof=razorpay_card razorpay_upi razorpay_wallet"`
	RazorpayCustomerID *string    `gorm:"size:100" json:"razorpay_customer_id,omitempty" validate:"omitempty,max=100"`
	MaskedCardNumber   *string    `gorm:"size:50" json:"masked_card_number,omitempty" validate:"omitempty,max=50"`
	CardBrand          *string    `gorm:"size:50" json:"card_brand,omitempty" validate:"omitempty,max=50"`
	EncryptedData      *string    `gorm:"type:text" json:"-" validate:"omitempty"`
	IsDefault          bool       `gorm:"default:false" json:"is_default"`
	IsActive           bool       `gorm:"default:true" json:"is_active"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`

	CredentialTokens []CredentialToken `gorm:"foreignKey:CredentialID" json:"-"`
}

func (pc *PaymentCredential) TableName() string {
	return "payment_credentials"
}

type CredentialToken struct {
	ID               string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	CredentialID     string    `gorm:"not null;index" json:"credential_id" validate:"required,uuid"`
	PaymentMandateID *string   `gorm:"index" json:"payment_mandate_id,omitempty" validate:"omitempty,uuid"`
	Token            string    `gorm:"column:token;index" json:"-"`
	TokenHash        string    `gorm:"column:token_hash;size:64;index" json:"-"`
	MandateID        *string   `gorm:"size:100" json:"mandate_id,omitempty" validate:"omitempty,max=100"`
	ExpiresAt        time.Time `gorm:"not null;index" json:"expires_at" validate:"required"`
	IsUsed           bool      `gorm:"default:false" json:"is_used"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (ct *CredentialToken) TableName() string {
	return "credential_tokens"
}
