package migrator

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	migrationbundle "invoice-backend/migrations"
)

func TestEmbeddedBundleAgainstEmptyPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping empty PostgreSQL migration integration")
	}
	credentials := credentialsFromTestDSN(t, dsn)
	runner, err := OpenPostgres(migrationbundle.Embedded, ".", credentials)
	if err != nil {
		t.Fatalf("open explicit PostgreSQL migration test database: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close PostgreSQL migration test database: %v", err)
		}
	})

	if version, dirty, err := runner.Version(); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("MIGRATION_TEST_DATABASE_URL must point to an empty migration database; version=%d dirty=%t error=%v", version, dirty, err)
	}
	postgresMigration, ok := runner.(*postgresRunner)
	if !ok {
		t.Fatalf("migration runner type = %T, want *postgresRunner", runner)
	}
	if err := postgresMigration.migrate.Steps(43); err != nil {
		t.Fatalf("apply embedded migrations through version 43: %v", err)
	}

	databaseURL, err := postgresURL(credentials)
	if err != nil {
		t.Fatalf("build explicit PostgreSQL integration URL: %v", err)
	}
	database, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL integration assertions connection: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close PostgreSQL integration assertions connection: %v", err)
		}
	})

	ctx := context.Background()
	const (
		businessID        = "a0000000-0000-4000-8000-000000000001"
		customerID        = "a0000000-0000-4000-8000-000000000002"
		invoiceID         = "a0000000-0000-4000-8000-000000000003"
		renderID          = "a0000000-0000-4000-8000-000000000004"
		passwordProfileID = "a0000000-0000-4000-8000-000000000005"
	)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO business_profiles (id, owner_id, name, email)
		VALUES ($1, 'migration-test-owner', 'Migration Test Seller', 'seller@example.com')`,
		businessID,
	); err != nil {
		t.Fatalf("seed version 43 business: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO customers (id, business_id, name, email, gstin)
		VALUES ($1, $2, 'Migration Test Buyer', 'buyer@example.com', '29ABCDE1234F1Z5')`,
		customerID, businessID,
	); err != nil {
		t.Fatalf("seed version 43 customer: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`ALTER TABLE invoices DROP CONSTRAINT invoices_status_check`,
	); err != nil {
		t.Fatalf("simulate legacy partial-payment status drift: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO invoices (
			id, business_id, customer_id, invoice_no, invoice_date, due_date,
			status, currency, subtotal, tax, total, balance_due
		) VALUES (
			$1, $2, $3, 'INV-MIGRATION-1', NOW(), NOW() + INTERVAL '30 days',
			'partial', 'INR', 100, 18, 118, 59
		)`,
		invoiceID, businessID, customerID,
	); err != nil {
		t.Fatalf("seed version 43 legacy partial-payment invoice: %v", err)
	}

	if err := postgresMigration.migrate.Steps(1); err != nil {
		t.Fatalf("apply canonical invoice migration to version 43 fixture: %v", err)
	}
	version, dirty, err := runner.Version()
	if err != nil {
		t.Fatalf("read final PostgreSQL migration version: %v", err)
	}
	if version != 44 || dirty {
		t.Fatalf("final PostgreSQL migration state = version %d dirty %t, want version 44 clean", version, dirty)
	}

	var status, buyerGSTIN string
	if err := database.QueryRowContext(ctx,
		`SELECT status, buyer_snapshot->>'gstin' FROM invoices WHERE id = $1`,
		invoiceID,
	).Scan(&status, &buyerGSTIN); err != nil {
		t.Fatalf("read upgraded legacy invoice: %v", err)
	}
	if status != "partially_paid" {
		t.Fatalf("upgraded legacy invoice status = %q, want partially_paid", status)
	}
	if buyerGSTIN != "29ABCDE1234F1Z5" {
		t.Fatalf("upgraded buyer snapshot GSTIN = %q, want seeded GSTIN", buyerGSTIN)
	}

	if _, err := database.ExecContext(ctx, `
		INSERT INTO document_render_jobs (
			id, document_id, business_id, invoice_id, kind, source_invoice_version, status
		) VALUES ($1, NULL, $2, $3, 'preview', 1, 'queued')`,
		renderID, businessID, invoiceID,
	); err != nil {
		t.Fatalf("seed invoice-only render job before canonical slice reversal: %v", err)
	}
	if err := postgresMigration.migrate.Steps(-1); err != nil {
		t.Fatalf("reverse canonical invoice schema slice: %v", err)
	}
	version, dirty, err = runner.Version()
	if err != nil {
		t.Fatalf("read PostgreSQL migration version after slice reversal: %v", err)
	}
	if version != 43 || dirty {
		t.Fatalf("reversed PostgreSQL migration state = version %d dirty %t, want version 43 clean", version, dirty)
	}
	var renderCount int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_render_jobs WHERE id = $1`,
		renderID,
	).Scan(&renderCount); err != nil {
		t.Fatalf("count invoice-only render jobs after slice reversal: %v", err)
	}
	if renderCount != 0 {
		t.Fatalf("invoice-only render jobs after slice reversal = %d, want 0", renderCount)
	}

	if err := postgresMigration.migrate.Steps(4); err != nil {
		t.Fatalf("apply embedded migrations through render profile password encryption: %v", err)
	}
	version, dirty, err = runner.Version()
	if err != nil || version != 47 || dirty {
		t.Fatalf("render profile password migration state = version %d dirty %t error %v, want version 47 clean", version, dirty, err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO render_profiles (
			id, business_id, name, password_protected, password_ciphertext
		) VALUES ($1, $2, 'Rollback Guard Profile', TRUE, 'rpw:v1:integration-fixture')`,
		passwordProfileID, businessID,
	); err != nil {
		t.Fatalf("seed encrypted render profile rollback fixture: %v", err)
	}
	if err := postgresMigration.migrate.Steps(-1); err == nil {
		t.Fatal("render profile password migration rolled back while ciphertext remained")
	}
	version, dirty, err = runner.Version()
	if err != nil || version != 46 || !dirty {
		t.Fatalf("guarded rollback state = version %d dirty %t error %v, want target version 46 dirty", version, dirty, err)
	}
	var ciphertextColumnCount int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'render_profiles'
		  AND column_name = 'password_ciphertext'`,
	).Scan(&ciphertextColumnCount); err != nil {
		t.Fatalf("check ciphertext column after guarded rollback: %v", err)
	}
	if ciphertextColumnCount != 1 {
		t.Fatalf("ciphertext column count after guarded rollback = %d, want 1", ciphertextColumnCount)
	}
	if _, err := database.ExecContext(ctx,
		`DELETE FROM render_profiles WHERE id = $1`,
		passwordProfileID,
	); err != nil {
		t.Fatalf("remove encrypted rollback fixture: %v", err)
	}
	if err := postgresMigration.migrate.Force(47); err != nil {
		t.Fatalf("clear expected dirty state after guarded rollback: %v", err)
	}
	if err := postgresMigration.migrate.Steps(-1); err != nil {
		t.Fatalf("roll back render profile password migration after removing ciphertext: %v", err)
	}
	if err := postgresMigration.migrate.Steps(1); err != nil {
		t.Fatalf("reapply render profile password migration after rollback validation: %v", err)
	}
	version, dirty, err = runner.Version()
	if err != nil || version != 47 || dirty {
		t.Fatalf("final PostgreSQL migration state = version %d dirty %t error %v, want version 47 clean", version, dirty, err)
	}

	if err := postgresMigration.migrate.Steps(13); err != nil {
		t.Fatalf("apply embedded migrations through cart and coupon controls: %v", err)
	}
	version, dirty, err = runner.Version()
	if err != nil || version != 60 || dirty {
		t.Fatalf("Task 8 migration state = version %d dirty %t error %v, want version 60 clean", version, dirty, err)
	}
	const (
		cartUserID  = "a0000000-0000-4000-8000-000000000006"
		cartAgentID = "a0000000-0000-4000-8000-000000000007"
		cartID      = "a0000000-0000-4000-8000-000000000008"
	)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO users (id, email, cognito_id, name)
		VALUES ($1, 'cart-migration@example.com', 'cart-migration-sub', 'Cart Migration User')`, cartUserID); err != nil {
		t.Fatalf("seed Task 8 cart user: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agents (id, owner_id, business_id, name, type)
		VALUES ($1, $2, $3, 'Cart Migration Agent', 'shopping')`, cartAgentID, cartUserID, businessID); err != nil {
		t.Fatalf("seed Task 8 cart agent: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO cart_mandates (
			id, business_id, user_id, agent_id, items, subtotal_amount, tax_amount,
			total_amount, currency, signature, status, version, expires_at
		) VALUES ($1, $2, $3, $4, '[]', 0, 0, 0, 'INR', 'cleared-cart', 'pending', 2, NOW() + INTERVAL '1 hour')`,
		cartID, businessID, cartUserID, cartAgentID); err != nil {
		t.Fatalf("seed cleared Task 8 cart: %v", err)
	}
	if err := postgresMigration.migrate.Steps(-1); err != nil {
		t.Fatalf("reverse Task 8 migration with a cleared cart: %v", err)
	}
	version, dirty, err = runner.Version()
	if err != nil || version != 59 || dirty {
		t.Fatalf("Task 8 rollback state = version %d dirty %t error %v, want version 59 clean", version, dirty, err)
	}
	var clearedTotal float64
	if err := database.QueryRowContext(ctx, `SELECT total_amount FROM cart_mandates WHERE id = $1`, cartID).Scan(&clearedTotal); err != nil {
		t.Fatalf("read cleared cart after Task 8 rollback: %v", err)
	}
	if clearedTotal != 0 {
		t.Fatalf("cleared cart total after rollback = %v, want 0", clearedTotal)
	}
	if err := postgresMigration.migrate.Steps(1); err != nil {
		t.Fatalf("reapply Task 8 migration after rollback validation: %v", err)
	}
}

func credentialsFromTestDSN(t *testing.T, dsn string) config.DatabaseCredentials {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.User == nil {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL with explicit credentials")
	}
	password, ok := parsed.User.Password()
	if !ok || password == "" {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must include a password")
	}
	port := 5432
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			t.Fatal("MIGRATION_TEST_DATABASE_URL has an invalid port")
		}
	}
	name := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if name == "" || strings.Contains(name, "/") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must include one database name")
	}
	sslMode := parsed.Query().Get("sslmode")
	if sslMode == "" {
		sslMode = "require"
	}
	return config.DatabaseCredentials{
		Host: parsed.Hostname(), Port: port, User: parsed.User.Username(),
		Password: password, Name: name, SSLMode: sslMode,
	}
}
