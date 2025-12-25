package models

import (
	"time"

	"github.com/google/uuid"
)

// InvoiceStatus represents the status of an invoice
type InvoiceStatus string

const (
	InvoiceStatusDraft     InvoiceStatus = "draft"
	InvoiceStatusSent      InvoiceStatus = "sent"
	InvoiceStatusPaid      InvoiceStatus = "paid"
	InvoiceStatusOverdue   InvoiceStatus = "overdue"
	InvoiceStatusCancelled InvoiceStatus = "cancelled"
)

// Invoice represents an invoice
type Invoice struct {
	ID                uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	BusinessProfileID uuid.UUID      `gorm:"column:business_profile_id;index;not null" json:"business_profile_id"`
	CustomerID        uuid.UUID      `gorm:"column:customer_id;index;not null" json:"customer_id"`
	InvoiceNumber     string         `gorm:"column:invoice_number;uniqueIndex;size:50;not null" json:"invoice_number"`
	IssueDate         time.Time      `gorm:"column:issue_date;index;not null" json:"issue_date"`
	DueDate           time.Time      `gorm:"column:due_date;not null" json:"due_date"`
	Status            InvoiceStatus  `gorm:"size:50;default:'draft'" json:"status"`
	Subtotal          float64        `gorm:"type:decimal(12,2);default:0" json:"subtotal"`
	TaxRate           float64        `gorm:"type:decimal(5,2);default:0" json:"tax_rate"`
	TaxAmount         float64        `gorm:"type:decimal(12,2);default:0" json:"tax_amount"`
	Total             float64        `gorm:"type:decimal(12,2);default:0" json:"total"`
	Notes             string         `gorm:"type:text" json:"notes,omitempty"`
	CreatedAt         time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt         *time.Time     `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`

	// Relations
	BusinessProfile BusinessProfile `gorm:"foreignKey:BusinessProfileID" json:"-"`
	Customer        Customer         `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Items           []InvoiceItem    `gorm:"foreignKey:InvoiceID" json:"items,omitempty"`
}

// TableName returns the table name for Invoice
func (Invoice) TableName() string {
	return "invoices"
}

// InvoiceItem represents a line item in an invoice
type InvoiceItem struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	InvoiceID   uuid.UUID  `gorm:"column:invoice_id;index;not null" json:"invoice_id"`
	Description string     `gorm:"type:text;not null" json:"description"`
	Quantity    float64    `gorm:"type:decimal(10,2);default:1" json:"quantity"`
	UnitPrice   float64    `gorm:"type:decimal(12,2);default:0" json:"unit_price"`
	Total       float64    `gorm:"type:decimal(12,2);default:0" json:"total"`
	CreatedAt   time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at" json:"updated_at"`

	// Relations
	Invoice Invoice `gorm:"foreignKey:InvoiceID" json:"-"`
}

// TableName returns the table name for InvoiceItem
func (InvoiceItem) TableName() string {
	return "invoice_items"
}

// CalculateItemTotal calculates the total for an invoice item
func (ii *InvoiceItem) CalculateItemTotal() {
	ii.Total = ii.Quantity * ii.UnitPrice
}

// IsValidStatus checks if an invoice status is valid
func IsValidStatus(status InvoiceStatus) bool {
	switch status {
	case InvoiceStatusDraft, InvoiceStatusSent, InvoiceStatusPaid, InvoiceStatusOverdue, InvoiceStatusCancelled:
		return true
	default:
		return false
	}
}
