package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	operationTypeInvoiceRender       = "invoice_render"
	operationTypeInvoiceDelivery     = "invoice_delivery"
	operationTypeOutbox              = "outbox"
	operationTypeRazorpayWebhook     = "razorpay_webhook"
	operationTypeGSTEInvoice         = "gst_einvoice"
	operationTypeGSTEWayBill         = "gst_ewaybill"
	operationTypeRecurringInvoice    = "recurring_invoice"
	operationTypeEmailDelivery       = "email_delivery"
	operationTypeWhatsAppDelivery    = "whatsapp_delivery"
	operationTypeNotification        = "notification"
	operationTypeImport              = "import"
	operationTypeVoiceReconciliation = "voice_reconciliation"
)

type OperationRepository struct {
	db *gorm.DB
}

func NewOperationRepository(db *gorm.DB) *OperationRepository {
	return &OperationRepository{db: db}
}

func (r *OperationRepository) ListOperations(
	ctx context.Context,
	businessID string,
	query interfaces.OperationRecordQuery,
) (interfaces.OperationRecordPage, error) {
	if r == nil || r.db == nil || strings.TrimSpace(businessID) == "" ||
		query.Limit < 1 || query.Limit > 101 || query.SnapshotAt.IsZero() {
		return interfaces.OperationRecordPage{}, errors.New("operation query is invalid")
	}
	page := interfaces.OperationRecordPage{}
	loaders := []struct {
		types []string
		load  func(context.Context, string, interfaces.OperationRecordQuery) ([]interfaces.OperationRecord, error)
	}{
		{[]string{operationTypeInvoiceRender}, r.listRenderOperations},
		{[]string{operationTypeInvoiceDelivery, operationTypeEmailDelivery}, r.listEmailOperations},
		{[]string{operationTypeOutbox}, r.listOutboxOperations},
		{[]string{operationTypeRazorpayWebhook}, r.listRazorpayOperations},
		{[]string{operationTypeGSTEInvoice, operationTypeGSTEWayBill}, r.listGSTOperations},
		{[]string{operationTypeRecurringInvoice}, r.listRecurringOperations},
		{[]string{operationTypeWhatsAppDelivery, operationTypeNotification}, r.listNotificationOperations},
		{[]string{operationTypeImport}, r.listImportOperations},
	}
	for _, loader := range loaders {
		if !anyRequestedOperationType(query.Types, loader.types) {
			continue
		}
		records, err := loader.load(ctx, businessID, query)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return interfaces.OperationRecordPage{}, ctxErr
			}
			page.UnavailableTypes = append(page.UnavailableTypes, requestedOperationTypes(query.Types, loader.types)...)
			continue
		}
		page.Records = append(page.Records, records...)
	}
	if anyRequestedOperationType(query.Types, []string{operationTypeVoiceReconciliation}) {
		page.UnavailableTypes = append(page.UnavailableTypes, operationTypeVoiceReconciliation)
	}
	sortOperationRepositoryRecords(page.Records)
	if len(page.Records) > query.Limit {
		page.Records = page.Records[:query.Limit]
	}
	sort.Strings(page.UnavailableTypes)
	return page, nil
}

func (r *OperationRepository) GetOperation(
	ctx context.Context,
	businessID, operationType, operationID string,
) (*interfaces.OperationRecord, error) {
	if r == nil || r.db == nil || businessID == "" || operationID == "" {
		return nil, interfaces.ErrOperationNotFound
	}
	query := interfaces.OperationRecordQuery{
		Types: []string{operationType}, Limit: 1, SnapshotAt: time.Now().UTC().Add(time.Minute),
	}
	page, err := r.ListOperations(ctx, businessID, query)
	if err != nil {
		return nil, err
	}
	for i := range page.Records {
		if page.Records[i].Type == operationType && page.Records[i].ID == operationID {
			result := page.Records[i]
			return &result, nil
		}
	}
	// A bounded list is not a valid get implementation when the requested row
	// is older than the newest operation. Use a direct type-scoped read.
	return r.getOperationDirect(ctx, businessID, operationType, operationID)
}

