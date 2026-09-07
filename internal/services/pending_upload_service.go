package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
)

const (
	UploadScanClean     = "clean"
	UploadScanMalicious = "malicious"
	UploadScanPending   = "pending"
	maximumPendingBytes = int64(25 * 1024 * 1024)
)

var (
	ErrPendingUploadNotFound = errors.New("pending upload not found")
	ErrPendingUploadInvalid  = errors.New("invalid pending upload")
	pendingKindPattern       = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	allowedPendingTypes      = map[string]struct{}{
		"text/csv": {}, "application/pdf": {}, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": {},
		"image/png": {}, "image/jpeg": {}, "image/webp": {},
	}
)

type PendingObjectSpec struct {
	Bucket         string
	Key            string
	ContentType    string
	SizeBytes      int64
	ChecksumSHA256 string
	Metadata       map[string]string
	ExpiresIn      time.Duration
}

func (s PendingObjectSpec) RequiredLength() string { return fmt.Sprintf("%d", s.SizeBytes) }

type PendingObjectMetadata struct {
	ContentType    string
	SizeBytes      int64
	ChecksumSHA256 string
	Metadata       map[string]string
}

type PendingObjectStore interface {
	PresignPendingUpload(context.Context, PendingObjectSpec) (*PresignedUpload, error)
	InspectPendingObject(context.Context, string, string) (PendingObjectMetadata, error)
	PresignPendingDownload(context.Context, string, string, time.Duration) (string, error)
	DeletePendingObject(context.Context, string, string) error
	ReadPendingObject(context.Context, string, string) ([]byte, error)
}

type UploadScanResult struct{ Status, Code string }

type PendingScanObject struct {
	Bucket, Key string
	Metadata    PendingObjectMetadata
	Content     []byte
}

type UploadScanner interface {
	Scan(context.Context, PendingScanObject) (UploadScanResult, error)
}

type FailClosedUploadScanner struct{}

func (FailClosedUploadScanner) Scan(context.Context, PendingScanObject) (UploadScanResult, error) {
	return UploadScanResult{Status: UploadScanPending, Code: "scanner_unavailable"}, nil
}

type PendingUploadOptions struct {
	Bucket string
	Now    func() time.Time
}

type PendingUploadService struct {
	repository interfaces.PendingUploadRepository
	audit      interface {
		RecordSecurityAudit(context.Context, *models.SecurityAuditEvent) error
	}
	store   PendingObjectStore
	scanner UploadScanner
	bucket  string
	now     func() time.Time
}

type PendingUploadCreateInput struct {
	BusinessID, UploaderID, Kind, ContentType, ChecksumSHA256 string
	SizeBytes                                                 int64
}

type PendingUploadCreated struct {
	Upload          *models.PendingUpload `json:"upload"`
	UploadURL       string                `json:"upload_url"`
	RequiredHeaders map[string]string     `json:"required_headers"`
}

type PendingUploadCleanupResult struct {
	Examined int `json:"examined"`
	Deleted  int `json:"deleted"`
	Failed   int `json:"failed"`
}

func NewPendingUploadService(repository interfaces.PendingUploadRepository, store PendingObjectStore, scanner UploadScanner, options PendingUploadOptions) *PendingUploadService {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	audit, _ := repository.(interface {
		RecordSecurityAudit(context.Context, *models.SecurityAuditEvent) error
	})
	return &PendingUploadService{repository: repository, audit: audit, store: store, scanner: scanner, bucket: strings.TrimSpace(options.Bucket), now: now}
}

