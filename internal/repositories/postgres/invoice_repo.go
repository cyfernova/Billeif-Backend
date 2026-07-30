package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type invoiceRepository struct {
	db *gorm.DB
}

func NewInvoiceRepository(db *gorm.DB) interfaces.CanonicalInvoiceRepository {
	return &invoiceRepository{db: db}
}

type atomicInvoicePersistenceError struct {
	stage string
	cause error
}

func (e *atomicInvoicePersistenceError) Error() string {
	return fmt.Sprintf("atomic invoice draft persistence failed at %s", e.stage)
}

func (e *atomicInvoicePersistenceError) Unwrap() error {
	return e.cause
}

func (r *invoiceRepository) ReplayCompletedDraft(
	ctx context.Context,
	businessID, command, idempotencyKey, requestHash string,
) (*interfaces.AtomicInvoiceDraftResult, error) {
	var existing models.APIIdempotencyKey
	err := r.db.WithContext(ctx).
		Where(
			"business_id = ? AND command = ? AND idempotency_key = ?",
			businessID,
			command,
			idempotencyKey,
		).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, atomicStageError("idempotency replay lookup", err)
	}
	if existing.RequestHash != requestHash {
		return nil, &idempotency.ConflictError{}
	}
	if existing.Status != models.IdempotencyStatusCompleted ||
		existing.ResultType == nil || *existing.ResultType != "invoice" ||
		existing.ResultID == nil {
		return nil, nil
	}
	invoice, err := r.getReplayInvoice(ctx, *existing.ResultID, businessID)
	if err != nil {
		return nil, atomicStageError("idempotency result replay", err)
	}
	return &interfaces.AtomicInvoiceDraftResult{Invoice: invoice, Replayed: true}, nil
}

func (r *invoiceRepository) CreateDraftAtomic(ctx context.Context, command interfaces.AtomicInvoiceDraft) (*interfaces.AtomicInvoiceDraftResult, error) {
	if err := validateAtomicInvoiceDraft(command); err != nil {
		return nil, atomicStageError("command validation", err)
	}
	var replayInvoiceID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claim := &models.APIIdempotencyKey{
			BusinessID:     command.BusinessID,
			Command:        command.Command,
			IdempotencyKey: command.IdempotencyKey,
			RequestHash:    command.RequestHash,
			Status:         models.IdempotencyStatusInProgress,
		}
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "business_id"},
				{Name: "command"},
				{Name: "idempotency_key"},
			},
			DoNothing: true,
		}).Create(claim)
		if result.Error != nil {
			return atomicStageError("idempotency claim", result.Error)
		}
		if result.RowsAffected == 0 {
			var existing models.APIIdempotencyKey
			if err := tx.Where(
				"business_id = ? AND command = ? AND idempotency_key = ?",
				command.BusinessID,
				command.Command,
				command.IdempotencyKey,
			).First(&existing).Error; err != nil {
				return atomicStageError("idempotency replay lookup", err)
			}
			if existing.RequestHash != command.RequestHash {
				return &idempotency.ConflictError{}
			}
			if existing.Status != models.IdempotencyStatusCompleted ||
				existing.ResultType == nil || *existing.ResultType != "invoice" ||
				existing.ResultID == nil {
				return &idempotency.InProgressError{}
			}
			replayInvoiceID = *existing.ResultID
			return nil
		}

		if err := tx.Omit(clause.Associations).Create(command.Invoice).Error; err != nil {
			return atomicStageError("invoice", err)
		}
		for _, item := range command.Invoice.Items {
			if err := tx.Create(item).Error; err != nil {
				return atomicStageError("invoice items", err)
			}
		}
		if err := tx.Omit(clause.Associations).Create(command.Document).Error; err != nil {
			return atomicStageError("document projection", err)
		}
		for _, line := range command.Document.Lines {
			if err := tx.Create(line).Error; err != nil {
				return atomicStageError("document projection items", err)
			}
		}
		if err := tx.Create(command.Activity).Error; err != nil {
			return atomicStageError("activity", err)
		}
		for _, event := range command.OutboxEvents {
			if err := tx.Create(event).Error; err != nil {
				return atomicStageError("outbox events", err)
			}
		}
		for _, job := range command.RenderJobs {
			if err := tx.Create(job).Error; err != nil {
				return atomicStageError("render jobs", err)
			}
		}
		for _, delivery := range command.EmailDeliveries {
			if err := tx.Create(delivery).Error; err != nil {
				return atomicStageError("email deliveries", err)
			}
		}

		now := time.Now().UTC()
		resultType := "invoice"
		update := tx.Model(&models.APIIdempotencyKey{}).
			Where(
				"business_id = ? AND command = ? AND idempotency_key = ? AND request_hash = ? AND status = ?",
				command.BusinessID,
				command.Command,
				command.IdempotencyKey,
				command.RequestHash,
				models.IdempotencyStatusInProgress,
			).
			Updates(map[string]interface{}{
				"status":       models.IdempotencyStatusCompleted,
				"result_type":  resultType,
				"result_id":    command.Invoice.ID,
				"completed_at": now,
				"updated_at":   now,
			})
		if update.Error != nil {
			return atomicStageError("idempotency completion", update.Error)
		}
		if update.RowsAffected != 1 {
			return atomicStageError("idempotency completion", errors.New("claim was not completed"))
		}
		return nil
	})
	if err != nil {
		var typedConflict *idempotency.ConflictError
		var typedInProgress *idempotency.InProgressError
		var persistence *atomicInvoicePersistenceError
		if errors.As(err, &typedConflict) || errors.As(err, &typedInProgress) || errors.As(err, &persistence) {
			return nil, err
		}
		return nil, atomicStageError("transaction commit", err)
	}
	if replayInvoiceID != "" {
		invoice, err := r.getReplayInvoice(ctx, replayInvoiceID, command.BusinessID)
		if err != nil {
			return nil, atomicStageError("idempotency result replay", err)
		}
		return &interfaces.AtomicInvoiceDraftResult{Invoice: invoice, Replayed: true}, nil
	}
	return &interfaces.AtomicInvoiceDraftResult{Invoice: command.Invoice}, nil
}