func (r *OperationRepository) ListOperationTimeline(
	ctx context.Context,
	businessID, operationType, operationID string,
	limit int,
) ([]interfaces.OperationTimelineRecord, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("operation timeline limit is invalid")
	}
	record, err := r.getOperationDirect(ctx, businessID, operationType, operationID)
	if err != nil {
		return nil, err
	}
	events := []interfaces.OperationTimelineRecord{{
		Status: record.InternalStatus, Code: record.ErrorCode, OccurredAt: record.UpdatedAt,
	}}
	var recoveries []models.OperationRecoveryCommand
	if err := r.db.WithContext(ctx).
		Where("business_id = ? AND operation_type = ? AND operation_id = ?", businessID, operationType, operationID).
		Order("created_at ASC, id ASC").Limit(limit).Find(&recoveries).Error; err != nil {
		return nil, fmt.Errorf("list operation recovery timeline: %w", err)
	}
	for i := range recoveries {
		events = append(events, interfaces.OperationTimelineRecord{
			Status: record.InternalStatus, Code: safeStoredOperationCode(recoveries[i].ResultCode),
			OccurredAt: recoveries[i].CreatedAt,
		})
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

func (r *OperationRepository) RetryRender(
	ctx context.Context,
	command interfaces.RenderRecoveryCommand,
) (*interfaces.OperationRecoveryResult, error) {
	if err := validateRenderRecoveryCommand(r, command); err != nil {
		return nil, err
	}
	var response *interfaces.OperationRecoveryResult
	var outcomeErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, found, err := findOperationRecoveryCommand(tx, command.BusinessID, command.ActorSubject, command.Action, command.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.RequestHash != command.RequestHash || existing.OperationID != command.OperationID ||
				existing.OperationType != operationTypeInvoiceRender {
				outcomeErr = interfaces.ErrUnsafeOperationReplay
				return nil
			}
			response = operationRecoveryResult(existing, true)
			return nil
		}

		version, err := time.Parse(time.RFC3339Nano, command.OperationVersion)
		if err != nil {
			return interfaces.ErrOperationRace
		}
		var job models.DocumentRenderJob
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", command.OperationID, command.BusinessID).
			First(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			outcomeErr = interfaces.ErrOperationNotFound
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock render recovery operation: %w", err)
		}
		if job.Status != models.RenderJobStatusFailed || !job.UpdatedAt.UTC().Equal(version.UTC()) {
			outcomeErr = interfaces.ErrOperationRace
			return nil
		}
		var prior models.OperationRecoveryCommand
		err = tx.Where(
			"business_id = ? AND operation_type = ? AND operation_id = ? AND action = ? AND operation_version = ? AND result_code = ?",
			command.BusinessID, operationTypeInvoiceRender, command.OperationID, command.Action,
			command.OperationVersion, "accepted",
		).First(&prior).Error
		if err == nil {
			outcomeErr = interfaces.ErrUnsafeOperationReplay
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("inspect render recovery revision: %w", err)
		}

		event, err := renderRecoveryOutboxEvent(tx, &job, command.OccurredAt)
		if errors.Is(err, interfaces.ErrUnsupportedRecovery) {
			outcomeErr = err
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.Select(
			"id", "business_id", "aggregate_type", "aggregate_id", "event_type", "payload",
			"publish_attempts", "available_at", "created_at",
		).Create(event).Error; err != nil {
			return fmt.Errorf("create render recovery outbox event: %w", err)
		}
		audit := operationRecoveryAuditFromRender(command, "accepted")
		if err := tx.Create(audit).Error; err != nil {
			return fmt.Errorf("create render recovery audit: %w", err)
		}
		response = operationRecoveryResult(audit, false)
		return nil
	})
	if err != nil {
		if isOperationRecoveryUniqueViolation(err) {
			return r.resolveRenderRecoveryRace(ctx, command)
		}
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return response, nil
}

func (r *OperationRepository) RecordRecoveryDecision(
	ctx context.Context,
	decision interfaces.OperationRecoveryDecision,
) (*interfaces.OperationRecoveryResult, error) {
	if err := validateOperationRecoveryDecision(r, decision); err != nil {
		return nil, err
	}
	var response *interfaces.OperationRecoveryResult
	var outcomeErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, found, err := findOperationRecoveryCommand(
			tx, decision.BusinessID, decision.ActorSubject, decision.Action, decision.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.RequestHash != decision.RequestHash || existing.OperationID != decision.OperationID ||
				existing.OperationType != decision.OperationType {
				outcomeErr = interfaces.ErrUnsafeOperationReplay
				return nil
			}
			response = operationRecoveryResult(existing, true)
			return nil
		}
		audit := &models.OperationRecoveryCommand{
			ID: uuid.NewString(), BusinessID: decision.BusinessID, OperationType: decision.OperationType,
			OperationID: decision.OperationID, ActorSubject: decision.ActorSubject,
			PrincipalKind: decision.PrincipalKind, Action: decision.Action, Reason: decision.Reason,
			IdempotencyKey: decision.IdempotencyKey, RequestHash: decision.RequestHash,
			OperationVersion: decision.OperationVersion, CorrelationID: decision.CorrelationID,
			Status: models.OperationRecoveryStatusRejected, ResultCode: decision.ResultCode,
			CreatedAt: decision.OccurredAt, CompletedAt: decision.OccurredAt,
		}
		if decision.ResultCode == "accepted" {
			audit.Status = models.OperationRecoveryStatusCompleted
		}
		if err := tx.Create(audit).Error; err != nil {
			return fmt.Errorf("create operation recovery decision: %w", err)
		}
		response = operationRecoveryResult(audit, false)
		return nil
	})
	if err != nil {
		if isOperationRecoveryUniqueViolation(err) {
			return r.resolveRecoveryDecisionRace(ctx, decision)
		}
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return response, nil
}

