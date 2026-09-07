package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	InvoiceSubscriptionStatusActive   = "active"
	InvoiceSubscriptionStatusPaused   = "paused"
	InvoiceSubscriptionStatusCanceled = "canceled"
)

const (
	InvoiceSubscriptionPricePolicyFreeze = "freeze_on_create"
	InvoiceSubscriptionPricePolicyFollow = "follow_product_price"
)

const (
	BulkJobStatusPending      = "pending"
	BulkJobStatusQueued       = "queued"
	BulkJobStatusProcessing   = "processing"
	BulkJobStatusCompleted    = "completed"
	BulkJobStatusFailed       = "failed"
	BulkJobStatusValidating   = "validating"
	BulkJobStatusValidated    = "validated"
	BulkJobStatusCommitQueued = "commit_queued"
	BulkJobStatusCommitting   = "committing"
	BulkJobStatusCanceled     = "canceled"
	BulkJobStatusExpired      = "expired"
)

const (
	BulkJobTypeImportCustomers = "import_customers"
	BulkJobTypeImportVendors   = "import_vendors"
	BulkJobTypeImportProducts  = "import_products"
	BulkJobTypeImportInvoices  = "import_invoices"
	BulkJobTypeImportDocuments = "import_documents"
	BulkJobTypeBulkInvoices    = "bulk_invoices"
	BulkJobTypeBulkDocuments   = "bulk_documents"
)

const (
	BulkJobActionEdit     = "edit"
	BulkJobActionDownload = "download"
	BulkJobActionConvert  = "convert"
	BulkJobActionMerge    = "merge"
	BulkJobActionCancel   = "cancel"
	BulkJobActionSign     = "sign"
)

const (
	PriceListScopeBusiness  = "business"
	PriceListScopeCustomer  = "customer"
	PriceListScopeVendor    = "vendor"
	PriceListScopeParty     = "party_group"
	PriceListScopeWarehouse = "warehouse"
)

const (
	PriceListEntityProduct = "product"
	PriceListEntityVariant = "variant"
)

type InvoiceSubscription struct {
	ID                 string                     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID         string                     `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	CustomerID         string                     `gorm:"not null;index" json:"customer_id" validate:"required,uuid"`
	Name               string                     `gorm:"not null;size:160" json:"name"`
	Status             string                     `gorm:"not null;size:30;default:'active';index" json:"status"`
	Cadence            string                     `gorm:"not null;size:40" json:"cadence"`
	Timezone           string                     `gorm:"not null;size:64;default:'UTC'" json:"timezone"`
	StartDate          time.Time                  `gorm:"not null" json:"start_date"`
	EndDate            *time.Time                 `json:"end_date,omitempty"`
	NextRunAt          *time.Time                 `gorm:"index" json:"next_run_at,omitempty"`
	LastRunAt          *time.Time                 `json:"last_run_at,omitempty"`
	AutoSend           bool                       `gorm:"default:false" json:"auto_send"`
	PricePolicy        string                     `gorm:"not null;size:40;default:'freeze_on_create'" json:"price_policy"`
	PriceListID        *string                    `gorm:"index" json:"price_list_id,omitempty" validate:"omitempty,uuid"`
	Currency           string                     `gorm:"not null;size:3;default:'INR'" json:"currency"`
	Notes              string                     `gorm:"type:text" json:"notes,omitempty"`
	Metadata           string                     `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	TemplateInvoiceID  *string                    `gorm:"index" json:"template_invoice_id,omitempty" validate:"omitempty,uuid"`
	TemplateDocumentID *string                    `gorm:"index" json:"template_document_id,omitempty" validate:"omitempty,uuid"`
	CreatedAt          time.Time                  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time                  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt             `gorm:"index" json:"-"`
	Lines              []*InvoiceSubscriptionLine `gorm:"foreignKey:SubscriptionID" json:"lines,omitempty"`
	RunCount           int64                      `gorm:"-" json:"run_count,omitempty"`
	LastRunStatus      string                     `gorm:"-" json:"last_run_status,omitempty"`
}

func (InvoiceSubscription) TableName() string {
	return "invoice_subscriptions"
}

