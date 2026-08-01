package models

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	DocumentTypeSalesInvoice    = "sales_invoice"
	DocumentTypePurchaseInvoice = "purchase_invoice"
	DocumentTypePurchaseOrder   = "purchase_order"
	DocumentTypeSalesOrder      = "sales_order"
	DocumentTypeQuotation       = "quotation"
	DocumentTypeProformaInvoice = "proforma_invoice"
	DocumentTypeDeliveryChallan = "delivery_challan"
	DocumentTypeCreditNote      = "credit_note"
	DocumentTypeDebitNote       = "debit_note"
	DocumentTypeBillOfSupply    = "bill_of_supply"
	DocumentTypePackingList     = "packing_list"
	DocumentTypeShippingLabel   = "shipping_label"
	DocumentTypeExpense         = "expense"
)

const (
	DocumentPartyTypeCustomer = "customer"
	DocumentPartyTypeVendor   = "vendor"
	DocumentPartyTypeManual   = "manual"
)

const (
	DocumentStatusDraft              = "draft"
	DocumentStatusIssued             = "issued"
	DocumentStatusSent               = "sent"
	DocumentStatusAccepted           = "accepted"
	DocumentStatusPartiallyConverted = "partially_converted"
	DocumentStatusFullyConverted     = "fully_converted"
	DocumentStatusCancelled          = "cancelled"
	DocumentStatusCompleted          = "completed"
)

const (
	DocumentDraftStateDraft    = "draft"
	DocumentDraftStateFinal    = "final"
	DocumentDraftStateArchived = "archived"
)

const (
	DocumentTaxModeGST    = "gst"
	DocumentTaxModeNonGST = "non_gst"
)

const (
	DocumentGSTTreatmentRegular     = "regular"
	DocumentGSTTreatmentComposition = "composition"
	DocumentGSTTreatmentExportLUT   = "export_lut"
	DocumentGSTTreatmentExportIGST  = "export_igst"
	DocumentGSTTreatmentSEZ         = "sez"
	DocumentGSTTreatmentExempt      = "exempt"
)

const (
	DocumentDirectionOutward = "outward"
	DocumentDirectionInward  = "inward"
)

const (
	DocumentLinkTypeConvertedFrom = "converted_from"
	DocumentLinkTypeConvertedTo   = "converted_to"
	DocumentLinkTypeReturnOf      = "return_of"
	DocumentLinkTypeChallanFor    = "challan_for"
	DocumentLinkTypePackingListOf = "packing_list_of"
	DocumentLinkTypeShippingOf    = "shipping_label_of"
	DocumentLinkTypeMergedFrom    = "merged_from"
	DocumentLinkTypeDuplicateOf   = "duplicate_of"
)

const (
	RenderJobStatusQueued     = "queued"
	RenderJobStatusProcessing = "processing"
	RenderJobStatusCompleted  = "completed"
	RenderJobStatusFailed     = "failed"
	RenderJobStatusObsolete   = "obsolete"
)

type RenderKind string

const (
	RenderKindPreview RenderKind = "preview"
	RenderKindFinal   RenderKind = "final"
)

var ErrInvalidRenderKind = errors.New("invalid render kind")

type DocumentDraftConflictError struct{}

func (e *DocumentDraftConflictError) Error() string {
	return "document draft changed before update"
}