func (r *OperationRepository) resolveRenderRecoveryRace(
	ctx context.Context,
	command interfaces.RenderRecoveryCommand,
) (*interfaces.OperationRecoveryResult, error) {
	existing, found, err := findOperationRecoveryCommand(
		r.db.WithContext(ctx), command.BusinessID, command.ActorSubject, command.Action, command.IdempotencyKey,
	)
	if err != nil {
		return nil, err
	}
	if found && existing.RequestHash == command.RequestHash && existing.OperationID == command.OperationID &&
		existing.OperationType == operationTypeInvoiceRender {
		return operationRecoveryResult(existing, true), nil
	}
	return nil, interfaces.ErrUnsafeOperationReplay
}

func (r *OperationRepository) resolveRecoveryDecisionRace(
	ctx context.Context,
	decision interfaces.OperationRecoveryDecision,
) (*interfaces.OperationRecoveryResult, error) {
	existing, found, err := findOperationRecoveryCommand(
		r.db.WithContext(ctx), decision.BusinessID, decision.ActorSubject, decision.Action, decision.IdempotencyKey,
	)
	if err != nil {
		return nil, err
	}
	if found && existing.RequestHash == decision.RequestHash && existing.OperationID == decision.OperationID &&
		existing.OperationType == decision.OperationType {
		return operationRecoveryResult(existing, true), nil
	}
	return nil, interfaces.ErrUnsafeOperationReplay
}

func isOperationRecoveryUniqueViolation(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var postgresError *pq.Error
	return errors.As(err, &postgresError) && string(postgresError.Code) == "23505"
}

