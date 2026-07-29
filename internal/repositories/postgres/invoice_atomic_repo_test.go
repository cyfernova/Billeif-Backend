package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestInvoiceRepositoryCreateDraftAtomicRollsBackEveryPersistenceStage(t *testing.T) {
	queryStages := []string{
		"idempotency claim",
		"invoice",
		"invoice items",
		"document projection",
		"document projection items",
		"activity",
		"outbox events",
		"render jobs",
		"email deliveries",
	}

	for failedIndex, failedStage := range queryStages {
		t.Run(failedStage, func(t *testing.T) {
			repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
			defer closeDatabase()
			mock.ExpectBegin()
			for successfulIndex := 0; successfulIndex < failedIndex; successfulIndex++ {
				expectSuccessfulInsert(mock)
			}
			mock.ExpectQuery("").WillReturnError(errors.New("injected stage failure"))
			mock.ExpectRollback()

			result, err := repository.CreateDraftAtomic(context.Background(), atomicRepositoryTestCommand())

			if result != nil {
				t.Fatalf("result = %#v, want nil", result)
			}
			if err == nil || !strings.Contains(err.Error(), "atomic invoice draft persistence failed") {
				t.Fatalf("error = %v, want sanitized atomic persistence error", err)
			}
			if strings.Contains(err.Error(), "injected stage failure") {
				t.Fatalf("raw database error leaked: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryCreateDraftAtomicDoesNotReportCompletionOrCommitFailureAsSuccess(t *testing.T) {
	fixtures := []struct {
		name       string
		arrangeEnd func(sqlmock.Sqlmock)
	}{
		{
			name: "idempotency completion",
			arrangeEnd: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("").WillReturnError(errors.New("completion failure"))
				mock.ExpectRollback()
			},
		},
		{
			name: "commit",
			arrangeEnd: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit().WillReturnError(errors.New("commit failure"))
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
			defer closeDatabase()
			mock.ExpectBegin()
			for insert := 0; insert < 9; insert++ {
				expectSuccessfulInsert(mock)
			}
			fixture.arrangeEnd(mock)

			result, err := repository.CreateDraftAtomic(context.Background(), atomicRepositoryTestCommand())

			if result != nil || err == nil {
				t.Fatalf("result/error = %#v/%v, want nil/error", result, err)
			}
			if strings.Contains(err.Error(), "completion failure") || strings.Contains(err.Error(), "commit failure") {
				t.Fatalf("raw database error leaked: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryCreateDraftAtomicPersistsLegalPartySnapshots(t *testing.T) {
	sqlDatabase, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer func() {
		mock.ExpectClose()
		if err := sqlDatabase.Close(); err != nil {
			t.Errorf("close sqlmock: %v", err)
		}
	}()
	gormDatabase, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDatabase}), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm postgres adapter: %v", err)
	}
	repository := &invoiceRepository{db: gormDatabase}
	command := atomicRepositoryTestCommand()
	command.Invoice.SellerSnapshot = models.PartySnapshot{Name: "Seller"}
	command.Invoice.BuyerSnapshot = models.PartySnapshot{Name: "Buyer"}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "api_idempotency_keys"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectQuery(`INSERT INTO "invoices" \([^)]*"seller_snapshot"[^)]*"buyer_snapshot"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "seller_snapshot", "buyer_snapshot"}).
			AddRow(command.Invoice.ID, `{"name":"Seller"}`, `{"name":"Buyer"}`))
	mock.ExpectQuery(`INSERT INTO "invoice_items"`).WillReturnError(errors.New("stop after invoice assertion"))
	mock.ExpectRollback()

	_, _ = repository.CreateDraftAtomic(context.Background(), command)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("legal snapshot SQL expectations: %v", err)
	}
}

func newAtomicSQLMockRepository(t *testing.T) (*invoiceRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDatabase, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(
		sqlmock.QueryMatcherFunc(func(_, _ string) error { return nil }),
	))
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	gormDatabase, err := gorm.Open(gormpostgres.New(gormpostgres.Config{
		Conn: sqlDatabase,
	}), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		_ = sqlDatabase.Close()
		t.Fatalf("open gorm postgres adapter: %v", err)
	}
	return &invoiceRepository{db: gormDatabase}, mock, func() {
		mock.ExpectClose()
		if err := sqlDatabase.Close(); err != nil {
			t.Errorf("close sqlmock: %v", err)
		}
	}
}

func expectSuccessfulInsert(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
}

func atomicRepositoryTestCommand() interfaces.AtomicInvoiceDraft {
	businessID := uuid.NewString()
	customerID := uuid.NewString()
	invoiceID := uuid.NewString()
	itemID := uuid.NewString()
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	invoice := &models.Invoice{
		ID:          invoiceID,
		BusinessID:  businessID,
		CustomerID:  &customerID,
		Version:     1,
		Status:      models.InvoiceStatusDraft,
		Origin:      models.InvoiceOriginManual,
		InvoiceDate: now,
		DueDate:     now.AddDate(0, 0, 30),
		Currency:    "INR",
		Subtotal:    100,
		Total:       100,
		BalanceDue:  100,
		Items: []*models.InvoiceItem{{
			ID:          itemID,
			InvoiceID:   invoiceID,
			Description: "Atomic item",
			Quantity:    1,
			UnitPrice:   100,
			Total:       100,
		}},
	}
	document := &models.Document{
		ID:           invoiceID,
		BusinessID:   businessID,
		DocumentType: models.DocumentTypeSalesInvoice,
		PartyType:    models.DocumentPartyTypeCustomer,
		PartyID:      &customerID,
		Status:       models.DocumentStatusDraft,
		DraftState:   models.DocumentDraftStateDraft,
		SerialNumber: "",
		IssueDate:    now,
		Currency:     "INR",
		Total:        100,
		Lines: []*models.DocumentLine{{
			ID:          itemID,
			DocumentID:  invoiceID,
			Description: "Atomic item",
			Quantity:    1,
			UnitPrice:   100,
			LineTotal:   100,
		}},
	}
	return interfaces.AtomicInvoiceDraft{
		BusinessID:     businessID,
		Command:        "invoice.create",
		IdempotencyKey: uuid.NewString(),
		RequestHash:    strings.Repeat("a", 64),
		Invoice:        invoice,
		Document:       document,
		Activity: &models.ActivityLog{
			ID:         uuid.NewString(),
			BusinessID: businessID,
			ActorID:    uuid.NewString(),
			EntityType: "invoice",
			EntityID:   invoiceID,
			Action:     "created",
		},
		OutboxEvents: []*models.OutboxEvent{{
			ID:            uuid.NewString(),
			BusinessID:    businessID,
			AggregateType: "invoice",
			AggregateID:   invoiceID,
			EventType:     "invoice.requested",
			Payload:       "{}",
			AvailableAt:   now,
		}},
		RenderJobs: []*models.DocumentRenderJob{{
			ID:         uuid.NewString(),
			BusinessID: businessID,
			DocumentID: &invoiceID,
			Status:     models.RenderJobStatusQueued,
		}},
		EmailDeliveries: []*models.EmailDelivery{{
			ID:         uuid.NewString(),
			BusinessID: businessID,
			InvoiceID:  &invoiceID,
			Status:     "queued",
			Recipient:  "buyer@example.test",
			Subject:    "Invoice",
		}},
	}
}
