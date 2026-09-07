package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	JournalStatusDraft    = "draft"
	JournalStatusPosted   = "posted"
	JournalStatusReversed = "reversed"
)

type Journal struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name           string         `gorm:"not null;size:150" json:"name"`
	Reference      string         `gorm:"size:100" json:"reference,omitempty"`
	ProjectID      *string        `gorm:"index" json:"project_id,omitempty" validate:"omitempty,uuid"`
	BranchID       *string        `gorm:"index" json:"branch_id,omitempty" validate:"omitempty,uuid"`
	LockOverrideID *string        `gorm:"index" json:"lock_override_id,omitempty" validate:"omitempty,uuid"`
	Status         string         `gorm:"not null;size:30;default:'draft';index" json:"status"`
	PostingDate    time.Time      `gorm:"not null;index" json:"posting_date"`
	Notes          string         `gorm:"type:text" json:"notes,omitempty"`
	SourceType     string         `gorm:"size:50" json:"source_type,omitempty"`
	SourceID       *string        `gorm:"index" json:"source_id,omitempty" validate:"omitempty,uuid"`
	ReversalOfID   *string        `gorm:"index" json:"reversal_of_id,omitempty" validate:"omitempty,uuid"`
	PostedAt       *time.Time     `json:"posted_at,omitempty"`
	ReversedAt     *time.Time     `json:"reversed_at,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`

	Lines []*JournalLine `gorm:"foreignKey:JournalID" json:"lines,omitempty"`
}

func (Journal) TableName() string {
	return "journals"
}

type JournalLine struct {
	ID             string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	JournalID      string    `gorm:"not null;index" json:"journal_id" validate:"required,uuid"`
	AccountCode    string    `gorm:"not null;size:60;index" json:"account_code"`
	AccountName    string    `gorm:"not null;size:120" json:"account_name"`
	EntryType      string    `gorm:"not null;size:10" json:"entry_type"`
	Amount         float64   `gorm:"type:decimal(15,2);not null" json:"amount"`
	Currency       string    `gorm:"not null;size:3;default:'INR'" json:"currency"`
	Description    string    `gorm:"size:500" json:"description,omitempty"`
	DocumentID     *string   `gorm:"index" json:"document_id,omitempty" validate:"omitempty,uuid"`
	DocumentLineID *string   `gorm:"index" json:"document_line_id,omitempty" validate:"omitempty,uuid"`
	Metadata       string    `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (JournalLine) TableName() string {
	return "journal_lines"
}
