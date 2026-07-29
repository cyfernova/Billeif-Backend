package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type atomicInvoiceIssuePersistenceError struct {
	stage string
	cause error
}

func (e *atomicInvoiceIssuePersistenceError) Error() string {
	return fmt.Sprintf("atomic invoice issue persistence failed at %s", e.stage)
}

func (e *atomicInvoiceIssuePersistenceError) Unwrap() error { return e.cause }

func issueStageError(stage string, err error) error {
	return &atomicInvoiceIssuePersistenceError{stage: stage, cause: err}
}

func (r *invoiceRepository) IssueDraftAtomic(
	ctx context.Context,
	command interfaces.AtomicInvoiceIssue,
) (*interfaces.AtomicInvoiceIssueResult, error) {
	if err := validateAtomicInvoiceIssue(command); err != nil {
		return nil, issueStageError("command validation", err)
	}

	var issuedInvoice *models.Invoice
	var finalRender *models.DocumentRenderJob
	var replayInvoiceID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimed, replayID, err := claimInvoiceIssueIdempotency(tx, command)
		if err != nil {
			return err
		}
		if !claimed {
			replayInvoiceID = replayID
			return nil
		}

		var invoice models.Invoice
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", command.InvoiceID, command.BusinessID).
			First(&invoice).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &invoiceissue.NotFoundError{}
			}
			return issueStageError("invoice lock", err)
		}
		if invoice.Version != command.ExpectedVersion {
			return &invoiceissue.StaleVersionError{Expected: command.ExpectedVersion, Actual: invoice.Version}
		}
		if invoice.Status != models.InvoiceStatusDraft || invoice.InvoiceNo != nil || invoice.IssuedAt != nil {
			return &invoiceissue.AlreadyIssuedError{}
		}
		if invoice.SellerSnapshot.IsEmpty() || invoice.BuyerSnapshot.IsEmpty() {
			return &invoiceissue.InvalidLifecycleError{Reason: "legal party snapshots are required"}
		}
		if err := invoice.ValidateState(); err != nil {
			return &invoiceissue.InvalidLifecycleError{Reason: err.Error()}
		}

		var business struct {
			Timezone string
		}
		if err := tx.Table("business_profiles").
			Select("timezone").
			Where("id = ? AND deleted_at IS NULL", command.BusinessID).
			Take(&business).Error; err != nil {
			return issueStageError("business timezone", err)
		}
		financialYear, err := invoiceissue.FinancialYear(invoice.InvoiceDate, business.Timezone)
		if err != nil {
			return err
		}

		var document models.Document
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", command.InvoiceID, command.BusinessID).
			First(&document).Error; err != nil {
			return issueStageError("document projection lock", err)
		}
		if document.Status != models.DocumentStatusDraft {
			return &invoiceissue.InvalidLifecycleError{Reason: "document projection is not draft"}
		}
		if (command.DocumentType == invoiceissue.DocumentTypeBillOfSupply) != document.BillOfSupply {
			return &invoiceissue.InvalidLifecycleError{Reason: "document type does not match legal projection"}
		}

		var nextNumber int
		sequence := tx.Raw(`
			INSERT INTO document_sequences
				(business_id, document_type, financial_year, series, last_number, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, NOW(), NOW())
			ON CONFLICT (business_id, document_type, financial_year, series)
			DO UPDATE SET last_number = document_sequences.last_number + 1, updated_at = NOW()
			WHERE document_sequences.last_number < 999999
			RETURNING last_number
		`, command.BusinessID, command.DocumentType, financialYear, command.Series).Scan(&nextNumber)
		if sequence.Error != nil {
			return issueStageError("sequence allocation", sequence.Error)
		}
		if sequence.RowsAffected != 1 || nextNumber < 1 {
			return &invoiceissue.SequenceExhaustedError{Number: 1000000}
		}
		invoiceNumber, err := invoiceissue.FormatNumber(command.DocumentType, command.Series, financialYear, nextNumber)
		if err != nil {
			return err
		}

		issuedAt := time.Now().UTC()
		nextVersion := invoice.Version + 1
		updateInvoice := tx.Model(&models.Invoice{}).
			Where("id = ? AND business_id = ? AND version = ? AND status = ? AND deleted_at IS NULL",
				invoice.ID, invoice.BusinessID, invoice.Version, models.InvoiceStatusDraft).
			Updates(map[string]interface{}{
				"invoice_no": invoiceNumber,
				"issued_at":  issuedAt,
				"status":     models.InvoiceStatusIssued,
				"version":    nextVersion,
				"updated_at": issuedAt,
			})
		if updateInvoice.Error != nil {
			return issueStageError("invoice update", updateInvoice.Error)
		}
		if updateInvoice.RowsAffected != 1 {
			return issueStageError("invoice update", errors.New("locked draft was not updated"))
		}
		invoice.InvoiceNo = &invoiceNumber
		invoice.IssuedAt = &issuedAt
		invoice.Status = models.InvoiceStatusIssued
		invoice.Version = nextVersion
		invoice.UpdatedAt = issuedAt

		sourceLinkage := map[string]interface{}{}
		if document.SourceLinkage != "" {
			_ = json.Unmarshal([]byte(document.SourceLinkage), &sourceLinkage)
		}
		sourceLinkage["source_invoice_id"] = invoice.ID
		sourceLinkage["source_invoice_version"] = invoice.Version
		sourceLinkage["issued_at"] = issuedAt
		sourceLinkage["seller_snapshot"] = invoice.SellerSnapshot
		sourceLinkage["buyer_snapshot"] = invoice.BuyerSnapshot
		sourceJSON, err := json.Marshal(sourceLinkage)
		if err != nil {
			return issueStageError("document projection", err)
		}
		updateDocument := tx.Model(&models.Document{}).
			Where("id = ? AND business_id = ? AND status = ? AND deleted_at IS NULL",
				document.ID, document.BusinessID, models.DocumentStatusDraft).
			Updates(map[string]interface{}{
				"status":         models.DocumentStatusIssued,
				"draft_state":    models.DocumentDraftStateFinal,
				"serial_number":  invoiceNumber,
				"issue_date":     invoice.InvoiceDate,
				"source_linkage": string(sourceJSON),
				"updated_at":     issuedAt,
			})
		if updateDocument.Error != nil {
			return issueStageError("document projection", updateDocument.Error)
		}
		if updateDocument.RowsAffected != 1 {
			return issueStageError("document projection", errors.New("draft projection was not updated"))
		}

		finalRender = &models.DocumentRenderJob{
			ID:                   uuid.NewString(),
			DocumentID:           &invoice.ID,
			InvoiceID:            &invoice.ID,
			BusinessID:           invoice.BusinessID,
			RenderProfileID:      invoice.RenderProfileID,
			Kind:                 models.RenderKindFinal,
			SourceInvoiceVersion: &invoice.Version,
			ObjectKey:            fmt.Sprintf("invoices/%s/%s/v%d/final.pdf", invoice.BusinessID, invoice.ID, invoice.Version),
			Status:               models.RenderJobStatusQueued,
			Locale:               firstNonEmptyIssue(document.Locale, "en-IN"),
			TemplateVersion:      "v1",
			OutputURL:            "",
		}
		if err := tx.Create(finalRender).Error; err != nil {
			return issueStageError("final render", err)
		}

		payload, err := json.Marshal(map[string]interface{}{
			"schema_version":  1,
			"aggregate_type":  "invoice",
			"aggregate_id":    invoice.ID,
			"invoice_no":      invoiceNumber,
			"invoice_version": invoice.Version,
			"render_job_id":   finalRender.ID,
		})
		if err != nil {
			return issueStageError("outbox event", err)
		}
		event := &models.OutboxEvent{
			ID:            uuid.NewString(),
			BusinessID:    invoice.BusinessID,
			AggregateType: "invoice",
			AggregateID:   invoice.ID,
			EventType:     "invoice.issued.v1",
			Payload:       string(payload),
			AvailableAt:   issuedAt,
		}
		if err := tx.Create(event).Error; err != nil {
			return issueStageError("outbox event", err)
		}
		activity := &models.ActivityLog{
			ID:         uuid.NewString(),
			BusinessID: invoice.BusinessID,
			ActorID:    command.ActorID,
			ActorRole:  command.ActorRole,
			RequestID:  command.RequestID,
			IPAddress:  command.IPAddress,
			EntityType: "invoice",
			EntityID:   invoice.ID,
			Action:     "issued",
			Snapshot:   mustMarshalIssue(invoice),
			Diff:       fmt.Sprintf(`{"status":"issued","invoice_no":%q,"version":%d}`, invoiceNumber, invoice.Version),
			Metadata:   fmt.Sprintf(`{"render_job_id":%q}`, finalRender.ID),
		}
		if err := tx.Create(activity).Error; err != nil {
			return issueStageError("activity", err)
		}
		if err := completeInvoiceIssueIdempotency(tx, command, invoice.ID); err != nil {
			return err
		}
		issuedInvoice = &invoice
		return nil
	})
	if err != nil {
		if isInvoiceIssueTypedError(err) {
			return nil, err
		}
		return nil, issueStageError("transaction commit", err)
	}
	if replayInvoiceID != "" {
		invoice, render, err := r.getIssueReplay(ctx, replayInvoiceID, command.BusinessID)
		if err != nil {
			return nil, issueStageError("idempotency result replay", err)
		}
		return &interfaces.AtomicInvoiceIssueResult{Invoice: invoice, FinalRender: render, Replayed: true}, nil
	}
	return &interfaces.AtomicInvoiceIssueResult{Invoice: issuedInvoice, FinalRender: finalRender}, nil
}

