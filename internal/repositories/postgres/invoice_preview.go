package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceissue"
	"invoice-backend/internal/invoiceprojection"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type atomicInvoicePreviewPersistenceError struct {
	stage string
	cause error
}

func (e *atomicInvoicePreviewPersistenceError) Error() string {
	return fmt.Sprintf("atomic invoice preview persistence failed at %s", e.stage)
}

func (e *atomicInvoicePreviewPersistenceError) Unwrap() error { return e.cause }

func previewStageError(stage string, err error) error {
	return &atomicInvoicePreviewPersistenceError{stage: stage, cause: err}
}

func (r *invoiceRepository) RequestPreviewAtomic(
	ctx context.Context,
	command interfaces.AtomicInvoicePreview,
) (*interfaces.AtomicInvoicePreviewResult, error) {
	if err := validateAtomicInvoicePreview(command); err != nil {
		return nil, previewStageError("command validation", err)
	}

	var renderJob *models.DocumentRenderJob
	var outboxEvent *models.OutboxEvent
	var replayJobID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimed, resultID, err := claimInvoicePreviewIdempotency(tx, command)
		if err != nil {
			return err
		}
		if !claimed {
			replayJobID = resultID
			return nil
		}

		var invoice models.Invoice
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", command.InvoiceID, command.BusinessID).
			First(&invoice).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &invoiceissue.NotFoundError{}
			}
			return previewStageError("invoice lock", err)
		}
		if invoice.Status != models.InvoiceStatusDraft || invoice.InvoiceNo != nil || invoice.IssuedAt != nil {
			return &invoiceissue.InvalidLifecycleError{Reason: "only draft invoices can be previewed"}
		}
		if err := invoice.ValidateState(); err != nil {
			return &invoiceissue.InvalidLifecycleError{Reason: err.Error()}
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("invoice_id = ?", invoice.ID).
			Order("created_at ASC, id ASC").
			Find(&invoice.Items).Error; err != nil {
			return previewStageError("invoice items lock", err)
		}

		var document models.Document
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", command.InvoiceID, command.BusinessID).
			First(&document).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &invoiceissue.InvalidLifecycleError{Reason: "canonical document projection is required"}
			}
			return previewStageError("document projection lock", err)
		}
		if document.Status != models.DocumentStatusDraft || document.DraftState != models.DocumentDraftStateDraft {
			return &invoiceissue.InvalidLifecycleError{Reason: "canonical document projection is not draft"}
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("document_id = ?", document.ID).
			Order("created_at ASC, id ASC").
			Find(&document.Lines).Error; err != nil {
			return previewStageError("document projection lines lock", err)
		}
		if err := invoiceprojection.Validate(&invoice, &document); err != nil {
			return &invoiceissue.InvalidLifecycleError{Reason: err.Error()}
		}
		renderProfileID, err := resolveInvoicePreviewProfile(tx, command.BusinessID, &invoice, &document)
		if err != nil {
			return previewStageError("render profile", err)
		}

		now := time.Now().UTC()
		renderJob = newInvoicePreviewRenderJob(command, &invoice, &document, renderProfileID, now)
		if err := tx.Create(renderJob).Error; err != nil {
			return previewStageError("render job", err)
		}
		outboxEvent, err = newInvoicePreviewOutboxEvent(&invoice, renderJob, now)
		if err != nil {
			return previewStageError("outbox event", err)
		}
		if err := tx.Create(outboxEvent).Error; err != nil {
			return previewStageError("outbox event", err)
		}
		if err := tx.Create(newInvoicePreviewActivity(command, renderJob)).Error; err != nil {
			return previewStageError("activity", err)
		}
		if err := completeInvoicePreviewIdempotency(tx, command, renderJob.ID, now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if isInvoicePreviewTypedError(err) {
			return nil, err
		}
		return nil, previewStageError("transaction commit", err)
	}
	if replayJobID != "" {
		job, err := r.getPreviewReplay(ctx, replayJobID, command.BusinessID, command.InvoiceID)
		if err != nil {
			return nil, previewStageError("idempotency result replay", err)
		}
		return &interfaces.AtomicInvoicePreviewResult{RenderJob: job, Replayed: true}, nil
	}
	return &interfaces.AtomicInvoicePreviewResult{
		RenderJob:   renderJob,
		OutboxEvent: outboxEvent,
	}, nil
}

