package postgres

import (
	"context"
	"regexp"
	"testing"
	"time"

	"invoice-backend/internal/invoicecursor"
	"invoice-backend/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInvoiceRepositoryListByCursorUsesTenantScopedDescendingKeysetQuery(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()

	businessID := uuid.NewString()
	createdAt := time.Date(2026, time.July, 30, 8, 0, 0, 123, time.UTC)
	cursorID := uuid.NewString()
	query := regexp.QuoteMeta(
		`SELECT * FROM "invoices" WHERE (business_id = $1 AND deleted_at IS NULL) AND ((created_at < $2) OR (created_at = $3 AND id < $4)) AND "invoices"."deleted_at" IS NULL ORDER BY created_at DESC, id DESC LIMIT $5`,
	)
	rows := sqlmock.NewRows([]string{"id", "business_id", "created_at"}).
		AddRow(uuid.NewString(), businessID, createdAt.Add(-time.Second)).
		AddRow(uuid.NewString(), businessID, createdAt.Add(-2*time.Second)).
		AddRow(uuid.NewString(), businessID, createdAt.Add(-3*time.Second))
	mock.ExpectQuery(query).
		WithArgs(businessID, createdAt, createdAt, cursorID, 3).
		WillReturnRows(rows)

	invoices, hasMore, err := repository.ListByCursor(
		context.Background(),
		businessID,
		&invoicecursor.Position{CreatedAt: createdAt, ID: cursorID},
		2,
	)
	if err != nil {
		t.Fatalf("ListByCursor() error = %v", err)
	}
	if len(invoices) != 2 || !hasMore {
		t.Fatalf("ListByCursor() returned len=%d hasMore=%v, want 2/true", len(invoices), hasMore)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryListByCursorFinalPageOmitsBoundaryAndExtraRow(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()

	businessID := uuid.NewString()
	createdAt := time.Date(2026, time.July, 30, 8, 0, 0, 123, time.UTC)
	query := regexp.QuoteMeta(
		`SELECT * FROM "invoices" WHERE (business_id = $1 AND deleted_at IS NULL) AND "invoices"."deleted_at" IS NULL ORDER BY created_at DESC, id DESC LIMIT $2`,
	)
	mock.ExpectQuery(query).
		WithArgs(businessID, 21).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "created_at"}).
			AddRow(uuid.NewString(), businessID, createdAt))

	invoices, hasMore, err := repository.ListByCursor(context.Background(), businessID, nil, 20)
	if err != nil {
		t.Fatalf("ListByCursor() error = %v", err)
	}
	if len(invoices) != 1 || hasMore {
		t.Fatalf("ListByCursor() returned len=%d hasMore=%v, want 1/false", len(invoices), hasMore)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryListByCursorDoesNotSkipOrDuplicateIdenticalTimestamps(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.Exec(`
		CREATE TABLE invoices (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			invoice_date DATETIME NOT NULL,
			status TEXT NOT NULL,
			currency TEXT NOT NULL,
			total REAL NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create invoice table: %v", err)
	}
	businessID := uuid.NewString()
	createdAt := time.Date(2026, time.July, 30, 8, 0, 0, 123, time.UTC)
	wantIDs := []string{
		"00000000-0000-0000-0000-000000000004",
		"00000000-0000-0000-0000-000000000003",
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000001",
	}
	for _, id := range wantIDs {
		if err := database.Exec(
			`INSERT INTO invoices (id, business_id, invoice_date, status, currency, total, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, businessID, createdAt, models.InvoiceStatusDraft, "INR", 1, createdAt,
		).Error; err != nil {
			t.Fatalf("insert invoice %s: %v", id, err)
		}
	}
	repository := &invoiceRepository{db: database}
	var (
		cursor  *invoicecursor.Position
		seenIDs []string
	)
	for {
		page, hasMore, err := repository.ListByCursor(context.Background(), businessID, cursor, 2)
		if err != nil {
			t.Fatalf("ListByCursor() error = %v", err)
		}
		for _, invoice := range page {
			seenIDs = append(seenIDs, invoice.ID)
		}
		if !hasMore {
			break
		}
		last := page[len(page)-1]
		cursor = &invoicecursor.Position{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(seenIDs) != len(wantIDs) {
		t.Fatalf("seen IDs = %v, want %v", seenIDs, wantIDs)
	}
	for i := range wantIDs {
		if seenIDs[i] != wantIDs[i] {
			t.Fatalf("seen IDs = %v, want stable descending IDs %v", seenIDs, wantIDs)
		}
	}
}
