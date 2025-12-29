package models

import (
	"time"

	"gorm.io/gorm"
)

type Invoice struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	CustomerID  string         `gorm:"not null;index" json:"customer_id" validate:"required,uuid"`
	InvoiceNo   string         `gorm:"not null;uniqueIndex:idx_business_invoice;size:50" json:"invoice_no" validate:"required,max=50"`
	InvoiceDate time.Time      `gorm:"not null" json:"invoice_date" validate:"required"`
	DueDate     time.Time      `json:"due_date,omitempty" validate:"omitempty"`
	Status      string         `gorm:"not null;size:50;default:'draft'" json:"status" validate:"required,oneof=draft sent paid overdue void canceled"`
	Currency    string         `gorm:"not null;size:3;default:'USD'" json:"currency" validate:"required,len=3"`
	Subtotal    float64        `gorm:"type:decimal(15,2);default:0" json:"subtotal" validate:"gte=0"`
	Tax         float64        `gorm:"type:decimal(15,2);default:0" json:"tax" validate:"gte=0"`
	Discount    float64        `gorm:"type:decimal(15,2);default:0" json:"discount" validate:"gte=0"`
	Total       float64        `gorm:"type:decimal(15,2);not null;default:0" json:"total" validate:"required,gt=0"`
	PaidAmount  float64        `gorm:"type:decimal(15,2);default:0" json:"paid_amount" validate:"gte=0"`
	BalanceDue  float64        `gorm:"type:decimal(15,2);default:0" json:"balance_due" validate:"gte=0"`
	Notes       string         `gorm:"type:text" json:"notes,omitempty"`
	PDFURL      string         `gorm:"size:500" json:"pdf_url,omitempty"`
	PDFFilename string         `gorm:"size:255" json:"pdf_filename,omitempty"`
	SentAt      *time.Time     `json:"sent_at,omitempty"`
	PaidAt      *time.Time     `json:"paid_at,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	Items []*InvoiceItem `gorm:"foreignKey:InvoiceID" json:"items,omitempty"`
}

type InvoiceItem struct {
	ID          string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	InvoiceID   string    `gorm:"not null;index" json:"invoice_id" validate:"required"`
	ProductID   *string   `gorm:"index" json:"product_id,omitempty" validate:"omitempty,uuid"`
	Description string    `gorm:"not null" json:"description" validate:"required"`
	Quantity    float64   `gorm:"not null;type:decimal(15,3)" json:"quantity" validate:"required,gt=0"`
	UnitPrice   float64   `gorm:"not null;type:decimal(15,2)" json:"unit_price" validate:"required,gt=0"`
	Discount    float64   `gorm:"type:decimal(15,2);default:0" json:"discount" validate:"gte=0"`
	TaxRate     float64   `gorm:"type:decimal(5,2);default:0" json:"tax_rate" validate:"gte=0,lte=100"`
	Total       float64   `gorm:"not null;type:decimal(15,2)" json:"total" validate:"required,gt=0"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (i *Invoice) TableName() string {
	return "invoices"
}

func (i *InvoiceItem) TableName() string {
	return "invoice_items"
}
