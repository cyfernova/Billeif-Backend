package interfaces

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
)

var ErrBusinessLogoFinalizeConflict = errors.New("business logo finalization conflict")

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

type PhoneLinkAuditRepository interface {
	LinkUserPhoneWithAudit(context.Context, string, string, time.Time, *models.SecurityAuditEvent) (bool, error)
}

type PendingUploadRepository interface {
	CreatePendingUpload(context.Context, *models.PendingUpload) error
	GetPendingUpload(context.Context, string, string, string) (*models.PendingUpload, error)
	ListExpiredPendingUploads(context.Context, time.Time, int) ([]*models.PendingUpload, error)
	SavePendingUpload(context.Context, *models.PendingUpload) error
}

type BusinessLogoFinalizeRequest struct {
	BusinessID     string
	UploaderID     string
	UploadID       string
	Bucket         string
	ObjectKey      string
	ContentType    string
	SizeBytes      int64
	ChecksumSHA256 string
	CompletedAt    time.Time
}

type BusinessLogoFinalizeResult struct {
	Business          *models.BusinessProfile
	PreviousObjectKey string
	Replayed          bool
}

type BusinessLogoRepository interface {
	PendingUploadRepository
	FinalizeBusinessLogo(context.Context, BusinessLogoFinalizeRequest) (*BusinessLogoFinalizeResult, error)
	CompleteBusinessLogoCleanup(context.Context, string, string, string, string) error
}

type SecurityPrivacyRepository interface {
	SecurityRepository
	BusinessLogoRepository
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
