package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func TestInvoiceRepositoryIssueDraftAtomicPersistsOneLegalResult(t *testing.T) {
	repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
	defer closeDatabase()
	command := issueRepositoryTestCommand()
	customerID := uuid.NewString()
	invoiceDate := time.Date(2026, time.April, 1, 0, 30, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectSuccessfulInsert(mock)
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
		"id", "business_id", "customer_id", "version", "status", "origin", "invoice_date",
		"seller_snapshot", "buyer_snapshot", "currency", "total",
	}).AddRow(
		command.InvoiceID, command.BusinessID, customerID, command.ExpectedVersion, models.InvoiceStatusDraft,
		models.InvoiceOriginManual, invoiceDate, `{"name":"Seller"}`, `{"name":"Buyer"}`, "INR", 100,
	))
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow("Asia/Kolkata"))
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
		"id", "business_id", "status", "bill_of_supply", "source_linkage", "locale",
	}).AddRow(command.InvoiceID, command.BusinessID, models.DocumentStatusDraft, false, `{}`, "en-IN"))
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"last_number"}).AddRow(1))
	mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 1))
	expectSuccessfulInsert(mock)
	expectSuccessfulInsert(mock)
	expectSuccessfulInsert(mock)
	mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.IssueDraftAtomic(context.Background(), command)

	if err != nil {
		t.Fatalf("issue draft: %v", err)
	}
	if result == nil || result.Invoice == nil || result.FinalRender == nil {
		t.Fatalf("result = %#v, want invoice and render", result)
	}
	if result.Invoice.Version != command.ExpectedVersion+1 || result.Invoice.Status != models.InvoiceStatusIssued ||
		result.Invoice.InvoiceNo == nil || *result.Invoice.InvoiceNo != "INV/26-27/000001" ||
		result.Invoice.IssuedAt == nil {
		t.Fatalf("issued invoice = %#v", result.Invoice)
	}
	if result.FinalRender.Kind != models.RenderKindFinal ||
		result.FinalRender.SourceInvoiceVersion == nil ||
		*result.FinalRender.SourceInvoiceVersion != command.ExpectedVersion+1 ||
		result.FinalRender.OutputURL != "" ||
		!strings.Contains(result.FinalRender.ObjectKey, command.BusinessID) ||
		!strings.Contains(result.FinalRender.ObjectKey, command.InvoiceID) {
		t.Fatalf("final render = %#v", result.FinalRender)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryIssueDraftAtomicReturnsTypedLifecycleErrors(t *testing.T) {
	fixtures := []struct {
		name    string
		version int
		status  string
		seller  string
		buyer   string
		assert  func(error) bool
	}{
		{
			name: "stale", version: 2, status: models.InvoiceStatusDraft, seller: `{"name":"Seller"}`, buyer: `{"name":"Buyer"}`,
			assert: func(err error) bool { var target *invoiceissue.StaleVersionError; return errors.As(err, &target) },
		},
		{
			name: "already issued", version: 1, status: models.InvoiceStatusIssued, seller: `{"name":"Seller"}`, buyer: `{"name":"Buyer"}`,
			assert: func(err error) bool { var target *invoiceissue.AlreadyIssuedError; return errors.As(err, &target) },
		},
		{
			name: "missing seller snapshot", version: 1, status: models.InvoiceStatusDraft, seller: `{}`, buyer: `{"name":"Buyer"}`,
			assert: func(err error) bool { var target *invoiceissue.InvalidLifecycleError; return errors.As(err, &target) },
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
			defer closeDatabase()
			command := issueRepositoryTestCommand()
			mock.ExpectBegin()
			expectSuccessfulInsert(mock)
			mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
				"id", "business_id", "customer_id", "version", "status", "origin", "invoice_date",
				"seller_snapshot", "buyer_snapshot",
			}).AddRow(
				command.InvoiceID, command.BusinessID, uuid.NewString(), fixture.version, fixture.status,
				models.InvoiceOriginManual, time.Now(), fixture.seller, fixture.buyer,
			))
			mock.ExpectRollback()

			result, err := repository.IssueDraftAtomic(context.Background(), command)

			if result != nil || err == nil || !fixture.assert(err) {
				t.Fatalf("result/error = %#v/%T %v, want typed lifecycle error", result, err, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryIssueDraftAtomicValidatesBeforeClaim(t *testing.T) {
	repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
	defer closeDatabase()
	command := issueRepositoryTestCommand()
	command.ActorID = ""

	result, err := repository.IssueDraftAtomic(context.Background(), command)

	if result != nil || err == nil {
		t.Fatalf("result/error = %#v/%v, want validation error", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL before validation: %v", err)
	}
}

func TestInvoiceRepositoryIssueDraftAtomicReplaysCompletedResultAndRejectsChangedPayload(t *testing.T) {
	t.Run("replay", func(t *testing.T) {
		repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
		defer closeDatabase()
		command := issueRepositoryTestCommand()
		resultType := "invoice_issue"
		issuedVersion := command.ExpectedVersion + 1

		mock.ExpectBegin()
		mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
			"business_id", "command", "idempotency_key", "request_hash", "status", "result_type", "result_id",
		}).AddRow(
			command.BusinessID, command.Command, command.IdempotencyKey, command.RequestHash,
			models.IdempotencyStatusCompleted, resultType, command.InvoiceID,
		))
		mock.ExpectCommit()
		mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "version", "status",
		}).AddRow(command.InvoiceID, command.BusinessID, issuedVersion, models.InvoiceStatusIssued))
		mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"id", "invoice_id"}))
		mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "invoice_id", "kind", "source_invoice_version",
		}).AddRow(uuid.NewString(), command.BusinessID, command.InvoiceID, models.RenderKindFinal, issuedVersion))

		result, err := repository.IssueDraftAtomic(context.Background(), command)

		if err != nil || result == nil || !result.Replayed ||
			result.Invoice == nil || result.Invoice.ID != command.InvoiceID ||
			result.FinalRender == nil || result.FinalRender.SourceInvoiceVersion == nil ||
			*result.FinalRender.SourceInvoiceVersion != issuedVersion {
			t.Fatalf("replay result/error = %#v/%v", result, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
		defer closeDatabase()
		command := issueRepositoryTestCommand()
		mock.ExpectBegin()
		mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
			"business_id", "command", "idempotency_key", "request_hash", "status",
		}).AddRow(
			command.BusinessID, command.Command, command.IdempotencyKey, strings.Repeat("b", 64),
			models.IdempotencyStatusCompleted,
		))
		mock.ExpectRollback()

		result, err := repository.IssueDraftAtomic(context.Background(), command)
		var conflict *idempotency.ConflictError
		if result != nil || err == nil || !errors.As(err, &conflict) {
			t.Fatalf("conflict result/error = %#v/%T %v", result, err, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})
}

