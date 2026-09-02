package models

import "time"

type StepUpGrant struct {
	ID              string     `gorm:"primaryKey;type:uuid" json:"id"`
	Subject         string     `gorm:"not null;size:255;index" json:"-"`
	BusinessID      string     `gorm:"type:uuid;not null;index" json:"business_id"`
	Action          string     `gorm:"not null;size:80" json:"action"`
	Resource        string     `gorm:"not null;size:255" json:"resource"`
	CommandHash     string     `gorm:"not null;size:64" json:"-"`
	Assurance       string     `gorm:"not null;size:32" json:"assurance"`
	TokenHash       string     `gorm:"not null;size:64;uniqueIndex" json:"-"`
	AuthenticatedAt time.Time  `gorm:"not null" json:"authenticated_at"`
	IssuedAt        time.Time  `gorm:"not null" json:"issued_at"`
	ExpiresAt       time.Time  `gorm:"not null;index" json:"expires_at"`
	ConsumedAt      *time.Time `json:"consumed_at,omitempty"`
	CreatedAt       time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (*StepUpGrant) TableName() string { return "security_step_up_grants" }

const (
	PendingUploadStatusPending     = "pending"
	PendingUploadStatusQuarantined = "quarantined"
	PendingUploadStatusClean       = "clean"
	PendingUploadStatusRejected    = "rejected"
	PendingUploadStatusDeleted     = "deleted"
	PendingUploadScanBusinessLogo  = "business_logo_attached"
)

type PendingUpload struct {
	ID               string     `gorm:"primaryKey;type:uuid" json:"id"`
	BusinessID       string     `gorm:"type:uuid;not null;index" json:"business_id"`
	UploaderID       string     `gorm:"not null;size:255;index" json:"uploader_id"`
	Kind             string     `gorm:"not null;size:64" json:"kind"`
	Bucket           string     `gorm:"not null;size:128" json:"-"`
	ObjectKey        string     `gorm:"not null;size:1024;uniqueIndex" json:"-"`
	ContentType      string     `gorm:"not null;size:128" json:"content_type"`
	SizeBytes        int64      `gorm:"not null" json:"size_bytes"`
	ChecksumSHA256   string     `gorm:"not null;size:64" json:"checksum_sha256"`
	CleanupObjectKey string     `gorm:"not null;size:1024" json:"-"`
	Status           string     `gorm:"not null;size:24;index" json:"status"`
	ScanCode         string     `gorm:"not null;size:80" json:"scan_code,omitempty"`
	CreatedAt        time.Time  `gorm:"autoCreateTime" json:"created_at"`
	ExpiresAt        time.Time  `gorm:"not null;index" json:"expires_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

func (*PendingUpload) TableName() string { return "security_pending_uploads" }

const (
	PrivacyRequestExport = "export"
	PrivacyRequestDelete = "delete"
	PrivacyStatusPending = "pending"
	PrivacyStatusHold    = "retention_hold"
	PrivacyStatusRunning = "processing"
	PrivacyStatusDone    = "completed"
	PrivacyStatusRecon   = "reconciliation_required"
)

type PrivacyRequest struct {
	ID             string     `gorm:"primaryKey;type:uuid" json:"id"`
	BusinessID     string     `gorm:"type:uuid;not null;index" json:"business_id"`
	Subject        string     `gorm:"not null;size:255;index" json:"subject"`
	Kind           string     `gorm:"not null;size:16" json:"kind"`
	Status         string     `gorm:"not null;size:32;index" json:"status"`
	IdempotencyKey string     `gorm:"not null;size:180" json:"-"`
	RequestHash    string     `gorm:"not null;size:64" json:"-"`
	ArtifactKey    string     `gorm:"not null;size:1024" json:"-"`
	ArtifactHash   string     `gorm:"not null;size:64" json:"artifact_hash,omitempty"`
	ErrorCode      string     `gorm:"not null;size:80" json:"error_code,omitempty"`
	RequestedAt    time.Time  `gorm:"not null" json:"requested_at"`
	PurgeAfter     *time.Time `json:"purge_after,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

func (*PrivacyRequest) TableName() string { return "privacy_requests" }

type SecurityAuditEvent struct {
	ID           string    `gorm:"primaryKey;type:uuid"`
	BusinessID   string    `gorm:"type:uuid;index;default:null"`
	Subject      string    `gorm:"not null;size:255"`
	EventType    string    `gorm:"not null;size:80"`
	ResourceType string    `gorm:"not null;size:64"`
	ResourceID   string    `gorm:"not null;size:255"`
	Outcome      string    `gorm:"not null;size:32"`
	ReasonCode   string    `gorm:"not null;size:80"`
	OccurredAt   time.Time `gorm:"not null"`
}

func (*SecurityAuditEvent) TableName() string { return "security_audit_events" }
