package models

import "time"

const (
	IdempotencyStatusInProgress = "in_progress"
	IdempotencyStatusCompleted  = "completed"
)

type DocumentSequence struct {
	ID            string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID    string    `gorm:"not null;index;uniqueIndex:idx_document_sequences_scope,priority:1" json:"business_id"`
	DocumentType  string    `gorm:"not null;size:50;uniqueIndex:idx_document_sequences_scope,priority:2" json:"document_type"`
	FinancialYear string    `gorm:"not null;size:9;uniqueIndex:idx_document_sequences_scope,priority:3" json:"financial_year"`
	Series        string    `gorm:"not null;size:32;uniqueIndex:idx_document_sequences_scope,priority:4" json:"series"`
	LastNumber    int       `gorm:"not null;default:0" json:"last_number"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (DocumentSequence) TableName() string {
	return "document_sequences"
}

type APIIdempotencyKey struct {
	ID             string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string     `gorm:"not null;index;uniqueIndex:idx_api_idempotency_scope,priority:1" json:"business_id"`
	Command        string     `gorm:"not null;size:100;uniqueIndex:idx_api_idempotency_scope,priority:2" json:"command"`
	IdempotencyKey string     `gorm:"not null;size:255;uniqueIndex:idx_api_idempotency_scope,priority:3" json:"idempotency_key"`
	RequestHash    string     `gorm:"not null;size:64" json:"request_hash"`
	Status         string     `gorm:"not null;size:20;default:'in_progress'" json:"status"`
	ResultType     *string    `gorm:"size:100" json:"result_type,omitempty"`
	ResultID       *string    `gorm:"type:uuid" json:"result_id,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

func (APIIdempotencyKey) TableName() string {
	return "api_idempotency_keys"
}

type OutboxEvent struct {
	ID              string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string     `gorm:"not null;index" json:"business_id"`
	AggregateType   string     `gorm:"not null;size:100" json:"aggregate_type"`
	AggregateID     string     `gorm:"not null;type:uuid;index" json:"aggregate_id"`
	EventType       string     `gorm:"not null;size:160;index" json:"event_type"`
	Payload         string     `gorm:"not null;type:jsonb" json:"payload"`
	PublishAttempts int        `gorm:"not null;default:0" json:"publish_attempts"`
	AvailableAt     time.Time  `gorm:"not null;index" json:"available_at"`
	LeaseOwner      *string    `gorm:"size:255" json:"lease_owner,omitempty"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CreatedAt       time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (OutboxEvent) TableName() string {
	return "outbox_events"
}
