package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/invoiceprojection"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	issueStageCount = 8

	issueClaimSQL         = `INSERT INTO "api_idempotency_keys".*"business_id".*"command".*"idempotency_key".*"request_hash".*"status".*ON CONFLICT \("business_id","command","idempotency_key"\) DO NOTHING.*RETURNING "id"`
	issueReplayLookupSQL  = `SELECT \* FROM "api_idempotency_keys".*business_id = \$1 AND command = \$2 AND idempotency_key = \$3.*LIMIT \$4`
	issueReplayInvoiceSQL = `SELECT \* FROM "invoices".*id = \$1 AND business_id = \$2.*LIMIT \$3`
	issueReplayItemsSQL   = `SELECT \* FROM "invoice_items".*"invoice_items"."invoice_id" = \$1.*ORDER BY created_at ASC, id ASC`
	issueReplayRenderSQL  = `SELECT \* FROM "document_render_jobs".*invoice_id = \$1 AND business_id = \$2 AND kind = \$3 AND source_invoice_version = \$4.*LIMIT \$5`

	issueSequenceSQL          = `INSERT INTO document_sequences .*business_id, document_type, financial_year, series, last_number.*ON CONFLICT \(business_id, document_type, financial_year, series\).*DO UPDATE SET last_number = document_sequences.last_number \+ 1, updated_at = NOW\(\).*WHERE document_sequences.last_number < 999999.*RETURNING last_number`
	issueInvoiceUpdateSQL     = `UPDATE "invoices" SET .*"invoice_no".*"status".*"version".*WHERE \(id = \$[0-9]+ AND business_id = \$[0-9]+ AND version = \$[0-9]+ AND status = \$[0-9]+ AND deleted_at IS NULL\).*"invoices"."deleted_at" IS NULL`
	issueDocumentUpdateSQL    = `UPDATE "documents" SET .*"draft_state".*"serial_number".*"source_linkage".*"status".*WHERE \(id = \$[0-9]+ AND business_id = \$[0-9]+ AND status = \$[0-9]+ AND deleted_at IS NULL\).*"documents"."deleted_at" IS NULL`
	issueFinalRenderInsertSQL = `INSERT INTO "document_render_jobs".*"document_id".*"invoice_id".*"business_id".*"kind".*"source_invoice_version".*"object_key".*"output_url".*RETURNING "id"`
	issueFinalRevisionSQL     = `INSERT INTO "document_revisions".*"document_id".*"business_id".*"action".*"snapshot".*"metadata".*RETURNING "id"`
	issueOutboxInsertSQL      = `INSERT INTO "outbox_events".*"business_id".*"aggregate_type".*"aggregate_id".*"event_type".*"payload".*"available_at".*RETURNING "id"`
	issueActivityInsertSQL    = `INSERT INTO "activity_logs".*"business_id".*"actor_id".*"request_id".*"ip_address".*"entity_type".*"entity_id".*"action".*"snapshot".*RETURNING "id"`
	issueCompletionSQL        = `UPDATE "api_idempotency_keys" SET .*"result_id".*"result_type".*"status".*WHERE business_id = \$[0-9]+ AND command = \$[0-9]+ AND idempotency_key = \$[0-9]+ AND request_hash = \$[0-9]+ AND status = \$[0-9]+`
)

