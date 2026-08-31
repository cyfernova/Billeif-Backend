package services

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPaymentCreationRollsBackInvoiceAndPaymentWhenLedgerProjectionFails(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_payment_ledger_insert
		BEFORE INSERT ON ledger_entries
		BEGIN
			SELECT RAISE(ABORT, 'synthetic payment ledger failure');
		END;
	`).Error)

	_, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID:      invoiceID,
		IdempotencyKey: uuid.NewString(),
		Amount:         40,
		PaymentMethod:  "bank_transfer",
	})
	require.ErrorContains(t, err, "synthetic payment ledger failure")

	var paymentCount int64
	require.NoError(t, db.Model(&models.Payment{}).Count(&paymentCount).Error)
	require.Zero(t, paymentCount)
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 0, 100, models.InvoiceStatusSent)
	var claimCount int64
	require.NoError(t, db.Model(&models.APIIdempotencyKey{}).Count(&claimCount).Error)
	require.Zero(t, claimCount)
}

func TestPaymentCreationReplaysSameIdempotencyKeyExactlyOnce(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	input := CreatePaymentInput{
		InvoiceID:      invoiceID,
		IdempotencyKey: uuid.NewString(),
		Amount:         40,
		PaymentMethod:  "bank_transfer",
		Reference:      "bank-ref-1",
	}

	first, err := service.Create(context.Background(), businessID, input)
	require.NoError(t, err)
	second, err := service.Create(context.Background(), businessID, input)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	var paymentCount int64
	require.NoError(t, db.Model(&models.Payment{}).Count(&paymentCount).Error)
	require.Equal(t, int64(1), paymentCount)
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 40, 60, models.InvoiceStatusPartiallyPaid)
	var ledgerCount int64
	require.NoError(t, db.Model(&models.LedgerEntry{}).Where("transaction_id IN (SELECT id FROM journals)").Count(&ledgerCount).Error)
	require.Equal(t, int64(2), ledgerCount)
}

func TestPaymentCreationRejectsChangedRequestForSameIdempotencyKey(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	key := uuid.NewString()
	first, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: key, Amount: 40, PaymentMethod: "bank_transfer",
	})
	require.NoError(t, err)
	require.NotNil(t, first)

	_, err = service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: key, Amount: 50, PaymentMethod: "bank_transfer",
	})
	var conflict *idempotency.ConflictError
	require.True(t, errors.As(err, &conflict), "error = %T %v", err, err)

	var paymentCount int64
	require.NoError(t, db.Model(&models.Payment{}).Count(&paymentCount).Error)
	require.Equal(t, int64(1), paymentCount)
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 40, 60, models.InvoiceStatusPartiallyPaid)
}

func TestPaymentCreationIdempotencyAndInvoiceScopeAreTenantBound(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessA, invoiceA := seedPaymentInvariantInvoice(t, db, 100, 100)
	businessB, invoiceB := seedPaymentInvariantInvoice(t, db, 100, 100)
	key := uuid.NewString()

	_, err := service.Create(context.Background(), businessA, CreatePaymentInput{
		InvoiceID: invoiceB, IdempotencyKey: key, Amount: 20, PaymentMethod: "cash",
	})
	require.ErrorContains(t, err, "invoice not found")

	paymentA, err := service.Create(context.Background(), businessA, CreatePaymentInput{
		InvoiceID: invoiceA, IdempotencyKey: key, Amount: 20, PaymentMethod: "cash",
	})
	require.NoError(t, err)
	paymentB, err := service.Create(context.Background(), businessB, CreatePaymentInput{
		InvoiceID: invoiceB, IdempotencyKey: key, Amount: 20, PaymentMethod: "cash",
	})
	require.NoError(t, err)
	require.NotEqual(t, paymentA.ID, paymentB.ID)

	var claims int64
	require.NoError(t, db.Model(&models.APIIdempotencyKey{}).Where("idempotency_key = ?", key).Count(&claims).Error)
	require.Equal(t, int64(2), claims)
}

func TestPaymentCreationRejectsSettlementBeyondInvoiceBalance(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 20)

	_, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID:      invoiceID,
		IdempotencyKey: uuid.NewString(),
		Amount:         20.005,
		PaymentMethod:  "cash",
	})
	require.ErrorContains(t, err, "exceeds invoice balance")

	var paymentCount int64
	require.NoError(t, db.Model(&models.Payment{}).Count(&paymentCount).Error)
	require.Zero(t, paymentCount)
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 80, 20, models.InvoiceStatusPartiallyPaid)
}

func TestPaymentCreationRejectsInconsistentInvoiceTotals(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	require.NoError(t, db.Model(&models.Invoice{}).Where("id = ?", invoiceID).Update("paid_amount", 10).Error)

	_, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 20, PaymentMethod: "cash",
	})
	require.ErrorContains(t, err, "invoice payment totals are inconsistent")

	var paymentCount int64
	require.NoError(t, db.Model(&models.Payment{}).Count(&paymentCount).Error)
	require.Zero(t, paymentCount)
}

func TestPaymentJournalBalancesCashAndWithholdingSettlement(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)

	payment, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID:      invoiceID,
		IdempotencyKey: uuid.NewString(),
		Amount:         90,
		PaymentMethod:  "bank_transfer",
		Withholding: &WithholdingInput{
			SectionCode:     "194Q",
			WithholdingType: models.WithholdingTypeTDS,
			TaxableAmount:   100,
			Rate:            10,
			Amount:          10,
		},
	})
	require.NoError(t, err)
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 100, 0, models.InvoiceStatusPaid)

	var journal models.Journal
	require.NoError(t, db.Preload("Lines").Where("reference = ?", payment.ID).First(&journal).Error)
	require.Len(t, journal.Lines, 3)
	var debits, credits int64
	for _, line := range journal.Lines {
		minor, conversionErr := journalMinorUnits(line.Amount)
		require.NoError(t, conversionErr)
		if line.EntryType == "debit" {
			debits += minor
		} else {
			credits += minor
		}
	}
	require.Equal(t, int64(10000), debits)
	require.Equal(t, debits, credits)
}

func TestPostedPaymentCannotBeEditedOrDeleted(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	payment, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 40, PaymentMethod: "cash",
	})
	require.NoError(t, err)

	_, err = service.UpdateByBusiness(context.Background(), businessID, payment.ID, UpdatePaymentInput{Amount: 50})
	require.ErrorContains(t, err, "posted payments are immutable")
	require.ErrorContains(t, service.DeleteByBusiness(context.Background(), businessID, payment.ID), "posted payments are immutable")

	var stored models.Payment
	require.NoError(t, db.Where("id = ? AND deleted_at IS NULL", payment.ID).First(&stored).Error)
	require.Equal(t, float64(40), stored.Amount)
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 40, 60, models.InvoiceStatusPartiallyPaid)
}

func TestPaymentReversalAtomicallyRestoresInvoiceAndReversesJournal(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	payment, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 90, PaymentMethod: "bank_transfer",
		Withholding: &WithholdingInput{SectionCode: "194Q", WithholdingType: models.WithholdingTypeTDS, Amount: 10},
	})
	require.NoError(t, err)

	reversed, err := service.ReverseByBusiness(context.Background(), businessID, payment.ID, ReversePaymentInput{Reason: "Bank transfer returned"})
	require.NoError(t, err)
	require.Equal(t, models.PaymentStatusReversed, reversed.Status)
	require.NotNil(t, reversed.ReversedAt)
	require.Equal(t, "Bank transfer returned", reversed.ReversalReason)
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 0, 100, models.InvoiceStatusSent)

	var original models.Journal
	require.NoError(t, db.Preload("Lines").Where("source_type = ? AND source_id = ?", "payment", payment.ID).First(&original).Error)
	require.Equal(t, models.JournalStatusReversed, original.Status)
	var reversal models.Journal
	require.NoError(t, db.Preload("Lines").Where("source_type = ? AND source_id = ?", "payment_reversal", payment.ID).First(&reversal).Error)
	require.Equal(t, models.JournalStatusPosted, reversal.Status)
	require.NotNil(t, reversal.ReversalOfID)
	require.Equal(t, original.ID, *reversal.ReversalOfID)
	_, err = service.journals.ReverseByBusiness(context.Background(), businessID, reversal.ID)
	require.ErrorContains(t, err, "payment journals must be reversed through the payment workflow")

	var debit, credit int64
	for _, journal := range []models.Journal{original, reversal} {
		for _, line := range journal.Lines {
			minor, conversionErr := journalMinorUnits(line.Amount)
			require.NoError(t, conversionErr)
			if line.EntryType == "debit" {
				debit += minor
			} else {
				credit += minor
			}
		}
	}
	require.Equal(t, debit, credit)

	_, err = service.ReverseByBusiness(context.Background(), businessID, payment.ID, ReversePaymentInput{Reason: "Duplicate"})
	require.ErrorContains(t, err, "payment is already reversed")
	_, err = service.ReverseByBusiness(context.Background(), uuid.NewString(), payment.ID, ReversePaymentInput{Reason: "Wrong tenant"})
	require.ErrorContains(t, err, "payment not found")
}

func TestPaymentJournalCannotBeReversedOutsidePaymentWorkflow(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	payment, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 40, PaymentMethod: "cash",
	})
	require.NoError(t, err)

	var journal models.Journal
	require.NoError(t, db.Where("source_type = ? AND source_id = ?", "payment", payment.ID).First(&journal).Error)
	_, err = service.journals.ReverseByBusiness(context.Background(), businessID, journal.ID)
	require.ErrorContains(t, err, "payment journals must be reversed through the payment workflow")
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 40, 60, models.InvoiceStatusPartiallyPaid)
}

func TestPaymentReversalRequiresReason(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	payment, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 40, PaymentMethod: "cash",
	})
	require.NoError(t, err)

	_, err = service.ReverseByBusiness(context.Background(), businessID, payment.ID, ReversePaymentInput{Reason: "  "})
	require.ErrorContains(t, err, "payment reversal reason is required")
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 40, 60, models.InvoiceStatusPartiallyPaid)
}

func TestPaymentReversalRollsBackEveryFinancialEffectOnLedgerFailure(t *testing.T) {
	db := newPaymentInvariantDB(t)
	service := newPaymentInvariantService(db)
	businessID, invoiceID := seedPaymentInvariantInvoice(t, db, 100, 100)
	payment, err := service.Create(context.Background(), businessID, CreatePaymentInput{
		InvoiceID: invoiceID, IdempotencyKey: uuid.NewString(), Amount: 40, PaymentMethod: "cash",
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_payment_reversal_ledger
		BEFORE INSERT ON ledger_entries
		BEGIN
			SELECT RAISE(ABORT, 'synthetic payment reversal failure');
		END;
	`).Error)

	_, err = service.ReverseByBusiness(context.Background(), businessID, payment.ID, ReversePaymentInput{Reason: "Returned"})
	require.ErrorContains(t, err, "synthetic payment reversal failure")

	var stored models.Payment
	require.NoError(t, db.Where("id = ?", payment.ID).First(&stored).Error)
	require.Equal(t, models.PaymentStatusPosted, stored.Status)
	require.Nil(t, stored.ReversedAt)
	assertPaymentInvariantInvoiceTotals(t, db, invoiceID, 40, 60, models.InvoiceStatusPartiallyPaid)
	var journalCount int64
	require.NoError(t, db.Model(&models.Journal{}).Where("source_id = ?", payment.ID).Count(&journalCount).Error)
	require.Equal(t, int64(1), journalCount)
}

