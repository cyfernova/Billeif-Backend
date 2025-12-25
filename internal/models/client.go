package models

import (
	"time"

	"github.com/google/uuid"
)

// Client represents a client of a company
type Client struct {
	ID         uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	CompanyID  uuid.UUID  `gorm:"column:company_id;index;not null" json:"company_id"`
	Name       string     `gorm:"size:255;not null" json:"name"`
	Email      string     `gorm:"size:255" json:"email,omitempty"`
	Phone      string     `gorm:"size:20" json:"phone,omitempty"`
	Address    string     `gorm:"type:text" json:"address,omitempty"`
	City       string     `gorm:"size:100" json:"city,omitempty"`
	State      string     `gorm:"size:100" json:"state,omitempty"`
	PostalCode string     `gorm:"column:postal_code;size:20" json:"postal_code,omitempty"`
	Country    string     `gorm:"size:100;default:'US'" json:"country"`
	TaxID      string     `gorm:"size:50" json:"tax_id,omitempty"`
	CreatedAt  time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt  *time.Time `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`

	// Relations
	Company  Company   `gorm:"foreignKey:CompanyID" json:"-"`
	Invoices []Invoice `gorm:"foreignKey:ClientID" json:"invoices,omitempty"`
}

// TableName returns the table name for Client
func (Client) TableName() string {
	return "clients"
}
