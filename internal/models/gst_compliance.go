package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	GSTOperationGenerateEInvoice = "generate_einvoice"
	GSTOperationCancelEInvoice   = "cancel_einvoice"
	GSTOperationGenerateEWayBill = "generate_ewaybill"
	GSTOperationUpdateEWayPartB  = "update_eway_part_b"
	GSTOperationMultiVehicle     = "multi_vehicle"
	GSTOperationFetchEWayPDF     = "fetch_eway_pdf"
)

const (
	GSTJobStatusQueued         = "queued"
	GSTJobStatusProcessing     = "processing"
	GSTJobStatusSucceeded      = "succeeded"
	GSTJobStatusRetrying       = "retrying"
	GSTJobStatusFailed         = "failed"
	GSTJobStatusNeedsAttention = "needs_attention"
)

const (
	GSTErrorClassRetriable   = "retriable"
	GSTErrorClassValidation  = "validation"
	GSTErrorClassCredentials = "credentials"
	GSTErrorClassDuplicate   = "duplicate"
	GSTErrorClassRule        = "rule"
	GSTErrorClassUnavailable = "unavailable"
	GSTErrorClassUnknown     = "unknown"
)

const (
	EInvoiceStatusPending   = "pending"
	EInvoiceStatusGenerated = "generated"
	EInvoiceStatusCancelled = "cancelled"
	EInvoiceStatusFailed    = "failed"
)

const (
	EWayBillStatusPending      = "pending"
	EWayBillStatusGenerated    = "generated"
	EWayBillStatusPartB        = "part_b_updated"
	EWayBillStatusMultiVehicle = "multi_vehicle"
	EWayBillStatusCancelled    = "cancelled"
	EWayBillStatusFailed       = "failed"
)

const (
	DistanceSourceAuto   = "auto"
	DistanceSourceManual = "manual"
)

type GSTIntegrationAccount struct {
	ID                   string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID           string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Provider             string         `gorm:"not null;size:80;index" json:"provider"`
	ServiceType          string         `gorm:"not null;size:40;index" json:"service_type"`
	GSPName              string         `gorm:"size:120" json:"gsp_name,omitempty"`
	PortalUsername       string         `gorm:"size:160" json:"portal_username,omitempty"`
	EncryptedCredentials string         `gorm:"type:text" json:"-"`
	CredentialHint       string         `gorm:"size:255" json:"credential_hint,omitempty"`
	Status               string         `gorm:"not null;size:30;default:'pending';index" json:"status"`
	LastValidatedAt      *time.Time     `json:"last_validated_at,omitempty"`
	LastError            string         `gorm:"type:text" json:"last_error,omitempty"`
	Metadata             string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt            time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GSTIntegrationAccount) TableName() string {
	return "gst_integration_accounts"
}