func (r *OperationRepository) listRenderOperations(
	ctx context.Context, businessID string, query interfaces.OperationRecordQuery,
) ([]interfaces.OperationRecord, error) {
	var rows []models.DocumentRenderJob
	db := operationScope(r.db.WithContext(ctx).Model(&models.DocumentRenderJob{}), businessID, query, operationTypeInvoiceRender, true)
	if err := db.Order("updated_at DESC, id ASC").Limit(query.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]interfaces.OperationRecord, 0, len(rows))
	for i := range rows {
		resourceID := firstOperationResource(rows[i].InvoiceID, rows[i].DocumentID)
		record := interfaces.OperationRecord{
			ID: rows[i].ID, Type: operationTypeInvoiceRender, ResourceType: "document", ResourceID: resourceID,
			InternalStatus: rows[i].Status, Attempts: rows[i].Attempts, Retryable: safeRenderRetryCandidate(&rows[i]),
			CreatedAt: rows[i].CreatedAt, UpdatedAt: rows[i].UpdatedAt, CompletedAt: rows[i].CompletedAt,
		}
		if rows[i].Status == models.RenderJobStatusFailed {
			record.ErrorCode = "render_failed"
		}
		if operationRecordMatches(query, record) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *OperationRepository) listEmailOperations(
	ctx context.Context, businessID string, query interfaces.OperationRecordQuery,
) ([]interfaces.OperationRecord, error) {
	var rows []models.EmailDelivery
	db := operationScope(r.db.WithContext(ctx).Model(&models.EmailDelivery{}), businessID, query, "", true)
	if err := db.Order("updated_at DESC, id ASC").Limit(query.Limit * 2).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]interfaces.OperationRecord, 0, len(rows))
	for i := range rows {
		operationType := operationTypeEmailDelivery
		resourceType, resourceID := "email", ""
		if rows[i].InvoiceID != nil && rows[i].RenderJobID != nil {
			operationType, resourceType, resourceID = operationTypeInvoiceDelivery, "invoice", *rows[i].InvoiceID
		}
		if !operationTypeRequested(query.Types, operationType) {
			continue
		}
		record := interfaces.OperationRecord{
			ID: rows[i].ID, Type: operationType, ResourceType: resourceType, ResourceID: resourceID,
			InternalStatus: rows[i].Status, Attempts: rows[i].Attempts,
			CreatedAt: rows[i].CreatedAt, UpdatedAt: rows[i].UpdatedAt,
			LastAttemptAt: firstOperationTime(rows[i].SentAt, rows[i].FailedAt),
			CompletedAt:   firstOperationTime(rows[i].DeliveredAt, rows[i].SentAt),
		}
		if isEmailFailureStatus(rows[i].Status) {
			record.ErrorCode = "delivery_" + rows[i].Status
		}
		if operationRecordMatches(query, record) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *OperationRepository) listOutboxOperations(
	ctx context.Context, businessID string, query interfaces.OperationRecordQuery,
) ([]interfaces.OperationRecord, error) {
	var rows []models.OutboxEvent
	db := operationScope(r.db.WithContext(ctx).Model(&models.OutboxEvent{}), businessID, query, operationTypeOutbox, false)
	if err := db.Order("created_at DESC, id ASC").Limit(query.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]interfaces.OperationRecord, 0, len(rows))
	for i := range rows {
		status := "pending"
		if rows[i].PublishedAt != nil {
			status = "published"
		} else if rows[i].LeaseExpiresAt != nil && rows[i].LeaseExpiresAt.After(query.SnapshotAt) {
			status = "publishing"
		} else if rows[i].PublishAttempts > 0 {
			status = "retrying"
		}
		record := interfaces.OperationRecord{
			ID: rows[i].ID, Type: operationTypeOutbox, ResourceType: safeOperationResourceType(rows[i].AggregateType),
			ResourceID: rows[i].AggregateID, InternalStatus: status, Attempts: rows[i].PublishAttempts,
			NextAttemptAt: operationTimePointer(rows[i].AvailableAt), Retryable: rows[i].PublishedAt == nil,
			CreatedAt: rows[i].CreatedAt, UpdatedAt: rows[i].CreatedAt, CompletedAt: rows[i].PublishedAt,
		}
		if rows[i].PublishAttempts > 0 && rows[i].PublishedAt == nil {
			record.ErrorCode = "outbox_publish_retried"
		}
		if operationRecordMatches(query, record) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *OperationRepository) listRazorpayOperations(
	ctx context.Context, businessID string, query interfaces.OperationRecordQuery,
) ([]interfaces.OperationRecord, error) {
	var rows []models.RazorpayWebhookEvent
	db := operationScope(r.db.WithContext(ctx).Model(&models.RazorpayWebhookEvent{}), businessID, query, operationTypeRazorpayWebhook, false)
	if err := db.Order("received_at DESC, id ASC").Limit(query.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]interfaces.OperationRecord, 0, len(rows))
	for i := range rows {
		updatedAt := rows[i].ReceivedAt
		if rows[i].ProcessedAt != nil {
			updatedAt = *rows[i].ProcessedAt
		}
		record := interfaces.OperationRecord{
			ID: rows[i].ID, Type: operationTypeRazorpayWebhook, ResourceType: "subscription", ResourceID: rows[i].SubscriptionID,
			InternalStatus: rows[i].ProcessingStatus, Attempts: rows[i].AttemptCount,
			ReconciliationRequired: rows[i].ProcessingStatus == "reconciliation_required",
			ErrorCode:              safeStoredOperationCode(rows[i].SanitizedErrorCode), CreatedAt: rows[i].ReceivedAt,
			UpdatedAt: updatedAt, CompletedAt: rows[i].ProcessedAt,
		}
		if operationRecordMatches(query, record) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *OperationRepository) listGSTOperations(
	ctx context.Context, businessID string, query interfaces.OperationRecordQuery,
) ([]interfaces.OperationRecord, error) {
	var rows []models.GSTSubmissionJob
	db := operationScope(r.db.WithContext(ctx).Model(&models.GSTSubmissionJob{}), businessID, query, "", true)
	if err := db.Order("updated_at DESC, id ASC").Limit(query.Limit * 2).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]interfaces.OperationRecord, 0, len(rows))
	for i := range rows {
		operationType := operationTypeGSTEInvoice
		if isEWayBillOperation(rows[i].Operation) {
			operationType = operationTypeGSTEWayBill
		}
		if !operationTypeRequested(query.Types, operationType) {
			continue
		}
		status := rows[i].Status
		needsReconciliation := status == models.GSTJobStatusNeedsAttention
		if needsReconciliation {
			status = "reconciliation_required"
		}
		record := interfaces.OperationRecord{
			ID: rows[i].ID, Type: operationType, ResourceType: "document", ResourceID: rows[i].DocumentID,
			InternalStatus: status, Attempts: rows[i].AttemptCount, LastAttemptAt: rows[i].LastAttemptAt,
			NextAttemptAt: rows[i].NextAttemptAt, ReconciliationRequired: needsReconciliation,
			ErrorCode: safeGSTErrorCode(rows[i].ErrorClass), CreatedAt: rows[i].CreatedAt,
			UpdatedAt: rows[i].UpdatedAt, CompletedAt: rows[i].SucceededAt,
		}
		if operationRecordMatches(query, record) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *OperationRepository) listRecurringOperations(
	ctx context.Context, businessID string, query interfaces.OperationRecordQuery,
) ([]interfaces.OperationRecord, error) {
	var rows []models.InvoiceSubscriptionRun
	db := operationScope(r.db.WithContext(ctx).Model(&models.InvoiceSubscriptionRun{}), businessID, query, operationTypeRecurringInvoice, true)
	if err := db.Order("updated_at DESC, id ASC").Limit(query.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]interfaces.OperationRecord, 0, len(rows))
	for i := range rows {
		record := interfaces.OperationRecord{
			ID: rows[i].ID, Type: operationTypeRecurringInvoice, ResourceType: "invoice_subscription", ResourceID: rows[i].SubscriptionID,
			InternalStatus: rows[i].Status, Attempts: rows[i].AttemptCount, CreatedAt: rows[i].CreatedAt,
			UpdatedAt: rows[i].UpdatedAt, CompletedAt: rows[i].CompletedAt,
		}
		if rows[i].Status == "failed" {
			record.ErrorCode = "recurring_invoice_failed"
		}
		if operationRecordMatches(query, record) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *OperationRepository) listNotificationOperations(
	ctx context.Context, businessID string, query interfaces.OperationRecordQuery,
) ([]interfaces.OperationRecord, error) {
	records := make([]interfaces.OperationRecord, 0, query.Limit*2)
	if operationTypeRequested(query.Types, operationTypeWhatsAppDelivery) || operationTypeRequested(query.Types, operationTypeNotification) {
		var deliveries []models.NotificationDelivery
		db := operationScope(r.db.WithContext(ctx).Model(&models.NotificationDelivery{}), businessID, query, "", true)
		if err := db.Order("updated_at DESC, id ASC").Limit(query.Limit * 2).Find(&deliveries).Error; err != nil {
			return nil, err
		}
		for i := range deliveries {
			operationType := operationTypeNotification
			if deliveries[i].Channel == models.NotificationChannelWhatsApp {
				operationType = operationTypeWhatsAppDelivery
			}
			if !operationTypeRequested(query.Types, operationType) {
				continue
			}
			record := interfaces.OperationRecord{
				ID: deliveries[i].ID, Type: operationType, ResourceType: "notification", InternalStatus: deliveries[i].Status,
				Attempts: deliveries[i].AttemptCount, LastAttemptAt: deliveries[i].LastAttemptAt,
				CreatedAt: deliveries[i].CreatedAt, UpdatedAt: deliveries[i].UpdatedAt, CompletedAt: deliveries[i].DeliveredAt,
			}
			if deliveries[i].Status == "failed" {
				record.ErrorCode = "notification_delivery_failed"
			}
			if operationRecordMatches(query, record) {
				records = append(records, record)
			}
		}
	}
	if operationTypeRequested(query.Types, operationTypeNotification) {
		var notifications []models.Notification
		db := operationScope(r.db.WithContext(ctx).Model(&models.Notification{}), businessID, query, operationTypeNotification, true)
		if err := db.Order("updated_at DESC, id ASC").Limit(query.Limit).Find(&notifications).Error; err != nil {
			return nil, err
		}
		for i := range notifications {
			record := interfaces.OperationRecord{
				ID: notifications[i].ID, Type: operationTypeNotification,
				ResourceType: safeOperationResourceType(notifications[i].ResourceType), ResourceID: notifications[i].ResourceID,
				InternalStatus: "completed", Attempts: 1, CreatedAt: notifications[i].CreatedAt,
				UpdatedAt: notifications[i].UpdatedAt, CompletedAt: operationTimePointer(notifications[i].CreatedAt),
			}
			if operationRecordMatches(query, record) {
				records = append(records, record)
			}
		}
	}
	return records, nil
}

