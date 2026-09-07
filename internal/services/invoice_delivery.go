package services

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
)

const invoiceDeliveryCommand = "invoice.delivery.create.v1"
const invoiceDeliveryImmediatePublishTimeout = 2 * time.Second

var ErrInvoiceNotDeliverable = interfaces.ErrInvoiceNotDeliverable

type DeliverInvoiceInput struct {
	IdempotencyKey string `json:"-"`
	Recipient      string `json:"recipient" binding:"required"`
}

type DeliverInvoiceResult struct {
	Delivery InvoiceDeliveryStatus `json:"delivery"`
	Replayed bool                  `json:"replayed"`
}

type InvoiceDeliveryStatus struct {
	ID          string     `json:"id"`
	InvoiceID   string     `json:"invoice_id"`
	RenderJobID string     `json:"render_job_id"`
	Recipient   string     `json:"recipient"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	FailedAt    *time.Time `json:"failed_at,omitempty"`
}

type canonicalInvoiceDeliveryPayload struct {
	BusinessID string `json:"business_id"`
	InvoiceID  string `json:"invoice_id"`
	Recipient  string `json:"recipient"`
	ActorID    string `json:"actor_id"`
}

func (s *InvoiceService) DeliverByBusiness(
	ctx context.Context,
	businessID, invoiceID string,
	input DeliverInvoiceInput,
) (*DeliverInvoiceResult, error) {
	businessUUID, businessErr := uuid.Parse(strings.TrimSpace(businessID))
	invoiceUUID, invoiceErr := uuid.Parse(strings.TrimSpace(invoiceID))
	if businessErr != nil || invoiceErr != nil {
		return nil, &idempotency.InvalidPayloadError{}
	}
	keyUUID, err := uuid.Parse(strings.TrimSpace(input.IdempotencyKey))
	if err != nil {
		return nil, &idempotency.InvalidKeyError{}
	}
	key := keyUUID.String()
	actor, err := s.invoiceActor(ctx, "delivery")
	if err != nil {
		return nil, err
	}
	recipient, err := canonicalDeliveryRecipient(input.Recipient)
	if err != nil {
		return nil, &idempotency.InvalidPayloadError{}
	}
	requestHash, err := idempotency.CanonicalHash(canonicalInvoiceDeliveryPayload{
		BusinessID: businessUUID.String(),
		InvoiceID:  invoiceUUID.String(),
		Recipient:  recipient,
		ActorID:    actor.UserID,
	})
	if err != nil {
		return nil, err
	}
	if s.invoiceDeliveries == nil {
		return nil, errors.New("invoice delivery repository is not configured")
	}
	result, err := s.invoiceDeliveries.CreateDeliveryAtomic(ctx, interfaces.AtomicInvoiceDelivery{
		BusinessID: businessUUID.String(), InvoiceID: invoiceUUID.String(),
		Command: invoiceDeliveryCommand, IdempotencyKey: key, RequestHash: requestHash,
		Recipient: recipient, ActorID: actor.UserID, ActorRole: actor.Role,
		RequestID: actor.RequestID, IPAddress: actor.IPAddress,
	})
	if err != nil {
		return nil, err
	}
	if result == nil || result.Delivery == nil || result.Delivery.InvoiceID == nil ||
		result.Delivery.RenderJobID == nil ||
		result.Delivery.BusinessID != businessUUID.String() ||
		*result.Delivery.InvoiceID != invoiceUUID.String() ||
		result.Delivery.Recipient != recipient ||
		!validDeliveryUUID(result.Delivery.ID) ||
		!validDeliveryUUID(*result.Delivery.RenderJobID) {
		return nil, errors.New("invoice delivery result is incomplete")
	}
	if !result.Replayed && result.OutboxEvent != nil && s.immediateOutboxPublisher != nil {
		publishContext, cancel := context.WithTimeout(ctx, invoiceDeliveryImmediatePublishTimeout)
		defer cancel()
		if err := s.immediateOutboxPublisher.TryPublish(publishContext, result.OutboxEvent); err != nil {
			s.log.Warn(
				"immediate invoice delivery publication failed; event remains pending",
				"invoice_id", invoiceID,
				"delivery_id", result.Delivery.ID,
				"outbox_event_id", result.OutboxEvent.ID,
				"error", err,
			)
		}
	}
	return &DeliverInvoiceResult{
		Delivery: safeInvoiceDeliveryStatus(result.Delivery),
		Replayed: result.Replayed,
	}, nil
}

func (s *InvoiceService) GetDeliveryStatusByBusiness(
	ctx context.Context,
	businessID, invoiceID, deliveryID string,
) (*InvoiceDeliveryStatus, error) {
	businessUUID, businessErr := uuid.Parse(strings.TrimSpace(businessID))
	invoiceUUID, invoiceErr := uuid.Parse(strings.TrimSpace(invoiceID))
	deliveryUUID, deliveryErr := uuid.Parse(strings.TrimSpace(deliveryID))
	if businessErr != nil || invoiceErr != nil || deliveryErr != nil {
		return nil, &idempotency.InvalidPayloadError{}
	}
	if s.invoiceDeliveries == nil {
		return nil, errors.New("invoice delivery repository is not configured")
	}
	delivery, err := s.invoiceDeliveries.GetInvoiceDelivery(
		ctx, businessUUID.String(), invoiceUUID.String(), deliveryUUID.String(),
	)
	if err != nil {
		return nil, err
	}
	if delivery == nil || delivery.InvoiceID == nil || delivery.RenderJobID == nil ||
		delivery.ID != deliveryUUID.String() ||
		delivery.BusinessID != businessUUID.String() ||
		*delivery.InvoiceID != invoiceUUID.String() ||
		!validDeliveryUUID(*delivery.RenderJobID) {
		return nil, interfaces.ErrInvoiceDeliveryNotFound
	}
	status := safeInvoiceDeliveryStatus(delivery)
	return &status, nil
}

func safeInvoiceDeliveryStatus(delivery *models.EmailDelivery) InvoiceDeliveryStatus {
	return InvoiceDeliveryStatus{
		ID: delivery.ID, InvoiceID: *delivery.InvoiceID, RenderJobID: *delivery.RenderJobID,
		Recipient: delivery.Recipient, Status: delivery.Status,
		CreatedAt: delivery.CreatedAt, UpdatedAt: delivery.UpdatedAt,
		SentAt: delivery.SentAt, DeliveredAt: delivery.DeliveredAt, FailedAt: delivery.FailedAt,
	}
}

func canonicalDeliveryRecipient(value string) (string, error) {
	recipient := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(recipient)
	if err != nil || address.Address != recipient || len(recipient) > 255 {
		return "", errors.New("invalid delivery recipient")
	}
	return recipient, nil
}

func validDeliveryUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}
