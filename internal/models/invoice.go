package models

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type InvoiceOrigin string

const (
	InvoiceOriginManual       InvoiceOrigin = "manual"
	InvoiceOriginPOS          InvoiceOrigin = "pos"
	InvoiceOriginStorefront   InvoiceOrigin = "storefront"
	InvoiceOriginSubscription InvoiceOrigin = "subscription"
	InvoiceOriginConversion   InvoiceOrigin = "conversion"
)

const (
	InvoiceStatusDraft         = "draft"
	InvoiceStatusIssued        = "issued"
	InvoiceStatusSent          = "sent"
	InvoiceStatusPartiallyPaid = "partially_paid"
	InvoiceStatusPaid          = "paid"
	InvoiceStatusOverdue       = "overdue"
	InvoiceStatusVoid          = "void"
	InvoiceStatusCanceled      = "canceled"
)

var (
	ErrInvalidInvoiceOrigin         = errors.New("invalid invoice origin")
	ErrInvoiceCustomerRequired      = errors.New("invoice customer required")
	ErrInvoicePartySnapshotRequired = errors.New("invoice party snapshot required")
	ErrInvalidInvoiceLifecycle      = errors.New("invalid invoice lifecycle")
)

type InvalidInvoiceStateError struct {
	Reason error
}

func (e *InvalidInvoiceStateError) Error() string {
	return fmt.Sprintf("invalid invoice state: %v", e.Reason)
}

func (e *InvalidInvoiceStateError) Unwrap() error {
	return e.Reason
}

type PartySnapshot struct {
	Name       string `json:"name"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Address    string `json:"address,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	Country    string `json:"country,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
	TaxID      string `json:"tax_id,omitempty"`
	GSTIN      string `json:"gstin,omitempty"`
}

func (p PartySnapshot) IsEmpty() bool {
	return strings.TrimSpace(p.Name) == ""
}