func validateAtomicInvoiceIssue(command interfaces.AtomicInvoiceIssue) error {
	if command.BusinessID == "" || command.InvoiceID == "" || command.Command != "invoice.issue" ||
		command.IdempotencyKey == "" || command.RequestHash == "" || command.ExpectedVersion < 1 ||
		command.ActorID == "" {
		return errors.New("missing required atomic issue command field")
	}
	if _, err := uuid.Parse(command.BusinessID); err != nil {
		return errors.New("invalid issue business")
	}
	if _, err := uuid.Parse(command.InvoiceID); err != nil {
		return errors.New("invalid issue invoice")
	}
	if _, err := uuid.Parse(command.ActorID); err != nil {
		return errors.New("invalid issue actor")
	}
	_, err := invoiceissue.FormatNumber(command.DocumentType, command.Series, "2000-2001", 1)
	return err
}

func claimInvoiceIssueIdempotency(
	tx *gorm.DB,
	command interfaces.AtomicInvoiceIssue,
) (bool, string, error) {
	claim := &models.APIIdempotencyKey{
		BusinessID:     command.BusinessID,
		Command:        command.Command,
		IdempotencyKey: command.IdempotencyKey,
		RequestHash:    command.RequestHash,
		Status:         models.IdempotencyStatusInProgress,
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "business_id"}, {Name: "command"}, {Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(claim)
	if result.Error != nil {
		return false, "", issueStageError("idempotency claim", result.Error)
	}
	if result.RowsAffected == 1 {
		return true, "", nil
	}
	var existing models.APIIdempotencyKey
	if err := tx.Where(
		"business_id = ? AND command = ? AND idempotency_key = ?",
		command.BusinessID, command.Command, command.IdempotencyKey,
	).First(&existing).Error; err != nil {
		return false, "", issueStageError("idempotency replay lookup", err)
	}
	if existing.RequestHash != command.RequestHash {
		return false, "", &idempotency.ConflictError{}
	}
	if existing.Status != models.IdempotencyStatusCompleted ||
		existing.ResultType == nil || *existing.ResultType != "invoice_issue" ||
		existing.ResultID == nil {
		return false, "", &idempotency.InProgressError{}
	}
	return false, *existing.ResultID, nil
}

func completeInvoiceIssueIdempotency(tx *gorm.DB, command interfaces.AtomicInvoiceIssue, invoiceID string) error {
	now := time.Now().UTC()
	resultType := "invoice_issue"
	result := tx.Model(&models.APIIdempotencyKey{}).
		Where(
			"business_id = ? AND command = ? AND idempotency_key = ? AND request_hash = ? AND status = ?",
			command.BusinessID, command.Command, command.IdempotencyKey, command.RequestHash,
			models.IdempotencyStatusInProgress,
		).
		Updates(map[string]interface{}{
			"status":       models.IdempotencyStatusCompleted,
			"result_type":  resultType,
			"result_id":    invoiceID,
			"completed_at": now,
			"updated_at":   now,
		})
	if result.Error != nil {
		return issueStageError("idempotency completion", result.Error)
	}
	if result.RowsAffected != 1 {
		return issueStageError("idempotency completion", errors.New("claim was not completed"))
	}
	return nil
}

func (r *invoiceRepository) getIssueReplay(
	ctx context.Context,
	invoiceID, businessID string,
) (*models.Invoice, *models.DocumentRenderJob, error) {
	invoice, err := r.getReplayInvoice(ctx, invoiceID, businessID)
	if err != nil {
		return nil, nil, err
	}
	var render models.DocumentRenderJob
	err = r.db.WithContext(ctx).Unscoped().
		Where("invoice_id = ? AND business_id = ? AND kind = ? AND source_invoice_version = ?",
			invoiceID, businessID, models.RenderKindFinal, invoice.Version).
		First(&render).Error
	return invoice, &render, err
}

func isInvoiceIssueTypedError(err error) bool {
	var notFound *invoiceissue.NotFoundError
	var stale *invoiceissue.StaleVersionError
	var issued *invoiceissue.AlreadyIssuedError
	var lifecycle *invoiceissue.InvalidLifecycleError
	var timezone *invoiceissue.InvalidTimezoneError
	var series *invoiceissue.InvalidSeriesError
	var documentType *invoiceissue.InvalidDocumentTypeError
	var exhausted *invoiceissue.SequenceExhaustedError
	var conflict *idempotency.ConflictError
	var inProgress *idempotency.InProgressError
	var persistence *atomicInvoiceIssuePersistenceError
	return errors.As(err, &notFound) ||
		errors.As(err, &stale) ||
		errors.As(err, &issued) ||
		errors.As(err, &lifecycle) ||
		errors.As(err, &timezone) ||
		errors.As(err, &series) ||
		errors.As(err, &documentType) ||
		errors.As(err, &exhausted) ||
		errors.As(err, &conflict) ||
		errors.As(err, &inProgress) ||
		errors.As(err, &persistence)
}

func mustMarshalIssue(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func firstNonEmptyIssue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
