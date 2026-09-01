package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/outbox"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOperationRepositoryAggregatesExistingDomainOwnersWithoutCrossTenantLeakage(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range operationFixtureSchemas() {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("create operation fixture schema: %v", err)
		}
	}

	now := time.Date(2026, time.September, 1, 11, 0, 0, 0, time.UTC)
	businessA := uuid.NewString()
	businessB := uuid.NewString()
	invoiceID := uuid.NewString()
	renderID := uuid.NewString()
	render := models.DocumentRenderJob{
		ID: renderID, BusinessID: businessA, InvoiceID: &invoiceID, Kind: models.RenderKindFinal,
		Status: models.RenderJobStatusFailed, Attempts: 2, ErrorMessage: "raw renderer stack",
		ObjectKey: "invoices/secret/internal.pdf", RequestedAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}
	deliveryRenderID := renderID
	delivery := models.EmailDelivery{
		ID: uuid.NewString(), BusinessID: businessA, InvoiceID: &invoiceID, RenderJobID: &deliveryRenderID,
		Status: models.EmailDeliveryStatusSent, ProviderMessageID: "ses-secret-message", Attempts: 1,
		CreatedAt: now.Add(-50 * time.Minute), UpdatedAt: now.Add(-40 * time.Minute),
	}
	accountID := uuid.NewString()
	standaloneEmail := models.EmailDelivery{
		ID: uuid.NewString(), BusinessID: businessA, EmailAccountID: &accountID, Status: models.EmailDeliveryStatusFailed,
		ProviderMessageID: "provider-secret", ErrorMessage: "recipient raw detail", Attempts: 2,
		CreatedAt: now.Add(-39 * time.Minute), UpdatedAt: now.Add(-38 * time.Minute),
	}
	outbox := models.OutboxEvent{
		ID: uuid.NewString(), BusinessID: businessA, AggregateType: "document_render_job", AggregateID: renderID,
		EventType: "invoice.render.requested.v1", Payload: `{"raw":"forbidden"}`, PublishAttempts: 1,
		AvailableAt: now.Add(-time.Minute), CreatedAt: now.Add(-37 * time.Minute),
	}
	razorpay := models.RazorpayWebhookEvent{
		ID: uuid.NewString(), RazorpayEventID: "provider-event-secret", ProviderMode: "test", EventType: "subscription.charged",
		PayloadHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SignatureVerified: true,
		ReceivedAt: now.Add(-36 * time.Minute), ProcessingStatus: "reconciliation_required", AttemptCount: 2,
		SanitizedErrorCode: "provider_state_ambiguous", BusinessID: businessA, CreatedAt: now.Add(-36 * time.Minute),
	}
	einvoice := models.GSTSubmissionJob{
		ID: uuid.NewString(), BusinessID: businessA, DocumentID: uuid.NewString(), Operation: models.GSTOperationGenerateEInvoice,
		Status: models.GSTJobStatusNeedsAttention, IdempotencyKey: uuid.NewString(), QueueMessageID: "queue-secret",
		AttemptCount: 3, ErrorClass: models.GSTErrorClassUnknown, LastError: "raw GST error", CreatedAt: now.Add(-35 * time.Minute), UpdatedAt: now.Add(-34 * time.Minute),
	}
	eway := models.GSTSubmissionJob{
		ID: uuid.NewString(), BusinessID: businessA, DocumentID: uuid.NewString(), Operation: models.GSTOperationGenerateEWayBill,
		Status: models.GSTJobStatusSucceeded, IdempotencyKey: uuid.NewString(), AttemptCount: 1,
		CreatedAt: now.Add(-33 * time.Minute), UpdatedAt: now.Add(-32 * time.Minute),
	}
	recurring := models.InvoiceSubscriptionRun{
		ID: uuid.NewString(), SubscriptionID: uuid.NewString(), BusinessID: businessA, ScheduledFor: now.Add(-31 * time.Minute),
		Status: "completed", IdempotencyKey: uuid.NewString(), AttemptCount: 1, CreatedAt: now.Add(-31 * time.Minute), UpdatedAt: now.Add(-30 * time.Minute),
	}
	whatsapp := models.NotificationDelivery{
		ID: uuid.NewString(), BusinessID: businessA, Channel: models.NotificationChannelWhatsApp, EventKey: "invoice.ready",
		Status: "failed", RequestPayload: `{"phone":"secret"}`, ResponsePayload: `{"provider":"secret"}`,
		ExternalMessageID: "external-secret", AttemptCount: 1, CreatedAt: now.Add(-29 * time.Minute), UpdatedAt: now.Add(-28 * time.Minute),
	}
	notificationDelivery := models.NotificationDelivery{
		ID: uuid.NewString(), BusinessID: businessA, Channel: "push", EventKey: "invoice.ready", Status: "delivered",
		CreatedAt: now.Add(-27 * time.Minute), UpdatedAt: now.Add(-26 * time.Minute),
	}
	notification := models.Notification{
		ID: uuid.NewString(), BusinessID: businessA, UserID: "user-secret", SourceEventKey: "source-secret",
		Type: "invoice", Title: "Ready", Body: "body", ResourceType: "invoice", ResourceID: invoiceID,
		CreatedAt: now.Add(-25 * time.Minute), UpdatedAt: now.Add(-24 * time.Minute),
	}
	importJob := models.BulkJob{
		ID: uuid.NewString(), BusinessID: businessA, CreatedBy: uuid.NewString(), JobType: models.BulkJobTypeImportCustomers,
		Status: models.BulkJobStatusFailed, FileKey: "private/import.csv", RequestPayload: `{"raw":"secret"}`,
		CreatedAt: now.Add(-23 * time.Minute), UpdatedAt: now.Add(-22 * time.Minute),
	}
	foreign := models.DocumentRenderJob{
		ID: uuid.NewString(), BusinessID: businessB, Kind: models.RenderKindPreview, Status: models.RenderJobStatusCompleted,
		RequestedAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
	}
	newerRender := models.DocumentRenderJob{
		ID: uuid.NewString(), BusinessID: businessA, Kind: models.RenderKindPreview,
		Status: models.RenderJobStatusCompleted, RequestedAt: now.Add(30 * time.Second),
		CreatedAt: now.Add(30 * time.Second), UpdatedAt: now.Add(30 * time.Second),
	}
	for _, value := range []any{
		&render, &delivery, &standaloneEmail, &outbox, &razorpay, &einvoice, &eway, &recurring,
		&whatsapp, &notificationDelivery, &notification, &importJob, &foreign, &newerRender,
	} {
		if err := createOperationFixture(database, value); err != nil {
			t.Fatalf("insert %T: %v", value, err)
		}
	}

	repository := NewOperationRepository(database)
	page, err := repository.ListOperations(context.Background(), businessA, interfaces.OperationRecordQuery{
		Limit: 100, SnapshotAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ListOperations() error = %v", err)
	}
	wantTypes := map[string]bool{
		"invoice_render": false, "invoice_delivery": false, "outbox": false, "razorpay_webhook": false,
		"gst_einvoice": false, "gst_ewaybill": false, "recurring_invoice": false, "email_delivery": false,
		"whatsapp_delivery": false, "notification": false, "import": false,
	}
	for _, record := range page.Records {
		if record.ID == foreign.ID {
			t.Fatal("cross-tenant render leaked")
		}
		if _, exists := wantTypes[record.Type]; exists {
			wantTypes[record.Type] = true
		}
		for _, leaked := range []string{"secret", "raw", "stack", "private"} {
			if record.ErrorCode == leaked || record.CorrelationID == leaked {
				t.Fatalf("record leaked source value: %#v", record)
			}
		}
		if record.ID == renderID && record.Retryable {
			t.Fatal("an unproven render replay must not be advertised as retryable")
		}
	}
	for operationType, found := range wantTypes {
		if !found {
			t.Errorf("operation type %q missing from %#v", operationType, page.Records)
		}
	}
	if len(page.UnavailableTypes) != 1 || page.UnavailableTypes[0] != "voice_reconciliation" {
		t.Fatalf("unavailable types = %#v", page.UnavailableTypes)
	}

	if _, err := repository.GetOperation(context.Background(), businessB, "invoice_render", renderID); err != interfaces.ErrOperationNotFound {
		t.Fatalf("cross-tenant GetOperation() error = %v", err)
	}
	got, err := repository.GetOperation(context.Background(), businessA, "invoice_render", renderID)
	if err != nil || got.ID != renderID {
		t.Fatalf("older exact GetOperation() = %#v, %v", got, err)
	}
}

