package interfaces

import (
	"context"
	"time"

	"invoice-backend/internal/models"
)

type StepUpConsumeRequest struct {
	TokenHash   string
	Subject     string
	BusinessID  string
	Action      string
	Resource    string
	CommandHash string
	ConsumedAt  time.Time
}

type SecurityRepository interface {
	CreateStepUpGrant(context.Context, *models.StepUpGrant) error
	ConsumeStepUpGrant(context.Context, StepUpConsumeRequest) (bool, error)
}

type PendingUploadRepository interface {
	CreatePendingUpload(context.Context, *models.PendingUpload) error
	GetPendingUpload(context.Context, string, string, string) (*models.PendingUpload, error)
	ListExpiredPendingUploads(context.Context, time.Time, int) ([]*models.PendingUpload, error)
	SavePendingUpload(context.Context, *models.PendingUpload) error
}

type SecurityPrivacyRepository interface {
	SecurityRepository
	PendingUploadRepository
	PrivacyRepository
}

type PrivacyRepository interface {
	CreateOrGetPrivacyRequest(context.Context, *models.PrivacyRequest, *models.SecurityAuditEvent) (*models.PrivacyRequest, bool, error)
	GetPrivacyRequest(context.Context, string, string, string) (*models.PrivacyRequest, error)
	ClaimPrivacyRequest(context.Context, string, string, string, string, string, time.Time, *models.SecurityAuditEvent) (*models.PrivacyRequest, bool, error)
	SavePrivacyRequest(context.Context, *models.PrivacyRequest) error
	SavePrivacyRequestWithAudit(context.Context, *models.PrivacyRequest, *models.SecurityAuditEvent) error
	RecordSecurityAudit(context.Context, *models.SecurityAuditEvent) error
}
