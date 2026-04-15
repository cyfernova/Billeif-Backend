package gst

import "testing"

func TestAggregateHSNSummaryGroupsByHSNUnitAndRate(t *testing.T) {
	result := AggregateHSNSummary([]HSNSummaryLine{
		{
			HSNSACCode:   "1234",
			Unit:         "cbm",
			Description:  "Ready Mix",
			TaxRate:      18,
			Quantity:     2,
			TaxableValue: 100,
			IGSTAmount:   18,
			TotalValue:   118,
			Sign:         1,
			WarningRef:   "doc-1",
		},
		{
			HSNSACCode:   "1234",
			Unit:         "CBM",
			Description:  "Ready Mix",
			TaxRate:      18,
			Quantity:     1,
			TaxableValue: 50,
			IGSTAmount:   9,
			TotalValue:   59,
			Sign:         1,
			WarningRef:   "doc-2",
		},
		{
			HSNSACCode:   "1234",
			Unit:         "CBM",
			Description:  "Ready Mix Return",
			TaxRate:      18,
			Quantity:     0.5,
			TaxableValue: 25,
			IGSTAmount:   4.5,
			TotalValue:   29.5,
			Sign:         -1,
			WarningRef:   "credit-note-1",
		},
	})

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 grouped row, got %d", len(result.Rows))
	}
	row := result.Rows[0]
	if row.Unit != "CBM" {
		t.Fatalf("expected unit CBM, got %s", row.Unit)
	}
	if row.Quantity != 2.5 {
		t.Fatalf("expected quantity 2.5, got %v", row.Quantity)
	}
	if row.TaxableValue != 125 {
		t.Fatalf("expected taxable 125, got %v", row.TaxableValue)
	}
	if row.IGSTAmount != 22.5 {
		t.Fatalf("expected igst 22.5, got %v", row.IGSTAmount)
	}
	if row.TotalValue != 147.5 {
		t.Fatalf("expected total 147.5, got %v", row.TotalValue)
	}
	if row.Description != "Mixed items" {
		t.Fatalf("expected mixed description, got %q", row.Description)
	}
}
