package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	GSTReportTypeGSTR1      = "gstr1"
	GSTReportTypeGSTR2B     = "gstr2b"
	GSTReportTypeCMP08      = "cmp08"
	GSTReportTypeGSTR4      = "gstr4"
	GSTReportTypeGSTR7      = "gstr7"
	GSTReportTypeHSNSummary = "hsn_summary"
)

const (
	GSTReportStatusQueued    = "queued"
	GSTReportStatusCompleted = "completed"
	GSTReportStatusFailed    = "failed"
)

const (
	WithholdingTypeTDS    = "tds"
	WithholdingTypeTCS    = "tcs"
	WithholdingTypeGSTTDS = "gst_tds"
)

const (
	GSTR2BMatchStatusMatched         = "matched"
	GSTR2BMatchStatusValueMismatch   = "value_mismatch"
	GSTR2BMatchStatusTaxMismatch     = "tax_mismatch"
	GSTR2BMatchStatusMissingInBooks  = "missing_in_books"
	GSTR2BMatchStatusMissingInPortal = "missing_in_portal"
)

type TaxSectionMaster struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Code        string         `gorm:"not null;uniqueIndex;size:30" json:"code"`
	Kind        string         `gorm:"not null;size:20" json:"kind"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Rate        float64        `gorm:"type:decimal(7,3);default:0" json:"rate"`
	IsGSTTDS    bool           `gorm:"default:false" json:"is_gst_tds"`
	IsTDS       bool           `gorm:"default:false" json:"is_tds"`
	IsTCS       bool           `gorm:"default:false" json:"is_tcs"`
	Metadata    string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TaxSectionMaster) TableName() string {
	return "tax_section_master"
}

type DocumentWithholding struct {
	ID              string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string         `gorm:"not null;index" json:"business_id"`
	DocumentID      string         `gorm:"not null;index" json:"document_id"`
	SectionCode     string         `gorm:"not null;size:30" json:"section_code"`
	WithholdingType string         `gorm:"not null;size:20" json:"withholding_type"`
	Rate            float64        `gorm:"type:decimal(7,3);default:0" json:"rate"`
	TaxableAmount   float64        `gorm:"type:decimal(15,2);default:0" json:"taxable_amount"`
	Amount          float64        `gorm:"type:decimal(15,2);default:0" json:"amount"`
	Metadata        string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt       time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (DocumentWithholding) TableName() string {
	return "document_withholdings"
}

type PaymentWithholding struct {
	ID              string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string         `gorm:"not null;index" json:"business_id"`
	PaymentID       string         `gorm:"not null;index" json:"payment_id"`
	InvoiceID       *string        `gorm:"index" json:"invoice_id,omitempty"`
	SectionCode     string         `gorm:"not null;size:30" json:"section_code"`
	WithholdingType string         `gorm:"not null;size:20" json:"withholding_type"`
	Rate            float64        `gorm:"type:decimal(7,3);default:0" json:"rate"`
	TaxableAmount   float64        `gorm:"type:decimal(15,2);default:0" json:"taxable_amount"`
	Amount          float64        `gorm:"type:decimal(15,2);default:0" json:"amount"`
	Metadata        string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt       time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PaymentWithholding) TableName() string {
	return "payment_withholdings"
}

type GSTReportRun struct {
	ID              string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string         `gorm:"not null;index" json:"business_id"`
	ReportType      string         `gorm:"not null;size:40;index" json:"report_type"`
	PeriodStart     time.Time      `gorm:"not null" json:"period_start"`
	PeriodEnd       time.Time      `gorm:"not null" json:"period_end"`
	FilingFrequency string         `gorm:"size:20;default:'monthly'" json:"filing_frequency"`
	ExportFormat    string         `gorm:"size:20;default:'json'" json:"export_format"`
	Status          string         `gorm:"size:20;default:'completed'" json:"status"`
	Warnings        string         `gorm:"type:jsonb;default:'[]'" json:"warnings,omitempty"`
	Payload         string         `gorm:"type:jsonb;default:'{}'" json:"payload,omitempty"`
	CreatedBy       string         `gorm:"size:255" json:"created_by,omitempty"`
	CreatedAt       time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GSTReportRun) TableName() string {
	return "gst_report_runs"
}

type GSTR2BImport struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string         `gorm:"not null;index" json:"business_id"`
	PeriodStart time.Time      `gorm:"not null" json:"period_start"`
	PeriodEnd   time.Time      `gorm:"not null" json:"period_end"`
	Source      string         `gorm:"not null;size:20;default:'api'" json:"source"`
	Status      string         `gorm:"size:20;default:'processed'" json:"status"`
	Notes       string         `gorm:"type:text" json:"notes,omitempty"`
	RawPayload  string         `gorm:"type:jsonb;default:'{}'" json:"raw_payload,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GSTR2BImport) TableName() string {
	return "gstr2b_imports"
}

type GSTR2BImportLine struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ImportID       string         `gorm:"not null;index" json:"import_id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id"`
	SupplierGSTIN  string         `gorm:"size:20" json:"supplier_gstin,omitempty"`
	SupplierName   string         `gorm:"size:255" json:"supplier_name,omitempty"`
	DocumentNumber string         `gorm:"size:80" json:"document_number,omitempty"`
	DocumentDate   *time.Time     `json:"document_date,omitempty"`
	DocumentType   string         `gorm:"size:30" json:"document_type,omitempty"`
	TaxableAmount  float64        `gorm:"type:decimal(15,2);default:0" json:"taxable_amount"`
	TaxAmount      float64        `gorm:"type:decimal(15,2);default:0" json:"tax_amount"`
	CGSTAmount     float64        `gorm:"type:decimal(15,2);default:0" json:"cgst_amount"`
	SGSTAmount     float64        `gorm:"type:decimal(15,2);default:0" json:"sgst_amount"`
	IGSTAmount     float64        `gorm:"type:decimal(15,2);default:0" json:"igst_amount"`
	CessAmount     float64        `gorm:"type:decimal(15,2);default:0" json:"cess_amount"`
	PlaceOfSupply  string         `gorm:"size:10" json:"place_of_supply,omitempty"`
	RawPayload     string         `gorm:"type:jsonb;default:'{}'" json:"raw_payload,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GSTR2BImportLine) TableName() string {
	return "gstr2b_import_lines"
}

type GSTR2BMatchResult struct {
	ID                  string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ImportID            string         `gorm:"not null;index" json:"import_id"`
	BusinessID          string         `gorm:"not null;index" json:"business_id"`
	ImportLineID        *string        `gorm:"index" json:"import_line_id,omitempty"`
	DocumentID          *string        `gorm:"index" json:"document_id,omitempty"`
	Status              string         `gorm:"not null;size:30;index" json:"status"`
	MismatchReason      string         `gorm:"type:text" json:"mismatch_reason,omitempty"`
	BooksTaxableAmount  float64        `gorm:"type:decimal(15,2);default:0" json:"books_taxable_amount"`
	ImportTaxableAmount float64        `gorm:"type:decimal(15,2);default:0" json:"import_taxable_amount"`
	BooksTaxAmount      float64        `gorm:"type:decimal(15,2);default:0" json:"books_tax_amount"`
	ImportTaxAmount     float64        `gorm:"type:decimal(15,2);default:0" json:"import_tax_amount"`
	Metadata            string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GSTR2BMatchResult) TableName() string {
	return "gstr2b_match_results"
}