func validateAtomicInvoiceDraft(command interfaces.AtomicInvoiceDraft) error {
	if command.BusinessID == "" || command.Command == "" || command.IdempotencyKey == "" || command.RequestHash == "" ||
		command.Invoice == nil || command.Document == nil || command.Activity == nil {
		return errors.New("missing required atomic command field")
	}
	if command.Invoice.BusinessID != command.BusinessID ||
		command.Document.BusinessID != command.BusinessID ||
		command.Activity.BusinessID != command.BusinessID {
		return errors.New("atomic command tenant mismatch")
	}
	if command.Invoice.ID == "" || command.Document.ID != command.Invoice.ID ||
		command.Activity.EntityID != command.Invoice.ID {
		return errors.New("atomic command aggregate mismatch")
	}
	for _, item := range command.Invoice.Items {
		if item == nil || item.InvoiceID != command.Invoice.ID {
			return errors.New("atomic invoice item mismatch")
		}
	}
	for _, line := range command.Document.Lines {
		if line == nil || line.DocumentID != command.Document.ID {
			return errors.New("atomic document line mismatch")
		}
	}
	for _, event := range command.OutboxEvents {
		if event == nil || event.BusinessID != command.BusinessID {
			return errors.New("atomic outbox tenant mismatch")
		}
	}
	for _, job := range command.RenderJobs {
		if job == nil || job.BusinessID != command.BusinessID {
			return errors.New("atomic render tenant mismatch")
		}
		if job.DocumentID != nil && *job.DocumentID != command.Document.ID {
			return errors.New("atomic render document mismatch")
		}
		if job.InvoiceID != nil && *job.InvoiceID != command.Invoice.ID {
			return errors.New("atomic render invoice mismatch")
		}
	}
	for _, delivery := range command.EmailDeliveries {
		if delivery == nil || delivery.BusinessID != command.BusinessID {
			return errors.New("atomic email tenant mismatch")
		}
		if delivery.InvoiceID != nil && *delivery.InvoiceID != command.Invoice.ID {
			return errors.New("atomic email invoice mismatch")
		}
	}
	return nil
}

func (r *invoiceRepository) getReplayInvoice(ctx context.Context, id, businessID string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.db.WithContext(ctx).
		Unscoped().
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC, id ASC")
		}).
		Where("id = ? AND business_id = ?", id, businessID).
		First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.Invoice{ID: id, BusinessID: businessID}, nil
	}
	if err != nil {
		return nil, err
	}
	return &invoice, nil
}

func atomicStageError(stage string, err error) error {
	return &atomicInvoicePersistenceError{stage: stage, cause: err}
}

func (r *invoiceRepository) Create(ctx context.Context, invoice *models.Invoice) error {
	return r.db.WithContext(ctx).Create(invoice).Error
}

func (r *invoiceRepository) GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.db.WithContext(ctx).Preload("Items").Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("invoice not found")
	}
	return &invoice, err
}

func (r *invoiceRepository) GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.db.WithContext(ctx).Preload("Items").Where("id = ? AND deleted_at IS NULL", id).First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("invoice not found")
	}
	return &invoice, err
}

func (r *invoiceRepository) GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.db.WithContext(ctx).Where("business_id = ? AND invoice_no = ? AND deleted_at IS NULL", businessID, invoiceNo).First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("invoice not found")
	}
	return &invoice, err
}

func (r *invoiceRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	var invoices []models.Invoice
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.Invoice{}).Where("business_id = ? AND deleted_at IS NULL", businessID).Order("created_at DESC")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&invoices).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Invoice, len(invoices))
	for i := range invoices {
		result[i] = &invoices[i]
	}

	return result, total, nil
}

func (r *invoiceRepository) GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error) {
	var items []models.InvoiceItem
	err := r.db.WithContext(ctx).Where("invoice_id = ?", invoiceID).Find(&items).Error
	if err != nil {
		return nil, err
	}

	result := make([]*models.InvoiceItem, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result, nil
}

func (r *invoiceRepository) Update(ctx context.Context, invoice *models.Invoice) error {
	return r.db.WithContext(ctx).Save(invoice).Error
}

func (r *invoiceRepository) UpdateStatus(ctx context.Context, invoiceID string, status string) error {
	return r.db.WithContext(ctx).Model(&models.Invoice{}).Where("id = ?", invoiceID).Update("status", status).Error
}

func (r *invoiceRepository) UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error {
	return r.db.WithContext(ctx).Model(&models.Invoice{}).Where("id = ?", invoiceID).Update("pdf_url", pdfURL).Error
}

func (r *invoiceRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Invoice{}).Error
}
