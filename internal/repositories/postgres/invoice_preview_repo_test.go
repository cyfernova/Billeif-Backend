package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

const (
	previewClaimSQL        = `INSERT INTO "api_idempotency_keys".*"business_id".*"command".*"idempotency_key".*"request_hash".*"status".*ON CONFLICT \("business_id","command","idempotency_key"\) DO NOTHING.*RETURNING "id"`
	previewReplayLookupSQL = `SELECT \* FROM "api_idempotency_keys".*business_id = \$1 AND command = \$2 AND idempotency_key = \$3.*LIMIT \$4`
	previewReplayJobSQL    = `SELECT \* FROM "document_render_jobs".*id = \$1 AND business_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*LIMIT \$4`
	previewInvoiceSQL      = `SELECT \* FROM "invoices".*id = \$1 AND business_id = \$2 AND deleted_at IS NULL.*FOR UPDATE`
	previewItemsSQL        = `SELECT \* FROM "invoice_items".*invoice_id = \$1.*ORDER BY created_at ASC, id ASC.*FOR UPDATE`
	previewDocumentSQL     = `SELECT \* FROM "documents".*id = \$1 AND business_id = \$2 AND deleted_at IS NULL.*FOR UPDATE`
	previewLinesSQL        = `SELECT \* FROM "document_lines".*document_id = \$1.*ORDER BY created_at ASC, id ASC.*FOR UPDATE`
	previewJobInsertSQL    = `INSERT INTO "document_render_jobs".*"document_id".*"invoice_id".*"business_id".*"kind".*"source_invoice_version".*"object_key".*RETURNING "id"`
	previewOutboxInsertSQL = `INSERT INTO "outbox_events".*"business_id".*"aggregate_type".*"aggregate_id".*"event_type".*"payload".*"available_at".*RETURNING "id"`
	previewActivitySQL     = `INSERT INTO "activity_logs".*"business_id".*"actor_id".*"request_id".*"ip_address".*"entity_type".*"entity_id".*"action".*"snapshot".*"metadata".*RETURNING "id"`
	previewCompletionSQL   = `UPDATE "api_idempotency_keys" SET .*"result_id".*"result_type".*"status".*WHERE business_id = \$[0-9]+ AND command = \$[0-9]+ AND idempotency_key = \$[0-9]+ AND request_hash = \$[0-9]+ AND status = \$[0-9]+`
)