type Invoice struct {
	ID                   string                 `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID           string                 `gorm:"not null;index;uniqueIndex:idx_business_invoice,priority:1" json:"business_id" validate:"required,uuid"`
	CustomerID           *string                `gorm:"index" json:"customer_id,omitempty" validate:"omitempty,uuid"`
	Version              int                    `gorm:"not null;default:1" json:"version"`
	ProjectID            *string                `gorm:"index" json:"project_id,omitempty" validate:"omitempty,uuid"`
	BranchID             *string                `gorm:"index" json:"branch_id,omitempty" validate:"omitempty,uuid"`
	PriceListID          *string                `gorm:"index" json:"price_list_id,omitempty" validate:"omitempty,uuid"`
	RenderProfileID      *string                `gorm:"index" json:"render_profile_id,omitempty" validate:"omitempty,uuid"`
	InvoiceNo            *string                `gorm:"uniqueIndex:idx_business_invoice,priority:2;size:50" json:"invoice_no,omitempty" validate:"omitempty,max=50"`
	Origin               InvoiceOrigin          `gorm:"not null;size:24;default:'conversion'" json:"origin"`
	IssuedAt             *time.Time             `json:"issued_at,omitempty"`
	SellerSnapshot       PartySnapshot          `gorm:"serializer:json;type:jsonb;not null;default:'{}'" json:"seller_snapshot"`
	BuyerSnapshot        PartySnapshot          `gorm:"serializer:json;type:jsonb;not null;default:'{}'" json:"buyer_snapshot"`
	InvoiceDate          time.Time              `gorm:"not null;index" json:"invoice_date" validate:"required"`
	DueDate              time.Time              `json:"due_date,omitempty" validate:"omitempty"`
	Status               string                 `gorm:"not null;size:50;default:'draft';index" json:"status" validate:"required,oneof=draft issued sent partially_paid paid overdue void canceled"`
	Currency             string                 `gorm:"not null;size:3;default:'USD'" json:"currency" validate:"required,len=3"`
	Subtotal             float64                `gorm:"type:decimal(15,2);default:0" json:"subtotal" validate:"gte=0"`
	Tax                  float64                `gorm:"type:decimal(15,2);default:0" json:"tax" validate:"gte=0"`
	Discount             float64                `gorm:"type:decimal(15,2);default:0" json:"discount" validate:"gte=0"`
	Total                float64                `gorm:"type:decimal(15,2);not null;default:0" json:"total" validate:"required,gt=0"`
	PaidAmount           float64                `gorm:"type:decimal(15,2);default:0" json:"paid_amount" validate:"gte=0"`
	BalanceDue           float64                `gorm:"type:decimal(15,2);default:0" json:"balance_due" validate:"gte=0"`
	Notes                string                 `gorm:"type:text" json:"notes,omitempty"`
	CustomerSnapshotRaw  string                 `gorm:"column:customer_snapshot;type:jsonb;default:'{}'" json:"-"`
	DocumentJSONRaw      string                 `gorm:"column:document_json;type:jsonb;default:'{}'" json:"-"`
	TemplateOverrideRaw  string                 `gorm:"column:template_override;type:jsonb;default:'{}'" json:"-"`
	PaymentDisplayRaw    string                 `gorm:"column:payment_display;type:jsonb;default:'{}'" json:"-"`
	TermsJSONRaw         string                 `gorm:"column:terms_json;type:jsonb;default:'{}'" json:"-"`
	EWayDetailsJSONRaw   string                 `gorm:"column:eway_details_json;type:jsonb;default:'{}'" json:"-"`
	EInvoiceSettingsRaw  string                 `gorm:"column:einvoice_settings_json;type:jsonb;default:'{}'" json:"-"`
	CustomFields         string                 `gorm:"type:jsonb;default:'{}'" json:"custom_fields,omitempty"`
	TermsAndConditions   string                 `gorm:"-" json:"terms_and_conditions,omitempty"`
	TemplateOverride     map[string]interface{} `gorm:"-" json:"template_override,omitempty"`
	CustomerSnapshot     map[string]interface{} `gorm:"-" json:"customer_snapshot,omitempty"`
	DocumentJSON         map[string]interface{} `gorm:"-" json:"document_json,omitempty"`
	PaymentDisplay       map[string]interface{} `gorm:"-" json:"payment_display,omitempty"`
	TermsJSON            map[string]interface{} `gorm:"-" json:"terms_json,omitempty"`
	EWayDetailsJSON      map[string]interface{} `gorm:"-" json:"eway_details_json,omitempty"`
	EInvoiceSettingsJSON map[string]interface{} `gorm:"-" json:"einvoice_settings_json,omitempty"`
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
	HSNSACCode       string    `gorm:"column:hsn_sac_code;size:40" json:"hsn_sac_code,omitempty"`
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
	SKU              string    `gorm:"-" json:"sku,omitempty"`
	Batch            string    `gorm:"-" json:"batch,omitempty"`
	ItemType         string    `gorm:"-" json:"item_type,omitempty"`
	Amount           float64   `gorm:"-" json:"amount,omitempty"`
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

func (i Invoice) IsEditableDraft() bool {
	return i.Status == InvoiceStatusDraft && i.InvoiceNo == nil && i.IssuedAt == nil
}

func (i Invoice) ValidateState() error {
	switch i.Origin {
	case InvoiceOriginManual, InvoiceOriginPOS:
		if !hasStringValue(i.CustomerID) && i.BuyerSnapshot.IsEmpty() {
			return invalidInvoiceState(ErrInvoicePartySnapshotRequired)
		}
	case InvoiceOriginStorefront, InvoiceOriginSubscription, InvoiceOriginConversion:
		if !hasStringValue(i.CustomerID) {
			return invalidInvoiceState(ErrInvoiceCustomerRequired)
		}
	default:
		return invalidInvoiceState(ErrInvalidInvoiceOrigin)
	}

	if i.Version < 1 {
		return invalidInvoiceState(ErrInvalidInvoiceLifecycle)
	}
	switch i.Status {
	case InvoiceStatusDraft, InvoiceStatusIssued, InvoiceStatusSent, InvoiceStatusPartiallyPaid, InvoiceStatusPaid,
		InvoiceStatusOverdue, InvoiceStatusVoid, InvoiceStatusCanceled:
	default:
		return invalidInvoiceState(ErrInvalidInvoiceLifecycle)
	}
	if i.Status == InvoiceStatusDraft {
		if !i.IsEditableDraft() {
			return invalidInvoiceState(ErrInvalidInvoiceLifecycle)
		}
		return nil
	}
	if i.InvoiceNo == nil || strings.TrimSpace(*i.InvoiceNo) == "" || i.IssuedAt == nil ||
		i.SellerSnapshot.IsEmpty() || i.BuyerSnapshot.IsEmpty() {
		return invalidInvoiceState(ErrInvalidInvoiceLifecycle)
	}
	return nil
}

func invalidInvoiceState(reason error) error {
	return &InvalidInvoiceStateError{Reason: reason}
}

func StringPointer(value string) *string {
	return &value
}

func StringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func hasStringValue(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}
