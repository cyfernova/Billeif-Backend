package models

import (
	"time"

	"gorm.io/gorm"
)

type LedgerEntry struct {
	ID            string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID    string    `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	InvoiceID     *string   `gorm:"index" json:"invoice_id,omitempty" validate:"omitempty,uuid"`
	PaymentID     *string   `gorm:"index" json:"payment_id,omitempty" validate:"omitempty,uuid"`
	TransactionID string    `gorm:"not null;size:100" json:"transaction_id" validate:"required,max=100"`
	EntryDate     time.Time `gorm:"not null" json:"entry_date" validate:"required"`
	EntryType     string    `gorm:"not null;size:50" json:"entry_type" validate:"required,oneof=debit credit"`
	Category      string    `gorm:"size:50" json:"category,omitempty" validate:"omitempty,max=50"`
	Description   string    `gorm:"not null;size:500" json:"description" validate:"required,max=500"`
	Amount        float64   `gorm:"not null;type:decimal(15,2)" json:"amount" validate:"required"`
	Currency      string    `gorm:"not null;size:3;default:'USD'" json:"currency" validate:"required,len=3"`
	Balance       float64   `gorm:"type:decimal(15,2)" json:"balance"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (l *LedgerEntry) TableName() string {
	return "ledger_entries"
}
