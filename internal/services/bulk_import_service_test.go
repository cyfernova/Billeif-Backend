package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type bulkImportObjectFake struct{ content []byte }

func (f bulkImportObjectFake) OpenPendingObject(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.content)), nil
}

type bulkImportQueueFake struct {
	messages []BulkImportQueueMessage
	err      error
}

func (f *bulkImportQueueFake) EnqueueBulkImport(_ context.Context, message BulkImportQueueMessage) error {
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, message)
	return nil
}

type bulkImportArtifactFake struct {
	uploads int
	deletes int
}

type bulkImportNotifierFake struct{ calls int }

func (f *bulkImportNotifierFake) Ingest(context.Context, NotificationInput) (*models.Notification, error) {
	f.calls++
	return &models.Notification{ID: uuid.NewString()}, nil
}

func (f *bulkImportArtifactFake) UploadIfAbsent(context.Context, string, string, []byte, string) error {
	f.uploads++
	return nil
}

func (f *bulkImportArtifactFake) Delete(context.Context, string, string) error {
	f.deletes++
	return nil
}

func (*bulkImportArtifactFake) GeneratePresignedDownloadURL(context.Context, string, string, int64) (string, error) {
	return "https://download.example.test/result", nil
}

func migrateBulkImportSQLite(t *testing.T, db *gorm.DB, values ...any) {
	t.Helper()
	for _, value := range values {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(value); err != nil {
			t.Fatal(err)
		}
		if id := statement.Schema.LookUpField("ID"); id != nil {
			id.HasDefaultValue = false
			id.DefaultValue = ""
			id.DefaultValueInterface = nil
		}
	}
	if err := db.AutoMigrate(values...); err != nil {
		t.Fatal(err)
	}
}

