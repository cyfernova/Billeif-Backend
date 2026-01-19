package models

import (
	"time"

	"gorm.io/gorm"
)

type BusinessProfile struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OwnerID    string         `gorm:"not null;size:255;index" json:"owner_id"`
	Name       string         `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	Email      string         `gorm:"not null;size:255" json:"email" validate:"required,email"`
	Phone      string         `gorm:"size:50" json:"phone,omitempty" validate:"omitempty,max=50"`
	Address    string         `gorm:"size:500" json:"address,omitempty"`
	City       string         `gorm:"size:100" json:"city,omitempty" validate:"omitempty,max=100"`
	State      string         `gorm:"size:100" json:"state,omitempty" validate:"omitempty,max=100"`
	Country    string         `gorm:"size:100" json:"country,omitempty" validate:"omitempty,max=100"`
	PostalCode string         `gorm:"size:20" json:"postal_code,omitempty" validate:"omitempty,max=20"`
	TaxID      string         `gorm:"size:100" json:"tax_id,omitempty" validate:"omitempty,max=100"`
	LogoURL    string         `gorm:"size:500" json:"logo_url,omitempty"`
	LogoKey    string         `gorm:"size:255" json:"logo_key,omitempty"`
	Currency   string         `gorm:"not null;size:3;default:'USD'" json:"currency" validate:"required,len=3"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (b *BusinessProfile) TableName() string {
	return "business_profiles"
}
