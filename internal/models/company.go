package models

import (
	"time"

	"github.com/google/uuid"
)

// Company represents a company/business entity
type Company struct {
	ID        uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	OwnerID   uuid.UUID  `gorm:"column:owner_id;index;not null" json:"owner_id"`
	Name      string     `gorm:"size:255;not null" json:"name"`
	TaxID     string     `gorm:"size:50" json:"tax_id,omitempty"`
	Address   string     `gorm:"type:text" json:"address,omitempty"`
	City      string     `gorm:"size:100" json:"city,omitempty"`
	State     string     `gorm:"size:100" json:"state,omitempty"`
	PostalCode string    `gorm:"column:postal_code;size:20" json:"postal_code,omitempty"`
	Country   string     `gorm:"size:100;default:'US'" json:"country"`
	Phone     string     `gorm:"size:20" json:"phone,omitempty"`
	Email     string     `gorm:"size:255" json:"email,omitempty"`
	LogoURL   string     `gorm:"column:logo_url;size:500" json:"logo_url,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt *time.Time `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`

	// Relations
	Owner   User     `gorm:"foreignKey:OwnerID" json:"-"`
	Clients []Client `gorm:"foreignKey:CompanyID" json:"clients,omitempty"`
	Invoices []Invoice `gorm:"foreignKey:CompanyID" json:"invoices,omitempty"`
}

// TableName returns the table name for Company
func (Company) TableName() string {
	return "companies"
}
