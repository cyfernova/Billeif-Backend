package models

import (
	"time"

	"gorm.io/gorm"
)

type Vendor struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID   string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name         string         `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	Email        string         `gorm:"size:255" json:"email,omitempty" validate:"omitempty,email,max=255"`
	Phone        string         `gorm:"size:50" json:"phone,omitempty" validate:"omitempty,max=50"`
	Address      string         `gorm:"size:500" json:"address,omitempty"`
	City         string         `gorm:"size:100" json:"city,omitempty" validate:"omitempty,max=100"`
	State        string         `gorm:"size:100" json:"state,omitempty" validate:"omitempty,max=100"`
	Country      string         `gorm:"size:100" json:"country,omitempty" validate:"omitempty,max=100"`
	PostalCode   string         `gorm:"size:20" json:"postal_code,omitempty" validate:"omitempty,max=20"`
	TaxID        string         `gorm:"size:100" json:"tax_id,omitempty" validate:"omitempty,max=100"`
	BankAccount  string         `gorm:"size:100" json:"bank_account,omitempty" validate:"omitempty,max=100"`
	PaymentTerms string         `gorm:"size:100" json:"payment_terms,omitempty" validate:"omitempty,max=100"`
	Notes        string         `gorm:"type:text" json:"notes,omitempty"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (v *Vendor) TableName() string {
	return "vendors"
}