func TestBulkImportValidatesWithoutMutationThenCommitsExactlyOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	migrateBulkImportSQLite(t, db, &models.BulkJob{}, &models.BulkJobRow{}, &models.BulkJobArtifact{}, &models.Customer{})
	content := []byte("name,email,phone,gstin\nAsha,asha@example.test,+919876543210,\n")
	digest := sha256.Sum256(content)
	businessID, uploaderID, uploadID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	upload := &models.PendingUpload{ID: uploadID, BusinessID: businessID, UploaderID: uploaderID, Kind: "bulk_import", Bucket: "private", ObjectKey: "pending/" + businessID + "/" + uploadID + "/bulk_import", ContentType: "text/csv", SizeBytes: int64(len(content)), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]), Status: models.PendingUploadStatusClean, ExpiresAt: time.Now().Add(time.Hour)}
	uploads := &pendingUploadRepositoryFake{upload: upload}
	queue := &bulkImportQueueFake{}
	artifacts := &bulkImportArtifactFake{}
	notifier := &bulkImportNotifierFake{}
	service := NewBulkImportService(db, uploads, bulkImportObjectFake{content: content}, queue, BulkImportOptions{
		Permissions: &recordingPermissionChecker{allow: true}, Capability: &recordingCapabilityGuard{},
		ArtifactStore: artifacts, ArtifactBucket: "private", Notifications: notifier,
	})
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: uploaderID})

	job, err := service.Validate(ctx, businessID, uploaderID, models.BulkJobTypeImportCustomers, ValidateBulkImportInput{UploadID: uploadID})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != models.BulkJobStatusValidated || job.TotalRows != 1 || job.FailedRows != 0 {
		t.Fatalf("validated job = status %s total %d failed %d", job.Status, job.TotalRows, job.FailedRows)
	}
	if _, err := service.Get(ctx, businessID, uuid.NewString(), job.ID); !errors.Is(err, ErrBulkImportNotFound) {
		t.Fatalf("cross-uploader read error = %v", err)
	}
	var before int64
	if err := db.Model(&models.Customer{}).Count(&before).Error; err != nil || before != 0 {
		t.Fatalf("validation mutated customers: count=%d err=%v", before, err)
	}
	replay, err := service.Validate(ctx, businessID, uploaderID, models.BulkJobTypeImportCustomers, ValidateBulkImportInput{UploadID: uploadID})
	if err != nil || replay.ID != job.ID {
		t.Fatalf("validation replay = %v, %v", replay, err)
	}
	if err := db.Model(&models.BulkJob{}).Where("id = ?", job.ID).Update("status", models.BulkJobStatusValidating).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := service.Validate(ctx, businessID, uploaderID, models.BulkJobTypeImportCustomers, ValidateBulkImportInput{UploadID: uploadID})
	if err != nil || recovered.ID != job.ID || recovered.TotalRows != 1 {
		t.Fatalf("validation restart recovery = %#v, %v", recovered, err)
	}
	commandID := uuid.NewString()
	queued, err := service.Commit(ctx, businessID, uploaderID, job.ID, commandID)
	if err != nil || queued.Status != models.BulkJobStatusCommitQueued || len(queue.messages) != 1 {
		t.Fatalf("commit queue = %#v messages=%d err=%v", queued, len(queue.messages), err)
	}
	if _, err := service.Commit(ctx, businessID, uploaderID, job.ID, commandID); err != nil || len(queue.messages) != 2 {
		t.Fatalf("commit retry messages=%d err=%v", len(queue.messages), err)
	}
	if err := service.Process(ctx, queue.messages[0]); err != nil {
		t.Fatal(err)
	}
	if err := service.Process(ctx, queue.messages[1]); err != nil {
		t.Fatal(err)
	}
	var customers int64
	if err := db.Model(&models.Customer{}).Count(&customers).Error; err != nil || customers != 1 {
		t.Fatalf("committed customers: count=%d err=%v", customers, err)
	}
	completed, err := service.Get(ctx, businessID, uploaderID, job.ID)
	if err != nil || completed.Status != models.BulkJobStatusCompleted || completed.SucceededRows != 1 {
		t.Fatalf("completed job = %#v err=%v", completed, err)
	}
	if artifacts.uploads != 1 || len(completed.Artifacts) != 1 || completed.NotificationState != "sent" || notifier.calls != 1 {
		t.Fatalf("finalization = uploads %d artifacts %d notification %s", artifacts.uploads, len(completed.Artifacts), completed.NotificationState)
	}
	download, err := service.GetArtifactDownload(ctx, businessID, uploaderID, job.ID, completed.Artifacts[0].ID)
	if err != nil || download.URL == "" {
		t.Fatalf("artifact download = %#v err=%v", download, err)
	}
	if err := db.Model(&models.BulkJob{}).Where("id = ?", job.ID).Update("retain_until", time.Unix(1, 0).UTC()).Error; err != nil {
		t.Fatal(err)
	}
	cleaned, err := service.CleanupExpired(ctx, 10)
	if err != nil || cleaned != 1 || artifacts.deletes != 2 {
		t.Fatalf("cleanup = jobs %d deletes %d err=%v", cleaned, artifacts.deletes, err)
	}
	expired, err := service.Get(ctx, businessID, uploaderID, job.ID)
	if err != nil || expired.Status != models.BulkJobStatusExpired || expired.ArtifactState != "expired" {
		t.Fatalf("expired job = %#v err=%v", expired, err)
	}
}

func TestBulkImportInvalidPreviewNeverMutatesBusinessData(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	migrateBulkImportSQLite(t, db, &models.BulkJob{}, &models.BulkJobRow{}, &models.BulkJobArtifact{}, &models.Customer{})
	content := []byte("name,email\nAsha,=HYPERLINK(\"https://example.test\")\n")
	digest := sha256.Sum256(content)
	businessID, uploaderID, uploadID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	upload := &models.PendingUpload{ID: uploadID, BusinessID: businessID, UploaderID: uploaderID, Kind: "bulk_import", Bucket: "private", ObjectKey: "pending/" + businessID + "/" + uploadID + "/bulk_import", ContentType: "text/csv", SizeBytes: int64(len(content)), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]), Status: models.PendingUploadStatusClean, ExpiresAt: time.Now().Add(time.Hour)}
	service := NewBulkImportService(db, &pendingUploadRepositoryFake{upload: upload}, bulkImportObjectFake{content: content}, &bulkImportQueueFake{}, BulkImportOptions{Permissions: &recordingPermissionChecker{allow: true}, Capability: &recordingCapabilityGuard{}})
	job, err := service.Validate(ContextWithActor(context.Background(), ActorContext{UserID: uploaderID}), businessID, uploaderID, models.BulkJobTypeImportCustomers, ValidateBulkImportInput{UploadID: uploadID})
	if err != nil || job.FailedRows != 1 {
		t.Fatalf("invalid preview = %#v err=%v", job, err)
	}
	var customers int64
	if err := db.Model(&models.Customer{}).Count(&customers).Error; err != nil || customers != 0 {
		t.Fatalf("invalid preview mutations = %d err=%v", customers, err)
	}
}