func newPaymentInvariantService(db *gorm.DB) *PaymentService {
	journalService := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	return NewPaymentService(
		db,
		postgres.NewPaymentRepository(db),
		postgres.NewInvoiceRepository(db),
		nil,
		journalService,
		logger.New(),
	)
}

func seedPaymentInvariantInvoice(t *testing.T, db *gorm.DB, total, balance float64) (string, string) {
	t.Helper()
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	paid := total - balance
	require.NoError(t, db.Create(&models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status: func() string {
			if paid > 0 {
				return models.InvoiceStatusPartiallyPaid
			}
			return models.InvoiceStatusSent
		}(),
		Currency:   "INR",
		Total:      total,
		PaidAmount: paid,
		BalanceDue: balance,
	}).Error)
	return businessID, invoiceID
}

func assertPaymentInvariantInvoiceTotals(t *testing.T, db *gorm.DB, invoiceID string, paid, balance float64, status string) {
	t.Helper()
	var invoice models.Invoice
	require.NoError(t, db.Where("id = ?", invoiceID).First(&invoice).Error)
	require.Equal(t, paid, invoice.PaidAmount)
	require.Equal(t, balance, invoice.BalanceDue)
	require.Equal(t, status, invoice.Status)
}

func newPaymentInvariantDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newJournalInvariantDB(t)
	for _, statement := range []string{
		`CREATE TABLE invoices (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			customer_id TEXT,
			version INTEGER NOT NULL DEFAULT 1,
			project_id TEXT,
			price_list_id TEXT,
			render_profile_id TEXT,
			invoice_no TEXT,
			origin TEXT DEFAULT 'manual',
			issued_at DATETIME,
			seller_snapshot TEXT DEFAULT '{}',
			buyer_snapshot TEXT DEFAULT '{}',
			invoice_date DATETIME,
			due_date DATETIME,
			status TEXT NOT NULL,
			currency TEXT NOT NULL,
			subtotal NUMERIC DEFAULT 0,
			tax NUMERIC DEFAULT 0,
			discount NUMERIC DEFAULT 0,
			total NUMERIC NOT NULL,
			paid_amount NUMERIC DEFAULT 0,
			balance_due NUMERIC DEFAULT 0,
			notes TEXT,
			customer_snapshot TEXT DEFAULT '{}',
			document_json TEXT DEFAULT '{}',
			template_override TEXT DEFAULT '{}',
			payment_display TEXT DEFAULT '{}',
			terms_json TEXT DEFAULT '{}',
			eway_details_json TEXT DEFAULT '{}',
			einvoice_settings_json TEXT DEFAULT '{}',
			custom_fields TEXT DEFAULT '{}',
			additional_charges TEXT DEFAULT '[]',
			origin_subscription_id TEXT,
			origin_run_id TEXT,
			signed_at DATETIME,
			signed_by_profile_id TEXT,
			sign_metadata TEXT DEFAULT '{}',
			pdf_url TEXT,
			pdf_filename TEXT,
			tax_profile TEXT DEFAULT '{}',
			sent_at DATETIME,
			paid_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE invoice_items (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			invoice_id TEXT NOT NULL
		)`,
		`CREATE TABLE payments (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			business_id TEXT NOT NULL,
			invoice_id TEXT NOT NULL,
			project_id TEXT,
			amount NUMERIC NOT NULL,
			currency TEXT NOT NULL,
			payment_date DATETIME NOT NULL,
			payment_type TEXT NOT NULL DEFAULT 'normal',
			status TEXT NOT NULL DEFAULT 'posted',
			payment_method TEXT NOT NULL,
			reference TEXT,
			withholding_data TEXT DEFAULT '{}',
			receipt_url TEXT,
			receipt_key TEXT,
			notes TEXT,
			reversal_reason TEXT,
			reversed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE payment_withholdings (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			business_id TEXT NOT NULL,
			payment_id TEXT NOT NULL,
			invoice_id TEXT,
			section_code TEXT NOT NULL,
			withholding_type TEXT NOT NULL,
			rate NUMERIC DEFAULT 0,
			taxable_amount NUMERIC DEFAULT 0,
			amount NUMERIC DEFAULT 0,
			metadata TEXT DEFAULT '{}',
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE api_idempotency_keys (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			business_id TEXT NOT NULL,
			command TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			status TEXT NOT NULL,
			result_type TEXT,
			result_id TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			completed_at DATETIME,
			UNIQUE (business_id, command, idempotency_key)
		)`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	return db
}
