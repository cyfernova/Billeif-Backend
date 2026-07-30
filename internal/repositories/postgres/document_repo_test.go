package postgres

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

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

func TestDocumentRepositoryClaimPreviewRenderUsesCASAndIncrementsAttempts(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	jobID := uuid.NewString()
	version := 5

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*"attempts"=attempts \+ 1.*"status".*WHERE .*id = \$[0-9]+ AND business_id = \$[0-9]+ AND kind = \$[0-9]+ AND source_invoice_version = \$[0-9]+ AND status IN \(\$[0-9]+,\$[0-9]+\) AND deleted_at IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	claimed, err := repository.ClaimPreviewRender(context.Background(), businessID, jobID, version)

	if err != nil || !claimed {
		t.Fatalf("claim result/error = %t/%v, want claimed", claimed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDocumentRepositoryClaimPreviewRenderDoesNotRegressTerminalJob(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &documentRepository{db: invoiceRepository.db}
	businessID := uuid.NewString()
	jobID := uuid.NewString()
	version := 5

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "document_render_jobs" SET .*WHERE .*status IN \(\$[0-9]+,\$[0-9]+\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT "id","business_id","kind","source_invoice_version","status" FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`).
		WithArgs(jobID, businessID, models.RenderKindPreview, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "kind", "source_invoice_version", "status",
		}).AddRow(jobID, businessID, models.RenderKindPreview, version, models.RenderJobStatusCompleted))

	claimed, err := repository.ClaimPreviewRender(context.Background(), businessID, jobID, version)

	if err != nil || claimed {
		t.Fatalf("claim result/error = %t/%v, want terminal no-op", claimed, err)
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

	if err := repository.FailPreviewRender(context.Background(), businessID, jobID, "late failure"); err != nil {
		t.Fatalf("terminal preview failure no-op: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