func (s *PendingUploadService) Create(ctx context.Context, input PendingUploadCreateInput) (*PendingUploadCreated, error) {
	input.BusinessID, input.UploaderID = strings.TrimSpace(input.BusinessID), strings.TrimSpace(input.UploaderID)
	input.Kind, input.ContentType = strings.ToLower(strings.TrimSpace(input.Kind)), strings.ToLower(strings.TrimSpace(input.ContentType))
	input.ChecksumSHA256 = strings.TrimSpace(input.ChecksumSHA256)
	if s == nil || s.repository == nil || s.store == nil || s.bucket == "" || uuid.Validate(input.BusinessID) != nil || input.UploaderID == "" ||
		!pendingKindPattern.MatchString(input.Kind) || !allowedPendingContentType(input.ContentType) || input.SizeBytes <= 0 || input.SizeBytes > maximumPendingBytes || !validChecksumSHA256(input.ChecksumSHA256) {
		return nil, ErrPendingUploadInvalid
	}
	id, now := uuid.NewString(), s.now().UTC()
	key := fmt.Sprintf("pending/%s/%s/%s", input.BusinessID, id, input.Kind)
	upload := &models.PendingUpload{
		ID: id, BusinessID: input.BusinessID, UploaderID: input.UploaderID, Kind: input.Kind,
		Bucket: s.bucket, ObjectKey: key, ContentType: input.ContentType, SizeBytes: input.SizeBytes,
		ChecksumSHA256: input.ChecksumSHA256, Status: models.PendingUploadStatusPending,
		CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	if err := s.repository.CreatePendingUpload(ctx, upload); err != nil {
		return nil, err
	}
	if err := s.recordAudit(ctx, upload, "upload_created", "accepted", "pending"); err != nil {
		return nil, err
	}
	presigned, err := s.store.PresignPendingUpload(ctx, PendingObjectSpec{
		Bucket: s.bucket, Key: key, ContentType: input.ContentType, SizeBytes: input.SizeBytes,
		ChecksumSHA256: input.ChecksumSHA256,
		Metadata:       map[string]string{"business-id": input.BusinessID, "uploader-id": input.UploaderID, "upload-id": id}, ExpiresIn: 15 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	return &PendingUploadCreated{Upload: upload, UploadURL: presigned.UploadURL, RequiredHeaders: presigned.RequiredHeaders}, nil
}

func allowedPendingContentType(value string) bool {
	_, ok := allowedPendingTypes[value]
	return ok
}

func (s *PendingUploadService) Complete(ctx context.Context, id, businessID, uploaderID string) (*models.PendingUpload, error) {
	upload, err := s.repository.GetPendingUpload(ctx, strings.TrimSpace(id), strings.TrimSpace(businessID), strings.TrimSpace(uploaderID))
	if err != nil || upload == nil {
		return nil, ErrPendingUploadNotFound
	}
	if upload.Status != models.PendingUploadStatusPending || !upload.ExpiresAt.After(s.now().UTC()) {
		return nil, ErrPendingUploadInvalid
	}
	metadata, err := s.store.InspectPendingObject(ctx, upload.Bucket, upload.ObjectKey)
	if err != nil || !pendingMetadataMatches(upload, metadata) {
		return nil, ErrPendingUploadInvalid
	}
	upload.Status = models.PendingUploadStatusQuarantined
	upload.ScanCode = UploadScanPending
	if s.scanner != nil {
		content, readErr := s.store.ReadPendingObject(ctx, upload.Bucket, upload.ObjectKey)
		result, scanErr := UploadScanResult{Status: UploadScanPending, Code: "object_read_failed"}, readErr
		if readErr == nil {
			result, scanErr = s.scanner.Scan(ctx, PendingScanObject{Bucket: upload.Bucket, Key: upload.ObjectKey, Metadata: metadata, Content: content})
		}
		if scanErr == nil {
			switch result.Status {
			case UploadScanClean:
				upload.Status = models.PendingUploadStatusClean
			case UploadScanMalicious:
				upload.Status = models.PendingUploadStatusRejected
			}
			upload.ScanCode = safeSecurityCode(result.Code)
		} else {
			upload.ScanCode = "scanner_error"
		}
	}
	now := s.now().UTC()
	upload.CompletedAt = &now
	if err := s.repository.SavePendingUpload(ctx, upload); err != nil {
		return nil, err
	}
	outcome := "accepted"
	if upload.Status == models.PendingUploadStatusClean {
		outcome = "completed"
	} else if upload.Status == models.PendingUploadStatusRejected {
		outcome = "rejected"
	}
	if err := s.recordAudit(ctx, upload, "upload_scanned", outcome, upload.ScanCode); err != nil {
		return nil, err
	}
	return upload, nil
}

func (s *PendingUploadService) Download(ctx context.Context, id, businessID, uploaderID string) (string, error) {
	upload, err := s.repository.GetPendingUpload(ctx, id, businessID, uploaderID)
	if err != nil || upload == nil || upload.Status != models.PendingUploadStatusClean {
		return "", ErrPendingUploadNotFound
	}
	url, err := s.store.PresignPendingDownload(ctx, upload.Bucket, upload.ObjectKey, 5*time.Minute)
	if err != nil {
		return "", err
	}
	if err := s.recordAudit(ctx, upload, "upload_downloaded", "accepted", "clean"); err != nil {
		return "", err
	}
	return url, nil
}

func (s *PendingUploadService) CleanupExpired(ctx context.Context, limit int) (PendingUploadCleanupResult, error) {
	if s == nil || s.repository == nil || s.store == nil || limit <= 0 || limit > 500 {
		return PendingUploadCleanupResult{}, ErrPendingUploadInvalid
	}
	uploads, err := s.repository.ListExpiredPendingUploads(ctx, s.now().UTC(), limit)
	if err != nil {
		return PendingUploadCleanupResult{}, err
	}
	result := PendingUploadCleanupResult{Examined: len(uploads)}
	var failures []error
	for _, upload := range uploads {
		if upload == nil {
			result.Failed++
			failures = append(failures, errors.New("expired upload is nil"))
			continue
		}
		if err := s.store.DeletePendingObject(ctx, upload.Bucket, upload.ObjectKey); err != nil {
			result.Failed++
			failures = append(failures, fmt.Errorf("delete pending upload %s: %w", upload.ID, err))
			continue
		}
		now := s.now().UTC()
		upload.Status, upload.DeletedAt = models.PendingUploadStatusDeleted, &now
		if err := s.recordAudit(ctx, upload, "upload_deleted", "completed", "expired"); err != nil {
			result.Failed++
			failures = append(failures, fmt.Errorf("audit pending upload deletion %s: %w", upload.ID, err))
			continue
		}
		if err := s.repository.SavePendingUpload(ctx, upload); err != nil {
			result.Failed++
			failures = append(failures, fmt.Errorf("record pending upload deletion %s: %w", upload.ID, err))
			continue
		}
		result.Deleted++
	}
	return result, errors.Join(failures...)
}

func (s *PendingUploadService) recordAudit(ctx context.Context, upload *models.PendingUpload, eventType, outcome, reason string) error {
	if s.audit == nil {
		return nil
	}
	eventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(upload.ID+"\x00"+eventType+"\x00"+outcome+"\x00"+reason)).String()
	return s.audit.RecordSecurityAudit(ctx, &models.SecurityAuditEvent{
		ID: eventID, BusinessID: upload.BusinessID, Subject: upload.UploaderID, EventType: eventType,
		ResourceType: "pending_upload", ResourceID: upload.ID, Outcome: outcome, ReasonCode: safeSecurityCode(reason), OccurredAt: s.now().UTC(),
	})
}

func validChecksumSHA256(value string) bool {
	raw, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(raw) == sha256.Size
}

func pendingMetadataMatches(upload *models.PendingUpload, metadata PendingObjectMetadata) bool {
	return metadata.SizeBytes == upload.SizeBytes && strings.EqualFold(metadata.ContentType, upload.ContentType) &&
		metadata.ChecksumSHA256 == upload.ChecksumSHA256 && metadata.Metadata["business-id"] == upload.BusinessID &&
		metadata.Metadata["uploader-id"] == upload.UploaderID && metadata.Metadata["upload-id"] == upload.ID
}

func safeSecurityCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 80 || !securityScopePattern.MatchString(value) {
		return "unspecified"
	}
	return value
}
