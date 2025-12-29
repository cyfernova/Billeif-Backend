package models

import (
	"time"

	"gorm.io/gorm"
)

type Payment struct {
	ID            string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID    string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	InvoiceID     string         `gorm:"not null;index" json:"invoice_id" validate:"required,uuid"`
	Amount        float64        `gorm:"not null;type:decimal(15,2)" json:"amount" validate:"required,gt=0"`
	Currency      string         `gorm:"not null;size:3;default:'USD'" json:"currency" validate:"required,len=3"`
	PaymentDate   time.Time      `gorm:"not null" json:"payment_date" validate:"required"`
	PaymentMethod string         `gorm:"not null;size:50" json:"payment_method" validate:"required,max=50"`
	Reference     string         `gorm:"size:100" json:"reference,omitempty" validate:"omitempty,max=100"`
	ReceiptURL    string         `gorm:"size:500" json:"receipt_url,omitempty"`
	ReceiptKey    string         `gorm:"size:255" json:"receipt_key,omitempty"`
	Notes         string         `gorm:"type:text" json:"notes,omitempty"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (p *Payment) TableName() string {
	return "payments"
}
