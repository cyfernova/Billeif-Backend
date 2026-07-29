package invoiceprojection

import (
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
)

func TestBuildCreatesCompleteCanonicalLegalAndPricingProjection(t *testing.T) {
	invoice := richProjectionInvoice()

	document := Build(invoice)

	if document.ID != invoice.ID || document.BusinessID != invoice.BusinessID ||
		document.PartyID == nil || *document.PartyID != *invoice.CustomerID ||
		document.DocumentType != models.DocumentTypeSalesInvoice ||
		document.Status != models.DocumentStatusDraft ||
		document.DraftState != models.DocumentDraftStateDraft {
		t.Fatalf("identity/lifecycle projection = %#v", document)
	}
	if document.GSTTreatment != models.DocumentGSTTreatmentRegular ||
		document.PlaceOfSupply != "27" ||
		document.PartyGSTIN != "27BBBBB0000B1Z5" ||
		document.GenerateEInvoice != true ||
		document.ReverseCharge != true {
		t.Fatalf("tax projection = %#v", document)
	}
	if !document.IssueDate.Equal(invoice.InvoiceDate) ||
		document.DueDate == nil || !document.DueDate.Equal(invoice.DueDate) ||
		document.Currency != "INR" || document.ExchangeRate != 1 {
		t.Fatalf("date/currency projection = %#v", document)
	}
	if document.Subtotal != 100 || document.TaxTotal != 9 || document.CessTotal != 1 ||
		document.Total != 110 || document.BalanceDue != 110 ||
		document.WithholdingTotal != 2 || document.TCSTotal != 2 {
		t.Fatalf("pricing projection = %#v", document)
	}
	if len(document.Lines) != 1 {
		t.Fatalf("line count = %d, want 1", len(document.Lines))
	}
	line := document.Lines[0]
	if line.ID != invoice.Items[0].ID || line.Description != "Consulting" ||
		line.UnitPrice != 100 || line.LineSubtotal != 100 || line.TaxAmount != 9 ||
		line.CessAmount != 1 || line.LineTotal != 110 ||
		line.CGSTRate != 4.5 || line.SGSTRate != 4.5 ||
		line.CGSTAmount != 4.5 || line.SGSTAmount != 4.5 {
		t.Fatalf("line projection = %#v", line)
	}
	if !strings.Contains(document.SourceLinkage, `"seller_snapshot"`) ||
		!strings.Contains(document.SourceLinkage, `"buyer_snapshot"`) ||
		!strings.Contains(document.ExtraFields, `"reference":"PO-7"`) {
		t.Fatalf("legal JSON projection source=%s extra=%s", document.SourceLinkage, document.ExtraFields)
	}
}

func TestValidateRejectsConflictingLegalPricingAndLineProjection(t *testing.T) {
	invoice := richProjectionInvoice()
	tests := []struct {
		name   string
		mutate func(*models.Document)
	}{
		{name: "party", mutate: func(document *models.Document) { document.PartyGSTIN = "29ATTACKER0000Z9" }},
		{name: "tax mode", mutate: func(document *models.Document) { document.TaxMode = models.DocumentTaxModeNonGST }},
		{name: "issue date", mutate: func(document *models.Document) { document.IssueDate = document.IssueDate.AddDate(0, 0, 1) }},
		{name: "due date", mutate: func(document *models.Document) {
			shifted := document.DueDate.AddDate(0, 0, 1)
			document.DueDate = &shifted
		}},
		{name: "currency", mutate: func(document *models.Document) { document.Currency = "USD" }},
		{name: "totals", mutate: func(document *models.Document) { document.Total++ }},
		{name: "source legal snapshots", mutate: func(document *models.Document) { document.SourceLinkage = `{"source_invoice_id":"wrong"}` }},
		{name: "line count", mutate: func(document *models.Document) {
			document.Lines = append(document.Lines, &models.DocumentLine{ID: uuid.NewString(), DocumentID: document.ID})
		}},
		{name: "line identity", mutate: func(document *models.Document) { document.Lines[0].ID = uuid.NewString() }},
		{name: "line description", mutate: func(document *models.Document) { document.Lines[0].Description = "Changed" }},
		{name: "line quantity", mutate: func(document *models.Document) { document.Lines[0].Quantity++ }},
		{name: "line price", mutate: func(document *models.Document) { document.Lines[0].UnitPrice++ }},
		{name: "line tax", mutate: func(document *models.Document) { document.Lines[0].TaxAmount++ }},
		{name: "line total", mutate: func(document *models.Document) { document.Lines[0].LineTotal++ }},
		{name: "line custom fields", mutate: func(document *models.Document) { document.Lines[0].CustomFields = `{"changed":true}` }},
	}

	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			document := Build(invoice)
			fixture.mutate(document)

			if err := Validate(invoice, document); err == nil {
				t.Fatalf("conflicting %s projection was accepted", fixture.name)
			}
		})
	}
}

