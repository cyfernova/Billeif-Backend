package services

import (
	"context"
	"testing"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
)

type countingCanonicalInvoiceIssuer struct {
	calls      int
	businessID string
	invoiceID  string
	input      IssueInvoiceInput
}

func (s *countingCanonicalInvoiceIssuer) IssueByBusiness(
	_ context.Context,
	businessID, invoiceID string,
	input IssueInvoiceInput,
) (*IssueInvoiceResult, error) {
	s.calls++
	s.businessID = businessID
	s.invoiceID = invoiceID
	s.input = input
	return &IssueInvoiceResult{Invoice: &models.Invoice{ID: invoiceID, BusinessID: businessID}}, nil
}

func TestGenericSalesDocumentIssueDelegatesCanonicalInvoiceExactlyOnce(t *testing.T) {
	canonical := &countingCanonicalInvoiceIssuer{}
	service := &DocumentService{salesInvoiceIssuer: newInvoiceSalesDocumentIssuer(canonical)}
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	input := IssueInvoiceInput{
		IdempotencyKey: uuid.NewString(), ExpectedVersion: 2, DocumentType: "tax_invoice", Series: "INV",
	}

	result, err := service.IssueSalesDocumentByBusiness(
		context.Background(), businessID, models.DocumentTypeSalesInvoice, invoiceID, input,
	)

	if err != nil {
		t.Fatalf("generic sales issue: %v", err)
	}
	if result == nil || result.Invoice == nil || canonical.calls != 1 {
		t.Fatalf("result/calls = %#v/%d, want one canonical delegation", result, canonical.calls)
	}
	if canonical.businessID != businessID || canonical.invoiceID != invoiceID || canonical.input != input {
		t.Fatalf("delegated command = %#v", canonical)
	}
}

func TestGenericNonSalesDocumentIssueDoesNotReachInvoice(t *testing.T) {
	canonical := &countingCanonicalInvoiceIssuer{}
	service := &DocumentService{salesInvoiceIssuer: newInvoiceSalesDocumentIssuer(canonical)}

	if _, err := service.IssueSalesDocumentByBusiness(
		context.Background(), uuid.NewString(), models.DocumentTypePurchaseInvoice, uuid.NewString(), IssueInvoiceInput{},
	); err == nil {
		t.Fatal("generic non-sales issue succeeded")
	}
	if canonical.calls != 0 {
		t.Fatalf("canonical invoice calls = %d, want 0", canonical.calls)
	}
}