func (r *OperationRepository) listImportOperations(
	ctx context.Context, businessID string, query interfaces.OperationRecordQuery,
) ([]interfaces.OperationRecord, error) {
	var rows []models.BulkJob
	db := operationScope(r.db.WithContext(ctx).Model(&models.BulkJob{}), businessID, query, operationTypeImport, true).
		Where("job_type IN ?", []string{
			models.BulkJobTypeImportCustomers, models.BulkJobTypeImportVendors, models.BulkJobTypeImportProducts,
			models.BulkJobTypeImportInvoices, models.BulkJobTypeImportDocuments,
		})
	if err := db.Order("updated_at DESC, id ASC").Limit(query.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]interfaces.OperationRecord, 0, len(rows))
	for i := range rows {
		record := interfaces.OperationRecord{
			ID: rows[i].ID, Type: operationTypeImport, ResourceType: "import", ResourceID: rows[i].ID,
			InternalStatus: rows[i].Status, Attempts: rows[i].ProcessedRows, CreatedAt: rows[i].CreatedAt,
			UpdatedAt: rows[i].UpdatedAt, CompletedAt: rows[i].CompletedAt,
		}
		if rows[i].Status == models.BulkJobStatusFailed {
			record.ErrorCode = "import_failed"
		}
		if operationRecordMatches(query, record) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *OperationRepository) getOperationDirect(
	ctx context.Context, businessID, operationType, operationID string,
) (*interfaces.OperationRecord, error) {
	query := interfaces.OperationRecordQuery{
		Types: []string{operationType}, ExactID: operationID, Limit: 1,
		SnapshotAt: time.Now().UTC().Add(time.Minute),
	}
	var records []interfaces.OperationRecord
	var err error
	switch operationType {
	case operationTypeInvoiceRender:
		var row models.DocumentRenderJob
		err = r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", operationID, businessID).First(&row).Error
		if err == nil {
			query.SnapshotAt = row.UpdatedAt.Add(time.Second)
			records, err = r.listRenderOperations(ctx, businessID, query)
		}
	case operationTypeInvoiceDelivery, operationTypeEmailDelivery:
		var row models.EmailDelivery
		err = r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", operationID, businessID).First(&row).Error
		if err == nil {
			query.SnapshotAt = row.UpdatedAt.Add(time.Second)
			records, err = r.listEmailOperations(ctx, businessID, query)
		}
	case operationTypeOutbox:
		var row models.OutboxEvent
		err = r.db.WithContext(ctx).Where("id = ? AND business_id = ?", operationID, businessID).First(&row).Error
		if err == nil {
			query.SnapshotAt = row.CreatedAt.Add(time.Second)
			records, err = r.listOutboxOperations(ctx, businessID, query)
		}
	case operationTypeRazorpayWebhook:
		var row models.RazorpayWebhookEvent
		err = r.db.WithContext(ctx).Where("id = ? AND business_id = ?", operationID, businessID).First(&row).Error
		if err == nil {
			query.SnapshotAt = time.Now().UTC().Add(time.Minute)
			records, err = r.listRazorpayOperations(ctx, businessID, query)
		}
	case operationTypeGSTEInvoice, operationTypeGSTEWayBill:
		var row models.GSTSubmissionJob
		err = r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", operationID, businessID).First(&row).Error
		if err == nil {
			query.SnapshotAt = row.UpdatedAt.Add(time.Second)
			records, err = r.listGSTOperations(ctx, businessID, query)
		}
	case operationTypeRecurringInvoice:
		var row models.InvoiceSubscriptionRun
		err = r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", operationID, businessID).First(&row).Error
		if err == nil {
			query.SnapshotAt = row.UpdatedAt.Add(time.Second)
			records, err = r.listRecurringOperations(ctx, businessID, query)
		}
	case operationTypeWhatsAppDelivery, operationTypeNotification:
		records, err = r.listNotificationOperations(ctx, businessID, query)
	case operationTypeImport:
		var row models.BulkJob
		err = r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", operationID, businessID).First(&row).Error
		if err == nil {
			query.SnapshotAt = row.UpdatedAt.Add(time.Second)
			records, err = r.listImportOperations(ctx, businessID, query)
		}
	default:
		return nil, interfaces.ErrOperationNotFound
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	for i := range records {
		if records[i].ID == operationID && records[i].Type == operationType {
			result := records[i]
			return &result, nil
		}
	}
	return nil, interfaces.ErrOperationNotFound
}

func operationScope(
	db *gorm.DB, businessID string, query interfaces.OperationRecordQuery, operationType string, usesUpdatedAt bool,
) *gorm.DB {
	timeColumn := "created_at"
	if usesUpdatedAt {
		timeColumn = "updated_at"
	}
	db = db.Where("business_id = ?", businessID).Where(timeColumn+" <= ?", query.SnapshotAt)
	if query.ExactID != "" {
		db = db.Where("id = ?", query.ExactID)
	}
	if query.AfterUpdatedAt != nil {
		if operationType == "" {
			db = db.Where(timeColumn+" <= ?", *query.AfterUpdatedAt)
		} else if operationType > query.AfterType {
			db = db.Where(timeColumn+" <= ?", *query.AfterUpdatedAt)
		} else if operationType == query.AfterType {
			db = db.Where("("+timeColumn+" < ?) OR ("+timeColumn+" = ? AND id > ?)", *query.AfterUpdatedAt, *query.AfterUpdatedAt, query.AfterID)
		} else {
			db = db.Where(timeColumn+" < ?", *query.AfterUpdatedAt)
		}
	}
	return db
}

func operationRecordMatches(query interfaces.OperationRecordQuery, record interfaces.OperationRecord) bool {
	if !operationTypeRequested(query.Types, record.Type) {
		return false
	}
	if query.AfterUpdatedAt != nil {
		if record.UpdatedAt.After(*query.AfterUpdatedAt) {
			return false
		}
		if record.UpdatedAt.Equal(*query.AfterUpdatedAt) &&
			(record.Type < query.AfterType || (record.Type == query.AfterType && record.ID <= query.AfterID)) {
			return false
		}
	}
	if len(query.Statuses) == 0 {
		return true
	}
	normalized := normalizedRepositoryStatus(record.Type, record.InternalStatus)
	for _, status := range query.Statuses {
		if status == normalized {
			return true
		}
	}
	return false
}

func safeRenderRetryCandidate(job *models.DocumentRenderJob) bool {
	if job == nil || job.Status != models.RenderJobStatusFailed || job.InvoiceID == nil || job.DocumentID == nil ||
		job.SourceInvoiceVersion == nil || *job.InvoiceID != *job.DocumentID || *job.SourceInvoiceVersion < 1 {
		return false
	}
	return job.Kind == models.RenderKindPreview || job.Kind == models.RenderKindFinal
}

func normalizedRepositoryStatus(operationType, status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "reconciliation_required" {
		return "reconciliation_required"
	}
	switch operationType {
	case operationTypeInvoiceRender:
		switch status {
		case "queued":
			return "queued"
		case "processing":
			return "in_progress"
		case "completed", "obsolete":
			return "succeeded"
		case "failed":
			return "failed"
		}
	case operationTypeInvoiceDelivery, operationTypeEmailDelivery:
		switch status {
		case "waiting_for_render", "queued":
			return "queued"
		case "processing":
			return "in_progress"
		case "sent", "delivered":
			return "succeeded"
		case "failed", "bounced", "complained":
			return "failed"
		}
	case operationTypeOutbox:
		switch status {
		case "pending", "retrying":
			return "queued"
		case "publishing":
			return "in_progress"
		case "published":
			return "succeeded"
		}
	default:
		switch status {
		case "pending", "queued", "waiting":
			return "queued"
		case "processing", "running", "retrying", "received":
			return "in_progress"
		case "completed", "succeeded", "sent", "delivered", "processed":
			return "succeeded"
		case "failed", "rejected", "bounced", "complained":
			return "failed"
		}
	}
	return "unknown"
}

func operationTypeRequested(requested []string, operationType string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, value := range requested {
		if value == operationType {
			return true
		}
	}
	return false
}

func anyRequestedOperationType(requested, candidates []string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, candidate := range candidates {
		if operationTypeRequested(requested, candidate) {
			return true
		}
	}
	return false
}

func requestedOperationTypes(requested, candidates []string) []string {
	if len(requested) == 0 {
		return append([]string(nil), candidates...)
	}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if operationTypeRequested(requested, candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func sortOperationRepositoryRecords(records []interfaces.OperationRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].UpdatedAt.After(records[j].UpdatedAt)
		}
		if records[i].Type != records[j].Type {
			return records[i].Type < records[j].Type
		}
		return records[i].ID < records[j].ID
	})
}

