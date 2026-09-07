package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFinalizeBusinessLogoCommitsOnce(t *testing.T) {
	db := newBusinessLogoRepositoryDB(t, true)
	business, upload, request := seedBusinessLogoRepository(t, db)
	repository := NewSecurityRepository(db)

	first, err := repository.FinalizeBusinessLogo(context.Background(), request)
	if err != nil || first.Replayed || first.PreviousObjectKey != business.LogoKey {
		t.Fatalf("unexpected first finalization: result=%+v err=%v", first, err)
	}
	if err := repository.CompleteBusinessLogoCleanup(context.Background(), upload.ID, business.ID, business.OwnerID, business.LogoKey); err != nil {
		t.Fatalf("complete durable old-logo cleanup: %v", err)
	}
	if err := db.Model(&models.PendingUpload{}).Where("id = ?", upload.ID).Update("expires_at", request.CompletedAt.Add(-time.Hour)).Error; err != nil {
		t.Fatalf("expire finalized upload: %v", err)
	}
	second, err := repository.FinalizeBusinessLogo(context.Background(), request)
	if err != nil || !second.Replayed || second.PreviousObjectKey != "" {
		t.Fatalf("unexpected replay: result=%+v err=%v", second, err)
	}
	var stored models.BusinessProfile
	if err := db.First(&stored, "id = ?", business.ID).Error; err != nil || stored.LogoKey != upload.ObjectKey {
		t.Fatalf("logo reference not committed: business=%+v err=%v", stored, err)
	}
	var audits int64
	if err := db.Model(&models.SecurityAuditEvent{}).Where("event_type = ? AND resource_id = ?", "business_logo_attached", upload.ID).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("expected one audit, count=%d err=%v", audits, err)
	}
}

func TestFinalizeBusinessLogoRollsBackReferenceWhenAuditFails(t *testing.T) {
	db := newBusinessLogoRepositoryDB(t, false)
	business, upload, request := seedBusinessLogoRepository(t, db)
	repository := NewSecurityRepository(db)

	if _, err := repository.FinalizeBusinessLogo(context.Background(), request); err == nil {
		t.Fatal("expected missing audit table failure")
	}
	var storedBusiness models.BusinessProfile
	if err := db.First(&storedBusiness, "id = ?", business.ID).Error; err != nil || storedBusiness.LogoKey != business.LogoKey {
		t.Fatalf("business reference escaped rollback: business=%+v err=%v", storedBusiness, err)
	}
	var storedUpload models.PendingUpload
	if err := db.First(&storedUpload, "id = ?", upload.ID).Error; err != nil || storedUpload.Status != models.PendingUploadStatusPending {
		t.Fatalf("upload state escaped rollback: upload=%+v err=%v", storedUpload, err)
	}
}

func newBusinessLogoRepositoryDB(t *testing.T, withAudit bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	statements := []string{
		`CREATE TABLE business_profiles (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, logo_url TEXT NOT NULL DEFAULT '', logo_key TEXT NOT NULL DEFAULT '', updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE security_pending_uploads (id TEXT PRIMARY KEY, business_id TEXT NOT NULL, uploader_id TEXT NOT NULL, kind TEXT NOT NULL, bucket TEXT NOT NULL, object_key TEXT NOT NULL UNIQUE, content_type TEXT NOT NULL, size_bytes INTEGER NOT NULL, checksum_sha256 TEXT NOT NULL, cleanup_object_key TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, scan_code TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL, expires_at DATETIME NOT NULL, completed_at DATETIME, deleted_at DATETIME)`,
	}
	if withAudit {
		statements = append(statements, `CREATE TABLE security_audit_events (id TEXT PRIMARY KEY, business_id TEXT, subject TEXT NOT NULL, event_type TEXT NOT NULL, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL, outcome TEXT NOT NULL, reason_code TEXT NOT NULL, occurred_at DATETIME NOT NULL)`)
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create logo test table: %v", err)
		}
	}
	return db
}

func seedBusinessLogoRepository(t *testing.T, db *gorm.DB) (*models.BusinessProfile, *models.PendingUpload, interfaces.BusinessLogoFinalizeRequest) {
	t.Helper()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	business := &models.BusinessProfile{ID: uuid.NewString(), OwnerID: "owner-1", LogoKey: "logos/old"}
	if err := db.Exec(`INSERT INTO business_profiles(id, owner_id, logo_key) VALUES (?, ?, ?)`, business.ID, business.OwnerID, business.LogoKey).Error; err != nil {
		t.Fatalf("create business: %v", err)
	}
	digest := sha256.Sum256([]byte("logo"))
	upload := &models.PendingUpload{
		ID: uuid.NewString(), BusinessID: business.ID, UploaderID: business.OwnerID, Kind: "business_logo", Bucket: "logos-private",
		ContentType: "image/png", SizeBytes: 4, ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]),
		Status: models.PendingUploadStatusPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	upload.ObjectKey = "pending/" + business.ID + "/" + upload.ID + "/business_logo"
	if err := db.Exec(`INSERT INTO security_pending_uploads(id, business_id, uploader_id, kind, bucket, object_key, content_type, size_bytes, checksum_sha256, status, scan_code, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, upload.ID, upload.BusinessID, upload.UploaderID, upload.Kind, upload.Bucket, upload.ObjectKey, upload.ContentType, upload.SizeBytes, upload.ChecksumSHA256, upload.Status, upload.ScanCode, upload.CreatedAt, upload.ExpiresAt).Error; err != nil {
		t.Fatalf("create pending upload: %v", err)
	}
	return business, upload, interfaces.BusinessLogoFinalizeRequest{
		BusinessID: business.ID, UploaderID: business.OwnerID, UploadID: upload.ID, Bucket: upload.Bucket,
		ObjectKey: upload.ObjectKey, ContentType: upload.ContentType, SizeBytes: upload.SizeBytes,
		ChecksumSHA256: upload.ChecksumSHA256, CompletedAt: now,
	}
}
