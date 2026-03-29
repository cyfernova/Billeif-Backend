package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	ReportRunStatusCompleted = "completed"
	ReportRunStatusFailed    = "failed"
)

const (
	ReportRunKindExport   = "export"
	ReportRunKindSnapshot = "snapshot"
)

const (
	ReportShareModeSnapshot = "snapshot"
	ReportShareModeLive     = "live"
)

type ReportRun struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id"`
	ReportKey      string         `gorm:"not null;size:80;index" json:"report_key"`
	RunKind        string         `gorm:"not null;size:30;default:'export'" json:"run_kind"`
	Filters        string         `gorm:"type:jsonb;default:'{}'" json:"filters,omitempty"`
	VisibleColumns string         `gorm:"type:jsonb;default:'[]'" json:"visible_columns,omitempty"`
	ExportFormat   string         `gorm:"size:20;default:'json'" json:"export_format"`
	Status         string         `gorm:"size:20;default:'completed'" json:"status"`
	Payload        string         `gorm:"type:jsonb;default:'{}'" json:"payload,omitempty"`
	Summary        string         `gorm:"type:jsonb;default:'{}'" json:"summary,omitempty"`
	GeneratedBy    string         `gorm:"size:255" json:"generated_by,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ReportRun) TableName() string {
	return "report_runs"
}

type ReportPreference struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id"`
	UserID     string         `gorm:"not null;index" json:"user_id"`
	ReportKey  string         `gorm:"not null;size:80;index" json:"report_key"`
	Config     string         `gorm:"type:jsonb;default:'{}'" json:"config,omitempty"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ReportPreference) TableName() string {
	return "report_preferences"
}

type ReportShare struct {
	ID              string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string         `gorm:"not null;index" json:"business_id"`
	ReportKey       string         `gorm:"not null;size:80;index" json:"report_key"`
	Title           string         `gorm:"size:160" json:"title,omitempty"`
	ShareMode       string         `gorm:"not null;size:20;default:'snapshot'" json:"share_mode"`
	ReportRunID     *string        `gorm:"index" json:"report_run_id,omitempty"`
	Filters         string         `gorm:"type:jsonb;default:'{}'" json:"filters,omitempty"`
	VisibleColumns  string         `gorm:"type:jsonb;default:'[]'" json:"visible_columns,omitempty"`
	TokenHash       string         `gorm:"not null;size:64;uniqueIndex" json:"-"`
	PasscodeHash    string         `gorm:"type:text" json:"-"`
	RequiresPasscode bool          `gorm:"default:false" json:"requires_passcode"`
	ExpiresAt       *time.Time     `json:"expires_at,omitempty"`
	RevokedAt       *time.Time     `json:"revoked_at,omitempty"`
	LastAccessedAt  *time.Time     `json:"last_accessed_at,omitempty"`
	AccessCount     int64          `gorm:"default:0" json:"access_count"`
	CreatedBy       string         `gorm:"size:255" json:"created_by,omitempty"`
	CreatedAt       time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ReportShare) TableName() string {
	return "report_shares"
}

type ReportShareAccessLog struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ReportShareID string        `gorm:"not null;index" json:"report_share_id"`
	AccessedAt   time.Time      `gorm:"not null;index" json:"accessed_at"`
	Successful   bool           `gorm:"default:false" json:"successful"`
	FailureReason string        `gorm:"size:120" json:"failure_reason,omitempty"`
	IPAddress    string         `gorm:"size:64" json:"ip_address,omitempty"`
	UserAgent    string         `gorm:"size:500" json:"user_agent,omitempty"`
	Metadata     string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ReportShareAccessLog) TableName() string {
	return "report_share_access_logs"
}