func TestBulkImportRecoversDispatchAndDoesNotSpendRetriesOnSuccessfulClaims(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	migrateBulkImportSQLite(t, db, &models.BulkJob{}, &models.BulkJobRow{})
	now := time.Now().UTC()
	businessID, uploaderID, jobID, commandID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	job := &models.BulkJob{ID: jobID, BusinessID: businessID, CreatedBy: uploaderID, JobType: models.BulkJobTypeImportCustomers, Status: models.BulkJobStatusValidated, ArtifactState: bulkImportArtifactPending, NotificationState: "pending"}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	queue := &bulkImportQueueFake{err: errors.New("queue down")}
	service := NewBulkImportService(db, nil, nil, queue, BulkImportOptions{Now: func() time.Time { return now }, Permissions: &recordingPermissionChecker{allow: true}})
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: uploaderID})
	if _, err := service.QueueCommit(ctx, businessID, uploaderID, jobID, commandID); !errors.Is(err, ErrBulkImportRetryable) {
		t.Fatalf("queue failure = %v", err)
	}
	var queued models.BulkJob
	if err := db.First(&queued, "id = ?", jobID).Error; err != nil || queued.NextRetryAt == nil || queued.AttemptCount != 1 {
		t.Fatalf("durable queue state = %#v err=%v", queued, err)
	}
	now = now.Add(2 * time.Minute)
	queue.err = nil
	recovered, err := service.RecoverPending(ctx, 10)
	if err != nil || recovered != 1 || len(queue.messages) != 1 {
		t.Fatalf("recovery = %d messages=%d err=%v", recovered, len(queue.messages), err)
	}
	var recoveredJob models.BulkJob
	if err := db.First(&recoveredJob, "id = ?", jobID).Error; err != nil || recoveredJob.NextRetryAt != nil {
		t.Fatalf("recovered queue state = %#v err=%v", recoveredJob, err)
	}
	row := &models.BulkJobRow{ID: uuid.NewString(), BulkJobID: jobID, RowNumber: 2, Status: bulkImportRowValidated, IdempotencyKey: jobID + ":2", Input: `{"name":"Asha","email":"asha@example.test"}`}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	claimedRows, claimed, err := service.claimBatch(ctx, businessID, jobID, commandID, "worker", 100)
	if err != nil || len(claimedRows) != 1 || claimed.AttemptCount != 1 {
		t.Fatalf("claim = rows %d attempts %d err=%v", len(claimedRows), claimed.AttemptCount, err)
	}
}

func TestBulkImportCommitStopsAtCancellationAndRecordsConcurrentDuplicate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	migrateBulkImportSQLite(t, db, &models.BulkJob{}, &models.BulkJobRow{}, &models.Customer{})
	businessID, uploaderID, jobID, commandID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	job := &models.BulkJob{ID: jobID, BusinessID: businessID, CreatedBy: uploaderID, JobType: models.BulkJobTypeImportCustomers, Status: models.BulkJobStatusCommitting, CommitCommandID: &commandID, LeaseOwner: "worker", CancelRequested: true, ArtifactState: bulkImportArtifactPending, NotificationState: "pending"}
	row := models.BulkJobRow{ID: uuid.NewString(), BulkJobID: jobID, RowNumber: 2, Status: bulkImportRowValidated, IdempotencyKey: jobID + ":2", Input: `{"name":"Asha","email":"asha@example.test"}`}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	service := NewBulkImportService(db, nil, nil, nil, BulkImportOptions{})
	if err := service.commitRow(context.Background(), job, row); !errors.Is(err, errBulkImportCanceled) {
		t.Fatalf("canceled row error = %v", err)
	}
	var customers int64
	if err := db.Model(&models.Customer{}).Count(&customers).Error; err != nil || customers != 0 {
		t.Fatalf("canceled job mutations = %d err=%v", customers, err)
	}
	if err := db.Model(job).Update("cancel_requested", false).Error; err != nil {
		t.Fatal(err)
	}
	job.CancelRequested = false
	if err := db.Create(&models.Customer{ID: uuid.NewString(), BusinessID: businessID, Name: "Existing", Email: "asha@example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.commitRow(context.Background(), job, row); err != nil {
		t.Fatalf("duplicate row commit error = %v", err)
	}
	var stored models.BulkJobRow
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil || stored.Status != bulkImportRowInvalid || stored.ErrorCode != "duplicate_at_commit" {
		t.Fatalf("duplicate row result = %#v err=%v", stored, err)
	}
	if err := db.Model(&models.Customer{}).Count(&customers).Error; err != nil || customers != 1 {
		t.Fatalf("duplicate mutation count = %d err=%v", customers, err)
	}
}

