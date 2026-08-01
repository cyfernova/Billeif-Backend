package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/emaildelivery"
	"invoice-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EmailDeliveryWorkerRepository struct {
	db *gorm.DB
}

func NewEmailDeliveryWorkerRepository(db *gorm.DB) *EmailDeliveryWorkerRepository {
	return &EmailDeliveryWorkerRepository{db: db}
}

func (r *EmailDeliveryWorkerRepository) Claim(
	ctx context.Context,
	message emaildelivery.DeliveryMessage,
	owner string,
	now, leaseUntil time.Time,
) (*emaildelivery.ClaimedDelivery, error) {
	if err := validateEmailDeliveryLeaseInput(r, message.DeliveryID, owner, now); err != nil {
		return nil, err
	}
	if !leaseUntil.After(now) {
		return nil, errors.New("email delivery lease expiry must follow claim time")
	}

	const claimQuery = `
WITH candidate AS (
	SELECT d.id
	FROM email_deliveries AS d
	JOIN invoices AS i
		ON i.id = d.invoice_id
		AND i.business_id = d.business_id
		AND i.deleted_at IS NULL
	JOIN document_render_jobs AS j
		ON j.id = d.render_job_id
		AND j.invoice_id = d.invoice_id
		AND j.business_id = d.business_id
		AND j.deleted_at IS NULL
	WHERE d.id = ?
		AND d.business_id = ?
		AND d.invoice_id = ?
		AND d.render_job_id = ?
		AND d.deleted_at IS NULL
		AND d.status IN ('queued', 'failed', 'processing')
		AND (d.lease_expires_at IS NULL OR d.lease_expires_at <= ?)
		AND i.status IN ('issued', 'sent', 'partially_paid', 'paid', 'overdue')
		AND i.invoice_no IS NOT NULL
		AND i.issued_at IS NOT NULL
		AND j.kind = 'final'
		AND j.status = 'completed'
		AND j.source_invoice_version = i.version
		AND j.object_key = CONCAT(
			'invoices/', d.business_id, '/', d.invoice_id,
			'/v', i.version, '/final.pdf'
		)
	FOR UPDATE OF d SKIP LOCKED
)
UPDATE email_deliveries AS d
SET status = 'processing',
	lease_owner = ?,
	lease_expires_at = ?,
	attempts = d.attempts + 1,
	error_message = '',
	updated_at = ?
FROM candidate, invoices AS i, document_render_jobs AS j
WHERE d.id = candidate.id
	AND i.id = d.invoice_id
	AND j.id = d.render_job_id
RETURNING
	d.id AS delivery_id,
	d.business_id,
	d.invoice_id,
	d.render_job_id,
	d.recipient,
	i.invoice_no,
	i.version AS invoice_version,
	j.object_key,
	j.output_filename`

	var claim emaildelivery.ClaimedDelivery
	result := r.db.WithContext(ctx).Raw(
		claimQuery,
		message.DeliveryID,
		message.BusinessID,
		message.InvoiceID,
		message.RenderJobID,
		now,
		owner,
		leaseUntil,
		now,
	).Scan(&claim)
	if result.Error != nil {
		return nil, fmt.Errorf("claim email delivery: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return &claim, nil
	}
	return nil, r.classifyUnclaimed(ctx, message, now)
}

func (r *EmailDeliveryWorkerRepository) VerifyLease(
	ctx context.Context,
	deliveryID, owner string,
	now time.Time,
) error {
	if err := validateEmailDeliveryLeaseInput(r, deliveryID, owner, now); err != nil {
		return err
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.EmailDelivery{}).
		Where(
			"id = ? AND status = ? AND lease_owner = ? AND lease_expires_at > ? AND deleted_at IS NULL",
			deliveryID, models.EmailDeliveryStatusProcessing, owner, now,
		).
		Count(&count).Error; err != nil {
		return fmt.Errorf("verify email delivery lease: %w", err)
	}
	if count != 1 {
		return emaildelivery.ErrLeaseUnavailable
	}
	return nil
}

func (r *EmailDeliveryWorkerRepository) MarkSent(
	ctx context.Context,
	deliveryID, owner, providerMessageID, source, subject string,
	now time.Time,
) error {
	if err := validateEmailDeliveryLeaseInput(r, deliveryID, owner, now); err != nil {
		return err
	}
	providerMessageID = strings.TrimSpace(providerMessageID)
	source = strings.TrimSpace(source)
	subject = strings.TrimSpace(subject)
	if providerMessageID == "" || source == "" || subject == "" {
		return errors.New("sent delivery metadata is required")
	}
	const query = `
UPDATE email_deliveries
SET status = ?,
	provider_message_id = ?,
	source_email = ?,
	subject = ?,
	sent_at = ?,
	failed_at = NULL,
	error_message = '',
	lease_owner = NULL,
	lease_expires_at = NULL,
	updated_at = ?
WHERE id = ?
	AND status = ?
	AND lease_owner = ?
	AND lease_expires_at > ?
	AND deleted_at IS NULL`
	result := r.db.WithContext(ctx).Exec(
		query,
		models.EmailDeliveryStatusSent,
		providerMessageID,
		source,
		subject,
		now,
		now,
		deliveryID,
		models.EmailDeliveryStatusProcessing,
		owner,
		now,
	)
	if result.Error != nil {
		return fmt.Errorf("mark email delivery sent: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return emaildelivery.ErrLeaseUnavailable
	}
	return nil
}

func (r *EmailDeliveryWorkerRepository) MarkFailed(
	ctx context.Context,
	deliveryID, owner, failure string,
	now time.Time,
) error {
	if err := validateEmailDeliveryLeaseInput(r, deliveryID, owner, now); err != nil {
		return err
	}
	failure = strings.TrimSpace(failure)
	if failure == "" {
		return errors.New("email delivery failure is required")
	}
	const query = `
UPDATE email_deliveries
SET status = ?,
	error_message = ?,
	failed_at = ?,
	lease_owner = NULL,
	lease_expires_at = NULL,
	updated_at = ?
WHERE id = ?
	AND status = ?
	AND lease_owner = ?
	AND lease_expires_at > ?
	AND deleted_at IS NULL`
	result := r.db.WithContext(ctx).Exec(
		query,
		models.EmailDeliveryStatusFailed,
		failure,
		now,
		now,
		deliveryID,
		models.EmailDeliveryStatusProcessing,
		owner,
		now,
	)
	if result.Error != nil {
		return fmt.Errorf("mark email delivery failed: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return emaildelivery.ErrLeaseUnavailable
	}
	return nil
}

func (r *EmailDeliveryWorkerRepository) classifyUnclaimed(
	ctx context.Context,
	message emaildelivery.DeliveryMessage,
	now time.Time,
) error {
	var delivery models.EmailDelivery
	err := r.db.WithContext(ctx).
		Select("status", "lease_expires_at").
		Where(
			"id = ? AND business_id = ? AND invoice_id = ? AND render_job_id = ? AND deleted_at IS NULL",
			message.DeliveryID, message.BusinessID, message.InvoiceID, message.RenderJobID,
		).
		First(&delivery).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return emaildelivery.ErrDeliveryMalformed
	}
	if err != nil {
		return fmt.Errorf("classify unclaimed email delivery: %w", err)
	}
	switch delivery.Status {
	case models.EmailDeliveryStatusSent,
		models.EmailDeliveryStatusDelivered,
		models.EmailDeliveryStatusBounced,
		models.EmailDeliveryStatusComplained:
		return emaildelivery.ErrDeliveryTerminal
	case models.EmailDeliveryStatusProcessing:
		if delivery.LeaseExpiresAt != nil && delivery.LeaseExpiresAt.After(now) {
			return emaildelivery.ErrLeaseUnavailable
		}
	}
	return emaildelivery.ErrDeliveryMalformed
}

func validateEmailDeliveryLeaseInput(
	repository *EmailDeliveryWorkerRepository,
	deliveryID, owner string,
	now time.Time,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("email delivery worker database is required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(deliveryID)); err != nil {
		return errors.New("email delivery ID is invalid")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 255 || now.IsZero() {
		return errors.New("email delivery lease identity is invalid")
	}
	return nil
}