func TestInvoiceRepositoryIssueDraftAtomicRollsBackEveryIssuanceStage(t *testing.T) {
	stages := []struct {
		name      string
		queryStep bool
	}{
		{name: "sequence allocation", queryStep: true},
		{name: "invoice update"},
		{name: "document projection"},
		{name: "final render", queryStep: true},
		{name: "outbox event", queryStep: true},
		{name: "activity", queryStep: true},
		{name: "idempotency completion"},
	}

	for failedIndex, failedStage := range stages {
		t.Run(failedStage.name, func(t *testing.T) {
			repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
			defer closeDatabase()
			command := issueRepositoryTestCommand()
			expectIssueTransactionPrefix(mock, command)
			for index := 0; index < failedIndex; index++ {
				expectSuccessfulIssueStage(mock, index)
			}
			if failedStage.queryStep {
				mock.ExpectQuery("").WillReturnError(errors.New("injected issue failure"))
			} else {
				mock.ExpectExec("").WillReturnError(errors.New("injected issue failure"))
			}
			mock.ExpectRollback()

			result, err := repository.IssueDraftAtomic(context.Background(), command)

			if result != nil || err == nil ||
				!strings.Contains(err.Error(), "atomic invoice issue persistence failed") {
				t.Fatalf("result/error = %#v/%v, want sanitized issue failure", result, err)
			}
			if strings.Contains(err.Error(), "injected issue failure") {
				t.Fatalf("raw database error leaked: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryIssueDraftAtomicDoesNotReturnCommitFailureAsSuccess(t *testing.T) {
	repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
	defer closeDatabase()
	command := issueRepositoryTestCommand()
	expectIssueTransactionPrefix(mock, command)
	for index := 0; index < 7; index++ {
		expectSuccessfulIssueStage(mock, index)
	}
	mock.ExpectCommit().WillReturnError(errors.New("injected commit failure"))

	result, err := repository.IssueDraftAtomic(context.Background(), command)

	if result != nil || err == nil || !strings.Contains(err.Error(), "transaction commit") {
		t.Fatalf("result/error = %#v/%v, want commit failure", result, err)
	}
	if strings.Contains(err.Error(), "injected commit failure") {
		t.Fatalf("raw database error leaked: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryIssueDraftAtomicReturnsTypedSequenceExhaustion(t *testing.T) {
	repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
	defer closeDatabase()
	command := issueRepositoryTestCommand()
	expectIssueTransactionPrefix(mock, command)
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"last_number"}))
	mock.ExpectRollback()

	result, err := repository.IssueDraftAtomic(context.Background(), command)
	var exhausted *invoiceissue.SequenceExhaustedError
	if result != nil || !errors.As(err, &exhausted) {
		t.Fatalf("result/error = %#v/%T %v, want typed exhaustion", result, err, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func expectIssueTransactionPrefix(mock sqlmock.Sqlmock, command interfaces.AtomicInvoiceIssue) {
	mock.ExpectBegin()
	expectSuccessfulInsert(mock)
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
		"id", "business_id", "customer_id", "version", "status", "origin", "invoice_date",
		"seller_snapshot", "buyer_snapshot", "currency", "total",
	}).AddRow(
		command.InvoiceID, command.BusinessID, uuid.NewString(), command.ExpectedVersion, models.InvoiceStatusDraft,
		models.InvoiceOriginManual, time.Date(2026, time.April, 1, 0, 30, 0, 0, time.UTC),
		`{"name":"Seller"}`, `{"name":"Buyer"}`, "INR", 100,
	))
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow("Asia/Kolkata"))
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{
		"id", "business_id", "status", "bill_of_supply", "source_linkage", "locale",
	}).AddRow(command.InvoiceID, command.BusinessID, models.DocumentStatusDraft, false, `{}`, "en-IN"))
}

func expectSuccessfulIssueStage(mock sqlmock.Sqlmock, index int) {
	switch index {
	case 0:
		mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"last_number"}).AddRow(1))
	case 1, 2, 6:
		mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 1))
	case 3, 4, 5:
		expectSuccessfulInsert(mock)
	}
}

func issueRepositoryTestCommand() interfaces.AtomicInvoiceIssue {
	return interfaces.AtomicInvoiceIssue{
		BusinessID:      uuid.NewString(),
		InvoiceID:       uuid.NewString(),
		Command:         "invoice.issue",
		IdempotencyKey:  uuid.NewString(),
		RequestHash:     strings.Repeat("a", 64),
		ExpectedVersion: 1,
		DocumentType:    invoiceissue.DocumentTypeTaxInvoice,
		Series:          "INV",
		ActorID:         uuid.NewString(),
		ActorRole:       "accountant",
		RequestID:       "request-issue",
		IPAddress:       "127.0.0.1",
	}
}