type InvoiceSubscriptionLine struct {
	ID               string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	SubscriptionID   string    `gorm:"not null;index" json:"subscription_id" validate:"required,uuid"`
	ProductID        *string   `gorm:"index" json:"product_id,omitempty" validate:"omitempty,uuid"`
	VariantID        *string   `gorm:"index" json:"variant_id,omitempty" validate:"omitempty,uuid"`
	WarehouseID      *string   `gorm:"index" json:"warehouse_id,omitempty" validate:"omitempty,uuid"`
	Description      string    `gorm:"not null;size:500" json:"description"`
	Quantity         float64   `gorm:"type:decimal(15,3);not null;default:0" json:"quantity"`
	FreeQuantity     float64   `gorm:"type:decimal(15,3);default:0" json:"free_quantity"`
	UnitPrice        float64   `gorm:"type:decimal(15,2);not null;default:0" json:"unit_price"`
	MRP              float64   `gorm:"type:decimal(15,2);default:0" json:"mrp"`
	DiscountAmount   float64   `gorm:"type:decimal(15,2);default:0" json:"discount_amount"`
	TaxRate          float64   `gorm:"type:decimal(7,3);default:0" json:"tax_rate"`
	CessRate         float64   `gorm:"type:decimal(7,3);default:0" json:"cess_rate"`
	CustomFields     string    `gorm:"type:jsonb;default:'{}'" json:"custom_fields,omitempty"`
	AdditionalCharge string    `gorm:"type:jsonb;default:'{}'" json:"additional_charge,omitempty"`
	Position         int       `gorm:"default:0" json:"position"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (InvoiceSubscriptionLine) TableName() string {
	return "invoice_subscription_lines"
}

type InvoiceSubscriptionRun struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	SubscriptionID string         `gorm:"not null;index" json:"subscription_id" validate:"required,uuid"`
	BusinessID     string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ScheduledFor   time.Time      `gorm:"not null;index" json:"scheduled_for"`
	Status         string         `gorm:"not null;size:30;default:'pending';index" json:"status"`
	IdempotencyKey string         `gorm:"not null;size:180;uniqueIndex" json:"idempotency_key"`
	InvoiceID      *string        `gorm:"index" json:"invoice_id,omitempty" validate:"omitempty,uuid"`
	DocumentID     *string        `gorm:"index" json:"document_id,omitempty" validate:"omitempty,uuid"`
	AttemptCount   int            `gorm:"default:0" json:"attempt_count"`
	LastError      *string        `gorm:"type:text" json:"last_error,omitempty"`
	Metadata       string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (InvoiceSubscriptionRun) TableName() string {
	return "invoice_subscription_runs"
}

type PriceList struct {
	ID          string           `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string           `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name        string           `gorm:"not null;size:160" json:"name"`
	Code        string           `gorm:"not null;size:80;index" json:"code"`
	Description string           `gorm:"type:text" json:"description,omitempty"`
	Currency    string           `gorm:"not null;size:3;default:'INR'" json:"currency"`
	IsDefault   bool             `gorm:"default:false" json:"is_default"`
	IsActive    bool             `gorm:"default:true;index" json:"is_active"`
	Metadata    string           `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt   time.Time        `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time        `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt   `gorm:"index" json:"-"`
	Items       []*PriceListItem `gorm:"foreignKey:PriceListID" json:"items,omitempty"`
}

func (PriceList) TableName() string {
	return "price_lists"
}

type PriceListItem struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	PriceListID string         `gorm:"not null;index" json:"price_list_id" validate:"required,uuid"`
	EntityType  string         `gorm:"not null;size:20" json:"entity_type"`
	ProductID   *string        `gorm:"index" json:"product_id,omitempty" validate:"omitempty,uuid"`
	VariantID   *string        `gorm:"index" json:"variant_id,omitempty" validate:"omitempty,uuid"`
	Price       float64        `gorm:"type:decimal(15,2);not null;default:0" json:"price"`
	MRP         float64        `gorm:"type:decimal(15,2);default:0" json:"mrp"`
	CessRate    float64        `gorm:"type:decimal(7,3);default:0" json:"cess_rate"`
	Metadata    string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PriceListItem) TableName() string {
	return "price_list_items"
}

type PriceListAssignment struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	PriceListID string         `gorm:"not null;index" json:"price_list_id" validate:"required,uuid"`
	ScopeType   string         `gorm:"not null;size:30;index" json:"scope_type"`
	ScopeID     string         `gorm:"not null;size:120;index" json:"scope_id"`
	Priority    int            `gorm:"default:0" json:"priority"`
	Metadata    string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PriceListAssignment) TableName() string {
	return "price_list_assignments"
}

