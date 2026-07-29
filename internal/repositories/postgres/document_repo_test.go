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
