package postgres

import (
	"context"
	"regexp"
	"testing"
	"time"

	"invoice-backend/internal/emaildelivery"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func TestEmailDeliveryWorkerRepositoryClaimUsesAtomicExpiredLeaseCAS(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := NewEmailDeliveryWorkerRepository(invoiceRepository.db)
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)
	message := emaildelivery.DeliveryMessage{
		SchemaVersion: 1, Type: "send_invoice_pdf",
		BusinessID: uuid.NewString(), DeliveryID: uuid.NewString(),
		InvoiceID: uuid.NewString(), RenderJobID: uuid.NewString(),
	}
	objectKey := "invoices/" + message.BusinessID + "/" + message.InvoiceID + "/v3/final.pdf"
	claimRows := sqlmock.NewRows([]string{
		"delivery_id", "business_id", "invoice_id", "render_job_id", "recipient",
		"invoice_no", "invoice_version", "object_key", "output_filename",
	}).AddRow(
		message.DeliveryID, message.BusinessID, message.InvoiceID, message.RenderJobID,
		"buyer@example.com", "INV/1", 3, objectKey, "INV_1.pdf",
	)
	claimSQL := `(?s)` + regexp.QuoteMeta("WITH candidate AS (") +
		`.*i\.status IN \('issued', 'sent', 'partially_paid', 'paid', 'overdue'\)` +
		`.*i\.invoice_no IS NOT NULL.*i\.issued_at IS NOT NULL` +
		`.*j\.kind = 'final'.*j\.status = 'completed'.*j\.source_invoice_version = i\.version` +
		`.*j\.object_key = CONCAT`
	mock.ExpectQuery(claimSQL).
		WithArgs(
			message.DeliveryID, message.BusinessID, message.InvoiceID, message.RenderJobID,
			now, "owner-1", leaseUntil, now,
		).
		WillReturnRows(claimRows)

	claim, err := repository.Claim(
		context.Background(), message, "owner-1", now, leaseUntil,
	)

	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim == nil || claim.ObjectKey != objectKey || claim.InvoiceVersion != 3 {
		t.Fatalf("claim = %#v", claim)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestEmailDeliveryWorkerRepositoryMarkSentRequiresLiveExactLease(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := NewEmailDeliveryWorkerRepository(invoiceRepository.db)
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	deliveryID := uuid.NewString()
	mock.ExpectExec(`UPDATE email_deliveries.*lease_owner = \$[0-9]+.*lease_expires_at > \$[0-9]+`).
		WithArgs(
			"sent", "ses-message-1", "billing@example.com", "Invoice INV/1",
			now, now, deliveryID, "processing", "owner-1", now,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repository.MarkSent(
		context.Background(), deliveryID, "owner-1", "ses-message-1",
		"billing@example.com", "Invoice INV/1", now,
	)

	if err != nil {
		t.Fatalf("mark sent: %v", err)
	}
}
