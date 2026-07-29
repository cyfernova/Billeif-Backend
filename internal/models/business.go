package models

import (
	"time"

	"gorm.io/gorm"
)

type BusinessProfile struct {
	ID                  string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OwnerID             string         `gorm:"not null;size:255;index" json:"owner_id"`
	Name                string         `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	Email               string         `gorm:"not null;size:255" json:"email" validate:"required,email"`
	Phone               string         `gorm:"size:50" json:"phone,omitempty" validate:"omitempty,max=50"`
	Address             string         `gorm:"size:500" json:"address,omitempty"`
	City                string         `gorm:"size:100" json:"city,omitempty" validate:"omitempty,max=100"`
	State               string         `gorm:"size:100" json:"state,omitempty" validate:"omitempty,max=100"`
	Country             string         `gorm:"size:100" json:"country,omitempty" validate:"omitempty,max=100"`
	PostalCode          string         `gorm:"size:20" json:"postal_code,omitempty" validate:"omitempty,max=20"`
	TaxID               string         `gorm:"size:100" json:"tax_id,omitempty" validate:"omitempty,max=100"`
	GSTIN               string         `gorm:"size:20" json:"gstin,omitempty" validate:"omitempty,max=20"`
	BusinessStateCode   string         `gorm:"column:business_state_code;size:10" json:"business_state_code,omitempty" validate:"omitempty,max=10"`
	CompositionEnabled  bool           `gorm:"column:composition_enabled;default:false" json:"composition_enabled"`
	DefaultGSTTreatment string         `gorm:"column:default_gst_treatment;size:50;default:'regular'" json:"default_gst_treatment,omitempty"`
	GSTFilingFrequency  string         `gorm:"column:gst_filing_frequency;size:20;default:'monthly'" json:"gst_filing_frequency,omitempty"`
	GSTRegistered       bool           `gorm:"column:gst_registered;default:false" json:"gst_registered"`
	GSTTDSEnabled       bool           `gorm:"column:gst_tds_enabled;default:false" json:"gst_tds_enabled"`
	ExportLUTEnabled    bool           `gorm:"column:export_lut_enabled;default:false" json:"export_lut_enabled"`
	SEZEnabled          bool           `gorm:"column:sez_enabled;default:false" json:"sez_enabled"`
	NumberingRules      string         `gorm:"type:jsonb;default:'{}'" json:"numbering_rules,omitempty"`
	TaxPreferencesJSON  string         `gorm:"type:jsonb;default:'{}'" json:"tax_preferences_json,omitempty"`
	LogoURL             string         `gorm:"size:500" json:"logo_url,omitempty"`
	LogoKey             string         `gorm:"size:255" json:"logo_key,omitempty"`
	Currency            string         `gorm:"not null;size:3;default:'USD'" json:"currency" validate:"required,len=3"`
	Timezone            string         `gorm:"not null;size:64;default:'Asia/Kolkata'" json:"timezone"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`
}

func (b *BusinessProfile) TableName() string {
	return "business_profiles"
}
