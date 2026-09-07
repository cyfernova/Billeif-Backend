package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"invoice-backend/internal/gst"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	bulkImportMaxBytes     = 16 * 1024 * 1024
	bulkImportMaxRows      = 10_000
	bulkImportMaxColumns   = 64
	bulkImportMaxFieldSize = 4_096
	bulkImportBatchSize    = 100
	bulkImportMaxAttempts  = 5
	bulkImportLease        = 2 * time.Minute
)

const (
	bulkImportRowValidated    = "validated"
	bulkImportRowInvalid      = "invalid"
	bulkImportRowSucceeded    = "succeeded"
	bulkImportArtifactPending = "pending"
	bulkImportArtifactReady   = "ready"
)

var (
	ErrBulkImportInvalid   = errors.New("invalid bulk import")
	ErrBulkImportNotFound  = errors.New("bulk import not found")
	ErrBulkImportConflict  = errors.New("bulk import state conflict")
	ErrBulkImportRetryable = errors.New("bulk import retryable failure")
	errBulkImportCanceled  = errors.New("bulk import canceled")
	bulkGSTINPattern       = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)
	bulkPhonePattern       = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{5,24}$`)
	bulkCurrencyPattern    = regexp.MustCompile(`^[A-Z]{3}$`)
)

type BulkImportObjectReader interface {
	OpenPendingObject(context.Context, string, string) (io.ReadCloser, error)
}

type BulkImportQueueSender interface {
	EnqueueBulkImport(context.Context, BulkImportQueueMessage) error
}

type BulkImportArtifactStore interface {
	UploadIfAbsent(context.Context, string, string, []byte, string) error
	Delete(context.Context, string, string) error
	GeneratePresignedDownloadURL(context.Context, string, string, int64) (string, error)
}

type BulkImportNotifier interface {
	Ingest(context.Context, NotificationInput) (*models.Notification, error)
}

type bulkImportSQSSender interface {
	SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

type sqsBulkImportQueueSender struct {
	queueURL string
	sender   bulkImportSQSSender
}

func NewSQSBulkImportQueueSender(queueURL string, sender bulkImportSQSSender) BulkImportQueueSender {
	return &sqsBulkImportQueueSender{queueURL: strings.TrimSpace(queueURL), sender: sender}
}

func (s *sqsBulkImportQueueSender) EnqueueBulkImport(ctx context.Context, message BulkImportQueueMessage) error {
	if s == nil || s.sender == nil || s.queueURL == "" {
		return errors.New("bulk import queue is unavailable")
	}
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = s.sender.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: aws.String(s.queueURL), MessageBody: aws.String(string(body))})
	return err
}

type BulkImportQueueMessage struct {
	BusinessID      string `json:"business_id"`
	JobID           string `json:"job_id"`
	CommitCommandID string `json:"commit_command_id"`
}

type BulkImportOptions struct {
	Now            func() time.Time
	Permissions    PermissionChecker
	Capability     CapabilityGuard
	ArtifactStore  BulkImportArtifactStore
	ArtifactBucket string
	Notifications  BulkImportNotifier
}

type BulkImportService struct {
	db          *gorm.DB
	uploads     interfaces.PendingUploadRepository
	objects     BulkImportObjectReader
	queue       BulkImportQueueSender
	now         func() time.Time
	permissions PermissionChecker
	capability  CapabilityGuard
	artifacts   BulkImportArtifactStore
	bucket      string
	notifier    BulkImportNotifier
}

type BulkImportArtifactDownload struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Validate is the public two-phase validation entry point. input may be a
// ValidateBulkImportInput or a pointer to one; the explicit arguments are the
// authority for tenant and actor binding.
func (s *BulkImportService) Validate(ctx context.Context, businessID, uploaderID, jobType string, input any) (*models.BulkJob, error) {
	var in ValidateBulkImportInput
	switch v := input.(type) {
	case ValidateBulkImportInput:
		in = v
	case *ValidateBulkImportInput:
		if v != nil {
			in = *v
		}
	default:
		return nil, ErrBulkImportInvalid
	}
	in.BusinessID, in.UploaderID, in.JobType = businessID, uploaderID, jobType
	return s.ValidatePreview(ctx, in)
}

// Commit transitions a validated job exactly once and publishes its durable
// command. UUID values are accepted as either uuid.UUID or string for callers.
func (s *BulkImportService) Commit(ctx context.Context, businessID, uploaderID, jobID string, command any) (*models.BulkJob, error) {
	var id string
	switch v := command.(type) {
	case uuid.UUID:
		id = v.String()
	case string:
		id = v
	default:
		return nil, ErrBulkImportInvalid
	}
	return s.QueueCommit(ctx, businessID, uploaderID, jobID, id)
}

// Process handles one queue delivery; callers may invoke it again after a
// bounded batch to continue durable progress.

func (s *BulkImportService) Process(ctx context.Context, raw any) error {
	var message BulkImportQueueMessage
	switch v := raw.(type) {
	case BulkImportQueueMessage:
		message = v
	case *BulkImportQueueMessage:
		if v == nil {
			return ErrBulkImportInvalid
		}
		message = *v
	case []byte:
		if err := json.Unmarshal(v, &message); err != nil {
			return ErrBulkImportInvalid
		}
	case string:
		if err := json.Unmarshal([]byte(v), &message); err != nil {
			return ErrBulkImportInvalid
		}
	default:
		return ErrBulkImportInvalid
	}
	if _, err := s.ProcessBulkImportBatch(ctx, message.BusinessID, message.JobID, message.CommitCommandID, "bulk-import-worker", bulkImportBatchSize); err != nil {
		return err
	}
	return nil
}

func NewBulkImportService(db *gorm.DB, uploads interfaces.PendingUploadRepository, objects BulkImportObjectReader, queue BulkImportQueueSender, options BulkImportOptions) *BulkImportService {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &BulkImportService{
		db: db, uploads: uploads, objects: objects, queue: queue, now: now,
		permissions: options.Permissions, capability: options.Capability,
		artifacts: options.ArtifactStore, bucket: strings.TrimSpace(options.ArtifactBucket), notifier: options.Notifications,
	}
}

type ValidateBulkImportInput struct {
	BusinessID string            `json:"-"`
	UploaderID string            `json:"-"`
	UploadID   string            `json:"upload_id"`
	JobType    string            `json:"job_type"`
	Delimiter  string            `json:"delimiter,omitempty"`
	Mapping    map[string]string `json:"mapping,omitempty"`
}

func (s *BulkImportService) ValidatePreview(ctx context.Context, input ValidateBulkImportInput) (*models.BulkJob, error) {
	if s == nil || s.db == nil || s.uploads == nil || s.objects == nil || !bulkImportTypeAllowed(input.JobType) || strings.TrimSpace(input.BusinessID) == "" || strings.TrimSpace(input.UploaderID) == "" || uuid.Validate(input.UploadID) != nil {
		return nil, ErrBulkImportInvalid
	}
	if err := requireCapability(ctx, s.capability, CapabilityRequest{BusinessID: input.BusinessID, UserID: input.UploaderID, Platform: CapabilityPlatformWeb, Capability: CapabilityBulkImports}); err != nil {
		return nil, err
	}
	if err := requireBulkImportPermission(ctx, s.permissions, input.BusinessID, input.JobType); err != nil {
		return nil, err
	}
	upload, err := s.uploads.GetPendingUpload(ctx, input.UploadID, input.BusinessID, input.UploaderID)
	if err != nil || upload == nil {
		return nil, ErrBulkImportNotFound
	}
	if upload.Status != models.PendingUploadStatusClean || upload.Kind != "bulk_import" || strings.ToLower(upload.ContentType) != "text/csv" || upload.SizeBytes <= 0 || upload.SizeBytes > bulkImportMaxBytes || !upload.ExpiresAt.After(s.now().UTC()) {
		return nil, ErrBulkImportInvalid
	}
	expectedObjectKey := fmt.Sprintf("pending/%s/%s/bulk_import", input.BusinessID, upload.ID)
	if upload.ObjectKey != expectedObjectKey {
		return nil, ErrBulkImportInvalid
	}
	delimiter, err := bulkImportDelimiter(input.Delimiter)
	if err != nil {
		return nil, err
	}
	requestPayload := mustMarshalMap(map[string]any{"delimiter": string(delimiter), "mapping": input.Mapping})
	var existing models.BulkJob
	existingErr := s.db.WithContext(ctx).Where("business_id = ? AND created_by = ? AND upload_id = ? AND deleted_at IS NULL", input.BusinessID, input.UploaderID, upload.ID).First(&existing).Error
	var job *models.BulkJob
	if existingErr == nil {
		if existing.JobType != input.JobType || existing.RequestPayload != requestPayload {
			return nil, ErrBulkImportConflict
		}
		if existing.Status != models.BulkJobStatusValidating {
			return s.Get(ctx, input.BusinessID, input.UploaderID, existing.ID)
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Unscoped().Where("bulk_job_id = ?", existing.ID).Delete(&models.BulkJobRow{}).Error; err != nil {
				return err
			}
			return tx.Model(&existing).Updates(map[string]any{"total_rows": 0, "processed_rows": 0, "succeeded_rows": 0, "failed_rows": 0, "last_error": nil}).Error
		}); err != nil {
			return nil, err
		}
		job = &existing
	}
	if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		if existingErr != nil {
			return nil, existingErr
		}
	}
	object, err := s.objects.OpenPendingObject(ctx, upload.Bucket, upload.ObjectKey)
	if err != nil || object == nil {
		return nil, ErrBulkImportInvalid
	}
	defer object.Close()
	now := s.now().UTC()
	if job == nil {
		job = &models.BulkJob{
			ID: uuid.NewString(), BusinessID: input.BusinessID, CreatedBy: input.UploaderID, JobType: input.JobType,
			Status: models.BulkJobStatusValidating, UploadID: &upload.ID, ValidationVersion: 1,
			ContentType: upload.ContentType, FileKey: upload.ObjectKey, ArtifactState: bulkImportArtifactPending,
			NotificationState: "pending", RetainUntil: bulkTimePointer(now.Add(30 * 24 * time.Hour)), StartedAt: &now,
			RequestPayload: requestPayload,
		}
		if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
			return nil, err
		}
	}

	hasher := sha256.New()
	reader := csv.NewReader(io.TeeReader(io.LimitReader(object, bulkImportMaxBytes+1), hasher))
	reader.Comma, reader.FieldsPerRecord, reader.ReuseRecord = delimiter, -1, true
	header, err := reader.Read()
	if err != nil || len(header) == 0 || len(header) > bulkImportMaxColumns || !bulkImportRecordEncodingValid(header) {
		s.failValidation(ctx, job, "invalid_header")
		return s.Get(ctx, input.BusinessID, input.UploaderID, job.ID)
	}
	canonical, err := bulkImportHeaders(header, input.Mapping, input.JobType)
	if err != nil {
		s.failValidation(ctx, job, "invalid_mapping")
		return s.Get(ctx, input.BusinessID, input.UploaderID, job.ID)
	}
	rowNumber := 1
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, readErr := reader.Read()
		if readErr != nil {
			if errors.Is(readErr, context.Canceled) {
				return nil, readErr
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			rowNumber++
			if err := s.persistValidationRow(ctx, job, &models.BulkJobRow{ID: uuid.NewString(), BulkJobID: job.ID, RowNumber: rowNumber, Status: bulkImportRowInvalid, ErrorCode: "malformed_csv", Error: "row is malformed", ErrorDetails: `{}`}); err != nil {
				return nil, err
			}
			break
		}
		rowNumber++
		if rowNumber > bulkImportMaxRows+1 {
			s.failValidation(ctx, job, "row_limit_exceeded")
			return s.Get(ctx, input.BusinessID, input.UploaderID, job.ID)
		}
		row := s.validateBulkImportRow(ctx, job, canonical, record, rowNumber)
		if err := s.persistValidationRow(ctx, job, row); err != nil {
			return nil, err
		}
	}
	if reader.InputOffset() <= 0 || reader.InputOffset() > bulkImportMaxBytes || reader.InputOffset() != upload.SizeBytes || !bulkImportDigestMatches(hasher.Sum(nil), upload.ChecksumSHA256) {
		s.failValidation(ctx, job, "object_metadata_mismatch")
		return s.Get(ctx, input.BusinessID, input.UploaderID, job.ID)
	}
	if err := s.db.WithContext(ctx).Model(&models.BulkJob{}).Where("id = ? AND business_id = ? AND status = ?", job.ID, job.BusinessID, models.BulkJobStatusValidating).Updates(map[string]any{"status": models.BulkJobStatusValidated, "updated_at": now}).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, input.BusinessID, input.UploaderID, job.ID)
}

func (s *BulkImportService) Get(ctx context.Context, businessID, uploaderID, jobID string) (*models.BulkJob, error) {
	var job models.BulkJob
	err := s.db.WithContext(ctx).Preload("Rows").Preload("Artifacts").Where("id = ? AND business_id = ? AND created_by = ? AND deleted_at IS NULL", jobID, businessID, uploaderID).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBulkImportNotFound
	}
	return &job, err
}

func (s *BulkImportService) GetArtifactDownload(ctx context.Context, businessID, uploaderID, jobID, artifactID string) (*BulkImportArtifactDownload, error) {
	if s == nil || s.db == nil || s.artifacts == nil || s.bucket == "" || uuid.Validate(jobID) != nil || uuid.Validate(artifactID) != nil {
		return nil, ErrBulkImportInvalid
	}
	job, err := s.Get(ctx, businessID, uploaderID, jobID)
	if err != nil {
		return nil, err
	}
	if err := requireBulkImportPermission(ctx, s.permissions, businessID, job.JobType); err != nil {
		return nil, err
	}
	var artifact models.BulkJobArtifact
	if err := s.db.WithContext(ctx).Where("id = ? AND bulk_job_id = ? AND status = ? AND deleted_at IS NULL", artifactID, jobID, bulkImportArtifactReady).First(&artifact).Error; err != nil {
		return nil, ErrBulkImportNotFound
	}
	if artifact.ExpiresAt == nil || !artifact.ExpiresAt.After(s.now().UTC()) {
		return nil, ErrBulkImportNotFound
	}
	url, err := s.artifacts.GeneratePresignedDownloadURL(ctx, s.bucket, artifact.FileKey, 300)
	if err != nil {
		return nil, fmt.Errorf("%w: artifact unavailable", ErrBulkImportRetryable)
	}
	return &BulkImportArtifactDownload{URL: url, ExpiresAt: s.now().UTC().Add(5 * time.Minute)}, nil
}

func (s *BulkImportService) QueueCommit(ctx context.Context, businessID, uploaderID, jobID, commandID string) (*models.BulkJob, error) {
	if uuid.Validate(commandID) != nil {
		return nil, ErrBulkImportInvalid
	}
	if s.queue == nil {
		return nil, fmt.Errorf("%w: queue unavailable", ErrBulkImportRetryable)
	}
	now := s.now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job models.BulkJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ? AND created_by = ? AND deleted_at IS NULL", jobID, businessID, uploaderID).First(&job).Error; err != nil {
			return ErrBulkImportNotFound
		}
		if job.CommitCommandID != nil {
			if *job.CommitCommandID == commandID {
				return nil
			}
			return ErrBulkImportConflict
		}
		if err := requireBulkImportPermission(ctx, s.permissions, businessID, job.JobType); err != nil {
			return err
		}
		if job.Status != models.BulkJobStatusValidated || job.FailedRows != 0 {
			return ErrBulkImportConflict
		}
		return tx.Model(&job).Updates(map[string]any{"status": models.BulkJobStatusCommitQueued, "commit_command_id": commandID, "queued_at": now, "next_retry_at": now, "last_error": nil}).Error
	})
	if err != nil {
		return nil, err
	}
	message := BulkImportQueueMessage{BusinessID: businessID, JobID: jobID, CommitCommandID: commandID}
	if err := s.queue.EnqueueBulkImport(ctx, message); err != nil {
		s.recordRetry(ctx, businessID, jobID, "queue_unavailable")
		return nil, fmt.Errorf("%w: queue unavailable", ErrBulkImportRetryable)
	}
	if err := s.clearBulkImportDispatchDue(ctx, businessID, jobID, commandID); err != nil {
		return nil, err
	}
	return s.Get(ctx, businessID, uploaderID, jobID)
}

// RecoverPending republishes due commands left behind by queue failures or
// process crashes. Publishing is intentionally at-least-once; row and command
// idempotency make duplicate deliveries safe.
func (s *BulkImportService) RecoverPending(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil || s.queue == nil {
		return 0, errors.New("bulk import recovery is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	now := s.now().UTC()
	var jobs []models.BulkJob
	if err := s.db.WithContext(ctx).
		Where("deleted_at IS NULL AND commit_command_id IS NOT NULL AND ((status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?) OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?))", models.BulkJobStatusCommitQueued, now, models.BulkJobStatusCommitting, now).
		Order("COALESCE(next_retry_at, lease_expires_at) ASC").Limit(limit).Find(&jobs).Error; err != nil {
		return 0, err
	}
	recovered := 0
	for i := range jobs {
		job := &jobs[i]
		if job.CommitCommandID == nil {
			continue
		}
		commandID := *job.CommitCommandID
		claimed := false
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var locked models.BulkJob
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ? AND commit_command_id = ?", job.ID, job.BusinessID, commandID).First(&locked).Error; err != nil {
				return err
			}
			dueQueued := locked.Status == models.BulkJobStatusCommitQueued && locked.NextRetryAt != nil && !locked.NextRetryAt.After(now)
			dueLease := locked.Status == models.BulkJobStatusCommitting && locked.LeaseExpiresAt != nil && !locked.LeaseExpiresAt.After(now)
			if locked.CancelRequested || (!dueQueued && !dueLease) {
				return nil
			}
			claimed = true
			return tx.Model(&locked).Updates(map[string]any{"status": models.BulkJobStatusCommitQueued, "next_retry_at": now.Add(time.Minute), "lease_owner": "", "lease_expires_at": nil}).Error
		})
		if err != nil {
			return recovered, err
		}
		if !claimed {
			continue
		}
		if err := s.queue.EnqueueBulkImport(ctx, BulkImportQueueMessage{BusinessID: job.BusinessID, JobID: job.ID, CommitCommandID: commandID}); err != nil {
			s.recordRetry(ctx, job.BusinessID, job.ID, "recovery_queue_unavailable")
			return recovered, fmt.Errorf("%w: recover queue dispatch", ErrBulkImportRetryable)
		}
		if err := s.clearBulkImportDispatchDue(ctx, job.BusinessID, job.ID, commandID); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func (s *BulkImportService) clearBulkImportDispatchDue(ctx context.Context, businessID, jobID, commandID string) error {
	return s.db.WithContext(ctx).Model(&models.BulkJob{}).
		Where("id = ? AND business_id = ? AND commit_command_id = ? AND status = ?", jobID, businessID, commandID, models.BulkJobStatusCommitQueued).
		Updates(map[string]any{"next_retry_at": nil, "last_error": nil}).Error
}

func (s *BulkImportService) Cancel(ctx context.Context, businessID, uploaderID, jobID string) (*models.BulkJob, error) {
	var job models.BulkJob
	if err := s.db.WithContext(ctx).Select("job_type").Where("id = ? AND business_id = ? AND created_by = ?", jobID, businessID, uploaderID).First(&job).Error; err != nil {
		return nil, ErrBulkImportNotFound
	}
	if err := requireBulkImportPermission(ctx, s.permissions, businessID, job.JobType); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&models.BulkJob{}).Where("id = ? AND business_id = ? AND created_by = ? AND deleted_at IS NULL", jobID, businessID, uploaderID).
		Where("status IN ?", []string{models.BulkJobStatusValidating, models.BulkJobStatusValidated, models.BulkJobStatusCommitQueued, models.BulkJobStatusCommitting}).
		Updates(map[string]any{"cancel_requested": true, "status": models.BulkJobStatusCanceled, "completed_at": now, "lease_owner": "", "lease_expires_at": nil})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrBulkImportConflict
	}
	return s.Get(ctx, businessID, uploaderID, jobID)
}

func (s *BulkImportService) ProcessBulkImportBatch(ctx context.Context, businessID, jobID, commandID, owner string, batchSize int) (bool, error) {
	if uuid.Validate(jobID) != nil || uuid.Validate(commandID) != nil || strings.TrimSpace(businessID) == "" || strings.TrimSpace(owner) == "" {
		return false, ErrBulkImportInvalid
	}
	if batchSize <= 0 || batchSize > bulkImportBatchSize {
		batchSize = bulkImportBatchSize
	}
	rows, job, err := s.claimBatch(ctx, businessID, jobID, commandID, owner, batchSize)
	if err != nil {
		return false, err
	}
	if job.Status == models.BulkJobStatusCompleted {
		if err := s.sendBulkImportCompletionNotification(ctx, job); err != nil {
			return false, fmt.Errorf("%w: completion notification", ErrBulkImportRetryable)
		}
		return true, nil
	}
	if job.Status == models.BulkJobStatusCanceled {
		return true, nil
	}
	for _, row := range rows {
		if err := s.commitRow(ctx, job, row); err != nil {
			if errors.Is(err, errBulkImportCanceled) {
				return true, nil
			}
			s.recordRetry(ctx, businessID, jobID, "row_commit_failed")
			return false, fmt.Errorf("%w: row commit failed", ErrBulkImportRetryable)
		}
	}
	if err := s.db.WithContext(ctx).Model(&models.BulkJob{}).
		Where("id = ? AND business_id = ? AND status = ? AND lease_owner = ? AND cancel_requested = ?", jobID, businessID, models.BulkJobStatusCommitting, owner, false).
		Updates(map[string]any{"attempt_count": 0, "last_error": nil}).Error; err != nil {
		return false, err
	}
	var remaining int64
	if err := s.db.WithContext(ctx).Model(&models.BulkJobRow{}).Where("bulk_job_id = ? AND status = ? AND deleted_at IS NULL", jobID, bulkImportRowValidated).Count(&remaining).Error; err != nil {
		return false, err
	}
	now := s.now().UTC()
	if remaining == 0 {
		if err := s.finalizeBulkImport(ctx, job, commandID, now); err != nil {
			if errors.Is(err, errBulkImportCanceled) {
				return true, nil
			}
			s.recordRetry(ctx, businessID, jobID, "finalization_failed")
			return false, fmt.Errorf("%w: finalize import", ErrBulkImportRetryable)
		}
		return true, nil
	}
	result := s.db.WithContext(ctx).Model(&models.BulkJob{}).
		Where("id = ? AND business_id = ? AND status = ? AND lease_owner = ? AND cancel_requested = ?", jobID, businessID, models.BulkJobStatusCommitting, owner, false).
		Updates(map[string]any{"status": models.BulkJobStatusCommitQueued, "lease_owner": "", "lease_expires_at": nil})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return true, nil
	}
	if s.queue != nil {
		if err := s.queue.EnqueueBulkImport(ctx, BulkImportQueueMessage{BusinessID: businessID, JobID: jobID, CommitCommandID: commandID}); err != nil {
			s.recordRetry(ctx, businessID, jobID, "queue_unavailable")
			return false, fmt.Errorf("%w: queue unavailable", ErrBulkImportRetryable)
		}
	}
	return false, nil
}

func (s *BulkImportService) finalizeBulkImport(ctx context.Context, job *models.BulkJob, commandID string, now time.Time) error {
	if s.artifacts == nil || s.bucket == "" {
		return errors.New("bulk import artifact store is unavailable")
	}
	var rows []models.BulkJobRow
	if err := s.db.WithContext(ctx).Where("bulk_job_id = ? AND deleted_at IS NULL", job.ID).Order("row_number ASC").Find(&rows).Error; err != nil {
		return err
	}
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"row_number", "status", "entity_type", "entity_id", "error_code"}); err != nil {
		return err
	}
	for _, row := range rows {
		entityID := ""
		if row.EntityID != nil {
			entityID = *row.EntityID
		}
		if err := writer.Write([]string{strconv.Itoa(row.RowNumber), row.Status, row.EntityType, entityID, row.ErrorCode}); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	fileName := "bulk-import-results.csv"
	key, err := tenantArtifactObjectKey("bulk-import-results", job.BusinessID, job.ID, fileName)
	if err != nil {
		return err
	}
	expiresAt := now.Add(30 * 24 * time.Hour)
	artifact := &models.BulkJobArtifact{
		ID: uuid.NewString(), BulkJobID: job.ID, ArtifactType: "result", FileName: fileName,
		FileKey: key, Status: bulkImportArtifactPending, ExpiresAt: &expiresAt,
		Metadata: mustMarshalMap(map[string]any{"content_type": "text/csv"}),
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked models.BulkJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ? AND commit_command_id = ?", job.ID, job.BusinessID, commandID).First(&locked).Error; err != nil {
			return err
		}
		if locked.Status == models.BulkJobStatusCanceled || locked.CancelRequested {
			return errBulkImportCanceled
		}
		if locked.Status != models.BulkJobStatusCommitting {
			return ErrBulkImportConflict
		}
		return tx.Where("bulk_job_id = ? AND artifact_type = ? AND deleted_at IS NULL", job.ID, "result").FirstOrCreate(artifact).Error
	}); err != nil {
		return err
	}
	if err := s.artifacts.UploadIfAbsent(ctx, s.bucket, key, output.Bytes(), "text/csv"); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&models.BulkJobArtifact{}).
		Where("bulk_job_id = ? AND artifact_type = ? AND deleted_at IS NULL", job.ID, "result").
		Update("status", bulkImportArtifactReady).Error; err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&models.BulkJob{}).
		Where("id = ? AND business_id = ? AND commit_command_id = ? AND status = ? AND cancel_requested = ?", job.ID, job.BusinessID, commandID, models.BulkJobStatusCommitting, false).
		Updates(map[string]any{
			"status": models.BulkJobStatusCompleted, "completed_at": now, "lease_owner": "", "lease_expires_at": nil,
			"next_retry_at": nil, "last_error": nil, "artifact_state": bulkImportArtifactReady,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errBulkImportCanceled
	}
	job.Status = models.BulkJobStatusCompleted
	return s.sendBulkImportCompletionNotification(ctx, job)
}

func (s *BulkImportService) sendBulkImportCompletionNotification(ctx context.Context, job *models.BulkJob) error {
	if job.NotificationState == "sent" || job.NotificationState == "skipped" {
		return nil
	}
	state := "skipped"
	if s.notifier != nil {
		if _, err := s.notifier.Ingest(ctx, NotificationInput{
			BusinessID: job.BusinessID, UserID: job.CreatedBy,
			SourceEventKey: "bulk-import:" + job.ID + ":completed", Type: "system",
			Title: "Bulk import completed", Body: "Your bulk import has completed.",
			ResourceType: "bulk_job", ResourceID: job.ID,
		}); err != nil {
			_ = s.db.WithContext(ctx).Model(&models.BulkJob{}).Where("id = ? AND status = ?", job.ID, models.BulkJobStatusCompleted).Update("notification_state", "failed").Error
			return err
		}
		state = "sent"
	}
	return s.db.WithContext(ctx).Model(&models.BulkJob{}).Where("id = ? AND status = ?", job.ID, models.BulkJobStatusCompleted).Update("notification_state", state).Error
}

func (s *BulkImportService) CleanupExpired(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil || s.artifacts == nil || s.bucket == "" {
		return 0, errors.New("bulk import cleanup is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var jobs []models.BulkJob
	if err := s.db.WithContext(ctx).Preload("Artifacts").
		Where("retain_until <= ? AND status <> ? AND deleted_at IS NULL", s.now().UTC(), models.BulkJobStatusExpired).
		Order("retain_until ASC").Limit(limit).Find(&jobs).Error; err != nil {
		return 0, err
	}
	cleaned := 0
	for i := range jobs {
		job := &jobs[i]
		for _, artifact := range job.Artifacts {
			if artifact.FileKey != "" {
				if err := s.artifacts.Delete(ctx, s.bucket, artifact.FileKey); err != nil {
					return cleaned, err
				}
			}
		}
		if job.UploadID != nil && s.uploads != nil {
			if upload, err := s.uploads.GetPendingUpload(ctx, *job.UploadID, job.BusinessID, job.CreatedBy); err == nil && upload != nil && upload.ObjectKey != "" {
				if err := s.artifacts.Delete(ctx, upload.Bucket, upload.ObjectKey); err != nil {
					return cleaned, err
				}
			}
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.BulkJobArtifact{}).Where("bulk_job_id = ? AND deleted_at IS NULL", job.ID).Updates(map[string]any{"status": "expired"}).Error; err != nil {
				return err
			}
			return tx.Model(&models.BulkJob{}).Where("id = ? AND business_id = ?", job.ID, job.BusinessID).Updates(map[string]any{"status": models.BulkJobStatusExpired, "artifact_state": "expired"}).Error
		}); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}

func (s *BulkImportService) claimBatch(ctx context.Context, businessID, jobID, commandID, owner string, limit int) ([]models.BulkJobRow, *models.BulkJob, error) {
	var job models.BulkJob
	var rows []models.BulkJobRow
	now := s.now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ? AND commit_command_id = ? AND deleted_at IS NULL", jobID, businessID, commandID).First(&job).Error; err != nil {
			return ErrBulkImportNotFound
		}
		if job.Status == models.BulkJobStatusCompleted || job.Status == models.BulkJobStatusCanceled {
			return nil
		}
		if job.Status != models.BulkJobStatusCommitQueued && !(job.Status == models.BulkJobStatusCommitting && (job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now))) {
			return ErrBulkImportConflict
		}
		if job.CancelRequested {
			job.Status, job.CompletedAt = models.BulkJobStatusCanceled, &now
			return tx.Save(&job).Error
		}
		lease := now.Add(bulkImportLease)
		if err := tx.Model(&job).Updates(map[string]any{"status": models.BulkJobStatusCommitting, "lease_owner": owner, "lease_expires_at": lease, "started_at": now}).Error; err != nil {
			return err
		}
		job.Status, job.LeaseOwner, job.LeaseExpiresAt = models.BulkJobStatusCommitting, owner, &lease
		return tx.Where("bulk_job_id = ? AND status = ? AND deleted_at IS NULL", jobID, bulkImportRowValidated).Order("row_number ASC").Limit(limit).Find(&rows).Error
	})
	return rows, &job, err
}

func (s *BulkImportService) commitRow(ctx context.Context, job *models.BulkJob, row models.BulkJobRow) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedJob models.BulkJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ? AND commit_command_id = ?", job.ID, job.BusinessID, job.CommitCommandID).First(&lockedJob).Error; err != nil {
			return err
		}
		if lockedJob.Status == models.BulkJobStatusCanceled || lockedJob.CancelRequested {
			return errBulkImportCanceled
		}
		if lockedJob.Status != models.BulkJobStatusCommitting || lockedJob.LeaseOwner != job.LeaseOwner {
			return ErrBulkImportConflict
		}
		var locked models.BulkJobRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND bulk_job_id = ?", row.ID, job.ID).First(&locked).Error; err != nil {
			return err
		}
		if locked.Status == bulkImportRowSucceeded {
			return nil
		}
		if locked.Status != bulkImportRowValidated {
			return ErrBulkImportConflict
		}
		var values map[string]string
		if err := json.Unmarshal([]byte(locked.Input), &values); err != nil {
			return err
		}
		entityID := uuid.NewString()
		if err := createBulkImportEntity(tx, job, entityID, values); errors.Is(err, ErrBulkImportConflict) {
			now := s.now().UTC()
			if updateErr := tx.Model(&locked).Updates(map[string]any{"status": bulkImportRowInvalid, "error_code": "duplicate_at_commit", "error": "row conflicts with an existing entity", "error_details": `{"issues":["duplicate_at_commit"]}`, "committed_at": now, "attempt_count": gorm.Expr("attempt_count + 1")}).Error; updateErr != nil {
				return updateErr
			}
			return tx.Model(&lockedJob).Updates(map[string]any{"processed_rows": gorm.Expr("processed_rows + 1"), "failed_rows": gorm.Expr("failed_rows + 1")}).Error
		} else if err != nil {
			return err
		}
		now := s.now().UTC()
		if err := tx.Model(&locked).Updates(map[string]any{"status": bulkImportRowSucceeded, "entity_id": entityID, "entity_type": bulkImportEntityType(job.JobType), "committed_at": now, "attempt_count": gorm.Expr("attempt_count + 1"), "result": mustMarshalMap(map[string]any{"entity_id": entityID})}).Error; err != nil {
			return err
		}
		return tx.Model(&models.BulkJob{}).Where("id = ?", job.ID).Updates(map[string]any{"processed_rows": gorm.Expr("processed_rows + 1"), "succeeded_rows": gorm.Expr("succeeded_rows + 1")}).Error
	})
}

func createBulkImportEntity(tx *gorm.DB, job *models.BulkJob, id string, v map[string]string) error {
	switch job.JobType {
	case models.BulkJobTypeImportCustomers:
		var count int64
		q := tx.Model(&models.Customer{}).Where("business_id = ? AND deleted_at IS NULL", job.BusinessID)
		q = bulkPartyDuplicateQuery(q, v)
		if err := q.Count(&count).Error; err != nil || count != 0 {
			return coalesceError(err, ErrBulkImportConflict)
		}
		return tx.Create(&models.Customer{ID: id, BusinessID: job.BusinessID, Name: v["name"], Email: v["email"], Phone: v["phone"], GSTIN: v["gstin"], Address: v["address"], City: v["city"], State: v["state"], Country: v["country"], PostalCode: v["postal_code"]}).Error
	case models.BulkJobTypeImportVendors:
		var count int64
		q := tx.Model(&models.Vendor{}).Where("business_id = ? AND deleted_at IS NULL", job.BusinessID)
		q = bulkPartyDuplicateQuery(q, v)
		if err := q.Count(&count).Error; err != nil || count != 0 {
			return coalesceError(err, ErrBulkImportConflict)
		}
		return tx.Create(&models.Vendor{ID: id, BusinessID: job.BusinessID, Name: v["name"], Email: v["email"], Phone: v["phone"], GSTIN: v["gstin"], Address: v["address"], City: v["city"], State: v["state"], Country: v["country"], PostalCode: v["postal_code"]}).Error
	case models.BulkJobTypeImportProducts:
		price, _ := strconv.ParseFloat(v["price"], 64)
		var count int64
		if err := tx.Model(&models.Product{}).Where("business_id = ? AND UPPER(sku) = ? AND deleted_at IS NULL", job.BusinessID, v["sku"]).Count(&count).Error; err != nil || count != 0 {
			return coalesceError(err, ErrBulkImportConflict)
		}
		return tx.Create(&models.Product{
			ID: id, BusinessID: job.BusinessID, Name: v["name"], SKU: v["sku"], Barcode: v["sku"],
			Description: v["description"], HSNSACCode: v["hsn_sac_code"], Price: price,
			Currency: v["currency"], Unit: v["unit"], UQCCode: v["unit"], IsActive: true,
			ValuationMethod: "last_purchase", GSTMetadata: mustMarshalMap(map[string]any{"tax_rate": v["tax_rate"]}), ExtraAttributes: `{}`,
		}).Error
	default:
		return ErrBulkImportInvalid
	}
}

func (s *BulkImportService) validateBulkImportRow(ctx context.Context, job *models.BulkJob, headers []string, record []string, number int) *models.BulkJobRow {
	row := &models.BulkJobRow{ID: uuid.NewString(), BulkJobID: job.ID, RowNumber: number, Status: bulkImportRowValidated, ErrorDetails: `{}`, IdempotencyKey: job.ID + ":" + strconv.Itoa(number)}
	values, issues := make(map[string]string, len(headers)), make([]string, 0)
	if !bulkImportRecordEncodingValid(record) {
		issues = append(issues, "encoding_invalid")
	} else if len(record) != len(headers) {
		issues = append(issues, "column_count")
	} else {
		for i, value := range record {
			value = strings.TrimSpace(value)
			if len(value) > bulkImportMaxFieldSize {
				issues = append(issues, headers[i]+"_too_long")
			}
			if bulkFormulaValue(value) && !(headers[i] == "phone" && strings.HasPrefix(value, "+") && bulkPhonePattern.MatchString(value)) {
				issues = append(issues, headers[i]+"_formula")
			}
			values[headers[i]] = value
		}
	}
	bulkNormalizeValues(values, job.JobType)
	issues = append(issues, bulkValidateValues(values, job.JobType)...)
	row.ValidationKey = bulkValidationKey(values, job.JobType)
	if row.ValidationKey != "" {
		var duplicateRows int64
		s.db.WithContext(ctx).Model(&models.BulkJobRow{}).Where("bulk_job_id = ? AND validation_key = ? AND deleted_at IS NULL", job.ID, row.ValidationKey).Count(&duplicateRows)
		entityExists, err := s.bulkEntityExists(ctx, job.BusinessID, job.JobType, values)
		if err != nil {
			issues = append(issues, "duplicate_check_failed")
		} else if duplicateRows > 0 || entityExists {
			issues = append(issues, "duplicate")
		}
	}
	encoded, _ := json.Marshal(values)
	hash := sha256.Sum256(encoded)
	row.Input, row.InputHash = string(encoded), hex.EncodeToString(hash[:])
	if len(issues) != 0 {
		row.Status, row.ErrorCode, row.Error = bulkImportRowInvalid, issues[0], "row validation failed"
		details, _ := json.Marshal(map[string]any{"issues": issues})
		row.ErrorDetails = string(details)
	}
	return row
}

func (s *BulkImportService) bulkEntityExists(ctx context.Context, businessID, jobType string, v map[string]string) (bool, error) {
	var count int64
	var q *gorm.DB
	switch jobType {
	case models.BulkJobTypeImportCustomers:
		q = bulkPartyDuplicateQuery(s.db.WithContext(ctx).Model(&models.Customer{}).Where("business_id = ? AND deleted_at IS NULL", businessID), v)
	case models.BulkJobTypeImportVendors:
		q = bulkPartyDuplicateQuery(s.db.WithContext(ctx).Model(&models.Vendor{}).Where("business_id = ? AND deleted_at IS NULL", businessID), v)
	case models.BulkJobTypeImportProducts:
		q = s.db.WithContext(ctx).Model(&models.Product{}).Where("business_id = ? AND UPPER(sku) = ? AND deleted_at IS NULL", businessID, v["sku"])
	}
	if q == nil {
		return false, ErrBulkImportInvalid
	}
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func bulkPartyDuplicateQuery(q *gorm.DB, v map[string]string) *gorm.DB {
	if v["gstin"] != "" && v["email"] != "" {
		return q.Where("UPPER(gstin) = ? OR LOWER(email) = ?", v["gstin"], v["email"])
	}
	if v["gstin"] != "" {
		return q.Where("UPPER(gstin) = ?", v["gstin"])
	}
	return q.Where("LOWER(email) = ?", v["email"])
}

func (s *BulkImportService) persistValidationRow(ctx context.Context, job *models.BulkJob, row *models.BulkJobRow) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		updates := map[string]any{"total_rows": gorm.Expr("total_rows + 1")}
		if row.Status == bulkImportRowInvalid {
			updates["failed_rows"] = gorm.Expr("failed_rows + 1")
		}
		return tx.Model(&models.BulkJob{}).Where("id = ?", job.ID).Updates(updates).Error
	})
}

func (s *BulkImportService) failValidation(ctx context.Context, job *models.BulkJob, code string) {
	now := s.now().UTC()
	_ = s.db.WithContext(ctx).Model(&models.BulkJob{}).Where("id = ?", job.ID).Updates(map[string]any{"status": models.BulkJobStatusFailed, "last_error": code, "completed_at": now}).Error
}

func (s *BulkImportService) recordRetry(ctx context.Context, businessID, jobID, code string) {
	next := s.now().UTC().Add(time.Minute)
	_ = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job models.BulkJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND business_id = ?", jobID, businessID).First(&job).Error; err != nil {
			return err
		}
		if job.Status != models.BulkJobStatusCommitQueued && job.Status != models.BulkJobStatusCommitting {
			return nil
		}
		attempts := job.AttemptCount + 1
		updates := map[string]any{"status": models.BulkJobStatusCommitQueued, "attempt_count": attempts, "next_retry_at": next, "last_error": code, "lease_owner": "", "lease_expires_at": nil}
		if attempts >= bulkImportMaxAttempts {
			updates["status"], updates["completed_at"] = models.BulkJobStatusFailed, s.now().UTC()
			updates["next_retry_at"] = nil
		}
		return tx.Model(&job).Updates(updates).Error
	})
}

func bulkImportHeaders(header []string, mapping map[string]string, jobType string) ([]string, error) {
	result, seen := make([]string, len(header)), make(map[string]struct{}, len(header))
	allowed := bulkAllowedFields(jobType)
	for i, value := range header {
		source := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\ufeff")))
		canonical := source
		if mapped := strings.TrimSpace(mapping[source]); mapped != "" {
			canonical = strings.ToLower(mapped)
		}
		if _, ok := allowed[canonical]; !ok {
			return nil, ErrBulkImportInvalid
		}
		if _, ok := seen[canonical]; ok {
			return nil, ErrBulkImportInvalid
		}
		seen[canonical], result[i] = struct{}{}, canonical
	}
	for _, required := range bulkRequiredFields(jobType) {
		if _, ok := seen[required]; !ok {
			return nil, ErrBulkImportInvalid
		}
	}
	return result, nil
}

func bulkAllowedFields(jobType string) map[string]struct{} {
	fields := []string{"name", "email", "phone", "gstin", "address", "city", "state", "country", "postal_code"}
	if jobType == models.BulkJobTypeImportProducts {
		fields = []string{"name", "sku", "price", "currency", "unit", "tax_rate", "hsn_sac_code", "description"}
	}
	result := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		result[field] = struct{}{}
	}
	return result
}

func bulkRequiredFields(jobType string) []string {
	if jobType == models.BulkJobTypeImportProducts {
		return []string{"name", "sku", "price", "currency", "unit"}
	}
	return []string{"name", "email"}
}

func bulkNormalizeValues(v map[string]string, jobType string) {
	v["email"] = strings.ToLower(v["email"])
	v["gstin"] = strings.ToUpper(v["gstin"])
	if jobType == models.BulkJobTypeImportProducts {
		v["sku"], v["currency"], v["unit"] = strings.ToUpper(v["sku"]), strings.ToUpper(v["currency"]), strings.ToUpper(v["unit"])
	}
}

func bulkValidateValues(v map[string]string, jobType string) []string {
	issues := make([]string, 0)
	if len(v["name"]) < 2 || len(v["name"]) > 255 {
		issues = append(issues, "name_invalid")
	}
	if jobType != models.BulkJobTypeImportProducts {
		parsed, err := mail.ParseAddress(v["email"])
		if err != nil || parsed.Address != v["email"] {
			issues = append(issues, "email_invalid")
		}
		if v["phone"] != "" && !bulkPhonePattern.MatchString(v["phone"]) {
			issues = append(issues, "phone_invalid")
		}
		if v["gstin"] != "" && !bulkGSTINPattern.MatchString(v["gstin"]) {
			issues = append(issues, "gstin_invalid")
		}
		return issues
	}
	if v["sku"] == "" || len(v["sku"]) > 100 {
		issues = append(issues, "sku_invalid")
	}
	price, err := strconv.ParseFloat(v["price"], 64)
	if err != nil || price <= 0 || price > 90_000_000_000_000 || math.IsNaN(price) || math.IsInf(price, 0) || math.Abs(price*100-math.Round(price*100)) > 0.0000001 {
		issues = append(issues, "price_invalid")
	}
	if !bulkCurrencyPattern.MatchString(v["currency"]) {
		issues = append(issues, "currency_invalid")
	}
	if !gst.IsValidUQC(v["unit"]) {
		issues = append(issues, "unit_invalid")
	}
	if v["tax_rate"] != "" {
		rate, err := strconv.ParseFloat(v["tax_rate"], 64)
		if err != nil || rate < 0 || rate > 100 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			issues = append(issues, "tax_rate_invalid")
		}
	}
	return issues
}

func bulkValidationKey(v map[string]string, jobType string) string {
	if jobType == models.BulkJobTypeImportProducts {
		return "sku:" + v["sku"]
	}
	if v["gstin"] != "" {
		return "gstin:" + v["gstin"]
	}
	return "email:" + v["email"]
}

func bulkImportDigestMatches(sum []byte, expected string) bool {
	if strings.TrimSpace(expected) == "" {
		return false
	}
	return expected == base64.StdEncoding.EncodeToString(sum) || strings.EqualFold(expected, hex.EncodeToString(sum))
}

func bulkImportRecordEncodingValid(record []string) bool {
	for _, field := range record {
		if !utf8.ValidString(field) || strings.IndexByte(field, 0) >= 0 {
			return false
		}
	}
	return true
}

func bulkImportDelimiter(raw string) (rune, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "comma", ",":
		return ',', nil
	case "semicolon", ";":
		return ';', nil
	case "tab", "\\t":
		return '\t', nil
	default:
		return 0, ErrBulkImportInvalid
	}
}

func bulkFormulaValue(value string) bool {
	value = strings.TrimLeft(value, " \t\r\n")
	return value != "" && strings.ContainsRune("=+-@", rune(value[0]))
}

func bulkImportTypeAllowed(jobType string) bool {
	return jobType == models.BulkJobTypeImportCustomers || jobType == models.BulkJobTypeImportVendors || jobType == models.BulkJobTypeImportProducts
}

func requireBulkImportPermission(ctx context.Context, checker PermissionChecker, businessID, jobType string) error {
	permission := ""
	switch jobType {
	case models.BulkJobTypeImportCustomers:
		permission = PermissionCustomersCreate
	case models.BulkJobTypeImportVendors:
		permission = PermissionVendorsCreate
	case models.BulkJobTypeImportProducts:
		permission = PermissionProductsManage
	default:
		return ErrBulkImportInvalid
	}
	return requireMutationPermission(ctx, checker, businessID, permission)
}

func bulkImportEntityType(jobType string) string { return strings.TrimPrefix(jobType, "import_") }

func coalesceError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func bulkTimePointer(value time.Time) *time.Time { return &value }