func TestInvoiceRepositoryRequestPreviewAtomicPersistsVersionedJobAuditAndOutbox(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, document := strictPreviewFixture()
	defaultProfileID := uuid.NewString()

	expectPreviewClaim(mock, command, true)
	expectPreviewCanonicalLocks(t, mock, command, invoice, document)
	mock.ExpectQuery(`SELECT \* FROM "render_profiles".*business_id = \$1 AND is_default = \$2 AND deleted_at IS NULL.*LIMIT \$3`).
		WithArgs(command.BusinessID, true, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "is_default"}).
			AddRow(defaultProfileID, command.BusinessID, true))
	mock.ExpectQuery(previewJobInsertSQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectQuery(previewOutboxInsertSQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectQuery(previewActivitySQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectExec(previewCompletionSQL).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.RequestPreviewAtomic(context.Background(), command)

	if err != nil {
		t.Fatalf("request preview: %v", err)
	}
	if result == nil || result.RenderJob == nil || result.OutboxEvent == nil || result.Replayed {
		t.Fatalf("result = %#v, want new render job and outbox event", result)
	}
	job := result.RenderJob
	if job.Kind != models.RenderKindPreview || job.SourceInvoiceVersion == nil ||
		*job.SourceInvoiceVersion != invoice.Version || job.InvoiceID == nil ||
		*job.InvoiceID != invoice.ID || job.DocumentID == nil || *job.DocumentID != invoice.ID ||
		job.RenderProfileID == nil || *job.RenderProfileID != defaultProfileID {
		t.Fatalf("preview job = %#v", job)
	}
	keyPrefix := "invoices/" + command.BusinessID + "/" + command.InvoiceID + "/previews/v1/"
	keyID := strings.TrimSuffix(strings.TrimPrefix(job.ObjectKey, keyPrefix), ".pdf")
	if !strings.HasPrefix(job.ObjectKey, keyPrefix) || !strings.HasSuffix(job.ObjectKey, ".pdf") {
		t.Fatalf("object key = %q, want private versioned preview prefix %q", job.ObjectKey, keyPrefix)
	}
	if _, err := uuid.Parse(keyID); err != nil {
		t.Fatalf("object key job identity = %q, want UUID: %v", keyID, err)
	}
	if result.OutboxEvent.EventType != "invoice.preview.requested.v1" {
		t.Fatalf("event type = %q", result.OutboxEvent.EventType)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result.OutboxEvent.Payload), &payload); err != nil {
		t.Fatalf("decode outbox payload: %v", err)
	}
	if payload["type"] != "generate_document_pdf" ||
		payload["invoice_id"] != invoice.ID ||
		payload["document_id"] != invoice.ID ||
		payload["render_job_id"] != job.ID ||
		payload["invoice_version"] != float64(invoice.Version) ||
		payload["schema_version"] != float64(1) {
		t.Fatalf("outbox payload = %#v", payload)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryRequestPreviewAtomicReplaysAndRejectsKeyReuse(t *testing.T) {
	t.Run("completed replay", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command, _, _ := strictPreviewFixture()
		jobID := uuid.NewString()
		version := 3

		expectPreviewClaim(mock, command, false)
		mock.ExpectQuery(previewReplayLookupSQL).
			WithArgs(command.BusinessID, command.Command, command.IdempotencyKey, 1).
			WillReturnRows(sqlmock.NewRows([]string{
				"business_id", "command", "idempotency_key", "request_hash", "status", "result_type", "result_id",
			}).AddRow(
				command.BusinessID, command.Command, command.IdempotencyKey, command.RequestHash,
				models.IdempotencyStatusCompleted, "invoice_preview", jobID,
			))
		mock.ExpectCommit()
		mock.ExpectQuery(previewReplayJobSQL).
			WithArgs(jobID, command.BusinessID, models.RenderKindPreview, 1).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "business_id", "invoice_id", "document_id", "kind", "source_invoice_version",
			}).AddRow(jobID, command.BusinessID, command.InvoiceID, command.InvoiceID, models.RenderKindPreview, version))

		result, err := repository.RequestPreviewAtomic(context.Background(), command)

		if err != nil || result == nil || !result.Replayed || result.OutboxEvent != nil ||
			result.RenderJob == nil || result.RenderJob.ID != jobID ||
			result.RenderJob.SourceInvoiceVersion == nil || *result.RenderJob.SourceInvoiceVersion != version {
			t.Fatalf("replay result/error = %#v/%v", result, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})

	t.Run("corrupt completed result", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command, _, _ := strictPreviewFixture()
		jobID := uuid.NewString()
		wrongInvoiceID := uuid.NewString()

		expectPreviewClaim(mock, command, false)
		mock.ExpectQuery(previewReplayLookupSQL).
			WithArgs(command.BusinessID, command.Command, command.IdempotencyKey, 1).
			WillReturnRows(sqlmock.NewRows([]string{
				"business_id", "command", "idempotency_key", "request_hash", "status", "result_type", "result_id",
			}).AddRow(
				command.BusinessID, command.Command, command.IdempotencyKey, command.RequestHash,
				models.IdempotencyStatusCompleted, "invoice_preview", jobID,
			))
		mock.ExpectCommit()
		mock.ExpectQuery(previewReplayJobSQL).
			WithArgs(jobID, command.BusinessID, models.RenderKindPreview, 1).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "business_id", "invoice_id", "document_id", "kind", "source_invoice_version",
			}).AddRow(jobID, command.BusinessID, wrongInvoiceID, wrongInvoiceID, models.RenderKindPreview, 1))

		result, err := repository.RequestPreviewAtomic(context.Background(), command)

		if result != nil || err == nil ||
			!strings.Contains(err.Error(), "atomic invoice preview persistence failed at idempotency result replay") {
			t.Fatalf("result/error = %#v/%v, want fail-closed replay error", result, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})

	for _, fixture := range []struct {
		name       string
		resultType interface{}
		resultID   interface{}
	}{
		{name: "completed wrong result type", resultType: "invoice_issue", resultID: uuid.NewString()},
		{name: "completed missing result id", resultType: "invoice_preview", resultID: nil},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			command, _, _ := strictPreviewFixture()

			expectPreviewClaim(mock, command, false)
			mock.ExpectQuery(previewReplayLookupSQL).
				WithArgs(command.BusinessID, command.Command, command.IdempotencyKey, 1).
				WillReturnRows(sqlmock.NewRows([]string{
					"business_id", "command", "idempotency_key", "request_hash", "status", "result_type", "result_id",
				}).AddRow(
					command.BusinessID,
					command.Command,
					command.IdempotencyKey,
					command.RequestHash,
					models.IdempotencyStatusCompleted,
					fixture.resultType,
					fixture.resultID,
				))
			mock.ExpectRollback()

			result, err := repository.RequestPreviewAtomic(context.Background(), command)

			var inProgress *idempotency.InProgressError
			if result != nil || err == nil || errors.As(err, &inProgress) ||
				!strings.Contains(err.Error(), "atomic invoice preview persistence failed") {
				t.Fatalf("result/error = %#v/%T %v, want fail-closed persistence error", result, err, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}

	for _, fixture := range []struct {
		name        string
		requestHash string
		status      string
		assert      func(error) bool
	}{
		{
			name: "changed hash", requestHash: strings.Repeat("b", 64),
			status: models.IdempotencyStatusCompleted,
			assert: func(err error) bool {
				var target *idempotency.ConflictError
				return errors.As(err, &target)
			},
		},
		{
			name: "in progress", requestHash: strings.Repeat("a", 64),
			status: models.IdempotencyStatusInProgress,
			assert: func(err error) bool {
				var target *idempotency.InProgressError
				return errors.As(err, &target)
			},
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			command, _, _ := strictPreviewFixture()

			expectPreviewClaim(mock, command, false)
			mock.ExpectQuery(previewReplayLookupSQL).
				WithArgs(command.BusinessID, command.Command, command.IdempotencyKey, 1).
				WillReturnRows(sqlmock.NewRows([]string{
					"business_id", "command", "idempotency_key", "request_hash", "status",
				}).AddRow(
					command.BusinessID, command.Command, command.IdempotencyKey,
					fixture.requestHash, fixture.status,
				))
			mock.ExpectRollback()

			result, err := repository.RequestPreviewAtomic(context.Background(), command)

			if result != nil || err == nil || !fixture.assert(err) {
				t.Fatalf("result/error = %#v/%T %v", result, err, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryRequestPreviewAtomicRejectsMissingOrInvalidCanonicalSource(t *testing.T) {
	t.Run("tenant invoice missing", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command, _, _ := strictPreviewFixture()
		expectPreviewClaim(mock, command, true)
		mock.ExpectQuery(previewInvoiceSQL).
			WithArgs(command.InvoiceID, command.BusinessID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectRollback()

		result, err := repository.RequestPreviewAtomic(context.Background(), command)

		var notFound *invoiceissue.NotFoundError
		if result != nil || !errors.As(err, &notFound) {
			t.Fatalf("result/error = %#v/%T %v, want tenant not found", result, err, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})

	t.Run("projection missing", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command, invoice, _ := strictPreviewFixture()
		expectPreviewClaim(mock, command, true)
		expectPreviewInvoiceLock(t, mock, command, invoice)
		expectPreviewItems(mock, invoice)
		mock.ExpectQuery(previewDocumentSQL).
			WithArgs(command.InvoiceID, command.BusinessID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectRollback()

		result, err := repository.RequestPreviewAtomic(context.Background(), command)

		var invalid *invoiceissue.InvalidLifecycleError
		if result != nil || !errors.As(err, &invalid) {
			t.Fatalf("result/error = %#v/%T %v, want invalid projection", result, err, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})

	t.Run("invoice not draft", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command, invoice, _ := strictPreviewFixture()
		invoice.Status = models.InvoiceStatusIssued
		expectPreviewClaim(mock, command, true)
		expectPreviewInvoiceLock(t, mock, command, invoice)
		mock.ExpectRollback()

		result, err := repository.RequestPreviewAtomic(context.Background(), command)

		var invalid *invoiceissue.InvalidLifecycleError
		if result != nil || !errors.As(err, &invalid) {
			t.Fatalf("result/error = %#v/%T %v, want invalid lifecycle", result, err, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})
}

func TestInvoiceRepositoryRequestPreviewAtomicRollsBackEveryWriteStage(t *testing.T) {
	stages := []struct {
		name    string
		pattern string
		exec    bool
	}{
		{name: "render job", pattern: previewJobInsertSQL},
		{name: "outbox event", pattern: previewOutboxInsertSQL},
		{name: "activity", pattern: previewActivitySQL},
		{name: "idempotency completion", pattern: previewCompletionSQL, exec: true},
	}

	for failedIndex, failedStage := range stages {
		t.Run(failedStage.name, func(t *testing.T) {
			repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			command, invoice, document := strictPreviewFixture()
			expectPreviewClaim(mock, command, true)
			expectPreviewCanonicalLocks(t, mock, command, invoice, document)
			expectNoDefaultPreviewProfile(mock, command.BusinessID)
			for index := 0; index < failedIndex; index++ {
				expectSuccessfulPreviewWrite(mock, index)
			}
			if failedStage.exec {
				mock.ExpectExec(failedStage.pattern).WillReturnError(errors.New("injected preview failure"))
			} else {
				mock.ExpectQuery(failedStage.pattern).WillReturnError(errors.New("injected preview failure"))
			}
			mock.ExpectRollback()

			result, err := repository.RequestPreviewAtomic(context.Background(), command)

			if result != nil || err == nil ||
				!strings.Contains(err.Error(), "atomic invoice preview persistence failed") ||
				strings.Contains(err.Error(), "injected preview failure") {
				t.Fatalf("result/error = %#v/%v, want sanitized atomic failure", result, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func strictPreviewFixture() (interfaces.AtomicInvoicePreview, *models.Invoice, *models.Document) {
	_, invoice, document := strictIssueFixture()
	return interfaces.AtomicInvoicePreview{
		BusinessID:     invoice.BusinessID,
		InvoiceID:      invoice.ID,
		Command:        "invoice.preview",
		IdempotencyKey: uuid.NewString(),
		RequestHash:    strings.Repeat("a", 64),
		ActorID:        uuid.NewString(),
		ActorRole:      "accountant",
		RequestID:      "request-preview",
		IPAddress:      "127.0.0.1",
	}, invoice, document
}

func expectPreviewClaim(mock sqlmock.Sqlmock, command interfaces.AtomicInvoicePreview, inserted bool) {
	mock.ExpectBegin()
	rows := sqlmock.NewRows([]string{"id"})
	if inserted {
		rows.AddRow(uuid.NewString())
	}
	mock.ExpectQuery(previewClaimSQL).WillReturnRows(rows)
}

func expectPreviewCanonicalLocks(
	t *testing.T,
	mock sqlmock.Sqlmock,
	command interfaces.AtomicInvoicePreview,
	invoice *models.Invoice,
	document *models.Document,
) {
	t.Helper()
	expectPreviewInvoiceLock(t, mock, command, invoice)
	expectPreviewItems(mock, invoice)
	expectPreviewDocumentLock(mock, command, document)
	expectPreviewLines(mock, document)
}

func expectPreviewInvoiceLock(
	t *testing.T,
	mock sqlmock.Sqlmock,
	command interfaces.AtomicInvoicePreview,
	invoice *models.Invoice,
) {
	t.Helper()
	mock.ExpectQuery(previewInvoiceSQL).
		WithArgs(command.InvoiceID, command.BusinessID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "customer_id", "version", "status", "origin",
			"seller_snapshot", "buyer_snapshot", "invoice_date", "due_date", "currency",
			"subtotal", "tax", "discount", "total", "paid_amount", "balance_due",
			"notes", "custom_fields", "additional_charges", "tax_profile",
		}).AddRow(
			invoice.ID, invoice.BusinessID, invoice.CustomerID, invoice.Version, invoice.Status, invoice.Origin,
			mustJSONTest(t, invoice.SellerSnapshot), mustJSONTest(t, invoice.BuyerSnapshot),
			invoice.InvoiceDate, invoice.DueDate, invoice.Currency,
			invoice.Subtotal, invoice.Tax, invoice.Discount, invoice.Total, invoice.PaidAmount, invoice.BalanceDue,
			invoice.Notes, invoice.CustomFields, invoice.AdditionalCharges, invoice.TaxProfile,
		))
}

func expectPreviewItems(mock sqlmock.Sqlmock, invoice *models.Invoice) {
	rows := sqlmock.NewRows([]string{
		"id", "invoice_id", "description", "unit", "quantity", "unit_price", "discount",
		"tax_rate", "cess_rate", "cess_amount", "custom_fields", "charge_snapshot",
		"batch_allocations", "serial_ids", "total",
	})
	for _, item := range invoice.Items {
		rows.AddRow(
			item.ID, item.InvoiceID, item.Description, item.Unit, item.Quantity, item.UnitPrice, item.Discount,
			item.TaxRate, item.CessRate, item.CessAmount, item.CustomFields, item.ChargeSnapshot,
			item.BatchAllocations, item.SerialIDs, item.Total,
		)
	}
	mock.ExpectQuery(previewItemsSQL).WithArgs(invoice.ID).WillReturnRows(rows)
}

func expectPreviewDocumentLock(
	mock sqlmock.Sqlmock,
	command interfaces.AtomicInvoicePreview,
	document *models.Document,
) {
	mock.ExpectQuery(previewDocumentSQL).
		WithArgs(command.InvoiceID, command.BusinessID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "document_type", "party_type", "party_id", "status", "draft_state",
			"tax_mode", "gst_treatment", "bill_of_supply", "serial_number", "issue_date", "due_date",
			"currency", "exchange_rate", "locale", "source_linkage", "render_profile_id",
			"profit_snapshot_enabled", "notes", "direction", "subtotal", "discount_total", "tax_total",
			"cess_total", "withholding_total", "tds_total", "tcs_total", "total", "paid_amount",
			"balance_due", "dispatch_from", "dispatch_to", "transporter", "vehicle",
			"multi_vehicle_plan", "extra_fields", "report_tags",
		}).AddRow(
			document.ID, document.BusinessID, document.DocumentType, document.PartyType, document.PartyID,
			document.Status, document.DraftState, document.TaxMode, document.GSTTreatment,
			document.BillOfSupply, document.SerialNumber, document.IssueDate, document.DueDate,
			document.Currency, document.ExchangeRate, document.Locale, document.SourceLinkage,
			document.RenderProfileID, document.ProfitSnapshotEnabled, document.Notes, document.Direction,
			document.Subtotal, document.DiscountTotal, document.TaxTotal, document.CessTotal,
			document.WithholdingTotal, document.TDSTotal, document.TCSTotal, document.Total,
			document.PaidAmount, document.BalanceDue, document.DispatchFrom, document.DispatchTo,
			document.Transporter, document.Vehicle, document.MultiVehiclePlan, document.ExtraFields,
			document.ReportTags,
		))
}

func expectPreviewLines(mock sqlmock.Sqlmock, document *models.Document) {
	rows := sqlmock.NewRows([]string{
		"id", "document_id", "description", "hsn_sac_code", "uqc_code", "unit", "quantity",
		"free_quantity", "remaining_quantity", "unit_price", "mrp", "discount_amount",
		"tax_rate", "cgst_rate", "sgst_rate", "igst_rate", "cess_rate", "cgst_amount",
		"sgst_amount", "igst_amount", "cess_amount", "tax_amount", "line_subtotal",
		"line_total", "cost_snapshot", "margin_snapshot", "custom_fields", "charge_linkage",
		"packing_metadata", "batch_allocations", "serial_ids", "report_tags", "stock_effect",
	})
	for _, line := range document.Lines {
		rows.AddRow(
			line.ID, line.DocumentID, line.Description, line.HSNSACCode, line.UQCCode, line.Unit,
			line.Quantity, line.FreeQuantity, line.RemainingQuantity, line.UnitPrice, line.MRP,
			line.DiscountAmount, line.TaxRate, line.CGSTRate, line.SGSTRate, line.IGSTRate,
			line.CessRate, line.CGSTAmount, line.SGSTAmount, line.IGSTAmount, line.CessAmount,
			line.TaxAmount, line.LineSubtotal, line.LineTotal, line.CostSnapshot, line.MarginSnapshot,
			line.CustomFields, line.ChargeLinkage, line.PackingMetadata, line.BatchAllocations,
			line.SerialIDs, line.ReportTags, line.StockEffect,
		)
	}
	mock.ExpectQuery(previewLinesSQL).WithArgs(document.ID).WillReturnRows(rows)
}

func expectSuccessfulPreviewWrite(mock sqlmock.Sqlmock, index int) {
	switch index {
	case 0:
		mock.ExpectQuery(previewJobInsertSQL).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	case 1:
		mock.ExpectQuery(previewOutboxInsertSQL).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	case 2:
		mock.ExpectQuery(previewActivitySQL).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	case 3:
		mock.ExpectExec(previewCompletionSQL).WillReturnResult(sqlmock.NewResult(0, 1))
	}
}

func expectNoDefaultPreviewProfile(mock sqlmock.Sqlmock, businessID string) {
	mock.ExpectQuery(`SELECT \* FROM "render_profiles".*business_id = \$1 AND is_default = \$2 AND deleted_at IS NULL.*LIMIT \$3`).
		WithArgs(businessID, true, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_id", "is_default"}))
}
