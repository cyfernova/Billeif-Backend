package models

import "time"

const (
	OperationRecoveryStatusCompleted = "completed"
	OperationRecoveryStatusRejected  = "rejected"
)

type OperationRecoveryCommand struct {
	ID               string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID       string    `gorm:"not null;type:uuid;index" json:"business_id"`
	OperationType    string    `gorm:"not null;size:64;index" json:"operation_type"`
	OperationID      string    `gorm:"not null;type:uuid;index" json:"operation_id"`
	ActorSubject     string    `gorm:"not null;size:255" json:"actor_subject"`
	PrincipalKind    string    `gorm:"not null;size:16" json:"principal_kind"`
	Action           string    `gorm:"not null;size:80" json:"action"`
	Reason           string    `gorm:"not null;size:500" json:"reason"`
	IdempotencyKey   string    `gorm:"not null;size:180" json:"idempotency_key"`
	RequestHash      string    `gorm:"not null;size:64" json:"request_hash"`
	OperationVersion string    `gorm:"not null;size:64" json:"operation_version"`
	CorrelationID    string    `gorm:"not null;type:uuid" json:"correlation_id"`
	Status           string    `gorm:"not null;size:16" json:"status"`
	ResultCode       string    `gorm:"not null;size:80" json:"result_code"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
	CompletedAt      time.Time `gorm:"not null" json:"completed_at"`
}

func (OperationRecoveryCommand) TableName() string {
	return "operation_recovery_commands"
}
