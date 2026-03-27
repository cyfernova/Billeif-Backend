package services

import (
	"math"
	"testing"

	"invoice-backend/internal/models"
)

func TestIsAllowedConversionMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		target string
		want   bool
	}{
		{name: "quotation to sales invoice", source: models.DocumentTypeQuotation, target: models.DocumentTypeSalesInvoice, want: true},
		{name: "quotation to purchase invoice denied", source: models.DocumentTypeQuotation, target: models.DocumentTypePurchaseInvoice, want: false},
		{name: "sales order to delivery challan", source: models.DocumentTypeSalesOrder, target: models.DocumentTypeDeliveryChallan, want: true},
		{name: "sales invoice to packing list", source: models.DocumentTypeSalesInvoice, target: models.DocumentTypePackingList, want: true},
		{name: "purchase invoice to debit note", source: models.DocumentTypePurchaseInvoice, target: models.DocumentTypeDebitNote, want: true},
		{name: "bill of supply to credit note", source: models.DocumentTypeBillOfSupply, target: models.DocumentTypeCreditNote, want: true},
		{name: "bill of supply to sales invoice denied", source: models.DocumentTypeBillOfSupply, target: models.DocumentTypeSalesInvoice, want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isAllowedConversion(tc.source, tc.target)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestDefaultTaxMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		documentType string
		gstTreatment string
		want         string
	}{
		{name: "sales invoice regular gst", documentType: models.DocumentTypeSalesInvoice, gstTreatment: models.DocumentGSTTreatmentRegular, want: models.DocumentTaxModeGST},
		{name: "bill of supply non gst", documentType: models.DocumentTypeBillOfSupply, gstTreatment: models.DocumentGSTTreatmentRegular, want: models.DocumentTaxModeNonGST},
		{name: "composition non gst", documentType: models.DocumentTypeSalesInvoice, gstTreatment: models.DocumentGSTTreatmentComposition, want: models.DocumentTaxModeNonGST},
		{name: "exempt non gst", documentType: models.DocumentTypeSalesInvoice, gstTreatment: models.DocumentGSTTreatmentExempt, want: models.DocumentTaxModeNonGST},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := defaultTaxMode(tc.documentType, tc.gstTreatment)
			if got != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, got)
			}
		})
	}
}

func TestStockBehaviorForDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		document      *models.Document
		wantDirection string
		wantPost      bool
		wantReserve   bool
	}{
		{
			name: "sales order reserves stock",
			document: &models.Document{
				DocumentType: models.DocumentTypeSalesOrder,
			},
			wantDirection: models.StockMoveDirectionReserve,
			wantPost:      false,
			wantReserve:   true,
		},
		{
			name: "delivery challan inward posts stock in",
			document: &models.Document{
				DocumentType: models.DocumentTypeDeliveryChallan,
				Direction:    models.DocumentDirectionInward,
			},
			wantDirection: models.StockMoveDirectionIn,
			wantPost:      true,
			wantReserve:   false,
		},
		{
			name: "bill of supply posts stock out",
			document: &models.Document{
				DocumentType: models.DocumentTypeBillOfSupply,
			},
			wantDirection: models.StockMoveDirectionOut,
			wantPost:      true,
			wantReserve:   false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotDirection, gotPost, gotReserve := stockBehaviorForDocument(tc.document)
			if gotDirection != tc.wantDirection || gotPost != tc.wantPost || gotReserve != tc.wantReserve {
				t.Fatalf("expected (%s,%v,%v), got (%s,%v,%v)", tc.wantDirection, tc.wantPost, tc.wantReserve, gotDirection, gotPost, gotReserve)
			}
		})
	}
}

func TestBuildJournalLinesForDocument(t *testing.T) {
	t.Parallel()

	salesInvoice := &models.Document{
		ID:            "doc-1",
		DocumentType:  models.DocumentTypeSalesInvoice,
		SerialNumber:  "SI-2026-000001",
		Currency:      "INR",
		Subtotal:      1000,
		DiscountTotal: 100,
		TaxTotal:      90,
		CessTotal:     10,
		Total:         1000,
	}

	lines := buildJournalLinesForDocument(salesInvoice)
	if len(lines) != 3 {
		t.Fatalf("expected 3 journal lines, got %d", len(lines))
	}

	var debits, credits float64
	for _, line := range lines {
		if line.EntryType == "debit" {
			debits += line.Amount
		} else {
			credits += line.Amount
		}
	}
	if math.Abs(debits-credits) > 0.005 {
		t.Fatalf("expected balanced journal lines, got debits=%v credits=%v", debits, credits)
	}

	quotationLines := buildJournalLinesForDocument(&models.Document{
		DocumentType: models.DocumentTypeQuotation,
	})
	if len(quotationLines) != 0 {
		t.Fatalf("expected quotation to produce no journal lines, got %d", len(quotationLines))
	}
}