func TestOperationRepositoryPreservesCancellationAndStableCursorOrder(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range operationFixtureSchemas() {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("create operation fixture schema: %v", err)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewOperationRepository(database).ListOperations(cancelled, uuid.NewString(), interfaces.OperationRecordQuery{
		Types: []string{operationTypeInvoiceRender}, Limit: 10, SnapshotAt: time.Now().UTC(),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ListOperations() error = %v", err)
	}

	now := time.Now().UTC()
	query := interfaces.OperationRecordQuery{
		AfterUpdatedAt: &now, AfterType: operationTypeEmailDelivery, AfterID: "b",
	}
	for _, test := range []struct {
		name   string
		record interfaces.OperationRecord
		want   bool
	}{
		{name: "later type at same instant", record: interfaces.OperationRecord{Type: operationTypeInvoiceRender, ID: "a", UpdatedAt: now}, want: true},
		{name: "later id at same instant", record: interfaces.OperationRecord{Type: operationTypeEmailDelivery, ID: "c", UpdatedAt: now}, want: true},
		{name: "cursor itself", record: interfaces.OperationRecord{Type: operationTypeEmailDelivery, ID: "b", UpdatedAt: now}, want: false},
		{name: "earlier type at same instant", record: interfaces.OperationRecord{Type: operationTypeEmailDelivery, ID: "a", UpdatedAt: now}, want: false},
		{name: "newer instant", record: interfaces.OperationRecord{Type: operationTypeImport, ID: "z", UpdatedAt: now.Add(time.Second)}, want: false},
		{name: "older instant", record: interfaces.OperationRecord{Type: operationTypeEmailDelivery, ID: "a", UpdatedAt: now.Add(-time.Second)}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := operationRecordMatches(query, test.record); got != test.want {
				t.Fatalf("operationRecordMatches() = %t, want %t", got, test.want)
			}
		})
	}
	if operationRecordMatches(
		interfaces.OperationRecordQuery{SnapshotAt: now},
		interfaces.OperationRecord{Type: operationTypeImport, ID: "future", UpdatedAt: now.Add(time.Second)},
	) {
		t.Fatal("record updated after the immutable snapshot was included")
	}

	businessID := uuid.NewString()
	now = time.Now().UTC()
	for index, status := range []string{models.RenderJobStatusCompleted, models.RenderJobStatusCompleted, models.RenderJobStatusFailed} {
		job := models.DocumentRenderJob{
			ID: uuid.NewString(), BusinessID: businessID, Kind: models.RenderKindPreview, Status: status,
			RequestedAt: now.Add(-time.Duration(index) * time.Minute), CreatedAt: now.Add(-time.Duration(index) * time.Minute),
			UpdatedAt: now.Add(-time.Duration(index) * time.Minute),
		}
		if err := createOperationFixture(database, &job); err != nil {
			t.Fatalf("insert filtered render %d: %v", index, err)
		}
	}
	filtered, err := NewOperationRepository(database).ListOperations(context.Background(), businessID, interfaces.OperationRecordQuery{
		Types: []string{operationTypeInvoiceRender}, Statuses: []string{"failed"}, Limit: 2, SnapshotAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("filtered ListOperations() error = %v", err)
	}
	if len(filtered.Records) != 1 || filtered.Records[0].InternalStatus != models.RenderJobStatusFailed {
		t.Fatalf("filtered operations were truncated before filtering: %#v", filtered.Records)
	}

	compositeBusinessID := uuid.NewString()
	invoiceID := uuid.NewString()
	renderID := uuid.NewString()
	for _, delivery := range []models.EmailDelivery{
		{ID: "00000000-0000-4000-8000-000000000001", BusinessID: compositeBusinessID, Status: models.EmailDeliveryStatusSent, CreatedAt: now, UpdatedAt: now},
		{ID: "ffffffff-ffff-4fff-8fff-ffffffffffff", BusinessID: compositeBusinessID, InvoiceID: &invoiceID, RenderJobID: &renderID, Status: models.EmailDeliveryStatusSent, CreatedAt: now, UpdatedAt: now},
	} {
		if err := createOperationFixture(database, &delivery); err != nil {
			t.Fatalf("insert composite delivery: %v", err)
		}
	}
	compositePage, err := NewOperationRepository(database).ListOperations(context.Background(), compositeBusinessID, interfaces.OperationRecordQuery{
		Types: []string{operationTypeEmailDelivery, operationTypeInvoiceDelivery}, Limit: 1, SnapshotAt: now.Add(time.Minute),
		AfterUpdatedAt: &now, AfterType: operationTypeEmailDelivery, AfterID: "80000000-0000-4000-8000-000000000000",
	})
	if err != nil {
		t.Fatalf("composite ListOperations() error = %v", err)
	}
	if len(compositePage.Records) != 1 || compositePage.Records[0].Type != operationTypeInvoiceDelivery {
		t.Fatalf("composite operation cursor was truncated before type normalization: %#v", compositePage.Records)
	}
}

func TestOperationRecoveryUniqueConstraintClassification(t *testing.T) {
	if isOperationRecoveryUniqueViolation(errors.New("unrelated")) {
		t.Fatal("unrelated error classified as recovery uniqueness race")
	}
	if !isOperationRecoveryUniqueViolation(gorm.ErrDuplicatedKey) {
		t.Fatal("GORM duplicate key was not classified as recovery uniqueness race")
	}
	if !isOperationRecoveryUniqueViolation(&pq.Error{Code: "23505"}) {
		t.Fatal("PostgreSQL unique violation was not classified as recovery uniqueness race")
	}
}

func TestOperationRepositoryRenderRecoveryIsAuditedIdempotentAndRevisionBound(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE document_render_jobs (id TEXT PRIMARY KEY, business_id TEXT, document_id TEXT, invoice_id TEXT, kind TEXT, source_invoice_version INTEGER, status TEXT, attempts INTEGER, object_key TEXT, requested_at DATETIME, created_at DATETIME, updated_at DATETIME, completed_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE invoices (id TEXT PRIMARY KEY, business_id TEXT, invoice_no TEXT, status TEXT, version INTEGER, issued_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE outbox_events (id TEXT PRIMARY KEY, business_id TEXT, aggregate_type TEXT, aggregate_id TEXT, event_type TEXT, payload TEXT, publish_attempts INTEGER, available_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE operation_recovery_commands (id TEXT PRIMARY KEY, business_id TEXT, operation_type TEXT, operation_id TEXT, actor_subject TEXT, principal_kind TEXT, action TEXT, reason TEXT, idempotency_key TEXT, request_hash TEXT, operation_version TEXT, correlation_id TEXT, status TEXT, result_code TEXT, created_at DATETIME, completed_at DATETIME, UNIQUE (business_id, actor_subject, action, idempotency_key))`,
		`CREATE UNIQUE INDEX ux_operation_recovery_effect ON operation_recovery_commands (business_id, operation_type, operation_id, action, operation_version) WHERE result_code = 'accepted'`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("create recovery fixture schema: %v", err)
		}
	}
	now := time.Date(2026, time.September, 1, 13, 0, 0, 0, time.UTC)
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	renderID := uuid.NewString()
	version := 7
	if err := database.Exec(
		`INSERT INTO document_render_jobs (id,business_id,document_id,invoice_id,kind,source_invoice_version,status,attempts,object_key,requested_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		renderID, businessID, invoiceID, invoiceID, string(models.RenderKindFinal), version, models.RenderJobStatusFailed, 2,
		"invoices/"+businessID+"/"+invoiceID+"/v7/final.pdf", now.Add(-time.Hour), now.Add(-time.Hour), now.Add(-time.Minute),
	).Error; err != nil {
		t.Fatalf("insert render: %v", err)
	}
	if err := database.Exec(
		`INSERT INTO invoices (id,business_id,invoice_no,status,version,issued_at) VALUES (?,?,?,?,?,?)`,
		invoiceID, businessID, "INV/7", models.InvoiceStatusIssued, version, now.Add(-time.Hour),
	).Error; err != nil {
		t.Fatalf("insert invoice: %v", err)
	}
	repository := NewOperationRepository(database)
	command := interfaces.RenderRecoveryCommand{
		BusinessID: businessID, OperationID: renderID, ActorSubject: uuid.NewString(), PrincipalKind: "business",
		Action: "retry", Reason: "retry deterministic final render", IdempotencyKey: uuid.NewString(),
		CorrelationID: uuid.NewString(), RequestHash: strings.Repeat("a", 64),
		OperationVersion: now.Add(-time.Minute).Format(time.RFC3339Nano), OccurredAt: now,
	}

	first, err := repository.RetryRender(context.Background(), command)
	if err != nil || first.ResultCode != "accepted" || first.Replayed {
		t.Fatalf("first RetryRender() = %#v, %v", first, err)
	}
	second, err := repository.RetryRender(context.Background(), command)
	if err != nil || !second.Replayed || second.CommandID != first.CommandID {
		t.Fatalf("replayed RetryRender() = %#v, %v", second, err)
	}
	var events []models.OutboxEvent
	if err := database.Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatalf("outbox events = %#v, %v", events, err)
	}
	if _, err := outbox.MapInvoiceEventToSQSMessage(&events[0]); err != nil {
		t.Fatalf("recovery outbox payload is not accepted by worker mapper: %v", err)
	}
	var audits []models.OperationRecoveryCommand
	if err := database.Find(&audits).Error; err != nil || len(audits) != 1 {
		t.Fatalf("recovery audits = %#v, %v", audits, err)
	}
	if audits[0].ActorSubject != command.ActorSubject || audits[0].BusinessID != businessID ||
		audits[0].OperationID != renderID || audits[0].Reason != command.Reason ||
		audits[0].IdempotencyKey != command.IdempotencyKey || audits[0].CorrelationID != command.CorrelationID ||
		audits[0].ResultCode != "accepted" {
		t.Fatalf("durable audit = %#v", audits[0])
	}
	timeline, err := repository.ListOperationTimeline(context.Background(), businessID, operationTypeInvoiceRender, renderID, 10)
	if err != nil || len(timeline) != 2 || timeline[1].Code != "accepted" {
		t.Fatalf("recovery timeline = %#v, %v", timeline, err)
	}

	conflict := command
	conflict.RequestHash = strings.Repeat("b", 64)
	if _, err := repository.RetryRender(context.Background(), conflict); !errors.Is(err, interfaces.ErrUnsafeOperationReplay) {
		t.Fatalf("changed idempotency replay error = %v", err)
	}
	race := command
	race.IdempotencyKey = uuid.NewString()
	race.RequestHash = strings.Repeat("c", 64)
	if _, err := repository.RetryRender(context.Background(), race); !errors.Is(err, interfaces.ErrUnsafeOperationReplay) {
		t.Fatalf("same revision second recovery error = %v", err)
	}
	var eventCount int64
	if err := database.Model(&models.OutboxEvent{}).Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("outbox count after races = %d, %v", eventCount, err)
	}
	for index := 1; index <= 3; index++ {
		audit := models.OperationRecoveryCommand{
			ID: uuid.NewString(), BusinessID: businessID, OperationType: operationTypeInvoiceRender,
			OperationID: renderID, ActorSubject: uuid.NewString(), PrincipalKind: "operator", Action: "resolve",
			Reason: "bounded timeline review", IdempotencyKey: uuid.NewString(), RequestHash: strings.Repeat("d", 64),
			OperationVersion: command.OperationVersion, CorrelationID: uuid.NewString(),
			Status: models.OperationRecoveryStatusRejected, ResultCode: fmt.Sprintf("review_%d", index),
			CreatedAt: now.Add(time.Duration(index) * time.Minute), CompletedAt: now.Add(time.Duration(index) * time.Minute),
		}
		if err := database.Create(&audit).Error; err != nil {
			t.Fatalf("insert timeline audit %d: %v", index, err)
		}
	}
	latest, err := repository.ListOperationTimeline(context.Background(), businessID, operationTypeInvoiceRender, renderID, 3)
	if err != nil {
		t.Fatalf("latest bounded timeline error = %v", err)
	}
	latestCodes := map[string]bool{}
	for _, event := range latest {
		latestCodes[event.Code] = true
	}
	if len(latest) != 3 || !latestCodes["render_failed"] || !latestCodes["review_2"] || !latestCodes["review_3"] {
		t.Fatalf("latest bounded timeline = %#v", latest)
	}
}

func operationFixtureSchemas() []string {
	return []string{
		`CREATE TABLE document_render_jobs (id TEXT PRIMARY KEY, business_id TEXT, document_id TEXT, invoice_id TEXT, kind TEXT, status TEXT, attempts INTEGER, error_message TEXT, object_key TEXT, requested_at DATETIME, created_at DATETIME, updated_at DATETIME, completed_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE email_deliveries (id TEXT PRIMARY KEY, business_id TEXT, email_account_id TEXT, invoice_id TEXT, render_job_id TEXT, status TEXT, provider_message_id TEXT, error_message TEXT, attempts INTEGER, sent_at DATETIME, delivered_at DATETIME, failed_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE outbox_events (id TEXT PRIMARY KEY, business_id TEXT, aggregate_type TEXT, aggregate_id TEXT, event_type TEXT, payload TEXT, publish_attempts INTEGER, available_at DATETIME, lease_expires_at DATETIME, published_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE razorpay_webhook_events (id TEXT PRIMARY KEY, razorpay_event_id TEXT, provider_mode TEXT, event_type TEXT, payload_hash TEXT, signature_verified BOOLEAN, received_at DATETIME, processing_status TEXT, attempt_count INTEGER, sanitized_error_code TEXT, processed_at DATETIME, business_id TEXT, subscription_id TEXT, created_at DATETIME)`,
		`CREATE TABLE gst_submission_jobs (id TEXT PRIMARY KEY, business_id TEXT, document_id TEXT, operation TEXT, status TEXT, idempotency_key TEXT, queue_message_id TEXT, attempt_count INTEGER, next_attempt_at DATETIME, last_attempt_at DATETIME, succeeded_at DATETIME, last_error TEXT, error_class TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE invoice_subscription_runs (id TEXT PRIMARY KEY, subscription_id TEXT, business_id TEXT, scheduled_for DATETIME, status TEXT, idempotency_key TEXT, attempt_count INTEGER, created_at DATETIME, updated_at DATETIME, completed_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE notification_deliveries (id TEXT PRIMARY KEY, business_id TEXT, channel TEXT, event_key TEXT, status TEXT, request_payload TEXT, response_payload TEXT, external_message_id TEXT, attempt_count INTEGER, last_attempt_at DATETIME, delivered_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE notifications (id TEXT PRIMARY KEY, business_id TEXT, user_id TEXT, source_event_key TEXT, type TEXT, title TEXT, body TEXT, resource_type TEXT, resource_id TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE bulk_jobs (id TEXT PRIMARY KEY, business_id TEXT, created_by TEXT, job_type TEXT, status TEXT, file_key TEXT, processed_rows INTEGER, request_payload TEXT, created_at DATETIME, updated_at DATETIME, completed_at DATETIME, deleted_at DATETIME)`,
	}
}

func createOperationFixture(database *gorm.DB, value any) error {
	var fields []string
	switch value.(type) {
	case *models.DocumentRenderJob:
		fields = []string{"id", "business_id", "document_id", "invoice_id", "kind", "status", "attempts", "error_message", "object_key", "requested_at", "created_at", "updated_at", "completed_at", "deleted_at"}
	case *models.EmailDelivery:
		fields = []string{"id", "business_id", "email_account_id", "invoice_id", "render_job_id", "status", "provider_message_id", "error_message", "attempts", "sent_at", "delivered_at", "failed_at", "created_at", "updated_at", "deleted_at"}
	case *models.OutboxEvent:
		fields = []string{"id", "business_id", "aggregate_type", "aggregate_id", "event_type", "payload", "publish_attempts", "available_at", "lease_expires_at", "published_at", "created_at"}
	case *models.RazorpayWebhookEvent:
		fields = []string{"id", "razorpay_event_id", "provider_mode", "event_type", "payload_hash", "signature_verified", "received_at", "processing_status", "attempt_count", "sanitized_error_code", "processed_at", "business_id", "subscription_id", "created_at"}
	case *models.GSTSubmissionJob:
		fields = []string{"id", "business_id", "document_id", "operation", "status", "idempotency_key", "queue_message_id", "attempt_count", "next_attempt_at", "last_attempt_at", "succeeded_at", "last_error", "error_class", "created_at", "updated_at", "deleted_at"}
	case *models.InvoiceSubscriptionRun:
		fields = []string{"id", "subscription_id", "business_id", "scheduled_for", "status", "idempotency_key", "attempt_count", "created_at", "updated_at", "completed_at", "deleted_at"}
	case *models.NotificationDelivery:
		fields = []string{"id", "business_id", "channel", "event_key", "status", "request_payload", "response_payload", "external_message_id", "attempt_count", "last_attempt_at", "delivered_at", "created_at", "updated_at", "deleted_at"}
	case *models.Notification:
		fields = []string{"id", "business_id", "user_id", "source_event_key", "type", "title", "body", "resource_type", "resource_id", "created_at", "updated_at"}
	case *models.BulkJob:
		fields = []string{"id", "business_id", "created_by", "job_type", "status", "file_key", "processed_rows", "request_payload", "created_at", "updated_at", "completed_at", "deleted_at"}
	}
	return database.Select(fields).Create(value).Error
}
