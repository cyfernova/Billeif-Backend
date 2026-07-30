package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
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
	mock.ExpectQuery(emailDeliveryClaimSQLPattern()).
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

func TestEmailDeliveryWorkerRepositoryClassifiesActiveLeaseAndTerminalDuplicate(t *testing.T) {
	tests := []struct {
		name   string
		status string
		lease  interface{}
		want   error
	}{
		{
			name:   "active processing lease",
			status: "processing", lease: time.Date(2026, time.July, 30, 12, 1, 0, 0, time.UTC),
			want: emaildelivery.ErrLeaseUnavailable,
		},
		{name: "sent terminal duplicate", status: "sent", lease: nil, want: emaildelivery.ErrDeliveryTerminal},
		{name: "delivered terminal duplicate", status: "delivered", lease: nil, want: emaildelivery.ErrDeliveryTerminal},
		{name: "bounced terminal duplicate", status: "bounced", lease: nil, want: emaildelivery.ErrDeliveryTerminal},
		{name: "complained terminal duplicate", status: "complained", lease: nil, want: emaildelivery.ErrDeliveryTerminal},
		{name: "failed but non-deliverable", status: "failed", lease: nil, want: emaildelivery.ErrDeliveryMalformed},
		{
			name:   "expired processing but non-deliverable",
			status: "processing", lease: time.Date(2026, time.July, 30, 11, 59, 0, 0, time.UTC),
			want: emaildelivery.ErrDeliveryMalformed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := NewEmailDeliveryWorkerRepository(invoiceRepository.db)
			now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
			message := workerRepositoryMessage()

			mock.ExpectQuery(emailDeliveryClaimSQLPattern()).
				WithArgs(
					message.DeliveryID, message.BusinessID, message.InvoiceID, message.RenderJobID,
					now, "owner-1", now.Add(2*time.Minute), now,
				).
				WillReturnRows(emptyEmailDeliveryClaimRows())
			mock.ExpectQuery(`SELECT .*status.*lease_expires_at.*FROM "email_deliveries"`).
				WithArgs(message.DeliveryID, message.BusinessID, message.InvoiceID, message.RenderJobID, 1).
				WillReturnRows(sqlmock.NewRows([]string{"status", "lease_expires_at"}).
					AddRow(test.status, test.lease))

			claim, err := repository.Claim(
				context.Background(), message, "owner-1", now, now.Add(2*time.Minute),
			)

			if claim != nil || !errors.Is(err, test.want) {
				t.Fatalf("claim/error = %#v/%v, want %v", claim, err, test.want)
			}
		})
	}
}