type PartyGroup struct {
	ID              string              `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string              `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name            string              `gorm:"not null;size:160" json:"name"`
	Description     string              `gorm:"type:text" json:"description,omitempty"`
	CreatedAt       time.Time           `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time           `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt      `gorm:"index" json:"-"`
	Members         []*PartyGroupMember `gorm:"foreignKey:PartyGroupID" json:"members,omitempty"`
	MemberCount     int                 `gorm:"-" json:"member_count,omitempty"`
	InvoiceCount    int64               `gorm:"-" json:"invoice_count,omitempty"`
	DocumentCount   int64               `gorm:"-" json:"document_count,omitempty"`
	ReceivableTotal float64             `gorm:"-" json:"receivable_total,omitempty"`
	PayableTotal    float64             `gorm:"-" json:"payable_total,omitempty"`
	NetBalance      float64             `gorm:"-" json:"net_balance,omitempty"`
}

func (PartyGroup) TableName() string {
	return "party_groups"
}

type PartyGroupMember struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	PartyGroupID string         `gorm:"not null;index" json:"party_group_id" validate:"required,uuid"`
	BusinessID   string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	PartyType    string         `gorm:"not null;size:20;index" json:"party_type"`
	PartyID      string         `gorm:"not null;index" json:"party_id"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PartyGroupMember) TableName() string {
	return "party_group_members"
}

type BulkJob struct {
	ID                string             `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string             `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	CreatedBy         string             `gorm:"not null;index" json:"created_by" validate:"required,uuid"`
	JobType           string             `gorm:"not null;size:50;index" json:"job_type"`
	Action            string             `gorm:"size:50;index" json:"action,omitempty"`
	Status            string             `gorm:"not null;size:30;default:'pending';index" json:"status"`
	FileName          string             `gorm:"size:255" json:"file_name,omitempty"`
	FileKey           string             `gorm:"size:500" json:"-"`
	ContentType       string             `gorm:"size:120" json:"content_type,omitempty"`
	UploadID          *string            `gorm:"type:uuid;index" json:"upload_id,omitempty" validate:"omitempty,uuid"`
	ValidationVersion int                `gorm:"not null;default:0" json:"validation_version"`
	CommitCommandID   *string            `gorm:"type:uuid;index" json:"commit_command_id,omitempty" validate:"omitempty,uuid"`
	CancelRequested   bool               `gorm:"not null;default:false" json:"cancel_requested"`
	AttemptCount      int                `gorm:"not null;default:0" json:"attempt_count"`
	NextRetryAt       *time.Time         `gorm:"index" json:"next_retry_at,omitempty"`
	LeaseOwner        string             `gorm:"size:160" json:"-"`
	LeaseExpiresAt    *time.Time         `gorm:"index" json:"-"`
	RetainUntil       *time.Time         `gorm:"index" json:"retain_until,omitempty"`
	ArtifactState     string             `gorm:"size:30;not null;default:'pending'" json:"artifact_state"`
	NotificationState string             `gorm:"size:30;not null;default:'pending'" json:"notification_state"`
	TotalRows         int                `gorm:"default:0" json:"total_rows"`
	ProcessedRows     int                `gorm:"default:0" json:"processed_rows"`
	SucceededRows     int                `gorm:"default:0" json:"succeeded_rows"`
	FailedRows        int                `gorm:"default:0" json:"failed_rows"`
	RequestPayload    string             `gorm:"type:jsonb;default:'{}'" json:"request_payload,omitempty"`
	ResultPayload     string             `gorm:"type:jsonb;default:'{}'" json:"result_payload,omitempty"`
	LastError         *string            `gorm:"type:text" json:"last_error,omitempty"`
	QueuedAt          *time.Time         `json:"queued_at,omitempty"`
	StartedAt         *time.Time         `json:"started_at,omitempty"`
	CompletedAt       *time.Time         `json:"completed_at,omitempty"`
	CreatedAt         time.Time          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt     `gorm:"index" json:"-"`
	Rows              []*BulkJobRow      `gorm:"foreignKey:BulkJobID" json:"rows,omitempty"`
	Artifacts         []*BulkJobArtifact `gorm:"foreignKey:BulkJobID" json:"artifacts,omitempty"`
}

func (BulkJob) TableName() string {
	return "bulk_jobs"
}

type BulkJobRow struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BulkJobID      string         `gorm:"not null;index" json:"bulk_job_id" validate:"required,uuid"`
	RowNumber      int            `gorm:"not null" json:"row_number"`
	Status         string         `gorm:"not null;size:30;default:'pending';index" json:"status"`
	EntityID       *string        `gorm:"index" json:"entity_id,omitempty"`
	EntityType     string         `gorm:"size:40" json:"entity_type,omitempty"`
	ValidationKey  string         `gorm:"size:320;index" json:"-"`
	InputHash      string         `gorm:"size:64" json:"-"`
	ErrorCode      string         `gorm:"size:80" json:"error_code,omitempty"`
	ErrorDetails   string         `gorm:"type:jsonb;default:'{}'" json:"error_details,omitempty"`
	IdempotencyKey string         `gorm:"size:180;uniqueIndex" json:"-"`
	AttemptCount   int            `gorm:"not null;default:0" json:"attempt_count"`
	CommittedAt    *time.Time     `json:"committed_at,omitempty"`
	Input          string         `gorm:"type:jsonb;default:'{}'" json:"input,omitempty"`
	Result         string         `gorm:"type:jsonb;default:'{}'" json:"result,omitempty"`
	Error          string         `gorm:"type:text" json:"error,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BulkJobRow) TableName() string {
	return "bulk_job_rows"
}

