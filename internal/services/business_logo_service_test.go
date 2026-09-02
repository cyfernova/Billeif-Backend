package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type businessLogoBusinessRepoFake struct{ business *models.BusinessProfile }

func (f *businessLogoBusinessRepoFake) Create(context.Context, *models.BusinessProfile) error {
	return nil
}
func (f *businessLogoBusinessRepoFake) GetByID(_ context.Context, id string) (*models.BusinessProfile, error) {
	if f.business == nil || f.business.ID != id {
		return nil, errors.New("business not found")
	}
	copy := *f.business
	return &copy, nil
}
func (f *businessLogoBusinessRepoFake) Update(context.Context, *models.BusinessProfile) error {
	return nil
}
func (f *businessLogoBusinessRepoFake) Delete(context.Context, string) error { return nil }
func (f *businessLogoBusinessRepoFake) List(context.Context, string, int, int) ([]*models.BusinessProfile, int64, error) {
	return nil, 0, nil
}

type businessLogoRepositoryFake struct {
	upload      *models.PendingUpload
	finalizeErr error
	finalized   int
	previousKey string
	currentKey  string
}

func (f *businessLogoRepositoryFake) CreatePendingUpload(_ context.Context, upload *models.PendingUpload) error {
	copy := *upload
	f.upload = &copy
	return nil
}
func (f *businessLogoRepositoryFake) GetPendingUpload(_ context.Context, id, businessID, uploaderID string) (*models.PendingUpload, error) {
	if f.upload == nil || f.upload.ID != id || f.upload.BusinessID != businessID || f.upload.UploaderID != uploaderID {
		return nil, ErrPendingUploadNotFound
	}
	copy := *f.upload
	return &copy, nil
}
func (f *businessLogoRepositoryFake) ListExpiredPendingUploads(context.Context, time.Time, int) ([]*models.PendingUpload, error) {
	return nil, nil
}
func (f *businessLogoRepositoryFake) SavePendingUpload(_ context.Context, upload *models.PendingUpload) error {
	copy := *upload
	f.upload = &copy
	return nil
}
func (f *businessLogoRepositoryFake) FinalizeBusinessLogo(_ context.Context, request interfaces.BusinessLogoFinalizeRequest) (*interfaces.BusinessLogoFinalizeResult, error) {
	if f.finalizeErr != nil {
		return nil, f.finalizeErr
	}
	f.finalized++
	if f.upload.ScanCode == models.PendingUploadScanBusinessLogo {
		return &interfaces.BusinessLogoFinalizeResult{Business: &models.BusinessProfile{ID: request.BusinessID, LogoKey: f.currentKey}, PreviousObjectKey: f.upload.CleanupObjectKey, Replayed: true}, nil
	}
	f.upload.ScanCode = models.PendingUploadScanBusinessLogo
	f.upload.Status = models.PendingUploadStatusClean
	f.upload.CleanupObjectKey = f.previousKey
	f.currentKey = request.ObjectKey
	return &interfaces.BusinessLogoFinalizeResult{
		Business: &models.BusinessProfile{ID: request.BusinessID, LogoKey: request.ObjectKey}, PreviousObjectKey: f.previousKey,
	}, nil
}
func (f *businessLogoRepositoryFake) CompleteBusinessLogoCleanup(_ context.Context, uploadID, businessID, uploaderID, objectKey string) error {
	if f.upload == nil || f.upload.ID != uploadID || f.upload.BusinessID != businessID || f.upload.UploaderID != uploaderID || f.upload.CleanupObjectKey != objectKey {
		return interfaces.ErrBusinessLogoFinalizeConflict
	}
	f.upload.CleanupObjectKey = ""
	return nil
}

type businessLogoObjectStoreFake struct {
	metadata    PendingObjectMetadata
	presigned   PendingObjectSpec
	inspectCall int
	deleted     []string
	deleteErr   error
}