func validateAtomicInvoicePreview(command interfaces.AtomicInvoicePreview) error {
	if command.Command != "invoice.preview" || command.IdempotencyKey == "" ||
		command.RequestHash == "" || command.ActorID == "" {
		return errors.New("missing required atomic preview command field")
	}
	if _, err := uuid.Parse(command.BusinessID); err != nil {
		return errors.New("invalid preview business")
	}
	if _, err := uuid.Parse(command.InvoiceID); err != nil {
		return errors.New("invalid preview invoice")
	}
	if _, err := uuid.Parse(command.ActorID); err != nil {
		return errors.New("invalid preview actor")
	}
	return nil
}

func claimInvoicePreviewIdempotency(
	tx *gorm.DB,
	command interfaces.AtomicInvoicePreview,
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
		return false, "", previewStageError("idempotency claim", result.Error)
	}
	if result.RowsAffected == 1 {
		return true, "", nil
	}

	var existing models.APIIdempotencyKey
	if err := tx.Where(
		"business_id = ? AND command = ? AND idempotency_key = ?",
		command.BusinessID, command.Command, command.IdempotencyKey,
	).First(&existing).Error; err != nil {
		return false, "", previewStageError("idempotency replay lookup", err)
	}
	if existing.RequestHash != command.RequestHash {
		return false, "", &idempotency.ConflictError{}
	}
	if existing.Status != models.IdempotencyStatusCompleted {
		return false, "", &idempotency.InProgressError{}
	}
	if existing.ResultType == nil || *existing.ResultType != "invoice_preview" ||
		existing.ResultID == nil {
		return false, "", previewStageError(
			"idempotency replay result",
			errors.New("completed preview idempotency result is invalid"),
		)
	}
	return false, *existing.ResultID, nil
}

func newInvoicePreviewRenderJob(
	command interfaces.AtomicInvoicePreview,
	invoice *models.Invoice,
	document *models.Document,
	renderProfileID *string,
	now time.Time,
) *models.DocumentRenderJob {
	jobID := uuid.NewString()
	return &models.DocumentRenderJob{
		ID:                   jobID,
		DocumentID:           &invoice.ID,
		InvoiceID:            &invoice.ID,
		BusinessID:           command.BusinessID,
		RenderProfileID:      renderProfileID,
		Kind:                 models.RenderKindPreview,
		SourceInvoiceVersion: &invoice.Version,
		ObjectKey: fmt.Sprintf(
			"invoices/%s/%s/previews/v%d/%s.pdf",
			command.BusinessID, invoice.ID, invoice.Version, jobID,
		),
		Status:          models.RenderJobStatusQueued,
		Locale:          firstNonEmptyIssue(document.Locale, "en-IN"),
		TemplateVersion: "v1",
		RequestedAt:     now,
	}
}

func resolveInvoicePreviewProfile(
	tx *gorm.DB,
	businessID string,
	invoice *models.Invoice,
	document *models.Document,
) (*string, error) {
	if invoice.RenderProfileID != nil {
		profileID := *invoice.RenderProfileID
		return &profileID, nil
	}
	if document.RenderProfileID != nil {
		profileID := *document.RenderProfileID
		return &profileID, nil
	}
	var profile models.RenderProfile
	err := tx.Where("business_id = ? AND is_default = ? AND deleted_at IS NULL", businessID, true).
		First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile.ID, nil
}

