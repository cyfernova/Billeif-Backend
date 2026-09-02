package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
)

type pendingUploadRepositoryFake struct{ upload *models.PendingUpload }

func (*pendingUploadRepositoryFake) RecordSecurityAudit(context.Context, *models.SecurityAuditEvent) error {
	return nil
}

func (f *pendingUploadRepositoryFake) CreatePendingUpload(_ context.Context, upload *models.PendingUpload) error {
	copy := *upload
	f.upload = &copy
	return nil
}
func (f *pendingUploadRepositoryFake) GetPendingUpload(_ context.Context, id, businessID, uploaderID string) (*models.PendingUpload, error) {
	if f.upload == nil || f.upload.ID != id || f.upload.BusinessID != businessID || f.upload.UploaderID != uploaderID {
		return nil, ErrPendingUploadNotFound
	}
	copy := *f.upload
	return &copy, nil
}
func (f *pendingUploadRepositoryFake) SavePendingUpload(_ context.Context, upload *models.PendingUpload) error {
	copy := *upload
	f.upload = &copy
	return nil
}
func (f *pendingUploadRepositoryFake) ListExpiredPendingUploads(_ context.Context, before time.Time, limit int) ([]*models.PendingUpload, error) {
	if f.upload == nil || f.upload.DeletedAt != nil || f.upload.ExpiresAt.After(before) || limit < 1 {
		return nil, nil
	}
	copy := *f.upload
	return []*models.PendingUpload{&copy}, nil
}

type pendingObjectStoreFake struct {
	metadata  PendingObjectMetadata
	deleteErr error
	deletes   int
}

func (f *pendingObjectStoreFake) PresignPendingUpload(_ context.Context, spec PendingObjectSpec) (*PresignedUpload, error) {
	if f.metadata.Metadata == nil {
		f.metadata.Metadata = map[string]string{}
	}
	for key, value := range spec.Metadata {
		f.metadata.Metadata[key] = value
	}
	return &PresignedUpload{UploadURL: "https://storage.example/upload", RequiredHeaders: map[string]string{"x-amz-checksum-sha256": f.metadata.ChecksumSHA256}}, nil
}
func (f *pendingObjectStoreFake) InspectPendingObject(context.Context, string, string) (PendingObjectMetadata, error) {
	return f.metadata, nil
}
func (*pendingObjectStoreFake) PresignPendingDownload(context.Context, string, string, time.Duration) (string, error) {
	return "https://storage.example/download", nil
}
func (f *pendingObjectStoreFake) DeletePendingObject(context.Context, string, string) error {
	f.deletes++
	return f.deleteErr
}
func (*pendingObjectStoreFake) ReadPendingObject(context.Context, string, string) ([]byte, error) {
	return []byte("safe content"), nil
}

type cleanScanner struct{}

func (cleanScanner) Scan(_ context.Context, object PendingScanObject) (UploadScanResult, error) {
	if len(object.Content) == 0 || object.Bucket == "" || object.Key == "" {
		return UploadScanResult{}, errors.New("missing object content")
	}
	return UploadScanResult{Status: UploadScanClean, Code: "clean"}, nil
}

func TestPendingUploadRejectsMissingUploadMetadata(t *testing.T) {
	upload := &models.PendingUpload{ID: "upload-1", BusinessID: "8cf06379-19ea-41f9-a0ce-230e99978807", UploaderID: "user-1", ContentType: "text/csv", SizeBytes: 12, ChecksumSHA256: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	metadata := PendingObjectMetadata{ContentType: upload.ContentType, SizeBytes: upload.SizeBytes, ChecksumSHA256: upload.ChecksumSHA256, Metadata: map[string]string{"business-id": upload.BusinessID, "uploader-id": upload.UploaderID}}
	if pendingMetadataMatches(upload, metadata) {
		t.Fatal("metadata without upload-id must be rejected")
	}
}

func TestPendingUploadCleanupRecordsOnlySuccessfulObjectDeletion(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	repository := &pendingUploadRepositoryFake{upload: &models.PendingUpload{ID: "upload-1", Status: models.PendingUploadStatusQuarantined, Bucket: "quarantine", ObjectKey: "pending/object", ExpiresAt: now.Add(-time.Hour)}}
	store := &pendingObjectStoreFake{deleteErr: errors.New("storage unavailable")}
	service := NewPendingUploadService(repository, store, nil, PendingUploadOptions{Bucket: "quarantine", Now: func() time.Time { return now }})

	result, err := service.CleanupExpired(context.Background(), 10)
	if err == nil || result.Failed != 1 || result.Deleted != 0 || repository.upload.DeletedAt != nil {
		t.Fatalf("failed cleanup result=%+v upload=%+v err=%v", result, repository.upload, err)
	}
	store.deleteErr = nil
	result, err = service.CleanupExpired(context.Background(), 10)
	if err != nil || result.Deleted != 1 || repository.upload.Status != models.PendingUploadStatusDeleted || repository.upload.DeletedAt == nil {
		t.Fatalf("successful cleanup result=%+v upload=%+v err=%v", result, repository.upload, err)
	}
}

func TestPendingUploadCompletesOnlyAfterBoundMetadataAndCleanScan(t *testing.T) {
	repository := &pendingUploadRepositoryFake{}
	store := &pendingObjectStoreFake{metadata: PendingObjectMetadata{
		ContentType: "text/csv", SizeBytes: 12, ChecksumSHA256: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		Metadata: map[string]string{"business-id": "8cf06379-19ea-41f9-a0ce-230e99978807", "uploader-id": "user-1"},
	}}
	service := NewPendingUploadService(repository, store, cleanScanner{}, PendingUploadOptions{Bucket: "quarantine", Now: func() time.Time {
		return time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	}})
	created, err := service.Create(context.Background(), PendingUploadCreateInput{
		BusinessID: "8cf06379-19ea-41f9-a0ce-230e99978807", UploaderID: "user-1", Kind: "import",
		ContentType: "text/csv", SizeBytes: 12, ChecksumSHA256: store.metadata.ChecksumSHA256,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	completed, err := service.Complete(context.Background(), created.Upload.ID, created.Upload.BusinessID, "user-1")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if completed.Status != models.PendingUploadStatusClean || completed.ScanCode != "clean" {
		t.Fatalf("completed=%+v", completed)
	}
}