func TestValidateAcceptsEquivalentJSONWithDifferentObjectKeyOrder(t *testing.T) {
	invoice := richProjectionInvoice()
	document := Build(invoice)
	document.SourceLinkage = `{
		"buyer_snapshot":{"name":"Buyer","gstin":"27BBBBB0000B1Z5"},
		"seller_snapshot":{"name":"Seller","gstin":"27AAAAA0000A1Z5"},
		"invoice_origin":"manual",
		"channel":"api",
		"source_invoice_id":"` + invoice.ID + `"
	}`

	if err := Validate(invoice, document); err != nil {
		t.Fatalf("equivalent JSON projection rejected: %v", err)
	}
}

func TestValidateAcceptsPostgresFixedScaleFractionalProjection(t *testing.T) {
	invoice := richProjectionInvoice()
	invoice.Subtotal = 26.64
	invoice.Tax = 3.56
	invoice.Total = 30.20
	invoice.BalanceDue = 30.20
	invoice.TaxProfile = `{
		"gst_treatment":"regular",
		"place_of_supply":"27",
		"distance_km":12.345,
		"tcs":[{"withholding_type":"tcs","amount":2.345}]
	}`
	item := invoice.Items[0]
	item.Quantity = 1.333
	item.UnitPrice = 19.99
	item.Discount = 0.01
	item.TaxRate = 12.34
	item.CessRate = 1.125
	item.CessAmount = 0.30
	item.Total = 30.20

	persisted := Build(invoice)
	persisted.DistanceKM = 12.35
	persisted.WithholdingTotal = 2.35
	persisted.TCSTotal = 2.35
	persisted.Lines[0].LineSubtotal = 26.64

	if err := Validate(invoice, persisted); err != nil {
		t.Fatalf("fixed-scale PostgreSQL projection rejected: %v", err)
	}
}

func TestValidateIgnoresUnrelatedOperationalDocumentMetadata(t *testing.T) {
	invoice := richProjectionInvoice()
	document := Build(invoice)
	shipmentID := uuid.NewString()
	complianceID := uuid.NewString()
	signerID := uuid.NewString()
	signedAt := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
	document.ShipmentID = &shipmentID
	document.CurrentEInvoiceID = &complianceID
	document.SignedAt = &signedAt
	document.SignedByProfileID = &signerID
	document.SignMetadata = `{"provider":"test"}`
	document.PDFURL = "private://preview.pdf"
	document.PDFFilename = "preview.pdf"
	document.Lines[0].CostSnapshot = 55
	document.Lines[0].MarginSnapshot = 45
	document.Lines[0].PackingMetadata = `{"box":"A"}`
	document.Lines[0].ReportTags = `{"preview":"ready"}`

	if err := Validate(invoice, document); err != nil {
		t.Fatalf("operational metadata was treated as legal projection drift: %v", err)
	}
}

func richProjectionInvoice() *models.Invoice {
	customerID := uuid.NewString()
	invoiceID := uuid.NewString()
	itemID := uuid.NewString()
	invoiceDate := time.Date(2026, time.April, 1, 0, 30, 0, 0, time.UTC)
	return &models.Invoice{
		ID:          invoiceID,
		BusinessID:  uuid.NewString(),
		CustomerID:  &customerID,
		Version:     1,
		Status:      models.InvoiceStatusDraft,
		Origin:      models.InvoiceOriginManual,
		InvoiceDate: invoiceDate,
		DueDate:     invoiceDate.AddDate(0, 0, 30),
		Currency:    "INR",
		SellerSnapshot: models.PartySnapshot{
			Name: "Seller", GSTIN: "27AAAAA0000A1Z5",
		},
		BuyerSnapshot: models.PartySnapshot{
			Name: "Buyer", GSTIN: "27BBBBB0000B1Z5",
		},
		Subtotal:     100,
		Tax:          10,
		Total:        110,
		BalanceDue:   110,
		Notes:        "Legal note",
		CustomFields: `{"reference":"PO-7","terms_and_conditions":"Net 30"}`,
		TaxProfile: `{
			"gst_treatment":"regular",
			"place_of_supply":"27",
			"counterparty_gstin":"27BBBBB0000B1Z5",
			"counterparty_pan":"BBBBB0000B",
			"counterparty_state_code":"27",
			"generate_einvoice":true,
			"reverse_charge":true,
			"reverse_charge_reason":"recipient liable",
			"source_linkage":{"channel":"api"},
			"tcs":[{"withholding_type":"tcs","amount":2}]
		}`,
		Items: []*models.InvoiceItem{{
			ID: itemID, InvoiceID: invoiceID, Description: "Consulting", Unit: "NOS",
			Quantity: 1, UnitPrice: 100, TaxRate: 9, CessRate: 1, CessAmount: 1,
			Total: 110, CustomFields: `{"engagement":"A"}`,
			ChargeSnapshot: "[]", BatchAllocations: "[]", SerialIDs: "[]",
		}},
	}
}