func (f *businessLogoObjectStoreFake) PresignPendingUpload(_ context.Context, spec PendingObjectSpec) (*PresignedUpload, error) {
	f.presigned = spec
	return &PresignedUpload{UploadURL: "https://upload.example", RequiredHeaders: map[string]string{"Content-Type": spec.ContentType}}, nil
}
func (f *businessLogoObjectStoreFake) InspectPendingObject(context.Context, string, string) (PendingObjectMetadata, error) {
	f.inspectCall++
	return f.metadata, nil
}
func (f *businessLogoObjectStoreFake) PresignPendingDownload(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}
func (f *businessLogoObjectStoreFake) DeletePendingObject(_ context.Context, _, key string) error {
	f.deleted = append(f.deleted, key)
	return f.deleteErr
}
func (f *businessLogoObjectStoreFake) ReadPendingObject(context.Context, string, string) ([]byte, error) {
	return nil, nil
}

func TestBusinessLogoUploadBindsTenantUploaderAndChecksum(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	businessID, uploaderID := "8cf06379-19ea-41f9-a0ce-230e99978807", "owner-1"
	digest := sha256.Sum256([]byte("logo"))
	checksum := base64.StdEncoding.EncodeToString(digest[:])
	repository := &businessLogoRepositoryFake{}
	objects := &businessLogoObjectStoreFake{}
	uploads := NewPendingUploadService(repository, objects, nil, PendingUploadOptions{Bucket: "logos-private", Now: func() time.Time { return now }})
	service := NewBusinessService(&businessLogoBusinessRepoFake{business: &models.BusinessProfile{ID: businessID, OwnerID: uploaderID}}, nil, logger.New()).
		WithLogoUploadWorkflow(repository, uploads, objects, "logos-private")

	created, err := service.CreateLogoUpload(context.Background(), uploaderID, businessID, BusinessLogoUploadInput{ContentType: "image/png", SizeBytes: 4, ChecksumSHA256: checksum})
	if err != nil {
		t.Fatalf("create logo upload: %v", err)
	}
	if created.Upload.ObjectKey != "pending/"+businessID+"/"+created.Upload.ID+"/business_logo" || objects.presigned.Key != created.Upload.ObjectKey {
		t.Fatalf("unexpected bound key: %q", created.Upload.ObjectKey)
	}
	if objects.presigned.Metadata["business-id"] != businessID || objects.presigned.Metadata["uploader-id"] != uploaderID || objects.presigned.ChecksumSHA256 != checksum {
		t.Fatalf("tenant/uploader/checksum not bound: %#v", objects.presigned)
	}
}

func TestBusinessLogoFinalizeRejectsCrossTenantAndMetadataMismatch(t *testing.T) {
	service, repository, objects, businessID, uploaderID := newBusinessLogoFinalizeFixture(t)
	if _, err := service.FinalizeLogoUpload(context.Background(), uploaderID, "00000000-0000-4000-8000-000000000001", repository.upload.ID); err == nil {
		t.Fatal("expected cross-tenant finalization to fail")
	}
	objects.metadata.SizeBytes++
	if _, err := service.FinalizeLogoUpload(context.Background(), uploaderID, businessID, repository.upload.ID); !errors.Is(err, ErrBusinessLogoInvalid) {
		t.Fatalf("expected metadata mismatch, got %v", err)
	}
	if repository.finalized != 0 {
		t.Fatal("metadata mismatch reached reference transaction")
	}
}