type BulkJobArtifact struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BulkJobID    string         `gorm:"not null;index" json:"bulk_job_id" validate:"required,uuid"`
	ArtifactType string         `gorm:"not null;size:50" json:"artifact_type"`
	FileName     string         `gorm:"not null;size:255" json:"file_name"`
	FileKey      string         `gorm:"not null;size:500" json:"-"`
	Status       string         `gorm:"not null;size:30;default:'pending'" json:"status"`
	ExpiresAt    *time.Time     `gorm:"index" json:"expires_at,omitempty"`
	Metadata     string         `gorm:"type:jsonb;default:'{}'" json:"-"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BulkJobArtifact) TableName() string {
	return "bulk_job_artifacts"
}

type ActivityLog struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ActorID    string         `gorm:"not null;index" json:"actor_id"`
	ActorRole  string         `gorm:"size:80" json:"actor_role,omitempty"`
	RequestID  string         `gorm:"size:100;index" json:"request_id,omitempty"`
	IPAddress  string         `gorm:"size:100" json:"ip_address,omitempty"`
	EntityType string         `gorm:"not null;size:60;index" json:"entity_type"`
	EntityID   string         `gorm:"not null;size:120;index" json:"entity_id"`
	Action     string         `gorm:"not null;size:80;index" json:"action"`
	Reason     string         `gorm:"type:text" json:"reason,omitempty"`
	Snapshot   string         `gorm:"type:jsonb;default:'{}'" json:"snapshot,omitempty"`
	Diff       string         `gorm:"type:jsonb;default:'{}'" json:"diff,omitempty"`
	Metadata   string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt  time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ActivityLog) TableName() string {
	return "activity_logs"
}

type SignatureProfile struct {
	ID            string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID    string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name          string         `gorm:"not null;size:160" json:"name"`
	Provider      string         `gorm:"size:80" json:"provider,omitempty"`
	SignerName    string         `gorm:"size:160" json:"signer_name,omitempty"`
	CertificateSN string         `gorm:"size:160" json:"certificate_sn,omitempty"`
	FileName      string         `gorm:"size:255" json:"file_name,omitempty"`
	FileKey       string         `gorm:"size:500" json:"file_key,omitempty"`
	EncryptionIV  string         `gorm:"size:255" json:"encryption_iv,omitempty"`
	Metadata      string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	IsActive      bool           `gorm:"default:true;index" json:"is_active"`
	LastUsedAt    *time.Time     `json:"last_used_at,omitempty"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (SignatureProfile) TableName() string {
	return "signature_profiles"
}

