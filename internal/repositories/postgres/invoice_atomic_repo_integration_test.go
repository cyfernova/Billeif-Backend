package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestEnsureEmptyDisposableDatabaseRejectsNonEmptyDatabase(t *testing.T) {
	repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
	defer closeDatabase()
	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := ensureEmptyDisposableDatabase(repository.db)

	if err == nil || !strings.Contains(err.Error(), "empty disposable PostgreSQL database") {
		t.Fatalf("guard error = %v, want empty disposable database error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryCreateDraftAtomicPostgresConcurrencyAndRollback(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping atomic invoice PostgreSQL integration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL")
	}

	admin, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable PostgreSQL database: %v", err)
	}
	if err := ensureEmptyDisposableDatabase(admin); err != nil {
		t.Fatal(err)
	}
	schema := "invoice_atomic_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("create isolated integration schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Errorf("drop isolated integration schema: %v", err)
		}
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := gorm.Open(gormpostgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	if err := database.Exec(`
		CREATE TABLE business_profiles (
			id UUID PRIMARY KEY
		);
		CREATE TABLE customers (
			id UUID PRIMARY KEY,
			business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE
		)
	`).Error; err != nil {
		t.Fatalf("create isolated tenant parents: %v", err)
	}
	if err := database.AutoMigrate(
		&models.APIIdempotencyKey{},
		&models.Invoice{},
		&models.InvoiceItem{},
		&models.Document{},
		&models.DocumentLine{},
		&models.ActivityLog{},
		&models.OutboxEvent{},
		&models.DocumentRenderJob{},
		&models.EmailDelivery{},
	); err != nil {
		t.Fatalf("create isolated atomic repository schema: %v", err)
	}
	for _, statement := range []string{
		`ALTER TABLE api_idempotency_keys ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE invoices ALTER COLUMN business_id TYPE UUID USING business_id::uuid, ALTER COLUMN customer_id TYPE UUID USING customer_id::uuid`,
		`ALTER TABLE documents ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE activity_logs ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE outbox_events ALTER COLUMN business_id TYPE UUID USING business_id::uuid`,
		`ALTER TABLE document_render_jobs ALTER COLUMN business_id TYPE UUID USING business_id::uuid, ALTER COLUMN document_id TYPE UUID USING document_id::uuid, ALTER COLUMN invoice_id TYPE UUID USING invoice_id::uuid`,
		`ALTER TABLE email_deliveries ALTER COLUMN business_id TYPE UUID USING business_id::uuid, ALTER COLUMN invoice_id TYPE UUID USING invoice_id::uuid, ALTER COLUMN render_job_id TYPE UUID USING render_job_id::uuid`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("align isolated production UUID type: %v", err)
		}
	}
	for _, statement := range []string{
		`ALTER TABLE api_idempotency_keys ADD CONSTRAINT fk_atomic_idempotency_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE invoices ADD CONSTRAINT fk_atomic_invoice_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE invoices ADD CONSTRAINT fk_atomic_invoice_customer FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE RESTRICT`,
		`ALTER TABLE documents ADD CONSTRAINT fk_atomic_document_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE activity_logs ADD CONSTRAINT fk_atomic_activity_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE outbox_events ADD CONSTRAINT fk_atomic_outbox_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE document_render_jobs ADD CONSTRAINT fk_atomic_render_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE document_render_jobs ADD CONSTRAINT fk_atomic_render_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE`,
		`ALTER TABLE document_render_jobs ADD CONSTRAINT fk_atomic_render_invoice FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE`,
		`ALTER TABLE email_deliveries ADD CONSTRAINT fk_atomic_email_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE`,
		`ALTER TABLE email_deliveries ADD CONSTRAINT fk_atomic_email_invoice FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE`,
		`ALTER TABLE email_deliveries ADD CONSTRAINT fk_atomic_email_render FOREIGN KEY (render_job_id) REFERENCES document_render_jobs(id) ON DELETE SET NULL`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("create isolated production FK: %v", err)
		}
	}

	repository := &invoiceRepository{db: database}
	firstCommand := atomicRepositoryTestCommand()
	secondCommand := atomicRepositoryTestCommand()
	copyAtomicScope(&secondCommand, firstCommand)
	if err := database.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, firstCommand.BusinessID).Error; err != nil {
		t.Fatalf("seed isolated business: %v", err)
	}
	customerIDs := []string{models.StringValue(firstCommand.Invoice.CustomerID), models.StringValue(secondCommand.Invoice.CustomerID)}
	for _, customerID := range customerIDs {
		if err := database.Exec(`INSERT INTO customers (id, business_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			customerID, firstCommand.BusinessID).Error; err != nil {
			t.Fatalf("seed isolated customer: %v", err)
		}
	}

	var group sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for _, command := range []interfaces.AtomicInvoiceDraft{firstCommand, secondCommand} {
		command := command
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := repository.CreateDraftAtomic(context.Background(), command)
			if result != nil && result.Invoice != nil {
				results <- result.Invoice.ID
			}
			errs <- err
		}()
	}
	group.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent atomic create: %v", err)
		}
	}
	var resultID string
	for id := range results {
		if resultID == "" {
			resultID = id
		}
		if id != resultID {
			t.Fatalf("concurrent result ID = %s, want %s", id, resultID)
		}
	}
	assertAtomicTableCount(t, database, "invoices", 1)
	assertAtomicTableCount(t, database, "invoice_items", 1)
	assertAtomicTableCount(t, database, "documents", 1)
	assertAtomicTableCount(t, database, "document_lines", 1)
	assertAtomicTableCount(t, database, "activity_logs", 1)
	assertAtomicTableCount(t, database, "api_idempotency_keys", 1)
	assertAtomicTableCount(t, database, "outbox_events", 1)
	assertAtomicTableCount(t, database, "document_render_jobs", 1)
	assertAtomicTableCount(t, database, "email_deliveries", 1)

	conflict := atomicRepositoryTestCommand()
	copyAtomicScope(&conflict, firstCommand)
	conflict.RequestHash = strings.Repeat("b", 64)
	if _, err := repository.CreateDraftAtomic(context.Background(), conflict); err == nil {
		t.Fatal("changed request hash unexpectedly succeeded")
	} else {
		var typedConflict *idempotency.ConflictError
		if !errors.As(err, &typedConflict) {
			t.Fatalf("changed request error = %T %v, want conflict", err, err)
		}
	}

	rollbackCommand := atomicRepositoryTestCommand()
	rollbackCommand.Activity = nil
	if _, err := repository.CreateDraftAtomic(context.Background(), rollbackCommand); err == nil {
		t.Fatal("invalid atomic command unexpectedly succeeded")
	}
	var rolledBackClaims int64
	if err := database.Model(&models.APIIdempotencyKey{}).
		Where("business_id = ? AND command = ? AND idempotency_key = ?",
			rollbackCommand.BusinessID, rollbackCommand.Command, rollbackCommand.IdempotencyKey).
		Count(&rolledBackClaims).Error; err != nil {
		t.Fatalf("count rolled back idempotency claim: %v", err)
	}
	if rolledBackClaims != 0 {
		t.Fatalf("rolled back claim count = %d, want 0", rolledBackClaims)
	}
	rollbackCommand.Activity = atomicRepositoryTestCommand().Activity
	rollbackCommand.Activity.BusinessID = rollbackCommand.BusinessID
	rollbackCommand.Activity.EntityID = rollbackCommand.Invoice.ID
	if _, err := repository.CreateDraftAtomic(context.Background(), rollbackCommand); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

func copyAtomicScope(target *interfaces.AtomicInvoiceDraft, source interfaces.AtomicInvoiceDraft) {
	target.BusinessID = source.BusinessID
	target.Command = source.Command
	target.IdempotencyKey = source.IdempotencyKey
	target.RequestHash = source.RequestHash
	target.Invoice.BusinessID = source.BusinessID
	target.Document.BusinessID = source.BusinessID
	target.Activity.BusinessID = source.BusinessID
	for _, event := range target.OutboxEvents {
		event.BusinessID = source.BusinessID
	}
	for _, job := range target.RenderJobs {
		job.BusinessID = source.BusinessID
	}
	for _, delivery := range target.EmailDeliveries {
		delivery.BusinessID = source.BusinessID
	}
}

func ensureEmptyDisposableDatabase(database *gorm.DB) error {
	var tableCount int64
	if err := database.Raw(`
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
	`).Scan(&tableCount).Error; err != nil {
		return fmt.Errorf("inspect disposable PostgreSQL database: %w", err)
	}
	if tableCount != 0 {
		return fmt.Errorf("MIGRATION_TEST_DATABASE_URL must point to an empty disposable PostgreSQL database")
	}
	return nil
}

func assertAtomicTableCount(t *testing.T, database *gorm.DB, table string, want int64) {
	t.Helper()
	var got int64
	if err := database.Table(table).Count(&got).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