func newInvoicePreviewOutboxEvent(
	invoice *models.Invoice,
	job *models.DocumentRenderJob,
	now time.Time,
) (*models.OutboxEvent, error) {
	payload, err := json.Marshal(map[string]interface{}{
		"schema_version":  1,
		"type":            "generate_document_pdf",
		"aggregate_type":  "invoice",
		"aggregate_id":    invoice.ID,
		"invoice_id":      invoice.ID,
		"document_id":     invoice.ID,
		"invoice_version": invoice.Version,
		"render_job_id":   job.ID,
	})
	if err != nil {
		return nil, err
	}
	return &models.OutboxEvent{
		ID:            uuid.NewString(),
		BusinessID:    invoice.BusinessID,
		AggregateType: "invoice",
		AggregateID:   invoice.ID,
		EventType:     "invoice.preview.requested.v1",
		Payload:       string(payload),
		AvailableAt:   now,
	}, nil
}

func newInvoicePreviewActivity(
	command interfaces.AtomicInvoicePreview,
	job *models.DocumentRenderJob,
) *models.ActivityLog {
	return &models.ActivityLog{
		ID:         uuid.NewString(),
		BusinessID: command.BusinessID,
		ActorID:    command.ActorID,
		ActorRole:  command.ActorRole,
		RequestID:  command.RequestID,
		IPAddress:  command.IPAddress,
		EntityType: "invoice",
		EntityID:   command.InvoiceID,
		Action:     "preview_requested",
		Snapshot:   mustMarshalIssue(job),
		Diff:       "{}",
		Metadata: fmt.Sprintf(
			`{"render_job_id":%q,"source_invoice_version":%d}`,
			job.ID, *job.SourceInvoiceVersion,
		),
	}
}

func completeInvoicePreviewIdempotency(
	tx *gorm.DB,
	command interfaces.AtomicInvoicePreview,
	jobID string,
	now time.Time,
) error {
	result := tx.Model(&models.APIIdempotencyKey{}).
		Where(
			"business_id = ? AND command = ? AND idempotency_key = ? AND request_hash = ? AND status = ?",
			command.BusinessID, command.Command, command.IdempotencyKey, command.RequestHash,
			models.IdempotencyStatusInProgress,
		).
		Updates(map[string]interface{}{
			"status":       models.IdempotencyStatusCompleted,
			"result_type":  "invoice_preview",
			"result_id":    jobID,
			"completed_at": now,
			"updated_at":   now,
		})
	if result.Error != nil {
		return previewStageError("idempotency completion", result.Error)
	}
	if result.RowsAffected != 1 {
		return previewStageError("idempotency completion", errors.New("claim was not completed"))
	}
	return nil
}

func (r *invoiceRepository) getPreviewReplay(
	ctx context.Context,
	jobID, businessID, invoiceID string,
) (*models.DocumentRenderJob, error) {
	var job models.DocumentRenderJob
	err := r.db.WithContext(ctx).
		Where(
			"id = ? AND business_id = ? AND kind = ? AND deleted_at IS NULL",
			jobID, businessID, models.RenderKindPreview,
		).
		First(&job).Error
	if err != nil {
		return nil, err
	}
	if job.InvoiceID == nil || job.DocumentID == nil || job.SourceInvoiceVersion == nil ||
		*job.InvoiceID != invoiceID || *job.DocumentID != invoiceID || *job.SourceInvoiceVersion < 1 {
		return nil, errors.New("stored preview result is invalid")
	}
	return &job, nil
}

func isInvoicePreviewTypedError(err error) bool {
	var notFound *invoiceissue.NotFoundError
	var invalid *invoiceissue.InvalidLifecycleError
	var conflict *idempotency.ConflictError
	var inProgress *idempotency.InProgressError
	var persistence *atomicInvoicePreviewPersistenceError
	return errors.As(err, &notFound) ||
		errors.As(err, &invalid) ||
		errors.As(err, &conflict) ||
		errors.As(err, &inProgress) ||
		errors.As(err, &persistence)
}