type Document struct {
	ID                    string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID            string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	BranchID              *string        `gorm:"index" json:"branch_id,omitempty" validate:"omitempty,uuid"`
	DocumentType          string         `gorm:"not null;size:50;index" json:"document_type"`
	PartyType             string         `gorm:"not null;size:20;index" json:"party_type"`
	PartyID               *string        `gorm:"index" json:"party_id,omitempty" validate:"omitempty,uuid"`
	Status                string         `gorm:"not null;size:50;default:'draft';index" json:"status"`
	DraftState            string         `gorm:"not null;size:50;default:'draft'" json:"draft_state"`
	TaxMode               string         `gorm:"not null;size:20;default:'gst'" json:"tax_mode"`
	GSTTreatment          string         `gorm:"not null;size:50;default:'regular'" json:"gst_treatment"`
	PlaceOfSupply         string         `gorm:"size:50" json:"place_of_supply,omitempty"`
	PartyGSTIN            string         `gorm:"size:20" json:"party_gstin,omitempty"`
	PartyPAN              string         `gorm:"size:10" json:"party_pan,omitempty"`
	PartyStateCode        string         `gorm:"size:10" json:"party_state_code,omitempty"`
	SupplyType            string         `gorm:"size:40" json:"supply_type,omitempty"`
	ExportType            string         `gorm:"size:40" json:"export_type,omitempty"`
	BillOfSupply          bool           `gorm:"default:false" json:"bill_of_supply"`
	SerialNumber          string         `gorm:"not null;size:80;index" json:"serial_number"`
	IssueDate             time.Time      `gorm:"not null;index" json:"issue_date"`
	DueDate               *time.Time     `json:"due_date,omitempty"`
	DispatchDate          *time.Time     `json:"dispatch_date,omitempty"`
	Currency              string         `gorm:"not null;size:3;default:'INR'" json:"currency"`
	ExchangeRate          float64        `gorm:"type:decimal(18,6);default:1" json:"exchange_rate"`
	FXProvider            string         `gorm:"size:80" json:"fx_provider,omitempty"`
	FXBaseCurrency        string         `gorm:"size:3" json:"fx_base_currency,omitempty"`
	FXQuoteCurrency       string         `gorm:"size:3" json:"fx_quote_currency,omitempty"`
	FXRateTimestamp       *time.Time     `json:"fx_rate_timestamp,omitempty"`
	FXMetadata            string         `gorm:"type:jsonb;default:'{}'" json:"fx_metadata,omitempty"`
	Locale                string         `gorm:"not null;size:20;default:'en-IN'" json:"locale"`
	SourceLinkage         string         `gorm:"type:jsonb;default:'{}'" json:"source_linkage,omitempty"`
	RenderProfileID       *string        `gorm:"index" json:"render_profile_id,omitempty" validate:"omitempty,uuid"`
	ShipmentID            *string        `gorm:"index" json:"shipment_id,omitempty" validate:"omitempty,uuid"`
	ProjectID             *string        `gorm:"index" json:"project_id,omitempty" validate:"omitempty,uuid"`
	PriceListID           *string        `gorm:"index" json:"price_list_id,omitempty" validate:"omitempty,uuid"`
	OriginSubscriptionID  *string        `gorm:"index" json:"origin_subscription_id,omitempty" validate:"omitempty,uuid"`
	OriginRunID           *string        `gorm:"index" json:"origin_run_id,omitempty" validate:"omitempty,uuid"`
	ProfitSnapshotEnabled bool           `gorm:"default:false" json:"profit_snapshot_enabled"`
	GenerateEInvoice      bool           `gorm:"default:false" json:"generate_einvoice"`
	GenerateEWayBill      bool           `gorm:"default:false" json:"generate_ewaybill"`
	ReverseCharge         bool           `gorm:"default:false" json:"reverse_charge"`
	ReverseChargeReason   string         `gorm:"type:text" json:"reverse_charge_reason,omitempty"`
	DispatchFrom          string         `gorm:"type:jsonb;default:'{}'" json:"dispatch_from,omitempty"`
	DispatchTo            string         `gorm:"type:jsonb;default:'{}'" json:"dispatch_to,omitempty"`
	DistanceKM            float64        `gorm:"type:decimal(10,2);default:0" json:"distance_km"`
	Transporter           string         `gorm:"type:jsonb;default:'{}'" json:"transporter,omitempty"`
	Vehicle               string         `gorm:"type:jsonb;default:'{}'" json:"vehicle,omitempty"`
	MultiVehiclePlan      string         `gorm:"type:jsonb;default:'{}'" json:"multi_vehicle_plan,omitempty"`
	CurrentEInvoiceID     *string        `gorm:"index" json:"current_einvoice_id,omitempty" validate:"omitempty,uuid"`
	CurrentEWayBillID     *string        `gorm:"index" json:"current_ewaybill_id,omitempty" validate:"omitempty,uuid"`
	SignedAt              *time.Time     `gorm:"index" json:"signed_at,omitempty"`
	SignedByProfileID     *string        `gorm:"index" json:"signed_by_profile_id,omitempty" validate:"omitempty,uuid"`
	SignMetadata          string         `gorm:"type:jsonb;default:'{}'" json:"sign_metadata,omitempty"`
	CancellationReason    *string        `gorm:"type:text" json:"cancellation_reason,omitempty"`
	CancelledAt           *time.Time     `json:"cancelled_at,omitempty"`
	PDFURL                string         `gorm:"size:500" json:"pdf_url,omitempty"`
	PDFFilename           string         `gorm:"size:255" json:"pdf_filename,omitempty"`
	Notes                 string         `gorm:"type:text" json:"notes,omitempty"`
	Terms                 string         `gorm:"type:text" json:"terms,omitempty"`
	Declaration           string         `gorm:"type:text" json:"declaration,omitempty"`
	Direction             string         `gorm:"size:20" json:"direction,omitempty"`
	Subtotal              float64        `gorm:"type:decimal(15,2);default:0" json:"subtotal"`
	DiscountTotal         float64        `gorm:"type:decimal(15,2);default:0" json:"discount_total"`
	TaxTotal              float64        `gorm:"type:decimal(15,2);default:0" json:"tax_total"`
	CessTotal             float64        `gorm:"type:decimal(15,2);default:0" json:"cess_total"`
	WithholdingTotal      float64        `gorm:"type:decimal(15,2);default:0" json:"withholding_total"`
	TDSTotal              float64        `gorm:"type:decimal(15,2);default:0" json:"tds_total"`
	TCSTotal              float64        `gorm:"type:decimal(15,2);default:0" json:"tcs_total"`
	Total                 float64        `gorm:"type:decimal(15,2);default:0" json:"total"`
	PaidAmount            float64        `gorm:"type:decimal(15,2);default:0" json:"paid_amount"`
	BalanceDue            float64        `gorm:"type:decimal(15,2);default:0" json:"balance_due"`
	ExtraFields           string         `gorm:"type:jsonb;default:'{}'" json:"extra_fields,omitempty"`
	ReportTags            string         `gorm:"type:jsonb;default:'{}'" json:"report_tags,omitempty"`
	CreatedAt             time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt             time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt             gorm.DeletedAt `gorm:"index" json:"-"`

	Lines []*DocumentLine `gorm:"foreignKey:DocumentID" json:"lines,omitempty"`
}

