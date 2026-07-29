package services

import (
	"context"
	"fmt"
	"strings"

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
	ActorRole       string `json:"actor_role"`
	RequestID       string `json:"request_id"`
	IPAddress       string `json:"ip_address"`
}

func (s *InvoiceService) IssueByBusiness(
	ctx context.Context,
	businessID, invoiceID string,
	input IssueInvoiceInput,
) (*IssueInvoiceResult, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if _, err := uuid.Parse(input.IdempotencyKey); err != nil {
		return nil, &idempotency.InvalidKeyError{}
	}
	actor := actorFromContext(ctx)
	if _, err := uuid.Parse(actor.UserID); err != nil {
		return nil, fmt.Errorf("invoice issue actor is required")
	}
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
		ActorRole:       actor.Role,
		RequestID:       actor.RequestID,
		IPAddress:       actor.IPAddress,
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
	hydrateInvoiceEditorFields(result.Invoice)
	return &IssueInvoiceResult{
		Invoice:     result.Invoice,
		FinalRender: result.FinalRender,
		Replayed:    result.Replayed,
	}, nil
}
