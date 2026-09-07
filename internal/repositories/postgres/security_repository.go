package postgres

import (
	"context"
	"errors"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type securityRepository struct{ db *gorm.DB }

func NewSecurityRepository(db *gorm.DB) interfaces.SecurityPrivacyRepository {
	return &securityRepository{db: db}
}

func (r *securityRepository) CreateStepUpGrant(ctx context.Context, grant *models.StepUpGrant) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(grant).Error
}

func (r *securityRepository) ConsumeStepUpGrant(ctx context.Context, request interfaces.StepUpConsumeRequest) (bool, error) {
	if r == nil || r.db == nil {
		return false, gorm.ErrInvalidDB
	}
	result := r.db.WithContext(ctx).Model(&models.StepUpGrant{}).
		Where("token_hash = ? AND subject = ? AND business_id = ? AND action = ? AND resource = ? AND command_hash = ?", request.TokenHash, request.Subject, request.BusinessID, request.Action, request.Resource, request.CommandHash).
		Where("consumed_at IS NULL AND issued_at <= ? AND expires_at > ?", request.ConsumedAt, request.ConsumedAt).
		Update("consumed_at", request.ConsumedAt)
	return result.RowsAffected == 1, result.Error
}

func (r *securityRepository) CreatePendingUpload(ctx context.Context, upload *models.PendingUpload) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Create(upload).Error
}

func (r *securityRepository) GetPendingUpload(ctx context.Context, id, businessID, uploaderID string) (*models.PendingUpload, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var upload models.PendingUpload
	err := r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND uploader_id = ?", id, businessID, uploaderID).First(&upload).Error
	return &upload, err
}

func (r *securityRepository) SavePendingUpload(ctx context.Context, upload *models.PendingUpload) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Save(upload).Error
}

func (r *securityRepository) ListExpiredPendingUploads(ctx context.Context, before time.Time, limit int) ([]*models.PendingUpload, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var uploads []*models.PendingUpload
	err := r.db.WithContext(ctx).
		Where("deleted_at IS NULL AND status <> ? AND expires_at <= ?", models.PendingUploadStatusDeleted, before).
		Where("NOT EXISTS (SELECT 1 FROM business_profiles WHERE business_profiles.logo_key = security_pending_uploads.object_key AND business_profiles.deleted_at IS NULL)").
		Order("expires_at ASC").Limit(limit).Find(&uploads).Error
	return uploads, err
}

func (r *securityRepository) FinalizeBusinessLogo(ctx context.Context, request interfaces.BusinessLogoFinalizeRequest) (*interfaces.BusinessLogoFinalizeResult, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	result := &interfaces.BusinessLogoFinalizeResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var upload models.PendingUpload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND uploader_id = ? AND kind = ? AND bucket = ? AND object_key = ?", request.UploadID, request.BusinessID, request.UploaderID, "business_logo", request.Bucket, request.ObjectKey).
			Where("content_type = ? AND size_bytes = ? AND checksum_sha256 = ?", request.ContentType, request.SizeBytes, request.ChecksumSHA256).
			Where("status IN ?", []string{models.PendingUploadStatusPending, models.PendingUploadStatusClean}).
			First(&upload).Error; err != nil {
			return err
		}
		if upload.ScanCode != models.PendingUploadScanBusinessLogo && !upload.ExpiresAt.After(request.CompletedAt) {
			return interfaces.ErrBusinessLogoFinalizeConflict
		}

		var business models.BusinessProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_id = ? AND deleted_at IS NULL", request.BusinessID, request.UploaderID).
			First(&business).Error; err != nil {
			return err
		}
		result.Business = &business
		if upload.ScanCode == models.PendingUploadScanBusinessLogo {
			result.Replayed = true
			result.PreviousObjectKey = upload.CleanupObjectKey
			return nil
		}

		result.PreviousObjectKey = business.LogoKey
		business.LogoKey = upload.ObjectKey
		business.LogoURL = ""
		if err := tx.Model(&models.BusinessProfile{}).Where("id = ?", business.ID).
			Updates(map[string]interface{}{"logo_key": business.LogoKey, "logo_url": ""}).Error; err != nil {
			return err
		}
		upload.Status = models.PendingUploadStatusClean
		upload.ScanCode = models.PendingUploadScanBusinessLogo
		upload.CleanupObjectKey = result.PreviousObjectKey
		upload.CompletedAt = &request.CompletedAt
		if err := tx.Model(&models.PendingUpload{}).Where("id = ?", upload.ID).
			Updates(map[string]interface{}{"status": upload.Status, "scan_code": upload.ScanCode, "cleanup_object_key": upload.CleanupObjectKey, "completed_at": upload.CompletedAt}).Error; err != nil {
			return err
		}
		result.Business = &business
		return tx.Create(&models.SecurityAuditEvent{
			ID: uuid.NewString(), BusinessID: business.ID, Subject: request.UploaderID, EventType: "business_logo_attached",
			ResourceType: "pending_upload", ResourceID: upload.ID, Outcome: "completed",
			ReasonCode: "metadata_verified", OccurredAt: request.CompletedAt,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, interfaces.ErrBusinessLogoFinalizeConflict
	}
	return result, err
}

func (r *securityRepository) CompleteBusinessLogoCleanup(ctx context.Context, uploadID, businessID, uploaderID, objectKey string) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	result := r.db.WithContext(ctx).Model(&models.PendingUpload{}).
		Where("id = ? AND business_id = ? AND uploader_id = ? AND scan_code = ? AND cleanup_object_key = ?", uploadID, businessID, uploaderID, models.PendingUploadScanBusinessLogo, objectKey).
		Update("cleanup_object_key", "")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return interfaces.ErrBusinessLogoFinalizeConflict
	}
	return nil
}

