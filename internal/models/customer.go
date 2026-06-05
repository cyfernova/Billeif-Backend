package models

import (
	"time"

	"gorm.io/gorm"
)

type Customer struct {
	ID                      string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID              string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name                    string         `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	Email                   string         `gorm:"size:255" json:"email,omitempty" validate:"omitempty,email,max=255"`
	Phone                   string         `gorm:"size:50" json:"phone,omitempty" validate:"omitempty,max=50"`
	Address                 string         `gorm:"size:500" json:"address,omitempty"`
	City                    string         `gorm:"size:100" json:"city,omitempty" validate:"omitempty,max=100"`
	State                   string         `gorm:"size:100" json:"state,omitempty" validate:"omitempty,max=100"`
	Country                 string         `gorm:"size:100" json:"country,omitempty" validate:"omitempty,max=100"`
	PostalCode              string         `gorm:"size:20" json:"postal_code,omitempty" validate:"omitempty,max=20"`
	TaxID                   string         `gorm:"size:100" json:"tax_id,omitempty" validate:"omitempty,max=100"`
	GSTIN                   string         `gorm:"size:20" json:"gstin,omitempty" validate:"omitempty,max=20"`
	PAN                     string         `gorm:"size:10" json:"pan,omitempty" validate:"omitempty,max=10"`
	CompanyName             string         `gorm:"size:255" json:"company_name,omitempty"`
	StateCode               string         `gorm:"size:10" json:"state_code,omitempty" validate:"omitempty,max=10"`
	BillingJSON             string         `gorm:"column:billing_address_json;type:jsonb;default:'{}'" json:"billing_address_json,omitempty"`
	ShippingJSON            string         `gorm:"column:shipping_address_json;type:jsonb;default:'{}'" json:"shipping_address_json,omitempty"`
	WithholdingDefaultsJSON string         `gorm:"type:jsonb;default:'{}'" json:"withholding_defaults_json,omitempty"`
	CreditLimit             float64        `gorm:"type:decimal(15,2);default:0" json:"credit_limit" validate:"gte=0"`
	DefaultPriceListID      *string        `gorm:"index" json:"default_price_list_id,omitempty" validate:"omitempty,uuid"`
	PreferencesJSON         string         `gorm:"type:jsonb;default:'{}'" json:"preferences_json,omitempty"`
	Notes                   string         `gorm:"type:text" json:"notes,omitempty"`
	CreatedAt               time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt               time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt               gorm.DeletedAt `gorm:"index" json:"-"`
}

func (c *Customer) TableName() string {
	return "customers"
}