func (Document) TableName() string {
	return "documents"
}

type DocumentLine struct {
	ID                string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	DocumentID        string    `gorm:"not null;index" json:"document_id" validate:"required,uuid"`
	ProductID         *string   `gorm:"index" json:"product_id,omitempty" validate:"omitempty,uuid"`
	VariantID         *string   `gorm:"index" json:"variant_id,omitempty" validate:"omitempty,uuid"`
	Description       string    `gorm:"not null;size:500" json:"description"`
	HSNSACCode        string    `gorm:"size:40" json:"hsn_sac_code,omitempty"`
	UQCCode           string    `gorm:"size:20;default:'OTH'" json:"uqc_code,omitempty"`
	Unit              string    `gorm:"size:40" json:"unit,omitempty"`
	WarehouseID       *string   `gorm:"index" json:"warehouse_id,omitempty" validate:"omitempty,uuid"`
	Quantity          float64   `gorm:"type:decimal(15,3);not null;default:0" json:"quantity"`
	FreeQuantity      float64   `gorm:"type:decimal(15,3);default:0" json:"free_quantity"`
	RemainingQuantity float64   `gorm:"type:decimal(15,3);default:0" json:"remaining_quantity"`
	UnitPrice         float64   `gorm:"type:decimal(15,2);not null;default:0" json:"unit_price"`
	MRP               float64   `gorm:"type:decimal(15,2);default:0" json:"mrp"`
	DiscountAmount    float64   `gorm:"type:decimal(15,2);default:0" json:"discount_amount"`
	TaxRate           float64   `gorm:"type:decimal(7,3);default:0" json:"tax_rate"`
	CGSTRate          float64   `gorm:"type:decimal(7,3);default:0" json:"cgst_rate"`
	SGSTRate          float64   `gorm:"type:decimal(7,3);default:0" json:"sgst_rate"`
	IGSTRate          float64   `gorm:"type:decimal(7,3);default:0" json:"igst_rate"`
	CessRate          float64   `gorm:"type:decimal(7,3);default:0" json:"cess_rate"`
	CGSTAmount        float64   `gorm:"type:decimal(15,2);default:0" json:"cgst_amount"`
	SGSTAmount        float64   `gorm:"type:decimal(15,2);default:0" json:"sgst_amount"`
	IGSTAmount        float64   `gorm:"type:decimal(15,2);default:0" json:"igst_amount"`
	CessAmount        float64   `gorm:"type:decimal(15,2);default:0" json:"cess_amount"`
	TaxAmount         float64   `gorm:"type:decimal(15,2);default:0" json:"tax_amount"`
	LineSubtotal      float64   `gorm:"type:decimal(15,2);default:0" json:"line_subtotal"`
	LineTotal         float64   `gorm:"type:decimal(15,2);default:0" json:"line_total"`
	CostSnapshot      float64   `gorm:"type:decimal(15,2);default:0" json:"cost_snapshot"`
	MarginSnapshot    float64   `gorm:"type:decimal(15,2);default:0" json:"margin_snapshot"`
	CustomFields      string    `gorm:"type:jsonb;default:'{}'" json:"custom_fields,omitempty"`
	ChargeLinkage     string    `gorm:"type:jsonb;default:'[]'" json:"charge_linkage,omitempty"`
	PackingMetadata   string    `gorm:"type:jsonb;default:'{}'" json:"packing_metadata,omitempty"`
	BatchAllocations  string    `gorm:"type:jsonb;default:'[]'" json:"batch_allocations,omitempty"`
	SerialIDs         string    `gorm:"type:jsonb;default:'[]'" json:"serial_ids,omitempty"`
	ReportTags        string    `gorm:"type:jsonb;default:'{}'" json:"report_tags,omitempty"`
	StockEffect       string    `gorm:"size:20;default:'none'" json:"stock_effect,omitempty"`
	CreatedAt         time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (DocumentLine) TableName() string {
	return "document_lines"
}

type DocumentLink struct {
	ID                string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string    `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	SourceDocumentID  string    `gorm:"not null;index" json:"source_document_id" validate:"required,uuid"`
	SourceLineID      *string   `gorm:"index" json:"source_line_id,omitempty" validate:"omitempty,uuid"`
	TargetDocumentID  string    `gorm:"not null;index" json:"target_document_id" validate:"required,uuid"`
	TargetLineID      *string   `gorm:"index" json:"target_line_id,omitempty" validate:"omitempty,uuid"`
	LinkType          string    `gorm:"not null;size:50;index" json:"link_type"`
	QuantityUsed      float64   `gorm:"type:decimal(15,3);default:0" json:"quantity_used"`
	QuantityRemaining float64   `gorm:"type:decimal(15,3);default:0" json:"quantity_remaining"`
	Metadata          string    `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt         time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (DocumentLink) TableName() string {
	return "document_links"
}

type RenderProfile struct {
	ID                string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name              string         `gorm:"not null;size:100" json:"name"`
	HeaderHTML        string         `gorm:"type:text" json:"header_html,omitempty"`
	FooterHTML        string         `gorm:"type:text" json:"footer_html,omitempty"`
	WatermarkText     string         `gorm:"size:255" json:"watermark_text,omitempty"`
	BannerText        string         `gorm:"size:255" json:"banner_text,omitempty"`
	FontFamily        string         `gorm:"size:80;default:'Noto Sans'" json:"font_family,omitempty"`
	PageSize          string         `gorm:"size:20;default:'A4'" json:"page_size,omitempty"`
	LayoutConfig      string         `gorm:"type:jsonb;default:'{}'" json:"layout_config,omitempty"`
	PasswordProtected bool           `gorm:"default:false" json:"password_protected"`
	Password          string         `gorm:"size:255" json:"password,omitempty"`
	CopyAllowed       bool           `gorm:"default:true" json:"copy_allowed"`
	PrintAllowed      bool           `gorm:"default:true" json:"print_allowed"`
	CustomLabels      string         `gorm:"type:jsonb;default:'{}'" json:"custom_labels,omitempty"`
	VisibilityConfig  string         `gorm:"type:jsonb;default:'{}'" json:"visibility_config,omitempty"`
	IsDefault         bool           `gorm:"default:false" json:"is_default"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (RenderProfile) TableName() string {
	return "render_profiles"
}

type DocumentRenderJob struct {
	ID                   string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	DocumentID           *string        `gorm:"index" json:"document_id,omitempty" validate:"omitempty,uuid"`
	InvoiceID            *string        `gorm:"index" json:"invoice_id,omitempty" validate:"omitempty,uuid"`
	BusinessID           string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	RenderProfileID      *string        `gorm:"index" json:"render_profile_id,omitempty" validate:"omitempty,uuid"`
	Kind                 RenderKind     `gorm:"not null;size:16;default:'preview'" json:"kind"`
	SourceInvoiceVersion *int           `json:"source_invoice_version,omitempty"`
	ObjectKey            string         `gorm:"size:1024" json:"object_key,omitempty"`
	Attempts             int            `gorm:"not null;default:0" json:"attempts"`
	LeaseOwner           *string        `gorm:"size:255" json:"lease_owner,omitempty"`
	LeaseExpiresAt       *time.Time     `json:"lease_expires_at,omitempty"`
	Status               string         `gorm:"not null;size:30;default:'queued';index" json:"status"`
	Locale               string         `gorm:"size:20;default:'en-IN'" json:"locale,omitempty"`
	TemplateVersion      string         `gorm:"size:50;default:'v1'" json:"template_version,omitempty"`
	OutputURL            string         `gorm:"size:500" json:"output_url,omitempty"`
	OutputFilename       string         `gorm:"size:255" json:"output_filename,omitempty"`
	ErrorMessage         string         `gorm:"type:text" json:"error_message,omitempty"`
	RequestedAt          time.Time      `gorm:"autoCreateTime" json:"requested_at"`
	CompletedAt          *time.Time     `json:"completed_at,omitempty"`
	CreatedAt            time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

func (DocumentRenderJob) TableName() string {
	return "document_render_jobs"
}

func (job DocumentRenderJob) ValidateKind() error {
	if job.Kind != RenderKindPreview && job.Kind != RenderKindFinal {
		return ErrInvalidRenderKind
	}
	return nil
}

type DocumentRevision struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	DocumentID string         `gorm:"not null;index" json:"document_id" validate:"required,uuid"`
	BusinessID string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Action     string         `gorm:"not null;size:60;index" json:"action"`
	Snapshot   string         `gorm:"type:jsonb;default:'{}'" json:"snapshot,omitempty"`
	Metadata   string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (DocumentRevision) TableName() string {
	return "document_revisions"
}