func TestNewInvoiceIssueActivitySnapshotsCompleteIssuedInvoice(t *testing.T) {
	command, invoice, _ := strictIssueFixture()
	invoice.Status = models.InvoiceStatusIssued
	invoice.Version++
	invoiceNumber := "INV/26-27/000001"
	renderID := uuid.NewString()

	activity := newInvoiceIssueActivity(command, invoice, invoiceNumber, renderID)

	var snapshot models.Invoice
	if err := json.Unmarshal([]byte(activity.Snapshot), &snapshot); err != nil {
		t.Fatalf("decode activity snapshot: %v", err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != invoice.Items[0].ID {
		t.Fatalf("activity snapshot items = %#v, want complete issued invoice items", snapshot.Items)
	}
	if activity.RequestID != command.RequestID || activity.IPAddress != command.IPAddress {
		t.Fatalf("activity request metadata = %#v", activity)
	}
}

func TestNewInvoiceFinalRenderRevisionFreezesIssuedDocumentAndLines(t *testing.T) {
	_, invoice, document := strictIssueFixture()
	renderJobID := uuid.NewString()
	sourceVersion := invoice.Version + 1
	document.Status = models.DocumentStatusIssued
	document.DraftState = models.DocumentDraftStateFinal
	document.SerialNumber = "INV/26-27/000001"
	document.PaidAmount = 0
	document.BalanceDue = 100
	document.SourceLinkage = `{"seller_snapshot":{"name":"Frozen Seller"},"buyer_snapshot":{"name":"Frozen Buyer"}}`
	job := &models.DocumentRenderJob{
		ID:                   renderJobID,
		BusinessID:           document.BusinessID,
		DocumentID:           models.StringPointer(document.ID),
		InvoiceID:            models.StringPointer(document.ID),
		Kind:                 models.RenderKindFinal,
		SourceInvoiceVersion: &sourceVersion,
	}

	revision, err := newInvoiceFinalRenderRevision(document, job)
	if err != nil {
		t.Fatalf("build final render revision: %v", err)
	}

	document.Status = models.DocumentStatusSent
	document.PaidAmount = 100
	document.BalanceDue = 0
	document.Lines[0].Description = "mutated after issue"
	now := time.Now()
	document.SignedAt = &now

	var frozen models.Document
	if err := json.Unmarshal([]byte(revision.Snapshot), &frozen); err != nil {
		t.Fatalf("decode frozen document: %v", err)
	}
	if frozen.Status != models.DocumentStatusIssued || frozen.PaidAmount != 0 ||
		frozen.BalanceDue != 100 || frozen.SignedAt != nil ||
		len(frozen.Lines) != 1 || frozen.Lines[0].Description != "Canonical item" {
		t.Fatalf("frozen render input changed with live document: %#v", frozen)
	}
	var metadata struct {
		RenderJobID          string `json:"render_job_id"`
		SourceInvoiceVersion int    `json:"source_invoice_version"`
	}
	if err := json.Unmarshal([]byte(revision.Metadata), &metadata); err != nil {
		t.Fatalf("decode revision metadata: %v", err)
	}
	if revision.Action != "final_render_snapshot" ||
		metadata.RenderJobID != renderJobID ||
		metadata.SourceInvoiceVersion != sourceVersion {
		t.Fatalf("revision identity = %#v metadata=%#v", revision, metadata)
	}
}

func TestInvoiceRepositoryIssueDraftAtomicRejectsDriftedProjectionBeforeSequence(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, document := strictIssueFixture()
	document.Total = 101

	expectStrictIssueClaim(mock)
	expectStrictLockedInvoice(t, mock, invoice)
	expectStrictInvoiceItems(mock, invoice.Items)
	mock.ExpectQuery(`SELECT "timezone" FROM "business_profiles".*id = \$1 AND deleted_at IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow("Asia/Kolkata"))
	expectStrictLockedDocument(mock, document)
	expectStrictDocumentLines(mock, document.Lines)
	mock.ExpectRollback()

	result, err := repository.IssueDraftAtomic(context.Background(), command)

	var lifecycle *invoiceissue.InvalidLifecycleError
	if result != nil || !errors.As(err, &lifecycle) ||
		!strings.Contains(lifecycle.Error(), "projection") {
		t.Fatalf("result/error = %#v/%T %v, want projection lifecycle error", result, err, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryIssueDraftAtomicPersistsOneLegalResult(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, document := strictIssueFixture()

	defaultProfileID := expectStrictIssueTransactionPrefix(t, mock, invoice, document)
	for stage := 0; stage < issueStageCount; stage++ {
		expectSuccessfulIssueStage(mock, stage)
	}
	mock.ExpectCommit()

	result, err := repository.IssueDraftAtomic(context.Background(), command)

	if err != nil {
		t.Fatalf("issue draft: %v", err)
	}
	if result == nil || result.Invoice == nil || result.FinalRender == nil || result.OutboxEvent == nil {
		t.Fatalf("result = %#v, want invoice, render, and new outbox event", result)
	}
	if len(result.Invoice.Items) != 1 || result.Invoice.Items[0].ID != invoice.Items[0].ID {
		t.Fatalf("first issue items = %#v, want locked canonical items", result.Invoice.Items)
	}
	if result.Invoice.Version != command.ExpectedVersion+1 || result.Invoice.Status != models.InvoiceStatusIssued ||
		result.Invoice.InvoiceNo == nil || *result.Invoice.InvoiceNo != "INV/26-27/000001" ||
		result.Invoice.IssuedAt == nil {
		t.Fatalf("issued invoice = %#v", result.Invoice)
	}
	if result.FinalRender.Kind != models.RenderKindFinal ||
		result.FinalRender.SourceInvoiceVersion == nil ||
		*result.FinalRender.SourceInvoiceVersion != command.ExpectedVersion+1 ||
		result.FinalRender.OutputURL != "" ||
		result.FinalRender.RenderProfileID == nil ||
		*result.FinalRender.RenderProfileID != defaultProfileID ||
		!strings.Contains(result.FinalRender.ObjectKey, command.BusinessID) ||
		!strings.Contains(result.FinalRender.ObjectKey, command.InvoiceID) {
		t.Fatalf("final render = %#v", result.FinalRender)
	}
	if result.OutboxEvent.EventType != "invoice.issued.v1" ||
		result.OutboxEvent.AggregateID != command.InvoiceID {
		t.Fatalf("issued outbox event = %#v", result.OutboxEvent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryIssueDraftAtomicReturnsTypedLifecycleErrors(t *testing.T) {
	fixtures := []struct {
		name    string
		version int
		status  string
		seller  string
		buyer   string
		assert  func(error) bool
	}{
		{
			name: "stale", version: 2, status: models.InvoiceStatusDraft, seller: `{"name":"Seller"}`, buyer: `{"name":"Buyer"}`,
			assert: func(err error) bool { var target *invoiceissue.StaleVersionError; return errors.As(err, &target) },
		},
		{
			name: "already issued", version: 1, status: models.InvoiceStatusIssued, seller: `{"name":"Seller"}`, buyer: `{"name":"Buyer"}`,
			assert: func(err error) bool { var target *invoiceissue.AlreadyIssuedError; return errors.As(err, &target) },
		},
		{
			name: "missing seller snapshot", version: 1, status: models.InvoiceStatusDraft, seller: `{}`, buyer: `{"name":"Buyer"}`,
			assert: func(err error) bool { var target *invoiceissue.InvalidLifecycleError; return errors.As(err, &target) },
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			command, invoice, _ := strictIssueFixture()
			invoice.Version = fixture.version
			invoice.Status = fixture.status
			invoice.SellerSnapshot = models.PartySnapshot{}
			invoice.BuyerSnapshot = models.PartySnapshot{}
			_ = json.Unmarshal([]byte(fixture.seller), &invoice.SellerSnapshot)
			_ = json.Unmarshal([]byte(fixture.buyer), &invoice.BuyerSnapshot)
			expectStrictIssueClaim(mock)
			expectStrictLockedInvoice(t, mock, invoice)
			mock.ExpectRollback()

			result, err := repository.IssueDraftAtomic(context.Background(), command)

			if result != nil || err == nil || !fixture.assert(err) {
				t.Fatalf("result/error = %#v/%T %v, want typed lifecycle error", result, err, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryIssueDraftAtomicValidatesBeforeClaim(t *testing.T) {
	repository, mock, closeDatabase := newAtomicSQLMockRepository(t)
	defer closeDatabase()
	command := issueRepositoryTestCommand()
	command.ActorID = ""

	result, err := repository.IssueDraftAtomic(context.Background(), command)

	if result != nil || err == nil {
		t.Fatalf("result/error = %#v/%v, want validation error", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL before validation: %v", err)
	}
}

func TestInvoiceRepositoryIssueDraftAtomicReplaysCompletedResultAndRejectsChangedPayload(t *testing.T) {
	t.Run("replay", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command := issueRepositoryTestCommand()
		resultType := "invoice_issue"
		issuedVersion := command.ExpectedVersion + 1
		itemID := uuid.NewString()

		mock.ExpectBegin()
		mock.ExpectQuery(issueClaimSQL).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery(issueReplayLookupSQL).WillReturnRows(sqlmock.NewRows([]string{
			"business_id", "command", "idempotency_key", "request_hash", "status", "result_type", "result_id",
		}).AddRow(
			command.BusinessID, command.Command, command.IdempotencyKey, command.RequestHash,
			models.IdempotencyStatusCompleted, resultType, command.InvoiceID,
		))
		mock.ExpectCommit()
		mock.ExpectQuery(issueReplayInvoiceSQL).WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "version", "status",
		}).AddRow(command.InvoiceID, command.BusinessID, issuedVersion, models.InvoiceStatusIssued))
		mock.ExpectQuery(issueReplayItemsSQL).WillReturnRows(sqlmock.NewRows([]string{
			"id", "invoice_id", "description",
		}).AddRow(itemID, command.InvoiceID, "Canonical item"))
		mock.ExpectQuery(issueReplayRenderSQL).WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "invoice_id", "kind", "source_invoice_version",
		}).AddRow(uuid.NewString(), command.BusinessID, command.InvoiceID, models.RenderKindFinal, issuedVersion))

		result, err := repository.IssueDraftAtomic(context.Background(), command)

		if err != nil || result == nil || !result.Replayed || result.OutboxEvent != nil ||
			result.Invoice == nil || result.Invoice.ID != command.InvoiceID ||
			len(result.Invoice.Items) != 1 || result.Invoice.Items[0].ID != itemID ||
			result.FinalRender == nil || result.FinalRender.SourceInvoiceVersion == nil ||
			*result.FinalRender.SourceInvoiceVersion != issuedVersion {
			t.Fatalf("replay result/error = %#v/%v", result, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		command := issueRepositoryTestCommand()
		mock.ExpectBegin()
		mock.ExpectQuery(issueClaimSQL).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery(issueReplayLookupSQL).WillReturnRows(sqlmock.NewRows([]string{
			"business_id", "command", "idempotency_key", "request_hash", "status",
		}).AddRow(
			command.BusinessID, command.Command, command.IdempotencyKey, strings.Repeat("b", 64),
			models.IdempotencyStatusCompleted,
		))
		mock.ExpectRollback()

		result, err := repository.IssueDraftAtomic(context.Background(), command)
		var conflict *idempotency.ConflictError
		if result != nil || err == nil || !errors.As(err, &conflict) {
			t.Fatalf("conflict result/error = %#v/%T %v", result, err, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL expectations: %v", err)
		}
	})
}

func TestInvoiceRepositoryIssueDraftAtomicRollsBackEveryIssuanceStage(t *testing.T) {
	stages := []struct {
		name      string
		queryStep bool
	}{
		{name: "sequence allocation", queryStep: true},
		{name: "invoice update"},
		{name: "document projection"},
		{name: "final render", queryStep: true},
		{name: "final render snapshot", queryStep: true},
		{name: "outbox event", queryStep: true},
		{name: "activity", queryStep: true},
		{name: "idempotency completion"},
	}

	for failedIndex, failedStage := range stages {
		t.Run(failedStage.name, func(t *testing.T) {
			repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			command, invoice, document := strictIssueFixture()
			expectStrictIssueTransactionPrefix(t, mock, invoice, document)
			for index := 0; index < failedIndex; index++ {
				expectSuccessfulIssueStage(mock, index)
			}
			expectFailedIssueStage(mock, failedIndex, failedStage.queryStep)
			mock.ExpectRollback()

			result, err := repository.IssueDraftAtomic(context.Background(), command)

			if result != nil || err == nil ||
				!strings.Contains(err.Error(), "atomic invoice issue persistence failed") {
				t.Fatalf("result/error = %#v/%v, want sanitized issue failure", result, err)
			}
			if strings.Contains(err.Error(), "injected issue failure") {
				t.Fatalf("raw database error leaked: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestInvoiceRepositoryIssueDraftAtomicDoesNotReturnCommitFailureAsSuccess(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, document := strictIssueFixture()
	expectStrictIssueTransactionPrefix(t, mock, invoice, document)
	for index := 0; index < issueStageCount; index++ {
		expectSuccessfulIssueStage(mock, index)
	}
	mock.ExpectCommit().WillReturnError(errors.New("injected commit failure"))

	result, err := repository.IssueDraftAtomic(context.Background(), command)

	if result != nil || err == nil || !strings.Contains(err.Error(), "transaction commit") {
		t.Fatalf("result/error = %#v/%v, want commit failure", result, err)
	}
	if strings.Contains(err.Error(), "injected commit failure") {
		t.Fatalf("raw database error leaked: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInvoiceRepositoryIssueDraftAtomicReturnsTypedSequenceExhaustion(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	command, invoice, document := strictIssueFixture()
	expectStrictIssueTransactionPrefix(t, mock, invoice, document)
	mock.ExpectQuery(issueSequenceSQL).WillReturnRows(sqlmock.NewRows([]string{"last_number"}))
	mock.ExpectRollback()

	result, err := repository.IssueDraftAtomic(context.Background(), command)
	var exhausted *invoiceissue.SequenceExhaustedError
	if result != nil || !errors.As(err, &exhausted) {
		t.Fatalf("result/error = %#v/%T %v, want typed exhaustion", result, err, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func expectSuccessfulIssueStage(mock sqlmock.Sqlmock, index int) {
	switch index {
	case 0:
		mock.ExpectQuery(issueSequenceSQL).WillReturnRows(sqlmock.NewRows([]string{"last_number"}).AddRow(1))
	case 1:
		mock.ExpectExec(issueInvoiceUpdateSQL).WillReturnResult(sqlmock.NewResult(0, 1))
	case 2:
		mock.ExpectExec(issueDocumentUpdateSQL).WillReturnResult(sqlmock.NewResult(0, 1))
	case 3:
		mock.ExpectQuery(issueFinalRenderInsertSQL).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	case 4:
		mock.ExpectQuery(issueFinalRevisionSQL).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	case 5:
		mock.ExpectQuery(issueOutboxInsertSQL).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	case 6:
		mock.ExpectQuery(issueActivityInsertSQL).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
	case 7:
		mock.ExpectExec(issueCompletionSQL).WillReturnResult(sqlmock.NewResult(0, 1))
	}
}

func expectFailedIssueStage(mock sqlmock.Sqlmock, index int, queryStep bool) {
	patterns := []string{
		issueSequenceSQL,
		issueInvoiceUpdateSQL,
		issueDocumentUpdateSQL,
		issueFinalRenderInsertSQL,
		issueFinalRevisionSQL,
		issueOutboxInsertSQL,
		issueActivityInsertSQL,
		issueCompletionSQL,
	}
	if queryStep {
		mock.ExpectQuery(patterns[index]).WillReturnError(errors.New("injected issue failure"))
		return
	}
	mock.ExpectExec(patterns[index]).WillReturnError(errors.New("injected issue failure"))
}

func issueRepositoryTestCommand() interfaces.AtomicInvoiceIssue {
	return interfaces.AtomicInvoiceIssue{
		BusinessID:      uuid.NewString(),
		InvoiceID:       uuid.NewString(),
		Command:         "invoice.issue",
		IdempotencyKey:  uuid.NewString(),
		RequestHash:     strings.Repeat("a", 64),
		ExpectedVersion: 1,
		DocumentType:    invoiceissue.DocumentTypeTaxInvoice,
		Series:          "INV",
		ActorID:         uuid.NewString(),
		ActorRole:       "accountant",
		RequestID:       "request-issue",
		IPAddress:       "127.0.0.1",
	}
}

func newStrictIssueSQLMockRepository(t *testing.T) (*invoiceRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDatabase, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open strict issue sqlmock: %v", err)
	}
	gormDatabase, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDatabase}), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		_ = sqlDatabase.Close()
		t.Fatalf("open strict issue gorm adapter: %v", err)
	}
	return &invoiceRepository{db: gormDatabase}, mock, func() {
		mock.ExpectClose()
		if err := sqlDatabase.Close(); err != nil {
			t.Errorf("close strict issue sqlmock: %v", err)
		}
	}
}

func strictIssueFixture() (interfaces.AtomicInvoiceIssue, *models.Invoice, *models.Document) {
	command := issueRepositoryTestCommand()
	customerID := uuid.NewString()
	itemID := uuid.NewString()
	invoiceDate := time.Date(2026, time.April, 1, 0, 30, 0, 0, time.UTC)
	invoice := &models.Invoice{
		ID: command.InvoiceID, BusinessID: command.BusinessID, CustomerID: &customerID,
		Version: 1, Status: models.InvoiceStatusDraft, Origin: models.InvoiceOriginManual,
		SellerSnapshot: models.PartySnapshot{Name: "Seller"},
		BuyerSnapshot:  models.PartySnapshot{Name: "Buyer"},
		InvoiceDate:    invoiceDate, DueDate: invoiceDate.AddDate(0, 0, 30),
		Currency: "INR", Subtotal: 100, Total: 100, BalanceDue: 100,
		CustomFields: "{}", AdditionalCharges: "[]", TaxProfile: "{}",
		Items: []*models.InvoiceItem{{
			ID: itemID, InvoiceID: command.InvoiceID, Description: "Canonical item",
			Unit: "NOS", Quantity: 1, UnitPrice: 100, Total: 100,
			CustomFields: "{}", ChargeSnapshot: "[]", BatchAllocations: "[]", SerialIDs: "[]",
		}},
	}
	return command, invoice, invoiceprojection.Build(invoice)
}

func expectStrictIssueClaim(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectQuery(issueClaimSQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.NewString()))
}

func expectStrictIssueTransactionPrefix(
	t *testing.T,
	mock sqlmock.Sqlmock,
	invoice *models.Invoice,
	document *models.Document,
) string {
	t.Helper()
	expectStrictIssueClaim(mock)
	expectStrictLockedInvoice(t, mock, invoice)
	expectStrictInvoiceItems(mock, invoice.Items)
	mock.ExpectQuery(`SELECT "timezone" FROM "business_profiles".*id = \$1 AND deleted_at IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow("Asia/Kolkata"))
	expectStrictLockedDocument(mock, document)
	expectStrictDocumentLines(mock, document.Lines)
	defaultProfileID := uuid.NewString()
	mock.ExpectQuery(`SELECT \* FROM "render_profiles".*business_id = \$1 AND is_default = \$2 AND deleted_at IS NULL.*LIMIT \$3`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(defaultProfileID))
	return defaultProfileID
}

func expectStrictLockedInvoice(t *testing.T, mock sqlmock.Sqlmock, invoice *models.Invoice) {
	mock.ExpectQuery(`SELECT \* FROM "invoices".*id = \$1 AND business_id = \$2 AND deleted_at IS NULL.*FOR UPDATE`).
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

func mustJSONTest(t *testing.T, value interface{}) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal SQL fixture: %v", err)
	}
	return string(encoded)
}

func expectStrictInvoiceItems(mock sqlmock.Sqlmock, items []*models.InvoiceItem) {
	rows := sqlmock.NewRows([]string{
		"id", "invoice_id", "description", "unit", "quantity", "unit_price", "discount",
		"tax_rate", "cess_rate", "cess_amount", "custom_fields", "charge_snapshot",
		"batch_allocations", "serial_ids", "total",
	})
	for _, item := range items {
		rows.AddRow(
			item.ID, item.InvoiceID, item.Description, item.Unit, item.Quantity, item.UnitPrice, item.Discount,
			item.TaxRate, item.CessRate, item.CessAmount, item.CustomFields, item.ChargeSnapshot,
			item.BatchAllocations, item.SerialIDs, item.Total,
		)
	}
	mock.ExpectQuery(`SELECT \* FROM "invoice_items".*invoice_id = \$1.*ORDER BY created_at ASC, id ASC.*FOR UPDATE`).
		WillReturnRows(rows)
}

func expectStrictLockedDocument(mock sqlmock.Sqlmock, document *models.Document) {
	mock.ExpectQuery(`SELECT \* FROM "documents".*id = \$1 AND business_id = \$2 AND deleted_at IS NULL.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "business_id", "document_type", "party_type", "party_id", "status", "draft_state",
			"tax_mode", "gst_treatment", "bill_of_supply", "serial_number", "issue_date", "due_date",
			"currency", "exchange_rate", "locale", "source_linkage", "profit_snapshot_enabled",
			"notes", "direction", "subtotal", "discount_total", "tax_total", "cess_total",
			"withholding_total", "tds_total", "tcs_total", "total", "paid_amount", "balance_due",
			"dispatch_from", "dispatch_to", "transporter", "vehicle", "multi_vehicle_plan",
			"extra_fields", "report_tags",
		}).AddRow(
			document.ID, document.BusinessID, document.DocumentType, document.PartyType, document.PartyID,
			document.Status, document.DraftState, document.TaxMode, document.GSTTreatment,
			document.BillOfSupply, document.SerialNumber, document.IssueDate, document.DueDate,
			document.Currency, document.ExchangeRate, document.Locale, document.SourceLinkage,
			document.ProfitSnapshotEnabled, document.Notes, document.Direction, document.Subtotal,
			document.DiscountTotal, document.TaxTotal, document.CessTotal, document.WithholdingTotal,
			document.TDSTotal, document.TCSTotal, document.Total, document.PaidAmount, document.BalanceDue,
			document.DispatchFrom, document.DispatchTo, document.Transporter, document.Vehicle,
			document.MultiVehiclePlan, document.ExtraFields, document.ReportTags,
		))
}

func expectStrictDocumentLines(mock sqlmock.Sqlmock, lines []*models.DocumentLine) {
	rows := sqlmock.NewRows([]string{
		"id", "document_id", "description", "hsn_sac_code", "uqc_code", "unit", "quantity",
		"free_quantity", "remaining_quantity", "unit_price", "mrp", "discount_amount",
		"tax_rate", "cgst_rate", "sgst_rate", "igst_rate", "cess_rate", "cgst_amount",
		"sgst_amount", "igst_amount", "cess_amount", "tax_amount", "line_subtotal",
		"line_total", "cost_snapshot", "margin_snapshot", "custom_fields", "charge_linkage",
		"packing_metadata", "batch_allocations", "serial_ids", "report_tags", "stock_effect",
	})
	for _, line := range lines {
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
	mock.ExpectQuery(`SELECT \* FROM "document_lines".*document_id = \$1.*ORDER BY created_at ASC, id ASC.*FOR UPDATE`).
		WillReturnRows(rows)
}
