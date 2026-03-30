package models

import (
	"time"

	"gorm.io/gorm"
)

type POSProfile struct {
	ID               string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID       string         `gorm:"not null;index" json:"business_id"`
	Name             string         `gorm:"not null;size:120" json:"name"`
	DefaultWarehouseID *string      `gorm:"index" json:"default_warehouse_id,omitempty"`
	ReceiptWidth     string         `gorm:"not null;size:20;default:'58mm'" json:"receipt_width"`
	ShowHSN          bool           `gorm:"default:false" json:"show_hsn"`
	FooterText       string         `gorm:"type:text" json:"footer_text,omitempty"`
	Template         string         `gorm:"size:60;default:'standard'" json:"template"`
	IsDefault        bool           `gorm:"default:false" json:"is_default"`
	Metadata         string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt        time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (POSProfile) TableName() string {
	return "pos_profiles"
}

type POSSession struct {
	ID               string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID       string         `gorm:"not null;index" json:"business_id"`
	UserID           string         `gorm:"not null;index" json:"user_id"`
	POSProfileID     *string        `gorm:"index" json:"pos_profile_id,omitempty"`
	Status           string         `gorm:"not null;size:30;default:'open';index" json:"status"`
	SessionName      string         `gorm:"size:120" json:"session_name,omitempty"`
	WarehouseID      *string        `gorm:"index" json:"warehouse_id,omitempty"`
	Currency         string         `gorm:"size:3;default:'INR'" json:"currency,omitempty"`
	CartPayload      string         `gorm:"type:jsonb;default:'{}'" json:"cart_payload,omitempty"`
	LastScannedCode  string         `gorm:"size:120" json:"last_scanned_code,omitempty"`
	LastCheckedOutDocumentID *string `gorm:"index" json:"last_checked_out_document_id,omitempty"`
	Metadata         string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	OpenedAt         time.Time      `gorm:"autoCreateTime" json:"opened_at"`
	ClosedAt         *time.Time     `json:"closed_at,omitempty"`
	CreatedAt        time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (POSSession) TableName() string {
	return "pos_sessions"
}
