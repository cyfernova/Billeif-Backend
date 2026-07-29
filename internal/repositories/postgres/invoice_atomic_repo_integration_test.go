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

	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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
	schema := "invoice_atomic_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		t.Fatalf("ensure disposable PostgreSQL UUID support: %v", err)
	}
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
	database, err := gorm.Open(gormpostgres.Open(parsed.String()), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
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

	repository := &invoiceRepository{db: database}
	firstCommand := atomicRepositoryTestCommand()
	secondCommand := atomicRepositoryTestCommand()
	copyAtomicScope(&secondCommand, firstCommand)

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
