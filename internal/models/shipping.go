package models

import (
	"time"

	"gorm.io/gorm"
)

type Shipment struct {
	ID                string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	DocumentID        string         `gorm:"not null;index" json:"document_id" validate:"required,uuid"`
	Provider          string         `gorm:"not null;size:60" json:"provider"`
	Courier           string         `gorm:"size:100" json:"courier,omitempty"`
	Status            string         `gorm:"not null;size:40;default:'draft';index" json:"status"`
	TrackingNumber    string         `gorm:"size:120;index" json:"tracking_number,omitempty"`
	TrackingURL       string         `gorm:"size:500" json:"tracking_url,omitempty"`
	PackageCount      int            `gorm:"default:1" json:"package_count"`
	PackageDimensions string         `gorm:"type:jsonb;default:'{}'" json:"package_dimensions,omitempty"`
	WeightKG          float64        `gorm:"type:decimal(10,3);default:0" json:"weight_kg"`
	AddressSnapshot   string         `gorm:"type:jsonb;default:'{}'" json:"address_snapshot,omitempty"`
	ProviderPayload   string         `gorm:"type:jsonb;default:'{}'" json:"provider_payload,omitempty"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Shipment) TableName() string {
	return "shipments"
}

type ShippingLabel struct {
	ID              string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ShipmentID      string         `gorm:"not null;index" json:"shipment_id" validate:"required,uuid"`
	DocumentID      string         `gorm:"not null;index" json:"document_id" validate:"required,uuid"`
	LabelFormat     string         `gorm:"not null;size:20;default:'pdf'" json:"label_format"`
	LabelURL        string         `gorm:"size:500" json:"label_url,omitempty"`
	LabelZPL        string         `gorm:"type:text" json:"label_zpl,omitempty"`
	ProviderLabelID string         `gorm:"size:120" json:"provider_label_id,omitempty"`
	ProviderPayload string         `gorm:"type:jsonb;default:'{}'" json:"provider_payload,omitempty"`
	CreatedAt       time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ShippingLabel) TableName() string {
	return "shipping_labels"
}
