package models

import (
	"time"

	"gorm.io/gorm"
)

type Invoice struct {
	ID                   string                 `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID           string                 `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	CustomerID           string                 `gorm:"not null;index" json:"customer_id" validate:"required,uuid"`
	ProjectID            *string                `gorm:"index" json:"project_id,omitempty" validate:"omitempty,uuid"`
	PriceListID          *string                `gorm:"index" json:"price_list_id,omitempty" validate:"omitempty,uuid"`
	RenderProfileID      *string                `gorm:"index" json:"render_profile_id,omitempty" validate:"omitempty,uuid"`
	InvoiceNo            string                 `gorm:"not null;uniqueIndex:idx_business_invoice;size:50" json:"invoice_no" validate:"required,max=50"`
	InvoiceDate          time.Time              `gorm:"not null;index" json:"invoice_date" validate:"required"`
	DueDate              time.Time              `json:"due_date,omitempty" validate:"omitempty"`
	Status               string                 `gorm:"not null;size:50;default:'draft';index" json:"status" validate:"required,oneof=draft sent paid overdue void canceled"`
	Currency             string                 `gorm:"not null;size:3;default:'USD'" json:"currency" validate:"required,len=3"`
	Subtotal             float64                `gorm:"type:decimal(15,2);default:0" json:"subtotal" validate:"gte=0"`
	Tax                  float64                `gorm:"type:decimal(15,2);default:0" json:"tax" validate:"gte=0"`
	Discount             float64                `gorm:"type:decimal(15,2);default:0" json:"discount" validate:"gte=0"`
	Total                float64                `gorm:"type:decimal(15,2);not null;default:0" json:"total" validate:"required,gt=0"`
	PaidAmount           float64                `gorm:"type:decimal(15,2);default:0" json:"paid_amount" validate:"gte=0"`
	BalanceDue           float64                `gorm:"type:decimal(15,2);default:0" json:"balance_due" validate:"gte=0"`
	Notes                string                 `gorm:"type:text" json:"notes,omitempty"`
	CustomFields         string                 `gorm:"type:jsonb;default:'{}'" json:"custom_fields,omitempty"`
	TermsAndConditions   string                 `gorm:"-" json:"terms_and_conditions,omitempty"`
	TemplateOverride     map[string]interface{} `gorm:"-" json:"template_override,omitempty"`
	PONumber             string                 `gorm:"-" json:"po_number,omitempty"`
	AdditionalCharges    string                 `gorm:"type:jsonb;default:'[]'" json:"additional_charges,omitempty"`
	OriginSubscriptionID *string                `gorm:"index" json:"origin_subscription_id,omitempty" validate:"omitempty,uuid"`
	OriginRunID          *string                `gorm:"index" json:"origin_run_id,omitempty" validate:"omitempty,uuid"`
	SignedAt             *time.Time             `gorm:"index" json:"signed_at,omitempty"`
	SignedByProfileID    *string                `gorm:"index" json:"signed_by_profile_id,omitempty" validate:"omitempty,uuid"`
	SignMetadata         string                 `gorm:"type:jsonb;default:'{}'" json:"sign_metadata,omitempty"`
	PDFURL               string                 `gorm:"size:500" json:"pdf_url,omitempty"`
	PDFFilename          string                 `gorm:"size:255" json:"pdf_filename,omitempty"`
	TaxProfile           string                 `gorm:"type:jsonb;default:'{}'" json:"tax_profile,omitempty"`
	SentAt               *time.Time             `json:"sent_at,omitempty"`
	PaidAt               *time.Time             `json:"paid_at,omitempty"`
	CreatedAt            time.Time              `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time              `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt            gorm.DeletedAt         `gorm:"index" json:"-"`

	Items []*InvoiceItem `gorm:"foreignKey:InvoiceID" json:"items,omitempty"`
}

type InvoiceItem struct {
	ID               string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	InvoiceID        string    `gorm:"not null;index" json:"invoice_id" validate:"required"`
	ProductID        *string   `gorm:"index" json:"product_id,omitempty" validate:"omitempty,uuid"`
	VariantID        *string   `gorm:"index" json:"variant_id,omitempty" validate:"omitempty,uuid"`
	WarehouseID      *string   `gorm:"index" json:"warehouse_id,omitempty" validate:"omitempty,uuid"`
	Description      string    `gorm:"not null" json:"description" validate:"required"`
	HSNSACCode       string    `gorm:"size:40" json:"hsn_sac_code,omitempty"`
	Unit             string    `gorm:"size:20;default:'OTH'" json:"unit,omitempty"`
	Quantity         float64   `gorm:"not null;type:decimal(15,3)" json:"quantity" validate:"required,gt=0"`
	FreeQuantity     float64   `gorm:"type:decimal(15,3);default:0" json:"free_quantity"`
	UnitPrice        float64   `gorm:"not null;type:decimal(15,2)" json:"unit_price" validate:"required,gt=0"`
	MRP              float64   `gorm:"type:decimal(15,2);default:0" json:"mrp"`
	Discount         float64   `gorm:"type:decimal(15,2);default:0" json:"discount" validate:"gte=0"`
	TaxRate          float64   `gorm:"type:decimal(5,2);default:0" json:"tax_rate" validate:"gte=0,lte=100"`
	CessRate         float64   `gorm:"type:decimal(7,3);default:0" json:"cess_rate"`
	CessAmount       float64   `gorm:"type:decimal(15,2);default:0" json:"cess_amount"`
	CustomFields     string    `gorm:"type:jsonb;default:'{}'" json:"custom_fields,omitempty"`
	ChargeSnapshot   string    `gorm:"type:jsonb;default:'[]'" json:"charge_snapshot,omitempty"`
	BatchAllocations string    `gorm:"type:jsonb;default:'[]'" json:"batch_allocations,omitempty"`
	SerialIDs        string    `gorm:"type:jsonb;default:'[]'" json:"serial_ids,omitempty"`
	Total            float64   `gorm:"not null;type:decimal(15,2)" json:"total" validate:"required,gt=0"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (i *Invoice) TableName() string {
	return "invoices"
}

func (i *InvoiceItem) TableName() string {
	return "invoice_items"
}
