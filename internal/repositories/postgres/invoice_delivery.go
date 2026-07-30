package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const invoiceDeliveryRequestedEvent = "invoice.delivery.requested.v1"

func (r *invoiceRepository) CreateDeliveryAtomic(
	ctx context.Context,
	command interfaces.AtomicInvoiceDelivery,
) (*interfaces.AtomicInvoiceDeliveryResult, error) {
	if err := validateAtomicInvoiceDelivery(command); err != nil {
		return nil, fmt.Errorf("atomic invoice delivery persistence failed at command validation: %w", err)
	}
	var result *interfaces.AtomicInvoiceDeliveryResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claim := &models.APIIdempotencyKey{
			BusinessID: command.BusinessID, Command: command.Command,
			IdempotencyKey: command.IdempotencyKey, RequestHash: command.RequestHash,
			Status: models.IdempotencyStatusInProgress,
		}
		created := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "business_id"}, {Name: "command"}, {Name: "idempotency_key"},
			},
			DoNothing: true,
		}).Create(claim)
		if created.Error != nil {
			return deliveryStageError("idempotency claim", created.Error)
		}
		if created.RowsAffected == 0 {
			replayed, err := replayInvoiceDelivery(tx, command)
			if err != nil {
				return err
			}
			result = replayed
			return nil
		}

		var finalJob models.DocumentRenderJob
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"business_id = ? AND invoice_id = ? AND kind = ? AND deleted_at IS NULL",
				command.BusinessID, command.InvoiceID, models.RenderKindFinal,
			).
			Order("source_invoice_version DESC, created_at DESC").
			First(&finalJob).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var invoice models.Invoice
			invoiceErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND business_id = ? AND deleted_at IS NULL", command.InvoiceID, command.BusinessID).
				First(&invoice).Error
			if errors.Is(invoiceErr, gorm.ErrRecordNotFound) {
				return interfaces.ErrInvoiceNotFound
			}
			if invoiceErr != nil {
				return deliveryStageError("invoice lock", invoiceErr)
			}
			return interfaces.ErrInvoiceNotDeliverable
		}
		if err != nil {
			return deliveryStageError("final render lock", err)
		}

		var invoice models.Invoice
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", command.InvoiceID, command.BusinessID).
			First(&invoice).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return interfaces.ErrInvoiceNotFound
			}
			return deliveryStageError("invoice lock", err)
		}
		if !invoiceHasDeliverableIssuanceFacts(&invoice) || finalJob.InvoiceID == nil ||
			*finalJob.InvoiceID != invoice.ID || finalJob.SourceInvoiceVersion == nil ||
			*finalJob.SourceInvoiceVersion != invoice.Version {
			return interfaces.ErrInvoiceNotDeliverable
		}

		status := models.EmailDeliveryStatusWaitingForRender
		switch finalJob.Status {
		case models.RenderJobStatusQueued, models.RenderJobStatusProcessing, models.RenderJobStatusFailed:
		case models.RenderJobStatusCompleted:
			if strings.TrimSpace(finalJob.ObjectKey) == "" {
				return deliveryStageError("final render validation", errors.New("completed final render has no object key"))
			}
			status = models.EmailDeliveryStatusQueued
		default:
			return interfaces.ErrInvoiceNotDeliverable
		}
		now := time.Now().UTC()
		delivery := &models.EmailDelivery{
			ID: uuid.NewString(), BusinessID: command.BusinessID,
			InvoiceID: &invoice.ID, RenderJobID: &finalJob.ID,
			Recipient: command.Recipient, Status: status, Metadata: "{}",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(delivery).Error; err != nil {
			return deliveryStageError("delivery insert", err)
		}
		if err := tx.Create(newInvoiceDeliveryActivity(command, delivery)).Error; err != nil {
			return deliveryStageError("activity insert", err)
		}
		var outboxEvent *models.OutboxEvent
		if status == models.EmailDeliveryStatusQueued {
			event, err := newInvoiceDeliveryOutboxEvent(delivery, now)
			if err != nil {
				return deliveryStageError("outbox payload", err)
			}
			if err := tx.Create(event).Error; err != nil {
				return deliveryStageError("outbox insert", err)
			}
			outboxEvent = event
		}
		completed := tx.Model(&models.APIIdempotencyKey{}).
			Where(
				"business_id = ? AND command = ? AND idempotency_key = ? AND request_hash = ? AND status = ?",
				command.BusinessID, command.Command, command.IdempotencyKey,
				command.RequestHash, models.IdempotencyStatusInProgress,
			).
			Updates(map[string]interface{}{
				"status": models.IdempotencyStatusCompleted, "result_type": "invoice_delivery",
				"result_id": delivery.ID, "completed_at": now, "updated_at": now,
			})
		if completed.Error != nil {
			return deliveryStageError("idempotency completion", completed.Error)
		}
		if completed.RowsAffected != 1 {
			return deliveryStageError("idempotency completion", errors.New("idempotency claim was not completed"))
		}
		result = &interfaces.AtomicInvoiceDeliveryResult{
			Delivery: delivery, OutboxEvent: outboxEvent,
		}
		return nil
	})
	return result, err
}