func TestEmailDeliveryWorkerRepositoryRejectsCrossIdentityAfterFailedCAS(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := NewEmailDeliveryWorkerRepository(invoiceRepository.db)
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	message := workerRepositoryMessage()

	mock.ExpectQuery(emailDeliveryClaimSQLPattern()).
		WithArgs(
			message.DeliveryID, message.BusinessID, message.InvoiceID, message.RenderJobID,
			now, "owner-1", now.Add(2*time.Minute), now,
		).
		WillReturnRows(emptyEmailDeliveryClaimRows())
	mock.ExpectQuery(`SELECT .*status.*lease_expires_at.*FROM "email_deliveries"`).
		WithArgs(message.DeliveryID, message.BusinessID, message.InvoiceID, message.RenderJobID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"status", "lease_expires_at"}))

	claim, err := repository.Claim(
		context.Background(), message, "owner-1", now, now.Add(2*time.Minute),
	)

	if claim != nil || !errors.Is(err, emaildelivery.ErrDeliveryMalformed) {
		t.Fatalf("claim/error = %#v/%v, want malformed cross-identity", claim, err)
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

func TestEmailDeliveryWorkerRepositoryMarkSentHandlesLostLeaseAndDatabaseError(t *testing.T) {
	tests := []struct {
		name        string
		result      driver.Result
		databaseErr error
		wantLease   bool
	}{
		{name: "lost lease", result: sqlmock.NewResult(0, 0), wantLease: true},
		{name: "database error", databaseErr: errors.New("database unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := NewEmailDeliveryWorkerRepository(invoiceRepository.db)
			now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
			deliveryID := uuid.NewString()
			expectation := mock.ExpectExec(`UPDATE email_deliveries.*lease_owner = \$[0-9]+.*lease_expires_at > \$[0-9]+`).
				WithArgs(
					"sent", "ses-message-1", "billing@example.com", "Invoice INV/1",
					now, now, deliveryID, "processing", "owner-1", now,
				)
			if test.databaseErr != nil {
				expectation.WillReturnError(test.databaseErr)
			} else {
				expectation.WillReturnResult(test.result)
			}

			err := repository.MarkSent(
				context.Background(), deliveryID, "owner-1", "ses-message-1",
				"billing@example.com", "Invoice INV/1", now,
			)

			if test.wantLease && !errors.Is(err, emaildelivery.ErrLeaseUnavailable) {
				t.Fatalf("mark sent error = %v, want lost lease", err)
			}
			if test.databaseErr != nil && !errors.Is(err, test.databaseErr) {
				t.Fatalf("mark sent error = %v, want database error", err)
			}
		})
	}
}

func TestEmailDeliveryWorkerRepositoryMarkFailedRequiresLiveExactLease(t *testing.T) {
	tests := []struct {
		name        string
		result      driver.Result
		databaseErr error
		want        error
	}{
		{name: "success", result: sqlmock.NewResult(0, 1)},
		{name: "lost lease", result: sqlmock.NewResult(0, 0), want: emaildelivery.ErrLeaseUnavailable},
		{name: "database error", databaseErr: errors.New("database unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := NewEmailDeliveryWorkerRepository(invoiceRepository.db)
			now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
			deliveryID := uuid.NewString()
			expectation := mock.ExpectExec(`UPDATE email_deliveries.*lease_owner = \$[0-9]+.*lease_expires_at > \$[0-9]+`).
				WithArgs(
					"failed", "invalid PDF", now, now, deliveryID, "processing", "owner-1", now,
				)
			if test.databaseErr != nil {
				expectation.WillReturnError(test.databaseErr)
			} else {
				expectation.WillReturnResult(test.result)
			}

			err := repository.MarkFailed(
				context.Background(), deliveryID, "owner-1", "invalid PDF", now,
			)

			if test.want == nil && test.databaseErr == nil && err != nil {
				t.Fatalf("mark failed: %v", err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("mark failed error = %v, want %v", err, test.want)
			}
			if test.databaseErr != nil && !errors.Is(err, test.databaseErr) {
				t.Fatalf("mark failed error = %v, want database error", err)
			}
		})
	}
}

func emailDeliveryClaimSQLPattern() string {
	return `(?s)` + regexp.QuoteMeta("WITH candidate AS (") +
		`.*d\.status IN \('queued', 'failed', 'processing'\)` +
		`.*\(d\.lease_expires_at IS NULL OR d\.lease_expires_at <= \$[0-9]+\)` +
		`.*i\.status IN \('issued', 'sent', 'partially_paid', 'paid', 'overdue'\)` +
		`.*i\.invoice_no IS NOT NULL.*i\.issued_at IS NOT NULL` +
		`.*j\.kind = 'final'.*j\.status = 'completed'.*j\.source_invoice_version = i\.version` +
		`.*j\.object_key = CONCAT`
}

func emptyEmailDeliveryClaimRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"delivery_id", "business_id", "invoice_id", "render_job_id", "recipient",
		"invoice_no", "invoice_version", "object_key", "output_filename",
	})
}

func workerRepositoryMessage() emaildelivery.DeliveryMessage {
	return emaildelivery.DeliveryMessage{
		SchemaVersion: 1, Type: "send_invoice_pdf",
		BusinessID: uuid.NewString(), DeliveryID: uuid.NewString(),
		InvoiceID: uuid.NewString(), RenderJobID: uuid.NewString(),
	}
}