type SignedDocumentArtifact struct {
	ID                 string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID         string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	DocumentID         *string        `gorm:"index" json:"document_id,omitempty" validate:"omitempty,uuid"`
	InvoiceID          *string        `gorm:"index" json:"invoice_id,omitempty" validate:"omitempty,uuid"`
	SignatureProfileID string         `gorm:"not null;index" json:"signature_profile_id" validate:"required,uuid"`
	FileName           string         `gorm:"not null;size:255" json:"file_name"`
	FileKey            string         `gorm:"not null;size:500" json:"file_key"`
	SourcePDFURL       string         `gorm:"size:500" json:"source_pdf_url,omitempty"`
	SignedPDFURL       string         `gorm:"size:500" json:"signed_pdf_url,omitempty"`
	Metadata           string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

func (SignedDocumentArtifact) TableName() string {
	return "signed_document_artifacts"
}

type CustomFieldDefinition struct {
	ID                 string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID         string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	EntityType         string         `gorm:"not null;size:40;index" json:"entity_type"`
	Name               string         `gorm:"not null;size:160" json:"name"`
	Slug               string         `gorm:"not null;size:160;index" json:"slug"`
	DataType           string         `gorm:"not null;size:40" json:"data_type"`
	Visibility         string         `gorm:"type:jsonb;default:'[]'" json:"visibility,omitempty"`
	DefaultValue       string         `gorm:"type:jsonb;default:'null'" json:"default_value,omitempty"`
	Options            string         `gorm:"type:jsonb;default:'[]'" json:"options,omitempty"`
	IsRequired         bool           `gorm:"default:false" json:"is_required"`
	IsActive           bool           `gorm:"default:true;index" json:"is_active"`
	SortOrder          int            `gorm:"default:0" json:"sort_order"`
	LinkedDefinitionID *string        `gorm:"index" json:"linked_definition_id,omitempty" validate:"omitempty,uuid"`
	Metadata           string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

func (CustomFieldDefinition) TableName() string {
	return "custom_field_definitions"
}

type CustomFieldValue struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID   string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	DefinitionID string         `gorm:"not null;index" json:"definition_id" validate:"required,uuid"`
	EntityType   string         `gorm:"not null;size:40;index" json:"entity_type"`
	EntityID     string         `gorm:"not null;size:120;index" json:"entity_id"`
	Value        string         `gorm:"type:jsonb;default:'null'" json:"value,omitempty"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (CustomFieldValue) TableName() string {
	return "custom_field_values"
}

type ChargeDefinition struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID   string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name         string         `gorm:"not null;size:160" json:"name"`
	Slug         string         `gorm:"not null;size:160;index" json:"slug"`
	ChargeType   string         `gorm:"not null;size:40" json:"charge_type"`
	ValueType    string         `gorm:"not null;size:40" json:"value_type"`
	DefaultValue float64        `gorm:"type:decimal(15,2);default:0" json:"default_value"`
	Taxable      bool           `gorm:"default:false" json:"taxable"`
	IsActive     bool           `gorm:"default:true;index" json:"is_active"`
	Visibility   string         `gorm:"type:jsonb;default:'[]'" json:"visibility,omitempty"`
	Metadata     string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ChargeDefinition) TableName() string {
	return "charge_definitions"
}

type PartyAddress struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	PartyType   string         `gorm:"not null;size:20;index" json:"party_type"`
	PartyID     string         `gorm:"not null;size:120;index" json:"party_id"`
	AddressType string         `gorm:"not null;size:30;index" json:"address_type"`
	Label       string         `gorm:"size:120" json:"label,omitempty"`
	ContactName string         `gorm:"size:160" json:"contact_name,omitempty"`
	Line1       string         `gorm:"size:255" json:"line1,omitempty"`
	Line2       string         `gorm:"size:255" json:"line2,omitempty"`
	City        string         `gorm:"size:120" json:"city,omitempty"`
	State       string         `gorm:"size:120" json:"state,omitempty"`
	Country     string         `gorm:"size:120" json:"country,omitempty"`
	PostalCode  string         `gorm:"size:30" json:"postal_code,omitempty"`
	GSTIN       string         `gorm:"size:20" json:"gstin,omitempty"`
	StateCode   string         `gorm:"size:10" json:"state_code,omitempty"`
	IsDefault   bool           `gorm:"default:false" json:"is_default"`
	Metadata    string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PartyAddress) TableName() string {
	return "party_addresses"
}