func firstOperationResource(values ...*string) string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return *value
		}
	}
	return ""
}

func firstOperationTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			clone := value.UTC()
			return &clone
		}
	}
	return nil
}

func operationTimePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	clone := value.UTC()
	return &clone
}

func safeOperationResourceType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "invoice", "document", "document_render_job", "email_delivery", "subscription", "notification", "import":
		return value
	default:
		return "operation"
	}
}

func safeStoredOperationCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if len(value) > 80 {
		return "operation_error"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return "operation_error"
		}
	}
	return value
}

func safeGSTErrorCode(value string) string {
	switch value {
	case models.GSTErrorClassRetriable, models.GSTErrorClassValidation, models.GSTErrorClassCredentials,
		models.GSTErrorClassDuplicate, models.GSTErrorClassRule, models.GSTErrorClassUnavailable, models.GSTErrorClassUnknown:
		return "gst_" + value
	default:
		return ""
	}
}

func isEWayBillOperation(value string) bool {
	switch value {
	case models.GSTOperationGenerateEWayBill, models.GSTOperationUpdateEWayPartB,
		models.GSTOperationMultiVehicle, models.GSTOperationFetchEWayPDF:
		return true
	default:
		return false
	}
}

func isEmailFailureStatus(value string) bool {
	switch value {
	case models.EmailDeliveryStatusFailed, models.EmailDeliveryStatusBounced, models.EmailDeliveryStatusComplained:
		return true
	default:
		return false
	}
}