func TestBulkImportPersistsCompleteVendorAndProductFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	migrateBulkImportSQLite(t, db, &models.Vendor{})
	if err := db.Exec(`CREATE TABLE products (id text PRIMARY KEY, business_id text NOT NULL, category_id text, name text NOT NULL, sku text NOT NULL, barcode text, description text, price real, mrp real, cost_price real, valuation_method text, hsn_sac_code text, uqc_code text, gst_metadata text, default_cess_rate real, default_price_list_id text, is_service numeric, currency text, unit text, stock_level integer, min_stock integer, low_stock_threshold integer, image_url text, image_key text, extra_attributes text, categories text, is_active numeric, created_at datetime, updated_at datetime, deleted_at datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	businessID, id, vendorID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := createBulkImportEntity(db, &models.BulkJob{BusinessID: businessID, JobType: models.BulkJobTypeImportVendors}, vendorID, map[string]string{"name": "Supplier", "email": "supplier@example.test", "phone": "+919876543210", "gstin": "", "city": "Pune"}); err != nil {
		t.Fatal(err)
	}
	var vendor models.Vendor
	if err := db.First(&vendor, "id = ?", vendorID).Error; err != nil || vendor.City != "Pune" || vendor.Phone != "+919876543210" {
		t.Fatalf("vendor = %#v err=%v", vendor, err)
	}
	job := &models.BulkJob{BusinessID: businessID, JobType: models.BulkJobTypeImportProducts}
	if err := createBulkImportEntity(db, job, id, map[string]string{"name": "Widget", "sku": "W-1", "price": "10.50", "currency": "INR", "unit": "PCS", "tax_rate": "18", "description": "Useful", "hsn_sac_code": "8471"}); err != nil {
		t.Fatal(err)
	}
	var product models.Product
	if err := db.First(&product, "id = ?", id).Error; err != nil || product.Description != "Useful" || product.HSNSACCode != "8471" {
		t.Fatalf("product = %#v err=%v", product, err)
	}
}

func TestBulkImportValidationHelpers(t *testing.T) {
	if bulkImportMaxRows != 10_000 {
		t.Fatalf("max rows = %d, want 10000", bulkImportMaxRows)
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"=SUM(A1)", true}, {"@cmd", true}, {"safe", false}, {"+919876543210", true},
	} {
		if got := bulkFormulaValue(tc.value); got != tc.want {
			t.Errorf("formula(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
	if got, err := bulkImportDelimiter("tab"); err != nil || got != '\t' {
		t.Fatalf("tab delimiter = %q, %v", got, err)
	}
	if _, err := bulkImportDelimiter("pipe"); err == nil {
		t.Fatal("unsupported delimiter accepted")
	}
}

func TestBulkImportCommitAcceptsUUIDAndRejectsOtherCommands(t *testing.T) {
	s := &BulkImportService{now: time.Now}
	if _, err := s.Commit(context.Background(), uuid.NewString(), uuid.NewString(), uuid.NewString(), 42); err != ErrBulkImportInvalid {
		t.Fatalf("invalid command error = %v", err)
	}
}

func TestBulkImportAllowedTypes(t *testing.T) {
	for _, typ := range []string{models.BulkJobTypeImportCustomers, models.BulkJobTypeImportVendors, models.BulkJobTypeImportProducts} {
		if !bulkImportTypeAllowed(typ) {
			t.Errorf("%s not allowed", typ)
		}
	}
	if bulkImportTypeAllowed(models.BulkJobTypeImportInvoices) {
		t.Fatal("invoice imports must remain unsupported")
	}
}
