package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func expectCanonicalRenderClaim(
	mock sqlmock.Sqlmock,
	businessID, jobID string,
	kind models.RenderKind,
	version int,
	owner string,
	now, leaseUntil time.Time,
) {
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"attempts"=attempts \+ 1.*"lease_expires_at"=\$3.*"lease_owner"=\$4.*"status"=\$5.*WHERE .*id = \$7 AND business_id = \$8 AND kind = \$9 AND source_invoice_version = \$10 AND \(status IN \(\$11,\$12\) OR \(status = \$13 AND \(lease_expires_at IS NULL OR lease_expires_at <= \$14\)\)\) AND deleted_at IS NULL`).
		WithArgs(nil, "", leaseUntil, owner, models.RenderJobStatusProcessing, sqlmock.AnyArg(), jobID, businessID, kind, version, models.RenderJobStatusQueued, models.RenderJobStatusFailed, models.RenderJobStatusProcessing, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

func TestDocumentRepositoryGetInvoiceRenderJobScopesByTenantInvoiceAndJob(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()

	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*id = \$1 AND invoice_id = \$2 AND business_id = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
		WithArgs(jobID, invoiceID, businessID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "invoice_id", "business_id", "kind", "source_invoice_version", "status",
		}).AddRow(jobID, invoiceID, businessID, models.RenderKindPreview, 3, models.RenderJobStatusQueued))

	job, err := repository.GetInvoiceRenderJob(context.Background(), businessID, invoiceID, jobID)
	if err != nil {
		t.Fatalf("GetInvoiceRenderJob() error = %v", err)
	}
	if job.ID != jobID || job.InvoiceID == nil || *job.InvoiceID != invoiceID || job.BusinessID != businessID {
		t.Fatalf("GetInvoiceRenderJob() = %#v, want exact tenant invoice job", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryGetCompletedFinalRenderJobScopesByTenantInvoiceAndVersion(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()

	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*business_id = \$1 AND invoice_id = \$2 AND kind = \$3 AND source_invoice_version = \$4 AND status = \$5 AND object_key <> '' AND deleted_at IS NULL.*ORDER BY completed_at DESC, created_at DESC.*LIMIT \$6`).
		WithArgs(businessID, invoiceID, models.RenderKindFinal, 7, models.RenderJobStatusCompleted, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "invoice_id", "business_id", "kind", "source_invoice_version", "object_key", "status",
		}).AddRow(jobID, invoiceID, businessID, models.RenderKindFinal, 7, "invoices/final.pdf", models.RenderJobStatusCompleted))

	job, err := repository.GetCompletedFinalRenderJob(context.Background(), businessID, invoiceID, 7)
	if err != nil {
		t.Fatalf("GetCompletedFinalRenderJob() error = %v", err)
	}
	if job.ID != jobID || job.SourceInvoiceVersion == nil || *job.SourceInvoiceVersion != 7 {
		t.Fatalf("GetCompletedFinalRenderJob() = %#v, want current completed final render", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryGetCompletedFinalRenderJobRequiresNonemptyObjectKey(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, invoiceID := uuid.NewString(), uuid.NewString()

	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*business_id = \$1 AND invoice_id = \$2 AND kind = \$3 AND source_invoice_version = \$4 AND status = \$5 AND object_key <> '' AND deleted_at IS NULL.*LIMIT \$6`).
		WithArgs(businessID, invoiceID, models.RenderKindFinal, 7, models.RenderJobStatusCompleted, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	if _, err := repository.GetCompletedFinalRenderJob(context.Background(), businessID, invoiceID, 7); !errors.Is(err, interfaces.ErrInvoiceRenderNotFound) {
		t.Fatalf("GetCompletedFinalRenderJob() error = %v, want ErrInvoiceRenderNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryClaimPreviewRenderReclaimsExpiredLeaseForNewOwner(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	jobID := uuid.NewString()
	owner := "sqs-message-owner-b"
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"attempts"=attempts \+ 1.*"lease_expires_at"=\$[0-9]+.*"lease_owner"=\$[0-9]+.*"status"=\$[0-9]+.*WHERE .*id = \$[0-9]+ AND business_id = \$[0-9]+ AND kind = \$[0-9]+ AND source_invoice_version = \$[0-9]+ AND \(status IN \(\$[0-9]+,\$[0-9]+\) OR \(status = \$[0-9]+ AND \(lease_expires_at IS NULL OR lease_expires_at <= \$[0-9]+\)\)\) AND deleted_at IS NULL`).
		WithArgs(nil, "", leaseUntil, owner, models.RenderJobStatusProcessing, sqlmock.AnyArg(), jobID, businessID, models.RenderKindPreview, 5, models.RenderJobStatusQueued, models.RenderJobStatusFailed, models.RenderJobStatusProcessing, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	state, err := repository.ClaimPreviewRender(context.Background(), businessID, jobID, 5, owner, now, leaseUntil)
	if err != nil || state != interfaces.PreviewRenderClaimed {
		t.Fatalf("claim state/error = %q/%v, want claimed", state, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryClaimGenericRenderAtomicallyClaimsQueuedJob(t *testing.T) {
	database, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: database.db}
	businessID, jobID := uuid.NewString(), uuid.NewString()
	owner := "sqs-message-owner-a"
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"attempts"=attempts \+ 1.*"lease_expires_at"=\$[0-9]+.*"lease_owner"=\$[0-9]+.*"status"=\$[0-9]+.*WHERE .*id = \$[0-9]+ AND business_id = \$[0-9]+ AND kind = \$[0-9]+ AND \(status IN \(\$[0-9]+,\$[0-9]+\) OR \(status = \$[0-9]+ AND \(lease_expires_at IS NULL OR lease_expires_at <= \$[0-9]+\)\)\) AND deleted_at IS NULL`).
		WithArgs(nil, "", leaseUntil, owner, models.RenderJobStatusProcessing, sqlmock.AnyArg(), jobID, businessID, models.RenderKindPreview, models.RenderJobStatusQueued, models.RenderJobStatusFailed, models.RenderJobStatusProcessing, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	state, err := repository.ClaimGenericRender(context.Background(), businessID, jobID, owner, now, leaseUntil)
	if err != nil || state != interfaces.GenericRenderClaimed {
		t.Fatalf("claim state/error = %q/%v, want claimed", state, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryClaimGenericRenderReportsConcurrentOwner(t *testing.T) {
	database, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: database.db}
	businessID, jobID := uuid.NewString(), uuid.NewString()
	owner := "sqs-message-owner-b"
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"attempts"=attempts \+ 1.*"lease_expires_at"=\$[0-9]+.*"lease_owner"=\$[0-9]+.*"status"=\$[0-9]+.*WHERE .*id = \$[0-9]+ AND business_id = \$[0-9]+ AND kind = \$[0-9]+ AND \(status IN \(\$[0-9]+,\$[0-9]+\) OR \(status = \$[0-9]+ AND \(lease_expires_at IS NULL OR lease_expires_at <= \$[0-9]+\)\)\) AND deleted_at IS NULL`).
		WithArgs(nil, "", leaseUntil, owner, models.RenderJobStatusProcessing, sqlmock.AnyArg(), jobID, businessID, models.RenderKindPreview, models.RenderJobStatusQueued, models.RenderJobStatusFailed, models.RenderJobStatusProcessing, now).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL`).
		WithArgs(jobID, businessID, models.RenderKindPreview, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "kind", "status"}).
			AddRow(jobID, businessID, models.RenderKindPreview, models.RenderJobStatusProcessing))

	state, err := repository.ClaimGenericRender(context.Background(), businessID, jobID, owner, now, leaseUntil)
	if err != nil || state != interfaces.GenericRenderAlreadyProcessing {
		t.Fatalf("claim state/error = %q/%v, want already processing", state, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryClaimFinalRenderReclaimsExpiredLeaseForNewOwner(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, jobID, owner := uuid.NewString(), uuid.NewString(), "sqs-message-owner-b"
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"attempts"=attempts \+ 1.*"lease_expires_at"=\$[0-9]+.*"lease_owner"=\$[0-9]+.*WHERE .*kind = \$[0-9]+.*source_invoice_version = \$[0-9]+.*status = \$[0-9]+ AND \(lease_expires_at IS NULL OR lease_expires_at <= \$[0-9]+\)`).
		WithArgs(nil, "", leaseUntil, owner, models.RenderJobStatusProcessing, sqlmock.AnyArg(), jobID, businessID, models.RenderKindFinal, 5, models.RenderJobStatusQueued, models.RenderJobStatusFailed, models.RenderJobStatusProcessing, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	state, err := repository.ClaimFinalRender(context.Background(), businessID, jobID, 5, owner, now, leaseUntil)
	if err != nil || state != interfaces.FinalRenderClaimed {
		t.Fatalf("claim state/error = %q/%v, want claimed", state, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryRejectsStalePreviewOwnerForFailureAndObsolete(t *testing.T) {
	for _, transition := range []struct {
		name string
		call func(*documentRepository, context.Context, string, string, string) error
	}{
		{name: "failure", call: func(r *documentRepository, ctx context.Context, businessID, jobID, owner string) error {
			return r.FailPreviewRender(ctx, businessID, jobID, owner, "stale worker")
		}},
		{name: "obsolete", call: func(r *documentRepository, ctx context.Context, businessID, jobID, owner string) error {
			return r.ObsoletePreviewRender(ctx, businessID, jobID, owner)
		}},
	} {
		t.Run(transition.name, func(t *testing.T) {
			invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := &documentRepository{db: invoiceRepository.db}
			businessID, jobID, oldOwner := uuid.NewString(), uuid.NewString(), "sqs-message-owner-a"

			mock.ExpectBegin()
			mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*id = \$[0-9]+ AND business_id = \$[0-9]+ AND kind = \$[0-9]+ AND status = \$[0-9]+ AND lease_owner = \$[0-9]+ AND deleted_at IS NULL`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			if transition.name == "failure" {
				mock.ExpectQuery(`SELECT "id","business_id","kind","status" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
					WithArgs(jobID, businessID, models.RenderKindPreview, 1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "kind", "status"}).AddRow(jobID, businessID, models.RenderKindPreview, models.RenderJobStatusProcessing))
			}

			if err := transition.call(repository, context.Background(), businessID, jobID, oldOwner); err == nil {
				t.Fatal("stale owner transition succeeded after a new owner reclaimed the lease")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestDocumentRepositoryRejectsStaleFinalOwnerFailure(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, jobID := uuid.NewString(), uuid.NewString()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*kind = \$[0-9]+ AND status = \$[0-9]+ AND lease_owner = \$[0-9]+`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT "id","business_id","kind","status" FROM "document_render_jobs".*LIMIT \$4`).WithArgs(jobID, businessID, models.RenderKindFinal, 1).WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "kind", "status"}).AddRow(jobID, businessID, models.RenderKindFinal, models.RenderJobStatusProcessing))
	if err := repository.FailFinalRender(context.Background(), businessID, jobID, "owner-a", "stale"); err == nil {
		t.Fatal("stale final owner failure succeeded")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryRejectsStaleOwnerCompletion(t *testing.T) {
	for _, test := range []struct {
		name     string
		kind     models.RenderKind
		complete func(*documentRepository, context.Context, string, string, string, string) (bool, error)
	}{
		{
			name: "preview",
			kind: models.RenderKindPreview,
			complete: func(r *documentRepository, ctx context.Context, businessID, invoiceID, jobID, owner string) (bool, error) {
				return r.CompletePreviewRender(ctx, businessID, jobID, 5, owner, "invoices/preview.pdf", "invoices/preview.pdf.attempt-owner-a", "preview.pdf")
			},
		},
		{
			name: "final",
			kind: models.RenderKindFinal,
			complete: func(r *documentRepository, ctx context.Context, businessID, invoiceID, jobID, owner string) (bool, error) {
				return r.CompleteFinalRender(ctx, businessID, invoiceID, jobID, 5, owner, "invoices/final.pdf", "final.pdf")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := &documentRepository{db: invoiceRepository.db}
			businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND status = \$4 AND lease_owner = \$5 AND deleted_at IS NULL.*FOR UPDATE`).
				WithArgs(jobID, businessID, test.kind, models.RenderJobStatusProcessing, "owner-a", 1).
				WillReturnError(gorm.ErrRecordNotFound)
			mock.ExpectRollback()

			completed, err := test.complete(repository, context.Background(), businessID, invoiceID, jobID, "owner-a")
			if err == nil || completed {
				t.Fatalf("stale %s completion = %t/%v, want rejected", test.name, completed, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestDocumentRepositoryPreviewLeaseLifecycleRejectsOwnerAAfterOwnerBReclaim(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	ownerA, ownerB, version := "receive-owner-a", "receive-owner-b", 5
	canonicalKey := "invoices/" + businessID + "/" + invoiceID + "/previews/v5/" + jobID + ".pdf"
	selectedKey := canonicalKey + ".attempt-owner-b"
	nowA := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	nowB := nowA.Add(3 * time.Minute)

	expectCanonicalRenderClaim(mock, businessID, jobID, models.RenderKindPreview, version, ownerA, nowA, nowA.Add(2*time.Minute))
	state, err := repository.ClaimPreviewRender(context.Background(), businessID, jobID, version, ownerA, nowA, nowA.Add(2*time.Minute))
	if err != nil || state != interfaces.PreviewRenderClaimed {
		t.Fatalf("owner A claim = %q/%v", state, err)
	}
	expectCanonicalRenderClaim(mock, businessID, jobID, models.RenderKindPreview, version, ownerB, nowB, nowB.Add(2*time.Minute))
	state, err = repository.ClaimPreviewRender(context.Background(), businessID, jobID, version, ownerB, nowB, nowB.Add(2*time.Minute))
	if err != nil || state != interfaces.PreviewRenderClaimed {
		t.Fatalf("owner B reclaim = %q/%v", state, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND status = \$4 AND lease_owner = \$5 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(jobID, businessID, models.RenderKindPreview, models.RenderJobStatusProcessing, ownerA, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectRollback()
	if completed, err := repository.CompletePreviewRender(context.Background(), businessID, jobID, version, ownerA, canonicalKey, canonicalKey+".attempt-owner-a", "preview.pdf"); err == nil || completed {
		t.Fatalf("stale owner A preview completion = %t/%v", completed, err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*id = \$7 AND business_id = \$8 AND kind = \$9 AND status = \$10 AND lease_owner = \$11 AND deleted_at IS NULL`).
		WithArgs(nil, "late A", nil, nil, models.RenderJobStatusFailed, sqlmock.AnyArg(), jobID, businessID, models.RenderKindPreview, models.RenderJobStatusProcessing, ownerA).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT "id","business_id","kind","status" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
		WithArgs(jobID, businessID, models.RenderKindPreview, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "kind", "status"}).AddRow(jobID, businessID, models.RenderKindPreview, models.RenderJobStatusProcessing))
	if err := repository.FailPreviewRender(context.Background(), businessID, jobID, ownerA, "late A"); err == nil {
		t.Fatal("stale owner A preview failure succeeded")
	}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*id = \$7 AND business_id = \$8 AND kind = \$9 AND status = \$10 AND lease_owner = \$11 AND deleted_at IS NULL`).
		WithArgs(nil, "", nil, nil, models.RenderJobStatusObsolete, sqlmock.AnyArg(), jobID, businessID, models.RenderKindPreview, models.RenderJobStatusProcessing, ownerA).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	if err := repository.ObsoletePreviewRender(context.Background(), businessID, jobID, ownerA); err == nil {
		t.Fatal("stale owner A preview obsolete succeeded")
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND status = \$4 AND lease_owner = \$5 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(jobID, businessID, models.RenderKindPreview, models.RenderJobStatusProcessing, ownerB, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "invoice_id", "business_id", "kind", "source_invoice_version", "object_key", "status", "lease_owner"}).
			AddRow(jobID, invoiceID, invoiceID, businessID, models.RenderKindPreview, version, canonicalKey, models.RenderJobStatusProcessing, ownerB))
	mock.ExpectQuery(`SELECT "id","business_id","version" FROM "invoices".*id = \$1 AND business_id = \$2 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(invoiceID, businessID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "version"}).AddRow(invoiceID, businessID, version))
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"object_key"=\$5.*"status"=\$8.*WHERE .*id = \$10 AND business_id = \$11 AND kind = \$12 AND status = \$13 AND lease_owner = \$14 AND deleted_at IS NULL`).
		WithArgs(sqlmock.AnyArg(), "", nil, nil, selectedKey, "preview.pdf", "", models.RenderJobStatusCompleted, sqlmock.AnyArg(), jobID, businessID, models.RenderKindPreview, models.RenderJobStatusProcessing, ownerB).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	completed, err := repository.CompletePreviewRender(context.Background(), businessID, jobID, version, ownerB, canonicalKey, selectedKey, "preview.pdf")
	if err != nil || !completed {
		t.Fatalf("owner B preview completion = %t/%v", completed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryFinalLeaseLifecycleRejectsOwnerAAfterOwnerBReclaim(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	ownerA, ownerB, version := "receive-owner-a", "receive-owner-b", 5
	objectKey := "invoices/" + businessID + "/" + invoiceID + "/v5/final.pdf"
	nowA := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	nowB := nowA.Add(3 * time.Minute)

	expectCanonicalRenderClaim(mock, businessID, jobID, models.RenderKindFinal, version, ownerA, nowA, nowA.Add(2*time.Minute))
	state, err := repository.ClaimFinalRender(context.Background(), businessID, jobID, version, ownerA, nowA, nowA.Add(2*time.Minute))
	if err != nil || state != interfaces.FinalRenderClaimed {
		t.Fatalf("owner A claim = %q/%v", state, err)
	}
	expectCanonicalRenderClaim(mock, businessID, jobID, models.RenderKindFinal, version, ownerB, nowB, nowB.Add(2*time.Minute))
	state, err = repository.ClaimFinalRender(context.Background(), businessID, jobID, version, ownerB, nowB, nowB.Add(2*time.Minute))
	if err != nil || state != interfaces.FinalRenderClaimed {
		t.Fatalf("owner B reclaim = %q/%v", state, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND status = \$4 AND lease_owner = \$5 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(jobID, businessID, models.RenderKindFinal, models.RenderJobStatusProcessing, ownerA, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectRollback()
	if completed, err := repository.CompleteFinalRender(context.Background(), businessID, invoiceID, jobID, version, ownerA, objectKey, "final.pdf"); err == nil || completed {
		t.Fatalf("stale owner A final completion = %t/%v", completed, err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*id = \$7 AND business_id = \$8 AND kind = \$9 AND status = \$10 AND lease_owner = \$11 AND deleted_at IS NULL`).
		WithArgs(nil, "late A", nil, nil, models.RenderJobStatusFailed, sqlmock.AnyArg(), jobID, businessID, models.RenderKindFinal, models.RenderJobStatusProcessing, ownerA).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT "id","business_id","kind","status" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
		WithArgs(jobID, businessID, models.RenderKindFinal, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "kind", "status"}).AddRow(jobID, businessID, models.RenderKindFinal, models.RenderJobStatusProcessing))
	if err := repository.FailFinalRender(context.Background(), businessID, jobID, ownerA, "late A"); err == nil {
		t.Fatal("stale owner A final failure succeeded")
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND status = \$4 AND lease_owner = \$5 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(jobID, businessID, models.RenderKindFinal, models.RenderJobStatusProcessing, ownerB, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "invoice_id", "business_id", "kind", "source_invoice_version", "object_key", "status", "lease_owner"}).
			AddRow(jobID, invoiceID, invoiceID, businessID, models.RenderKindFinal, version, objectKey, models.RenderJobStatusProcessing, ownerB))
	mock.ExpectQuery(`SELECT "version" FROM "invoices".*id = \$1 AND business_id = \$2 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(invoiceID, businessID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(version))
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"status"=\$7.*WHERE .*id = \$9 AND business_id = \$10 AND kind = \$11 AND status = \$12 AND lease_owner = \$13 AND deleted_at IS NULL`).
		WithArgs(sqlmock.AnyArg(), "", nil, nil, "final.pdf", "", models.RenderJobStatusCompleted, sqlmock.AnyArg(), jobID, businessID, models.RenderKindFinal, models.RenderJobStatusProcessing, ownerB).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE email_deliveries.*RETURNING id, business_id, invoice_id, render_job_id, recipient`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "invoice_id", "render_job_id", "recipient",
		}))
	mock.ExpectCommit()
	completed, err := repository.CompleteFinalRender(context.Background(), businessID, invoiceID, jobID, version, ownerB, objectKey, "final.pdf")
	if err != nil || !completed {
		t.Fatalf("owner B final completion = %t/%v", completed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryVerifiesExactOwnerBeforeCanonicalPublish(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, jobID, owner := uuid.NewString(), uuid.NewString(), "receive-owner-b"
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT "id" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND status = \$4 AND lease_owner = \$5 AND lease_expires_at > \$6 AND deleted_at IS NULL.*LIMIT \$7`).
		WithArgs(jobID, businessID, models.RenderKindFinal, models.RenderJobStatusProcessing, owner, now, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(jobID))

	if err := repository.VerifyRenderLease(context.Background(), businessID, jobID, models.RenderKindFinal, owner, now); err != nil {
		t.Fatalf("verify exact final lease: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryUpdateRejectsStaleDraftBeforeReplacingLines(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newAtomicSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	document := &models.Document{
		ID:         uuid.NewString(),
		BusinessID: uuid.NewString(),
		Status:     models.DocumentStatusIssued,
		DraftState: models.DocumentDraftStateFinal,
		Lines: []*models.DocumentLine{{
			ID: uuid.NewString(), Description: "stale draft line",
		}},
	}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "documents".*WHERE \(id = \$[0-9]+ AND business_id = \$[0-9]+ AND status = \$[0-9]+ AND draft_state = \$[0-9]+ AND deleted_at IS NULL\).*"documents"."deleted_at" IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repository.UpdateDraft(context.Background(), document)

	var conflict *models.DocumentDraftConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale draft update error = %T %v, want typed conflict", err, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryClaimPreviewRenderReclaimsFailedRedeliveryAndIncrementsAttempts(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	jobID := uuid.NewString()
	version := 5

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"attempts"=attempts \+ 1.*"status".*WHERE .*id = \$[0-9]+ AND business_id = \$[0-9]+ AND kind = \$[0-9]+ AND source_invoice_version = \$[0-9]+`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	state, err := repository.ClaimPreviewRender(context.Background(), businessID, jobID, version, "test-owner", time.Now().UTC(), time.Now().UTC().Add(time.Minute))

	if err != nil || state != interfaces.PreviewRenderClaimed {
		t.Fatalf("claim state/error = %q/%v, want claimed", state, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryClaimPreviewRenderDistinguishesExistingStates(t *testing.T) {
	tests := []struct {
		name      string
		jobStatus string
		wantState interfaces.PreviewRenderClaimState
	}{
		{
			name:      "processing",
			jobStatus: models.RenderJobStatusProcessing,
			wantState: interfaces.PreviewRenderAlreadyProcessing,
		},
		{
			name:      "completed",
			jobStatus: models.RenderJobStatusCompleted,
			wantState: interfaces.PreviewRenderAlreadyCompleted,
		},
		{
			name:      "obsolete",
			jobStatus: models.RenderJobStatusObsolete,
			wantState: interfaces.PreviewRenderAlreadyObsolete,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := &documentRepository{db: invoiceRepository.db}
			businessID := uuid.NewString()
			jobID := uuid.NewString()
			version := 5

			mock.ExpectBegin()
			mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*source_invoice_version = \$[0-9]+`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			mock.ExpectQuery(`SELECT "id","business_id","kind","source_invoice_version","status" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
				WithArgs(jobID, businessID, models.RenderKindPreview, 1).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "business_id", "kind", "source_invoice_version", "status",
				}).AddRow(jobID, businessID, models.RenderKindPreview, version, test.jobStatus))

			state, err := repository.ClaimPreviewRender(context.Background(), businessID, jobID, version, "test-owner", time.Now().UTC(), time.Now().UTC().Add(time.Minute))

			if err != nil || state != test.wantState {
				t.Fatalf("claim state/error = %q/%v, want %q", state, err, test.wantState)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestDocumentRepositoryClaimFinalRenderReclaimsFailedAndIncrementsAttempts(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	jobID := uuid.NewString()
	version := 5

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"attempts"=attempts \+ 1.*"status".*WHERE .*id = \$[0-9]+ AND business_id = \$[0-9]+ AND kind = \$[0-9]+ AND source_invoice_version = \$[0-9]+`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	state, err := repository.ClaimFinalRender(context.Background(), businessID, jobID, version, "test-owner", time.Now().UTC(), time.Now().UTC().Add(time.Minute))

	if err != nil || state != interfaces.FinalRenderClaimed {
		t.Fatalf("final claim state/error = %q/%v, want claimed", state, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryClaimFinalRenderDistinguishesProcessingAndCompleted(t *testing.T) {
	tests := []struct {
		name      string
		jobStatus string
		wantState interfaces.FinalRenderClaimState
	}{
		{
			name:      "processing",
			jobStatus: models.RenderJobStatusProcessing,
			wantState: interfaces.FinalRenderAlreadyProcessing,
		},
		{
			name:      "completed",
			jobStatus: models.RenderJobStatusCompleted,
			wantState: interfaces.FinalRenderAlreadyCompleted,
		},
		{
			name:      "obsolete",
			jobStatus: models.RenderJobStatusObsolete,
			wantState: interfaces.FinalRenderClaimState("obsolete"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := &documentRepository{db: invoiceRepository.db}
			businessID := uuid.NewString()
			jobID := uuid.NewString()
			version := 5

			mock.ExpectBegin()
			mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*source_invoice_version = \$[0-9]+`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			mock.ExpectQuery(`SELECT "id","business_id","kind","source_invoice_version","status" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
				WithArgs(jobID, businessID, models.RenderKindFinal, 1).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "business_id", "kind", "source_invoice_version", "status",
				}).AddRow(jobID, businessID, models.RenderKindFinal, version, test.jobStatus))

			state, err := repository.ClaimFinalRender(context.Background(), businessID, jobID, version, "test-owner", time.Now().UTC(), time.Now().UTC().Add(time.Minute))

			if err != nil || state != test.wantState {
				t.Fatalf("final claim state/error = %q/%v, want %q", state, err, test.wantState)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestDocumentRepositoryLoadsExactFinalRenderSnapshot(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	jobID := uuid.NewString()
	version := 5
	snapshot := `{"id":"` + invoiceID + `","business_id":"` + businessID + `","status":"issued","lines":[{"description":"Frozen line"}]}`

	mock.ExpectQuery(`SELECT \* FROM "document_revisions".*business_id = \$1 AND document_id = \$2 AND action = \$3.*metadata ->> 'render_job_id' = \$4.*metadata ->> 'source_invoice_version'.*\$5.*deleted_at IS NULL.*ORDER BY created_at DESC.*LIMIT \$6`).
		WithArgs(businessID, invoiceID, "final_render_snapshot", jobID, version, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "document_id", "business_id", "action", "snapshot", "metadata",
		}).AddRow(
			uuid.NewString(),
			invoiceID,
			businessID,
			"final_render_snapshot",
			snapshot,
			`{"render_job_id":"`+jobID+`","source_invoice_version":5}`,
		))

	document, err := repository.LoadFinalRenderSnapshot(
		context.Background(),
		businessID,
		invoiceID,
		jobID,
		version,
	)

	if err != nil || document == nil || document.ID != invoiceID ||
		len(document.Lines) != 1 || document.Lines[0].Description != "Frozen line" {
		t.Fatalf("snapshot/error = %#v/%v", document, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryFailFinalRenderDoesNotRegressCompletedJob(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	jobID := uuid.NewString()

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*kind = \$[0-9]+ AND status = \$[0-9]+`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT "id","business_id","kind","status" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
		WithArgs(jobID, businessID, models.RenderKindFinal, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "kind", "status",
		}).AddRow(jobID, businessID, models.RenderKindFinal, models.RenderJobStatusCompleted))

	if err := repository.FailFinalRender(context.Background(), businessID, jobID, "test-owner", "late failure"); err != nil {
		t.Fatalf("terminal final failure no-op: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryCompleteFinalRenderRechecksInvoiceVersionAtomically(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	jobID := uuid.NewString()
	version := 5
	objectKey := "invoices/" + businessID + "/" + invoiceID + "/v5/final.pdf"

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND status = \$4 AND lease_owner = \$5 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(jobID, businessID, models.RenderKindFinal, models.RenderJobStatusProcessing, "test-owner", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "document_id", "invoice_id", "business_id", "kind",
			"source_invoice_version", "object_key", "status",
		}).AddRow(
			jobID,
			invoiceID,
			invoiceID,
			businessID,
			models.RenderKindFinal,
			version,
			objectKey,
			models.RenderJobStatusProcessing,
		))
	mock.ExpectQuery(`SELECT "version" FROM "invoices".*id = \$1 AND business_id = \$2 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(invoiceID, businessID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(version))
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"output_filename".*"output_url".*"status".*WHERE .*id = \$[0-9]+ AND business_id = \$[0-9]+ AND kind = \$[0-9]+ AND status = \$[0-9]+`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE email_deliveries.*RETURNING id, business_id, invoice_id, render_job_id, recipient`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "invoice_id", "render_job_id", "recipient",
		}))
	mock.ExpectCommit()

	completed, err := repository.CompleteFinalRender(
		context.Background(),
		businessID,
		invoiceID,
		jobID,
		version,
		"test-owner",
		objectKey,
		"invoice-final.pdf",
	)

	if err != nil || !completed {
		t.Fatalf("complete final result/error = %t/%v, want completed", completed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryCompleteFinalRenderWakesOnlyWaitingDeliveriesWithOutbox(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID, invoiceID, jobID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	deliveryID := uuid.NewString()
	version := 5
	objectKey := "invoices/" + businessID + "/" + invoiceID + "/v5/final.pdf"

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND status = \$4 AND lease_owner = \$5 AND deleted_at IS NULL.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "document_id", "invoice_id", "business_id", "kind",
			"source_invoice_version", "object_key", "status",
		}).AddRow(
			jobID, invoiceID, invoiceID, businessID, models.RenderKindFinal,
			version, objectKey, models.RenderJobStatusProcessing,
		))
	mock.ExpectQuery(`SELECT "version" FROM "invoices".*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(version))
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"status".*WHERE .*lease_owner`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE email_deliveries.*SET status = \$1.*WHERE business_id = \$[0-9]+.*invoice_id = \$[0-9]+.*render_job_id = \$[0-9]+.*status = \$[0-9]+.*deleted_at IS NULL.*RETURNING id, business_id, invoice_id, render_job_id, recipient`).
		WithArgs(
			models.EmailDeliveryStatusQueued,
			sqlmock.AnyArg(),
			businessID,
			invoiceID,
			jobID,
			models.EmailDeliveryStatusWaitingForRender,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "invoice_id", "render_job_id", "recipient",
		}).AddRow(deliveryID, businessID, invoiceID, jobID, "buyer@example.com"))
	mock.ExpectQuery(`INSERT INTO "outbox_events".*"aggregate_type".*"aggregate_id".*"event_type".*"payload".*RETURNING "id"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectCommit()

	completed, err := repository.CompleteFinalRender(
		context.Background(), businessID, invoiceID, jobID, version,
		"test-owner", objectKey, "invoice-final.pdf",
	)
	if err != nil || !completed {
		t.Fatalf("complete/wake result = %t/%v", completed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryCompleteFinalRenderRejectsVersionDrift(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	jobID := uuid.NewString()
	version := 5
	objectKey := "invoices/" + businessID + "/" + invoiceID + "/v5/final.pdf"

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "document_render_jobs".*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "document_id", "invoice_id", "business_id", "kind",
			"source_invoice_version", "object_key", "status",
		}).AddRow(
			jobID,
			invoiceID,
			invoiceID,
			businessID,
			models.RenderKindFinal,
			version,
			objectKey,
			models.RenderJobStatusProcessing,
		))
	mock.ExpectQuery(`SELECT "version" FROM "invoices".*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(version + 1))
	mock.ExpectRollback()

	completed, err := repository.CompleteFinalRender(
		context.Background(),
		businessID,
		invoiceID,
		jobID,
		version,
		"test-owner",
		objectKey,
		"invoice-final.pdf",
	)

	if err == nil || completed {
		t.Fatalf("version drift complete result/error = %t/%v, want fail closed", completed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryFailPreviewRenderDoesNotRegressCompletedJob(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	jobID := uuid.NewString()

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*kind = \$[0-9]+ AND status = \$[0-9]+`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT "id","business_id","kind","status" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
		WithArgs(jobID, businessID, models.RenderKindPreview, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "kind", "status",
		}).AddRow(jobID, businessID, models.RenderKindPreview, models.RenderJobStatusCompleted))

	if err := repository.FailPreviewRender(context.Background(), businessID, jobID, "test-owner", "late failure"); err != nil {
		t.Fatalf("terminal preview failure no-op: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
