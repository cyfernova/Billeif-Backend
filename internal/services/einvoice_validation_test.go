package services

import (
	"context"
	"testing"
	"time"

	"invoice-backend/internal/models"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReviewEInvoiceDocumentUsesBusinessScope(t *testing.T) {
	service, db, businessID := newTaxComplianceTestService(t)
	document := newTestDocument("einvoice-review", businessID, models.DocumentTypeSalesInvoice, "INV-1", "29ABCDE1234F1Z5", "29", 100, 18, 118, "9403", "NOS")
	insertTestDocument(t, db, document)
	review, err := service.ReviewEInvoiceDocument(context.Background(), businessID, document.ID)
	require.NoError(t, err)
	require.Equal(t, document.ID, review.DocumentID)
	_, err = service.ReviewEInvoiceDocument(context.Background(), "other-business", document.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestEInvoiceDocumentIssues(t *testing.T) {
	now := time.Date(2026, 9, 9, 6, 0, 0, 0, time.UTC)
	valid := func() *models.Document {
		return &models.Document{DocumentType: models.DocumentTypeSalesInvoice, DraftState: models.DocumentDraftStateFinal,
			Status: models.DocumentStatusIssued, TaxMode: models.DocumentTaxModeGST, Currency: "INR", SerialNumber: "INV/26-27/000001",
			IssueDate: now, PartyGSTIN: "29ABCDE1234F1Z5", Lines: []*models.DocumentLine{{HSNSACCode: "9403", Quantity: 1, UnitPrice: 100}}}
	}
	for _, test := range []struct {
		name   string
		change func(*models.Document)
		code   string
	}{
		{"valid", func(*models.Document) {}, ""},
		{"quotation", func(d *models.Document) { d.DocumentType = models.DocumentTypeQuotation }, "document_type"},
		{"draft even when marked issued", func(d *models.Document) { d.DraftState = models.DocumentDraftStateDraft }, "document_final"},
		{"cancelled", func(d *models.Document) { d.Status = models.DocumentStatusCancelled }, "document_cancelled"},
		{"future date", func(d *models.Document) { d.IssueDate = now.AddDate(0, 0, 1) }, "issue_date"},
		{"invalid serial", func(d *models.Document) { d.SerialNumber = "INV/2026/123456789012345" }, "document_number"},
		{"missing items", func(d *models.Document) { d.Lines = nil }, "line_items"},
		{"missing HSN", func(d *models.Document) { d.Lines[0].HSNSACCode = "" }, "hsn_sac"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := valid()
			test.change(document)
			issues := eInvoiceDocumentIssues(document, now)
			if test.code == "" {
				require.Empty(t, issues)
				return
			}
			for _, issue := range issues {
				if issue.Code == test.code {
					return
				}
			}
			t.Fatalf("expected issue %s, got %#v", test.code, issues)
		})
	}
}