func validateRenderRecoveryCommand(repository *OperationRepository, command interfaces.RenderRecoveryCommand) error {
	if repository == nil || repository.db == nil ||
		!validOperationUUID(command.BusinessID) || !validOperationUUID(command.OperationID) ||
		!validOperationUUID(command.IdempotencyKey) || !validOperationUUID(command.CorrelationID) ||
		strings.TrimSpace(command.ActorSubject) == "" || len(command.ActorSubject) > 255 ||
		(command.PrincipalKind != "business" && command.PrincipalKind != "operator") ||
		command.Action != "retry" || strings.TrimSpace(command.Reason) == "" || len(command.Reason) > 500 ||
		!validOperationRequestHash(command.RequestHash) || command.OperationVersion == "" || command.OccurredAt.IsZero() {
		return errors.New("render recovery command is invalid")
	}
	return nil
}

func validateOperationRecoveryDecision(repository *OperationRepository, decision interfaces.OperationRecoveryDecision) error {
	if repository == nil || repository.db == nil ||
		!validOperationUUID(decision.BusinessID) || !validOperationUUID(decision.OperationID) ||
		!validOperationUUID(decision.IdempotencyKey) || !validOperationUUID(decision.CorrelationID) ||
		strings.TrimSpace(decision.OperationType) == "" || len(decision.OperationType) > 64 ||
		strings.TrimSpace(decision.ActorSubject) == "" || len(decision.ActorSubject) > 255 ||
		(decision.PrincipalKind != "business" && decision.PrincipalKind != "operator") ||
		strings.TrimSpace(decision.Action) == "" || len(decision.Action) > 80 ||
		strings.TrimSpace(decision.Reason) == "" || len(decision.Reason) > 500 ||
		!validOperationRequestHash(decision.RequestHash) || decision.OperationVersion == "" ||
		strings.TrimSpace(decision.ResultCode) == "" || len(decision.ResultCode) > 80 || decision.OccurredAt.IsZero() {
		return errors.New("operation recovery decision is invalid")
	}
	return nil
}

func validOperationUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func validOperationRequestHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func findOperationRecoveryCommand(
	tx *gorm.DB,
	businessID, actorSubject, action, idempotencyKey string,
) (*models.OperationRecoveryCommand, bool, error) {
	var command models.OperationRecoveryCommand
	err := tx.Where(
		"business_id = ? AND actor_subject = ? AND action = ? AND idempotency_key = ?",
		businessID, actorSubject, action, idempotencyKey,
	).First(&command).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find operation recovery command: %w", err)
	}
	return &command, true, nil
}

func operationRecoveryResult(command *models.OperationRecoveryCommand, replayed bool) *interfaces.OperationRecoveryResult {
	if command == nil {
		return nil
	}
	return &interfaces.OperationRecoveryResult{
		CommandID: command.ID, ResultCode: command.ResultCode, CorrelationID: command.CorrelationID,
		Replayed: replayed, AcceptedAt: command.CompletedAt.UTC(),
	}
}

func operationRecoveryAuditFromRender(
	command interfaces.RenderRecoveryCommand,
	resultCode string,
) *models.OperationRecoveryCommand {
	status := models.OperationRecoveryStatusRejected
	if resultCode == "accepted" {
		status = models.OperationRecoveryStatusCompleted
	}
	return &models.OperationRecoveryCommand{
		ID: uuid.NewString(), BusinessID: command.BusinessID, OperationType: operationTypeInvoiceRender,
		OperationID: command.OperationID, ActorSubject: command.ActorSubject, PrincipalKind: command.PrincipalKind,
		Action: command.Action, Reason: strings.TrimSpace(command.Reason), IdempotencyKey: command.IdempotencyKey,
		RequestHash: command.RequestHash, OperationVersion: command.OperationVersion,
		CorrelationID: command.CorrelationID, Status: status, ResultCode: resultCode,
		CreatedAt: command.OccurredAt.UTC(), CompletedAt: command.OccurredAt.UTC(),
	}
}

func renderRecoveryOutboxEvent(
	tx *gorm.DB,
	job *models.DocumentRenderJob,
	now time.Time,
) (*models.OutboxEvent, error) {
	if job == nil || job.InvoiceID == nil || job.DocumentID == nil || job.SourceInvoiceVersion == nil ||
		*job.InvoiceID != *job.DocumentID || *job.SourceInvoiceVersion < 1 {
		return nil, interfaces.ErrUnsupportedRecovery
	}
	var invoice models.Invoice
	err := tx.Where("id = ? AND business_id = ? AND deleted_at IS NULL", *job.InvoiceID, job.BusinessID).First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrUnsupportedRecovery
	}
	if err != nil {
		return nil, fmt.Errorf("load invoice for render recovery: %w", err)
	}
	if invoice.Version != *job.SourceInvoiceVersion {
		return nil, interfaces.ErrUnsupportedRecovery
	}
	payload := map[string]any{
		"schema_version": 1, "aggregate_type": "invoice", "aggregate_id": invoice.ID,
		"invoice_version": invoice.Version, "render_job_id": job.ID,
	}
	eventType := ""
	switch job.Kind {
	case models.RenderKindPreview:
		expectedKey := fmt.Sprintf("invoices/%s/%s/previews/v%d/%s.pdf", job.BusinessID, invoice.ID, invoice.Version, job.ID)
		if job.ObjectKey != expectedKey {
			return nil, interfaces.ErrUnsupportedRecovery
		}
		eventType = "invoice.preview.requested.v1"
		payload["type"] = "generate_document_pdf"
		payload["invoice_id"] = invoice.ID
		payload["document_id"] = invoice.ID
	case models.RenderKindFinal:
		expectedKey := fmt.Sprintf("invoices/%s/%s/v%d/final.pdf", job.BusinessID, invoice.ID, invoice.Version)
		if job.ObjectKey != expectedKey || invoice.Status != models.InvoiceStatusIssued ||
			invoice.IssuedAt == nil || invoice.InvoiceNo == nil || strings.TrimSpace(*invoice.InvoiceNo) == "" {
			return nil, interfaces.ErrUnsupportedRecovery
		}
		eventType = "invoice.issued.v1"
		payload["invoice_no"] = *invoice.InvoiceNo
	default:
		return nil, interfaces.ErrUnsupportedRecovery
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode render recovery event: %w", err)
	}
	return &models.OutboxEvent{
		ID: uuid.NewString(), BusinessID: job.BusinessID, AggregateType: "invoice", AggregateID: invoice.ID,
		EventType: eventType, Payload: string(encoded), AvailableAt: now.UTC(), CreatedAt: now.UTC(),
	}, nil
}
