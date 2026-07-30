package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestNewInvoiceDeliveryOutboxEventHasStrictWorkerIdentity(t *testing.T) {
	businessID, invoiceID, renderJobID, deliveryID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Date(2026, time.July, 30, 8, 30, 0, 0, time.UTC)
	event, err := newInvoiceDeliveryOutboxEvent(&models.EmailDelivery{
		ID: deliveryID, BusinessID: businessID, InvoiceID: &invoiceID,
		RenderJobID: &renderJobID, Recipient: "must-not-publish@example.com",
	}, now)
	if err != nil {
		t.Fatalf("newInvoiceDeliveryOutboxEvent() error = %v", err)
	}
	if event.AggregateType != "email_delivery" || event.AggregateID != deliveryID ||
		event.EventType != "invoice.delivery.requested.v1" ||
		event.BusinessID != businessID || !event.AvailableAt.Equal(now) {
		t.Fatalf("outbox identity = %#v", event)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	want := map[string]interface{}{
		"schema_version": float64(1),
		"delivery_id":    deliveryID, "invoice_id": invoiceID, "render_job_id": renderJobID,
	}
	if len(payload) != len(want) {
		t.Fatalf("payload = %#v, want exact %#v", payload, want)
	}
	for key, value := range want {
		if payload[key] != value {
			t.Fatalf("payload[%q] = %#v, want %#v", key, payload[key], value)
		}
	}
}

const (
	deliveryClaimSQL       = `INSERT INTO "api_idempotency_keys".*"business_id".*"command".*"idempotency_key".*"request_hash".*"status".*ON CONFLICT \("business_id","command","idempotency_key"\) DO NOTHING.*RETURNING "id"`
	deliveryReplaySQL      = `SELECT \* FROM "api_idempotency_keys".*business_id = \$1 AND command = \$2 AND idempotency_key = \$3.*LIMIT \$4`
	deliveryJobLockSQL     = `SELECT \* FROM "document_render_jobs".*business_id = \$1 AND invoice_id = \$2 AND kind = \$3 AND deleted_at IS NULL.*ORDER BY source_invoice_version DESC, created_at DESC.*LIMIT \$4 FOR UPDATE`
	deliveryInvoiceLockSQL = `SELECT \* FROM "invoices".*id = \$1 AND business_id = \$2 AND deleted_at IS NULL.*LIMIT \$3 FOR UPDATE`
	deliveryInsertSQL      = `INSERT INTO "email_deliveries".*"business_id".*"invoice_id".*"render_job_id".*"recipient".*"status".*"metadata".*RETURNING "id"`
	deliveryActivitySQL    = `INSERT INTO "activity_logs".*"business_id".*"actor_id".*"request_id".*"ip_address".*"entity_type".*"entity_id".*"action".*"snapshot".*"metadata".*RETURNING "id"`
	deliveryOutboxSQL      = `INSERT INTO "outbox_events".*"business_id".*"aggregate_type".*"aggregate_id".*"event_type".*"payload".*"available_at".*RETURNING "id"`
	deliveryCompleteSQL    = `UPDATE "api_idempotency_keys" SET .*"result_id".*"result_type".*"status".*WHERE business_id = \$[0-9]+ AND command = \$[0-9]+ AND idempotency_key = \$[0-9]+ AND request_hash = \$[0-9]+ AND status = \$[0-9]+`
	deliveryReadSQL        = `SELECT \* FROM "email_deliveries".*id = \$1 AND business_id = \$2 AND invoice_id = \$3 AND deleted_at IS NULL.*LIMIT \$4`
)

func TestInvoiceRepositoryCreateDeliveryAtomicWaitsForIncompleteFinalRender(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, job := deliveryRepositoryFixture(models.RenderJobStatusProcessing)
	expectDeliveryClaim(mock, command, true)
	expectDeliveryLocks(mock, command, invoice, job)
	deliveryID := uuid.NewString()
	mock.ExpectQuery(deliveryInsertSQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(deliveryID))
	mock.ExpectQuery(deliveryActivitySQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectExec(deliveryCompleteSQL).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.CreateDeliveryAtomic(context.Background(), command)

	if err != nil {
		t.Fatalf("create waiting delivery: %v", err)
	}
	if result == nil || result.Delivery == nil || result.Replayed ||
		result.OutboxEvent != nil ||
		result.Delivery.ID != deliveryID ||
		result.Delivery.Status != models.EmailDeliveryStatusWaitingForRender ||
		result.Delivery.RenderJobID == nil || *result.Delivery.RenderJobID != job.ID {
		t.Fatalf("result = %#v, want waiting delivery", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryCreateDeliveryAtomicQueuesCompletedFinalRenderWithOutbox(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, job := deliveryRepositoryFixture(models.RenderJobStatusCompleted)
	expectDeliveryClaim(mock, command, true)
	expectDeliveryLocks(mock, command, invoice, job)
	deliveryID, eventID := uuid.NewString(), uuid.NewString()
	mock.ExpectQuery(deliveryInsertSQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(deliveryID))
	mock.ExpectQuery(deliveryActivitySQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	mock.ExpectQuery(deliveryOutboxSQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(eventID))
	mock.ExpectExec(deliveryCompleteSQL).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.CreateDeliveryAtomic(context.Background(), command)

	if err != nil {
		t.Fatalf("create queued delivery: %v", err)
	}
	if result.Delivery.Status != models.EmailDeliveryStatusQueued {
		t.Fatalf("status = %q, want queued", result.Delivery.Status)
	}
	if result.OutboxEvent == nil ||
		result.OutboxEvent.EventType != invoiceDeliveryRequestedEvent ||
		result.OutboxEvent.AggregateID != result.Delivery.ID {
		t.Fatalf("queued outbox = %#v", result.OutboxEvent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryCreateDeliveryAtomicAllowsIssuedSettlementAndDeliveryStates(t *testing.T) {
	for _, status := range []string{
		models.InvoiceStatusIssued,
		models.InvoiceStatusSent,
		models.InvoiceStatusPartiallyPaid,
		models.InvoiceStatusPaid,
		models.InvoiceStatusOverdue,
	} {
		t.Run(status, func(t *testing.T) {
			repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			command, invoice, job := deliveryRepositoryFixture(models.RenderJobStatusProcessing)
			invoice.Status = status
			expectDeliveryClaim(mock, command, true)
			expectDeliveryLocks(mock, command, invoice, job)
			mock.ExpectQuery(deliveryInsertSQL).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
			mock.ExpectQuery(deliveryActivitySQL).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
			mock.ExpectExec(deliveryCompleteSQL).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			result, err := repository.CreateDeliveryAtomic(context.Background(), command)

			if err != nil || result == nil || result.Delivery == nil ||
				result.Delivery.Status != models.EmailDeliveryStatusWaitingForRender {
				t.Fatalf("%s delivery result/error = %#v/%v, want waiting delivery", status, result, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryCreateDeliveryAtomicReplaysAndRejectsChangedRequest(t *testing.T) {
	t.Run("replay", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command, invoice, job := deliveryRepositoryFixture(models.RenderJobStatusCompleted)
		deliveryID := uuid.NewString()
		expectDeliveryClaim(mock, command, false)
		mock.ExpectQuery(deliveryReplaySQL).
			WithArgs(command.BusinessID, command.Command, command.IdempotencyKey, 1).
			WillReturnRows(sqlmock.NewRows([]string{
				"business_id", "command", "idempotency_key", "request_hash", "status", "result_type", "result_id",
			}).AddRow(
				command.BusinessID, command.Command, command.IdempotencyKey, command.RequestHash,
				models.IdempotencyStatusCompleted, "invoice_delivery", deliveryID,
			))
		mock.ExpectQuery(deliveryReadSQL).
			WithArgs(deliveryID, command.BusinessID, command.InvoiceID, 1).
			WillReturnRows(deliveryRows(deliveryID, command, invoice, job, models.EmailDeliveryStatusSent))
		mock.ExpectCommit()

		result, err := repository.CreateDeliveryAtomic(context.Background(), command)
		if err != nil || result == nil || !result.Replayed || result.Delivery == nil ||
			result.Delivery.ID != deliveryID || result.Delivery.Status != models.EmailDeliveryStatusSent {
			t.Fatalf("replay result/error = %#v/%v", result, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})

	t.Run("changed request", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command, _, _ := deliveryRepositoryFixture(models.RenderJobStatusCompleted)
		expectDeliveryClaim(mock, command, false)
		mock.ExpectQuery(deliveryReplaySQL).
			WithArgs(command.BusinessID, command.Command, command.IdempotencyKey, 1).
			WillReturnRows(sqlmock.NewRows([]string{
				"business_id", "command", "idempotency_key", "request_hash", "status",
			}).AddRow(
				command.BusinessID, command.Command, command.IdempotencyKey, "different",
				models.IdempotencyStatusCompleted,
			))
		mock.ExpectRollback()

		result, err := repository.CreateDeliveryAtomic(context.Background(), command)
		var conflict *idempotency.ConflictError
		if result != nil || !errors.As(err, &conflict) {
			t.Fatalf("changed request result/error = %#v/%T %v, want conflict", result, err, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})
}

func TestInvoiceRepositoryGetInvoiceDeliveryIsTenantAndInvoiceScoped(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, job := deliveryRepositoryFixture(models.RenderJobStatusCompleted)
	deliveryID := uuid.NewString()
	mock.ExpectQuery(deliveryReadSQL).
		WithArgs(deliveryID, command.BusinessID, command.InvoiceID, 1).
		WillReturnRows(deliveryRows(deliveryID, command, invoice, job, models.EmailDeliveryStatusDelivered))

	delivery, err := repository.GetInvoiceDelivery(
		context.Background(), command.BusinessID, command.InvoiceID, deliveryID,
	)
	if err != nil || delivery == nil || delivery.ID != deliveryID {
		t.Fatalf("delivery/error = %#v/%v", delivery, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryCreateDeliveryAtomicRejectsInvalidLifecycleAndRenderIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*models.Invoice, *models.DocumentRenderJob)
	}{
		{name: "draft invoice", mutate: func(invoice *models.Invoice, _ *models.DocumentRenderJob) {
			invoice.Status = models.InvoiceStatusDraft
		}},
		{name: "canceled invoice", mutate: func(invoice *models.Invoice, _ *models.DocumentRenderJob) {
			invoice.Status = models.InvoiceStatusCanceled
		}},
		{name: "void invoice", mutate: func(invoice *models.Invoice, _ *models.DocumentRenderJob) {
			invoice.Status = models.InvoiceStatusVoid
		}},
		{name: "missing invoice number", mutate: func(invoice *models.Invoice, _ *models.DocumentRenderJob) {
			invoice.InvoiceNo = nil
		}},
		{name: "missing issued timestamp", mutate: func(invoice *models.Invoice, _ *models.DocumentRenderJob) {
			invoice.IssuedAt = nil
		}},
		{name: "stale render version", mutate: func(invoice *models.Invoice, job *models.DocumentRenderJob) {
			version := invoice.Version - 1
			job.SourceInvoiceVersion = &version
		}},
		{name: "wrong render invoice", mutate: func(_ *models.Invoice, job *models.DocumentRenderJob) {
			job.InvoiceID = models.StringPointer(uuid.NewString())
		}},
		{name: "obsolete render", mutate: func(_ *models.Invoice, job *models.DocumentRenderJob) {
			job.Status = models.RenderJobStatusObsolete
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			command, invoice, job := deliveryRepositoryFixture(models.RenderJobStatusProcessing)
			test.mutate(invoice, job)
			expectDeliveryClaim(mock, command, true)
			expectDeliveryLocks(mock, command, invoice, job)
			mock.ExpectRollback()

			result, err := repository.CreateDeliveryAtomic(context.Background(), command)
			if result != nil || !errors.Is(err, interfaces.ErrInvoiceNotDeliverable) {
				t.Fatalf("result/error = %#v/%v, want not deliverable", result, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryCreateDeliveryAtomicRollsBackEveryWriteStage(t *testing.T) {
	stageError := errors.New("forced stage failure")
	tests := []struct {
		name   string
		status string
		expect func(sqlmock.Sqlmock)
	}{
		{name: "delivery", status: models.RenderJobStatusProcessing, expect: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(deliveryInsertSQL).WillReturnError(stageError)
		}},
		{name: "activity", status: models.RenderJobStatusProcessing, expect: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(deliveryInsertSQL).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
			mock.ExpectQuery(deliveryActivitySQL).WillReturnError(stageError)
		}},
		{name: "outbox", status: models.RenderJobStatusCompleted, expect: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(deliveryInsertSQL).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
			mock.ExpectQuery(deliveryActivitySQL).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
			mock.ExpectQuery(deliveryOutboxSQL).WillReturnError(stageError)
		}},
		{name: "idempotency", status: models.RenderJobStatusProcessing, expect: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(deliveryInsertSQL).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
			mock.ExpectQuery(deliveryActivitySQL).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
			mock.ExpectExec(deliveryCompleteSQL).WillReturnError(stageError)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			command, invoice, job := deliveryRepositoryFixture(test.status)
			expectDeliveryClaim(mock, command, true)
			expectDeliveryLocks(mock, command, invoice, job)
			test.expect(mock)
			mock.ExpectRollback()

			result, err := repository.CreateDeliveryAtomic(context.Background(), command)
			if result != nil || !errors.Is(err, stageError) {
				t.Fatalf("result/error = %#v/%v, want rollback stage error", result, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryCreateDeliveryAtomicRequiresExistingFinalRender(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, _ := deliveryRepositoryFixture(models.RenderJobStatusProcessing)
	expectDeliveryClaim(mock, command, true)
	mock.ExpectQuery(deliveryJobLockSQL).
		WithArgs(command.BusinessID, command.InvoiceID, models.RenderKindFinal, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectQuery(deliveryInvoiceLockSQL).
		WithArgs(command.InvoiceID, command.BusinessID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "invoice_no", "issued_at", "status", "version",
		}).AddRow(
			invoice.ID, invoice.BusinessID, *invoice.InvoiceNo, *invoice.IssuedAt,
			invoice.Status, invoice.Version,
		))
	mock.ExpectRollback()

	result, err := repository.CreateDeliveryAtomic(context.Background(), command)
	if result != nil || !errors.Is(err, interfaces.ErrInvoiceNotDeliverable) {
		t.Fatalf("result/error = %#v/%v, want not deliverable", result, err)
	}
}

func deliveryRepositoryFixture(status string) (interfaces.AtomicInvoiceDelivery, *models.Invoice, *models.DocumentRenderJob) {
	businessID, invoiceID := uuid.NewString(), uuid.NewString()
	invoiceNumber := "INV/26-27/000001"
	issuedAt := time.Date(2026, time.July, 30, 8, 0, 0, 0, time.UTC)
	version := 3
	command := interfaces.AtomicInvoiceDelivery{
		BusinessID: businessID, InvoiceID: invoiceID, Command: "invoice.delivery.create.v1",
		IdempotencyKey: uuid.NewString(), RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Recipient: "buyer@example.com", ActorID: uuid.NewString(), ActorRole: "accountant",
	}
	invoice := &models.Invoice{
		ID: invoiceID, BusinessID: businessID, InvoiceNo: &invoiceNumber,
		IssuedAt: &issuedAt, Status: models.InvoiceStatusIssued, Version: version,
	}
	job := &models.DocumentRenderJob{
		ID: uuid.NewString(), BusinessID: businessID, InvoiceID: &invoiceID,
		DocumentID: &invoiceID, Kind: models.RenderKindFinal,
		SourceInvoiceVersion: &version, Status: status,
		ObjectKey: "invoices/" + businessID + "/" + invoiceID + "/v3/final.pdf",
	}
	return command, invoice, job
}

func expectDeliveryClaim(mock sqlmock.Sqlmock, command interfaces.AtomicInvoiceDelivery, inserted bool) {
	mock.ExpectBegin()
	rows := sqlmock.NewRows([]string{"id"})
	if inserted {
		rows.AddRow(uuid.NewString())
	}
	mock.ExpectQuery(deliveryClaimSQL).WillReturnRows(rows)
}

func expectDeliveryLocks(
	mock sqlmock.Sqlmock,
	command interfaces.AtomicInvoiceDelivery,
	invoice *models.Invoice,
	job *models.DocumentRenderJob,
) {
	var invoiceNumber interface{}
	if invoice.InvoiceNo != nil {
		invoiceNumber = *invoice.InvoiceNo
	}
	var issuedAt interface{}
	if invoice.IssuedAt != nil {
		issuedAt = *invoice.IssuedAt
	}
	mock.ExpectQuery(deliveryJobLockSQL).
		WithArgs(command.BusinessID, command.InvoiceID, models.RenderKindFinal, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "invoice_id", "document_id", "kind", "source_invoice_version",
			"status", "object_key",
		}).AddRow(
			job.ID, job.BusinessID, *job.InvoiceID, *job.DocumentID, job.Kind,
			*job.SourceInvoiceVersion, job.Status, job.ObjectKey,
		))
	mock.ExpectQuery(deliveryInvoiceLockSQL).
		WithArgs(command.InvoiceID, command.BusinessID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "invoice_no", "issued_at", "status", "version",
		}).AddRow(
			invoice.ID, invoice.BusinessID, invoiceNumber, issuedAt,
			invoice.Status, invoice.Version,
		))
}

func deliveryRows(
	deliveryID string,
	command interfaces.AtomicInvoiceDelivery,
	invoice *models.Invoice,
	job *models.DocumentRenderJob,
	status string,
) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "business_id", "invoice_id", "render_job_id", "recipient", "status",
	}).
		AddRow(deliveryID, command.BusinessID, invoice.ID, job.ID, command.Recipient, status)
}