func (r *securityRepository) CreateOrGetPrivacyRequest(ctx context.Context, request *models.PrivacyRequest, audit *models.SecurityAuditEvent) (*models.PrivacyRequest, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, gorm.ErrInvalidDB
	}
	var stored *models.PrivacyRequest
	var created bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(request)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if created {
			stored = request
		} else {
			var existing models.PrivacyRequest
			if err := tx.Where("business_id = ? AND subject = ? AND kind = ? AND idempotency_key = ?", request.BusinessID, request.Subject, request.Kind, request.IdempotencyKey).First(&existing).Error; err != nil {
				return err
			}
			stored = &existing
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(audit).Error
	})
	return stored, created, err
}

func (r *securityRepository) GetPrivacyRequest(ctx context.Context, id, businessID, subject string) (*models.PrivacyRequest, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var request models.PrivacyRequest
	err := r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND subject = ?", id, businessID, subject).First(&request).Error
	return &request, err
}

func (r *securityRepository) ClaimPrivacyRequest(ctx context.Context, id, businessID, subject, kind, fromStatus string, now time.Time, audit *models.SecurityAuditEvent) (*models.PrivacyRequest, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, gorm.ErrInvalidDB
	}
	var request models.PrivacyRequest
	claimed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.PrivacyRequest{}).
			Where("id = ? AND business_id = ? AND subject = ? AND kind = ? AND status = ?", id, businessID, subject, kind, fromStatus)
		if kind == models.PrivacyRequestDelete {
			query = query.Where("purge_after IS NOT NULL AND purge_after <= ?", now)
		}
		result := query.Updates(map[string]any{"status": models.PrivacyStatusRunning, "started_at": now})
		if result.Error != nil || result.RowsAffected != 1 {
			return result.Error
		}
		claimed = true
		if err := tx.Where("id = ?", id).First(&request).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(audit).Error
	})
	if err != nil || !claimed {
		return nil, false, err
	}
	return &request, true, nil
}

func (r *securityRepository) SavePrivacyRequest(ctx context.Context, request *models.PrivacyRequest) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Save(request).Error
}

func (r *securityRepository) SavePrivacyRequestWithAudit(ctx context.Context, request *models.PrivacyRequest, audit *models.SecurityAuditEvent) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(request).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(audit).Error
	})
}

func (r *securityRepository) RecordSecurityAudit(ctx context.Context, event *models.SecurityAuditEvent) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(event).Error
}

func (r *securityRepository) LinkUserPhoneWithAudit(ctx context.Context, userID, phoneNumber string, updatedAt time.Time, audit *models.SecurityAuditEvent) (bool, error) {
	if r == nil || r.db == nil || audit == nil {
		return false, gorm.ErrInvalidDB
	}
	linked := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`UPDATE users
			SET phone_number = ?, updated_at = ?
			WHERE id = ? AND deleted_at IS NULL
			  AND (phone_number IS NULL OR phone_number = '')
			  AND NOT EXISTS (
				SELECT 1 FROM users AS existing
				WHERE existing.phone_number = ? AND existing.deleted_at IS NULL AND existing.id <> ?
			  )`, phoneNumber, updatedAt, userID, phoneNumber, userID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if err := tx.Create(audit).Error; err != nil {
			return err
		}
		linked = true
		return nil
	})
	return linked, err
}
