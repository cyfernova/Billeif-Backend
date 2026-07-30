package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
)

const invoicePreviewImmediatePublishTimeout = 2 * time.Second

type ImmediateOutboxPublisher interface {
	TryPublish(ctx context.Context, event *models.OutboxEvent) error
}

type PreviewInvoiceInput struct {
	IdempotencyKey string `json:"-"`
}

type PreviewInvoiceResult struct {
	RenderJob *models.DocumentRenderJob `json:"render_job"`
	Replayed  bool                      `json:"replayed"`
}

type canonicalInvoicePreviewPayload struct {
	BusinessID string `json:"business_id"`
	InvoiceID  string `json:"invoice_id"`
	ActorID    string `json:"actor_id"`
}

func (s *InvoiceService) WithImmediateOutboxPublisher(
	publisher ImmediateOutboxPublisher,
) *InvoiceService {
	s.immediateOutboxPublisher = publisher
	return s
}

func (s *InvoiceService) PreviewByBusiness(
	ctx context.Context,
	businessID, invoiceID string,
	input PreviewInvoiceInput,
) (*PreviewInvoiceResult, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	businessID = strings.TrimSpace(businessID)
	invoiceID = strings.TrimSpace(invoiceID)

	idempotencyUUID, err := uuid.Parse(input.IdempotencyKey)
	if err != nil {
		return nil, &idempotency.InvalidKeyError{}
	}
	businessUUID, err := uuid.Parse(businessID)
	if err != nil {
		return nil, &idempotency.InvalidPayloadError{}
	}
	invoiceUUID, err := uuid.Parse(invoiceID)
	if err != nil {
		return nil, &idempotency.InvalidPayloadError{}
	}
	actor := actorFromContext(ctx)
	actorUUID, err := uuid.Parse(strings.TrimSpace(actor.UserID))
	if err != nil {
		return nil, fmt.Errorf("invoice preview actor is required")
	}
	input.IdempotencyKey = idempotencyUUID.String()
	businessID = businessUUID.String()
	invoiceID = invoiceUUID.String()
	actor.UserID = actorUUID.String()
	previewer, ok := s.repo.(interfaces.CanonicalInvoicePreviewer)
	if !ok {
		return nil, fmt.Errorf("canonical invoice preview repository is not configured")
	}
	requestHash, err := idempotency.CanonicalHash(canonicalInvoicePreviewPayload{
		BusinessID: businessID,
		InvoiceID:  invoiceID,
		ActorID:    actor.UserID,
	})
	if err != nil {
		return nil, err
	}
	result, err := previewer.RequestPreviewAtomic(ctx, interfaces.AtomicInvoicePreview{
		BusinessID:     businessID,
		InvoiceID:      invoiceID,
		Command:        "invoice.preview",
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    requestHash,
		ActorID:        actor.UserID,
		ActorRole:      actor.Role,
		RequestID:      actor.RequestID,
		IPAddress:      actor.IPAddress,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to request invoice preview: %w", err)
	}
	if result == nil || result.RenderJob == nil {
		return nil, fmt.Errorf("failed to request invoice preview: atomic repository returned incomplete result")
	}
	if !result.Replayed && result.OutboxEvent != nil && s.immediateOutboxPublisher != nil {
		publishContext, cancel := context.WithTimeout(ctx, invoicePreviewImmediatePublishTimeout)
		defer cancel()
		if err := s.immediateOutboxPublisher.TryPublish(publishContext, result.OutboxEvent); err != nil {
			s.log.Warn(
				"immediate invoice preview publication failed; event remains pending",
				"invoice_id", invoiceID,
				"outbox_event_id", result.OutboxEvent.ID,
				"error", err,
			)
		}
	}
	return &PreviewInvoiceResult{
		RenderJob: result.RenderJob,
		Replayed:  result.Replayed,
	}, nil
}
