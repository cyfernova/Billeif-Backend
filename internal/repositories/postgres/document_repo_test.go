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
				return r.CompletePreviewRender(ctx, businessID, jobID, 5, owner, "invoices/preview.pdf", "preview.pdf")
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