func invoiceHasDeliverableIssuanceFacts(invoice *models.Invoice) bool {
	if invoice == nil || invoice.InvoiceNo == nil || invoice.IssuedAt == nil {
		return false
	}
	switch invoice.Status {
	case models.InvoiceStatusIssued,
		models.InvoiceStatusSent,
		models.InvoiceStatusPartiallyPaid,
		models.InvoiceStatusPaid,
		models.InvoiceStatusOverdue:
		return true
	default:
		return false
	}
}

func (r *invoiceRepository) GetInvoiceDelivery(
	ctx context.Context,
	businessID, invoiceID, deliveryID string,
) (*models.EmailDelivery, error) {
	var delivery models.EmailDelivery
	err := r.db.WithContext(ctx).
		Where(
			"id = ? AND business_id = ? AND invoice_id = ? AND deleted_at IS NULL",
			deliveryID, businessID, invoiceID,
		).
		First(&delivery).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfaces.ErrInvoiceDeliveryNotFound
	}
	if err != nil {
		return nil, deliveryStageError("delivery lookup", err)
	}
	return &delivery, nil
}

func replayInvoiceDelivery(
	tx *gorm.DB,
	command interfaces.AtomicInvoiceDelivery,
) (*interfaces.AtomicInvoiceDeliveryResult, error) {
	var existing models.APIIdempotencyKey
	if err := tx.Where(
		"business_id = ? AND command = ? AND idempotency_key = ?",
		command.BusinessID, command.Command, command.IdempotencyKey,
	).First(&existing).Error; err != nil {
		return nil, deliveryStageError("idempotency replay lookup", err)
	}
	if existing.RequestHash != command.RequestHash {
		return nil, &idempotency.ConflictError{}
	}
	if existing.Status != models.IdempotencyStatusCompleted {
		return nil, &idempotency.InProgressError{}
	}
	if existing.ResultType == nil || *existing.ResultType != "invoice_delivery" || existing.ResultID == nil {
		return nil, deliveryStageError("idempotency result replay", errors.New("completed delivery result is invalid"))
	}
	var delivery models.EmailDelivery
	if err := tx.Where(
		"id = ? AND business_id = ? AND invoice_id = ? AND deleted_at IS NULL",
		*existing.ResultID, command.BusinessID, command.InvoiceID,
	).First(&delivery).Error; err != nil {
		return nil, deliveryStageError("idempotency result replay", err)
	}
	if delivery.Recipient != command.Recipient || delivery.RenderJobID == nil {
		return nil, deliveryStageError("idempotency result replay", errors.New("delivery result identity mismatch"))
	}
	return &interfaces.AtomicInvoiceDeliveryResult{Delivery: &delivery, Replayed: true}, nil
}

func newInvoiceDeliveryOutboxEvent(
	delivery *models.EmailDelivery,
	now time.Time,
) (*models.OutboxEvent, error) {
	if delivery == nil || delivery.InvoiceID == nil || delivery.RenderJobID == nil {
		return nil, errors.New("delivery outbox identity is incomplete")
	}
	payload, err := json.Marshal(map[string]interface{}{
		"schema_version": 1,
		"delivery_id":    delivery.ID,
		"invoice_id":     *delivery.InvoiceID,
		"render_job_id":  *delivery.RenderJobID,
	})
	if err != nil {
		return nil, err
	}
	return &models.OutboxEvent{
		ID: uuid.NewString(), BusinessID: delivery.BusinessID,
		AggregateType: "email_delivery", AggregateID: delivery.ID,
		EventType: invoiceDeliveryRequestedEvent, Payload: string(payload),
		AvailableAt: now, CreatedAt: now,
	}, nil
}

func validateAtomicInvoiceDelivery(command interfaces.AtomicInvoiceDelivery) error {
	if _, err := uuid.Parse(command.BusinessID); err != nil {
		return errors.New("business ID is required")
	}
	if _, err := uuid.Parse(command.InvoiceID); err != nil {
		return errors.New("invoice ID is required")
	}
	if _, err := uuid.Parse(command.IdempotencyKey); err != nil {
		return errors.New("idempotency key is required")
	}
	if _, err := uuid.Parse(command.ActorID); err != nil {
		return errors.New("actor ID is required")
	}
	if command.Command != "invoice.delivery.create.v1" ||
		len(command.RequestHash) != 64 || command.Recipient == "" {
		return errors.New("delivery command identity is incomplete")
	}
	return nil
}

func newInvoiceDeliveryActivity(
	command interfaces.AtomicInvoiceDelivery,
	delivery *models.EmailDelivery,
) *models.ActivityLog {
	return &models.ActivityLog{
		ID: uuid.NewString(), BusinessID: command.BusinessID,
		ActorID: command.ActorID, ActorRole: command.ActorRole,
		RequestID: command.RequestID, IPAddress: command.IPAddress,
		EntityType: "invoice", EntityID: command.InvoiceID,
		Action:   "delivery_requested",
		Snapshot: mustMarshalIssue(delivery), Diff: "{}",
		Metadata: fmt.Sprintf(
			`{"delivery_id":%q,"render_job_id":%q}`,
			delivery.ID, *delivery.RenderJobID,
		),
	}
}

func deliveryStageError(stage string, err error) error {
	return fmt.Errorf("atomic invoice delivery persistence failed at %s: %w", stage, err)
}