func TestBusinessLogoFinalizeIsIdempotentAndCleansOldObjectAfterCommit(t *testing.T) {
	service, repository, objects, businessID, uploaderID := newBusinessLogoFinalizeFixture(t)
	repository.previousKey = "logos/" + businessID + "/old"

	first, err := service.FinalizeLogoUpload(context.Background(), uploaderID, businessID, repository.upload.ID)
	if err != nil || first.Replayed || first.Status != "ready" || len(objects.deleted) != 1 {
		t.Fatalf("unexpected first completion: result=%+v err=%v deleted=%v", first, err, objects.deleted)
	}
	repository.upload.ExpiresAt = time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
	second, err := service.FinalizeLogoUpload(context.Background(), uploaderID, businessID, repository.upload.ID)
	if err != nil || !second.Replayed || len(objects.deleted) != 1 || objects.inspectCall != 1 {
		t.Fatalf("unexpected replay: result=%+v err=%v deleted=%v inspections=%d", second, err, objects.deleted, objects.inspectCall)
	}
}

func TestBusinessLogoFinalizeFailureDoesNotDeleteOldObject(t *testing.T) {
	service, repository, objects, businessID, uploaderID := newBusinessLogoFinalizeFixture(t)
	repository.previousKey = "logos/" + businessID + "/old"
	repository.finalizeErr = errors.New("transaction failed")
	if _, err := service.FinalizeLogoUpload(context.Background(), uploaderID, businessID, repository.upload.ID); err == nil {
		t.Fatal("expected transaction failure")
	}
	if len(objects.deleted) != 0 {
		t.Fatalf("old object deleted before commit: %v", objects.deleted)
	}
}

func TestBusinessLogoFinalizeReportsPostCommitCleanupFailure(t *testing.T) {
	service, repository, objects, businessID, uploaderID := newBusinessLogoFinalizeFixture(t)
	repository.previousKey = "logos/" + businessID + "/old"
	objects.deleteErr = errors.New("delete failed")
	result, err := service.FinalizeLogoUpload(context.Background(), uploaderID, businessID, repository.upload.ID)
	if err != nil || !result.CleanupPending || repository.currentKey != repository.upload.ObjectKey {
		t.Fatalf("reference must remain committed after cleanup failure: result=%+v err=%v", result, err)
	}
	objects.deleteErr = nil
	replayed, err := service.FinalizeLogoUpload(context.Background(), uploaderID, businessID, repository.upload.ID)
	if err != nil || !replayed.Replayed || replayed.CleanupPending || repository.upload.CleanupObjectKey != "" || len(objects.deleted) != 2 {
		t.Fatalf("replay must durably retry cleanup: result=%+v err=%v upload=%+v deleted=%v", replayed, err, repository.upload, objects.deleted)
	}
}

func newBusinessLogoFinalizeFixture(t *testing.T) (*BusinessService, *businessLogoRepositoryFake, *businessLogoObjectStoreFake, string, string) {
	t.Helper()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	businessID, uploaderID := "8cf06379-19ea-41f9-a0ce-230e99978807", "owner-1"
	uploadID := "0a143f2e-086f-46f3-bb36-d38ef78b0945"
	digest := sha256.Sum256([]byte("logo"))
	checksum := base64.StdEncoding.EncodeToString(digest[:])
	upload := &models.PendingUpload{
		ID: uploadID, BusinessID: businessID, UploaderID: uploaderID, Kind: "business_logo", Bucket: "logos-private",
		ObjectKey: "pending/" + businessID + "/" + uploadID + "/business_logo", ContentType: "image/png", SizeBytes: 4,
		ChecksumSHA256: checksum, Status: models.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour),
	}
	repository := &businessLogoRepositoryFake{upload: upload}
	objects := &businessLogoObjectStoreFake{metadata: PendingObjectMetadata{
		ContentType: upload.ContentType, SizeBytes: upload.SizeBytes, ChecksumSHA256: checksum,
		Metadata: map[string]string{"business-id": businessID, "uploader-id": uploaderID, "upload-id": uploadID},
	}}
	service := NewBusinessService(&businessLogoBusinessRepoFake{business: &models.BusinessProfile{ID: businessID, OwnerID: uploaderID}}, nil, logger.New()).
		WithLogoUploadWorkflow(repository, nil, objects, upload.Bucket)
	service.now = func() time.Time { return now }
	return service, repository, objects, businessID, uploaderID
}