type EInvoiceRecord struct {
	ID                   string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID           string         `gorm:"not null;index" json:"business_id"`
	DocumentID           string         `gorm:"not null;index" json:"document_id"`
	IntegrationAccountID *string        `gorm:"index" json:"integration_account_id,omitempty"`
	Status               string         `gorm:"not null;size:30;default:'pending';index" json:"status"`
	IRN                  string         `gorm:"size:120;index" json:"irn,omitempty"`
	AckNumber            string         `gorm:"size:120" json:"ack_number,omitempty"`
	AckDate              *time.Time     `json:"ack_date,omitempty"`
	SignedQRCodePayload  string         `gorm:"type:text" json:"signed_qr_code_payload,omitempty"`
	QRCodeURL            string         `gorm:"size:500" json:"qr_code_url,omitempty"`
	ProviderReferenceID  string         `gorm:"size:120" json:"provider_reference_id,omitempty"`
	ProviderName         string         `gorm:"size:80;index" json:"provider_name,omitempty"`
	RequestPayload       string         `gorm:"type:jsonb;default:'{}'" json:"request_payload,omitempty"`
	ResponsePayload      string         `gorm:"type:jsonb;default:'{}'" json:"response_payload,omitempty"`
	ErrorClass           string         `gorm:"size:30" json:"error_class,omitempty"`
	LastError            string         `gorm:"type:text" json:"last_error,omitempty"`
	GeneratedAt          *time.Time     `json:"generated_at,omitempty"`
	CancelledAt          *time.Time     `json:"cancelled_at,omitempty"`
	CreatedAt            time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

func (EInvoiceRecord) TableName() string {
	return "einvoice_records"
}

type EWayBillRecord struct {
	ID                   string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID           string         `gorm:"not null;index" json:"business_id"`
	DocumentID           string         `gorm:"not null;index" json:"document_id"`
	IntegrationAccountID *string        `gorm:"index" json:"integration_account_id,omitempty"`
	Status               string         `gorm:"not null;size:30;default:'pending';index" json:"status"`
	EWayBillNumber       string         `gorm:"size:120;index" json:"eway_bill_number,omitempty"`
	EWayBillDate         *time.Time     `json:"eway_bill_date,omitempty"`
	ValidUntil           *time.Time     `json:"eway_bill_valid_until,omitempty"`
	SupplyType           string         `gorm:"size:30" json:"supply_type,omitempty"`
	PartAStatus          string         `gorm:"size:30" json:"part_a_status,omitempty"`
	PartBStatus          string         `gorm:"size:30" json:"part_b_status,omitempty"`
	DistanceKM           float64        `gorm:"type:decimal(10,2);default:0" json:"distance_km"`
	DistanceSource       string         `gorm:"size:20;default:'auto'" json:"distance_source,omitempty"`
	Transporter          string         `gorm:"type:jsonb;default:'{}'" json:"transporter,omitempty"`
	Vehicle              string         `gorm:"type:jsonb;default:'{}'" json:"vehicle,omitempty"`
	DispatchFrom         string         `gorm:"type:jsonb;default:'{}'" json:"dispatch_from,omitempty"`
	DispatchTo           string         `gorm:"type:jsonb;default:'{}'" json:"dispatch_to,omitempty"`
	PDFURL               string         `gorm:"size:500" json:"pdf_url,omitempty"`
	ProviderReferenceID  string         `gorm:"size:120" json:"provider_reference_id,omitempty"`
	ProviderName         string         `gorm:"size:80;index" json:"provider_name,omitempty"`
	RequestPayload       string         `gorm:"type:jsonb;default:'{}'" json:"request_payload,omitempty"`
	ResponsePayload      string         `gorm:"type:jsonb;default:'{}'" json:"response_payload,omitempty"`
	ErrorClass           string         `gorm:"size:30" json:"error_class,omitempty"`
	LastError            string         `gorm:"type:text" json:"last_error,omitempty"`
	GeneratedAt          *time.Time     `json:"generated_at,omitempty"`
	UpdatedPartBAt       *time.Time     `json:"updated_part_b_at,omitempty"`
	CreatedAt            time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

func (EWayBillRecord) TableName() string {
	return "ewaybill_records"
}

type EWayBillVehicleMovement struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	EWayBillID     string         `gorm:"not null;index" json:"eway_bill_id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id"`
	DocumentID     string         `gorm:"not null;index" json:"document_id"`
	MovementType   string         `gorm:"not null;size:30" json:"movement_type"`
	VehicleNo      string         `gorm:"size:40;index" json:"vehicle_no,omitempty"`
	TransportDocNo string         `gorm:"size:80" json:"transport_doc_no,omitempty"`
	FromPlace      string         `gorm:"size:120" json:"from_place,omitempty"`
	FromState      string         `gorm:"size:10" json:"from_state,omitempty"`
	ReasonCode     string         `gorm:"size:40" json:"reason_code,omitempty"`
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	Payload        string         `gorm:"type:jsonb;default:'{}'" json:"payload,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (EWayBillVehicleMovement) TableName() string {
	return "ewaybill_vehicle_movements"
}

type GSTSubmissionJob struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id"`
	DocumentID     string         `gorm:"not null;index" json:"document_id"`
	Operation      string         `gorm:"not null;size:40;index" json:"operation"`
	Status         string         `gorm:"not null;size:30;default:'queued';index" json:"status"`
	IdempotencyKey string         `gorm:"not null;size:180;uniqueIndex" json:"idempotency_key"`
	QueueMessageID string         `gorm:"size:120" json:"queue_message_id,omitempty"`
	AttemptCount   int            `gorm:"default:0" json:"attempt_count"`
	NextAttemptAt  *time.Time     `gorm:"index" json:"next_attempt_at,omitempty"`
	LastAttemptAt  *time.Time     `json:"last_attempt_at,omitempty"`
	SucceededAt    *time.Time     `json:"succeeded_at,omitempty"`
	LastError      string         `gorm:"type:text" json:"last_error,omitempty"`
	ErrorClass     string         `gorm:"size:30" json:"error_class,omitempty"`
	RequestPayload string         `gorm:"type:jsonb;default:'{}'" json:"request_payload,omitempty"`
	ResultPayload  string         `gorm:"type:jsonb;default:'{}'" json:"result_payload,omitempty"`
	Source         string         `gorm:"size:40" json:"source,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GSTSubmissionJob) TableName() string {
	return "gst_submission_jobs"
}

type GSTSubmissionAttempt struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	GSTJobID       string         `gorm:"not null;index" json:"gst_job_id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id"`
	DocumentID     string         `gorm:"not null;index" json:"document_id"`
	AttemptNumber  int            `gorm:"not null" json:"attempt_number"`
	Status         string         `gorm:"not null;size:30" json:"status"`
	ErrorClass     string         `gorm:"size:30" json:"error_class,omitempty"`
	ErrorMessage   string         `gorm:"type:text" json:"error_message,omitempty"`
	RequestPayload string         `gorm:"type:jsonb;default:'{}'" json:"request_payload,omitempty"`
	ResultPayload  string         `gorm:"type:jsonb;default:'{}'" json:"result_payload,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (GSTSubmissionAttempt) TableName() string {
	return "gst_submission_attempts"
}
