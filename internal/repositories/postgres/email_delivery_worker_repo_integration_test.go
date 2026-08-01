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
	"time"

	"invoice-backend/internal/emaildelivery"

	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestEmailDeliveryWorkerRepositoryPostgresConcurrentClaimAndExpiredLeaseReclaim(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping email delivery PostgreSQL integration")
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
	schema := "email_delivery_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Errorf("drop isolated schema: %v", err)
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
CREATE TABLE invoices (
	id UUID PRIMARY KEY,
	business_id UUID NOT NULL,
	version INTEGER NOT NULL,
	status VARCHAR(50) NOT NULL,
	invoice_no VARCHAR(50),
	issued_at TIMESTAMPTZ,
	deleted_at TIMESTAMPTZ
);
CREATE TABLE document_render_jobs (
	id UUID PRIMARY KEY,
	business_id UUID NOT NULL,
	invoice_id UUID,
	kind VARCHAR(16) NOT NULL,
	status VARCHAR(30) NOT NULL,
	source_invoice_version INTEGER,
	object_key VARCHAR(1024),
	output_filename VARCHAR(255),
	deleted_at TIMESTAMPTZ
);
CREATE TABLE email_deliveries (
	id UUID PRIMARY KEY,
	business_id UUID NOT NULL,
	invoice_id UUID,
	render_job_id UUID,
	recipient VARCHAR(255),
	status VARCHAR(40) NOT NULL,
	provider_message_id VARCHAR(255),
	error_message TEXT,
	attempts INTEGER NOT NULL DEFAULT 0,
	lease_owner VARCHAR(255),
	lease_expires_at TIMESTAMPTZ,
	sent_at TIMESTAMPTZ,
	failed_at TIMESTAMPTZ,
	source_email VARCHAR(255),
	subject VARCHAR(255),
	updated_at TIMESTAMPTZ,
	deleted_at TIMESTAMPTZ
);`).Error; err != nil {
		t.Fatalf("create email delivery integration tables: %v", err)
	}

	message := workerRepositoryMessage()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	objectKey := fmt.Sprintf(
		"invoices/%s/%s/v3/final.pdf", message.BusinessID, message.InvoiceID,
	)
	if err := database.Exec(
		`INSERT INTO invoices (id, business_id, version, status, invoice_no, issued_at)
		 VALUES (?, ?, 3, 'paid', 'INV/3', ?)`,
		message.InvoiceID, message.BusinessID, now.Add(-time.Hour),
	).Error; err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	if err := database.Exec(
		`INSERT INTO document_render_jobs
		 (id, business_id, invoice_id, kind, status, source_invoice_version, object_key, output_filename)
		 VALUES (?, ?, ?, 'final', 'completed', 3, ?, 'INV_3.pdf')`,
		message.RenderJobID, message.BusinessID, message.InvoiceID, objectKey,
	).Error; err != nil {
		t.Fatalf("seed final render: %v", err)
	}
	if err := database.Exec(
		`INSERT INTO email_deliveries
		 (id, business_id, invoice_id, render_job_id, recipient, status, updated_at)
		 VALUES (?, ?, ?, ?, 'buyer@example.com', 'queued', ?)`,
		message.DeliveryID, message.BusinessID, message.InvoiceID, message.RenderJobID, now,
	).Error; err != nil {
		t.Fatalf("seed delivery: %v", err)
	}

	repository := NewEmailDeliveryWorkerRepository(database)
	type claimResult struct {
		claim *emaildelivery.ClaimedDelivery
		err   error
	}
	results := make(chan claimResult, 2)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, owner := range []string{"owner-a", "owner-b"} {
		owner := owner
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			claim, claimErr := repository.Claim(
				context.Background(), message, owner, now, now.Add(2*time.Minute),
			)
			results <- claimResult{claim: claim, err: claimErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes, unavailable := 0, 0
	for result := range results {
		switch {
		case result.err == nil && result.claim != nil:
			successes++
		case errors.Is(result.err, emaildelivery.ErrLeaseUnavailable):
			unavailable++
		default:
			t.Fatalf("unexpected concurrent claim result: %#v/%v", result.claim, result.err)
		}
	}
	if successes != 1 || unavailable != 1 {
		t.Fatalf("concurrent successes/unavailable = %d/%d, want 1/1", successes, unavailable)
	}

	expiredAt := now.Add(-time.Second)
	if err := database.Exec(
		`UPDATE email_deliveries SET status = 'processing', lease_expires_at = ?, lease_owner = 'expired-owner' WHERE id = ?`,
		expiredAt, message.DeliveryID,
	).Error; err != nil {
		t.Fatalf("expire delivery lease: %v", err)
	}
	reclaimed, err := repository.Claim(
		context.Background(), message, "reclaim-owner",
		now.Add(time.Minute), now.Add(3*time.Minute),
	)
	if err != nil {
		t.Fatalf("reclaim expired processing delivery: %v", err)
	}
	if reclaimed == nil || reclaimed.DeliveryID != message.DeliveryID {
		t.Fatalf("reclaimed delivery = %#v", reclaimed)
	}
	var row struct {
		Status     string
		LeaseOwner *string
		Attempts   int
	}
	if err := database.Table("email_deliveries").
		Select("status", "lease_owner", "attempts").
		Where("id = ?", message.DeliveryID).
		Scan(&row).Error; err != nil {
		t.Fatalf("read reclaimed delivery: %v", err)
	}
	if row.Status != "processing" || row.LeaseOwner == nil ||
		*row.LeaseOwner != "reclaim-owner" || row.Attempts != 2 {
		t.Fatalf("reclaimed row = %#v", row)
	}
}
