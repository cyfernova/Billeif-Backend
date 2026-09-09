package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"invoice-backend/internal/models"
)

// EInvoiceValidationIssue identifies a correction before any IRP submission.
type EInvoiceValidationIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type EInvoiceDocumentReview struct {
	DocumentID     string                    `json:"document_id"`
	DocumentNumber string                    `json:"document_number"`
	Issues         []EInvoiceValidationIssue `json:"issues"`
}

func (s *TaxComplianceService) ReviewEInvoiceDocument(ctx context.Context, businessID, documentID string) (*EInvoiceDocumentReview, error) {
	document, err := s.getDocumentForCompliance(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	return &EInvoiceDocumentReview{DocumentID: document.ID, DocumentNumber: document.SerialNumber, Issues: eInvoiceDocumentIssues(document, time.Now())}, nil
}

var eInvoiceNumberPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/-]{0,15}$`)
var eInvoiceHSNPattern = regexp.MustCompile(`^(\d{4}|\d{6}|\d{8})$`)

// Document checks are separate from taxpayer eligibility and IRP credentials.
// Reporting deadlines depend on the taxpayer's turnover, not every document.
func eInvoiceDocumentIssues(document *models.Document, now time.Time) []EInvoiceValidationIssue {
	issues := make([]EInvoiceValidationIssue, 0)
	add := func(code, field, message string) {
		issues = append(issues, EInvoiceValidationIssue{Code: code, Field: field, Message: message})
	}
	if document == nil {
		add("document_missing", "document", "Select an invoice to review.")
		return issues
	}
	switch document.DocumentType {
	case models.DocumentTypeSalesInvoice, models.DocumentTypeCreditNote, models.DocumentTypeDebitNote:
	default:
		add("document_type", "document_type", "Choose a sales invoice, credit note or debit note.")
	}
	if document.DraftState != models.DocumentDraftStateFinal || document.Status == models.DocumentStatusDraft {
		add("document_final", "draft_state", "Finalize this document before registering its e-invoice.")
	}
	if document.Status == models.DocumentStatusCancelled {
		add("document_cancelled", "status", "A cancelled document cannot be registered.")
	}
	if document.TaxMode != models.DocumentTaxModeGST || document.BillOfSupply {
		add("gst_invoice", "tax_mode", "E-invoicing requires a GST tax invoice or GST credit/debit note.")
	}
	if !eInvoiceNumberPattern.MatchString(document.SerialNumber) {
		add("document_number", "serial_number", "Use 1–16 letters, digits, slashes or hyphens, starting with a letter or digit.")
	}
	ist := time.FixedZone("IST", 5*3600+30*60)
	if document.IssueDate.IsZero() || document.IssueDate.In(ist).Format("2006-01-02") > now.In(ist).Format("2006-01-02") {
		add("issue_date", "issue_date", "Choose an invoice date that is today or earlier.")
	}
	if len(document.Lines) == 0 || len(document.Lines) > 1000 {
		add("line_items", "lines", "An e-invoice must contain between 1 and 1,000 items.")
	}
	for index, line := range document.Lines {
		if line == nil || !eInvoiceHSNPattern.MatchString(strings.TrimSpace(line.HSNSACCode)) {
			add("hsn_sac", fmt.Sprintf("lines.%d.hsn_sac_code", index), fmt.Sprintf("Add a valid 4, 6 or 8 digit HSN/SAC code for item %d.", index+1))
		}
	}
	return issues
}
