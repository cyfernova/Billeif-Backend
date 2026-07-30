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

type IssueInvoiceInput struct {
	IdempotencyKey  string `json:"-"`
	ExpectedVersion int    `json:"-"`
	DocumentType    string `json:"document_type" binding:"required"`
	Series          string `json:"series" binding:"required"`
}

type IssueInvoiceResult struct {
	Invoice     *models.Invoice           `json:"invoice"`
	FinalRender *models.DocumentRenderJob `json:"final_render"`
	Replayed    bool                      `json:"replayed"`
}

type canonicalInvoiceIssuePayload struct {
	BusinessID      string `json:"business_id"`
	InvoiceID       string `json:"invoice_id"`
	ExpectedVersion int    `json:"expected_version"`
	DocumentType    string `json:"document_type"`
	Series          string `json:"series"`
	ActorID         string `json:"actor_id"`
}

const invoiceIssueImmediatePublishTimeout = 2 * time.Second

func (s *InvoiceService) IssueByBusiness(
	ctx context.Context,
	businessID, invoiceID string,
	input IssueInvoiceInput,
) (*IssueInvoiceResult, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	idempotencyUUID, err := uuid.Parse(input.IdempotencyKey)
	if err != nil {
		return nil, &idempotency.InvalidKeyError{}
	}
	businessUUID, err := uuid.Parse(strings.TrimSpace(businessID))
	if err != nil {
		return nil, &idempotency.InvalidPayloadError{}
	}
	invoiceUUID, err := uuid.Parse(strings.TrimSpace(invoiceID))
	if err != nil {
		return nil, &idempotency.InvalidPayloadError{}
	}
	actor := actorFromContext(ctx)
	actorUUID, err := uuid.Parse(strings.TrimSpace(actor.UserID))
	if err != nil {
		return nil, fmt.Errorf("invoice issue actor is required")
	}
	input.IdempotencyKey = idempotencyUUID.String()
	businessID = businessUUID.String()
	invoiceID = invoiceUUID.String()
	actor.UserID = actorUUID.String()
	issuer, ok := s.repo.(interfaces.CanonicalInvoiceIssuer)
	if !ok {
		return nil, fmt.Errorf("canonical invoice issuer is not configured")
	}
	requestHash, err := idempotency.CanonicalHash(canonicalInvoiceIssuePayload{
		BusinessID:      businessID,
		InvoiceID:       invoiceID,
		ExpectedVersion: input.ExpectedVersion,
		DocumentType:    input.DocumentType,
		Series:          input.Series,
		ActorID:         actor.UserID,
	})
	if err != nil {
		return nil, err
	}
	result, err := issuer.IssueDraftAtomic(ctx, interfaces.AtomicInvoiceIssue{
		BusinessID:      businessID,
		InvoiceID:       invoiceID,
		Command:         "invoice.issue",
		IdempotencyKey:  input.IdempotencyKey,
		RequestHash:     requestHash,
		ExpectedVersion: input.ExpectedVersion,
		DocumentType:    input.DocumentType,
		Series:          input.Series,
		ActorID:         actor.UserID,
		ActorRole:       actor.Role,
		RequestID:       actor.RequestID,
		IPAddress:       actor.IPAddress,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to issue invoice: %w", err)
	}
	if result == nil || result.Invoice == nil || result.FinalRender == nil {
		return nil, fmt.Errorf("failed to issue invoice: atomic repository returned incomplete result")
	}
	if !result.Replayed && result.OutboxEvent != nil && s.immediateOutboxPublisher != nil {
		publishContext, cancel := context.WithTimeout(ctx, invoiceIssueImmediatePublishTimeout)
		defer cancel()
		if err := s.immediateOutboxPublisher.TryPublish(publishContext, result.OutboxEvent); err != nil {
			s.log.Warn(
				"immediate invoice issue publication failed; event remains pending",
				"invoice_id", invoiceID,
				"outbox_event_id", result.OutboxEvent.ID,
				"error", err,
			)
		}
	}
	hydrateInvoiceEditorFields(result.Invoice)
	return &IssueInvoiceResult{
		Invoice:     result.Invoice,
		FinalRender: result.FinalRender,
		Replayed:    result.Replayed,
	}, nil
}
