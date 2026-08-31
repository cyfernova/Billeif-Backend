package services

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPaymentCreatePostgresSerializesConcurrentInvoiceSettlements(t *testing.T) {
	database := newPaymentPostgresIntegrationDB(t)
	service := newPaymentInvariantService(database)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, database, 100, 100)

	inputs := []CreatePaymentInput{
		{InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 60, PaymentMethod: "bank_transfer"},
		{InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 40, PaymentMethod: "bank_transfer"},
	}
	start := make(chan struct{})
	errs := make(chan error, len(inputs))
	var group sync.WaitGroup
	for _, input := range inputs {
		input := input
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := service.Create(context.Background(), businessID, input)
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	assertPaymentInvariantInvoiceTotals(t, database, invoiceID, 100, 0, models.InvoiceStatusPaid)
	assertPaymentPostgresTableCount(t, database, "payments", 2)
	assertPaymentPostgresTableCount(t, database, "journals", 2)
	assertPaymentPostgresTableCount(t, database, "ledger_entries", 4)
}

func TestPaymentCreatePostgresConcurrentSameKeyCommitsExactlyOnce(t *testing.T) {
	database := newPaymentPostgresIntegrationDB(t)
	service := newPaymentInvariantService(database)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, database, 100, 100)
	input := CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 40, PaymentMethod: "cash",
	}

	start := make(chan struct{})
	ids := make(chan string, 2)
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			payment, err := service.Create(context.Background(), businessID, input)
			if payment != nil {
				ids <- payment.ID
			}
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var paymentID string
	for id := range ids {
		if paymentID == "" {
			paymentID = id
		}
		require.Equal(t, paymentID, id)
	}
	require.NotEmpty(t, paymentID)

	assertPaymentInvariantInvoiceTotals(t, database, invoiceID, 40, 60, models.InvoiceStatusPartiallyPaid)
	assertPaymentPostgresTableCount(t, database, "payments", 1)
	assertPaymentPostgresTableCount(t, database, "journals", 1)
	assertPaymentPostgresTableCount(t, database, "ledger_entries", 2)
	assertPaymentPostgresTableCount(t, database, "api_idempotency_keys", 1)
}

func newPaymentPostgresIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping payment PostgreSQL concurrency integration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	admin, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable PostgreSQL database: %v", err)
	}
	schema := "payment_invariants_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("create isolated payment schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Errorf("drop isolated payment schema: %v", err)
		}
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := gorm.Open(gormpostgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated payment schema: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("access PostgreSQL payment pool: %v", err)
	}
	sqlDatabase.SetMaxOpenConns(6)
	t.Cleanup(func() {
		if err := sqlDatabase.Close(); err != nil {
			t.Errorf("close payment PostgreSQL pool: %v", err)
		}
	})
	if err := database.AutoMigrate(
		&models.Invoice{},
		&models.InvoiceItem{},
		&models.Payment{},
		&models.PaymentWithholding{},
		&models.APIIdempotencyKey{},
		&models.Journal{},
		&models.JournalLine{},
		&models.LedgerEntry{},
	); err != nil {
		t.Fatalf("create isolated payment schema: %v", err)
	}
	return database
}

func assertPaymentPostgresTableCount(t *testing.T, database *gorm.DB, table string, want int64) {
	t.Helper()
	var count int64
	require.NoError(t, database.Table(table).Count(&count).Error)
	require.Equal(t, want, count, table)
}
