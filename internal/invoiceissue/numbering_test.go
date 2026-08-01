package invoiceissue

import (
	"errors"
	"testing"
	"time"
)

func TestFinancialYearUsesIndiaAprilBoundaryAndBusinessTimezone(t *testing.T) {
	tests := []struct {
		name     string
		at       time.Time
		timezone string
		want     string
	}{
		{
			name:     "last instant before April in India",
			at:       time.Date(2026, time.March, 31, 18, 29, 59, 0, time.UTC),
			timezone: "Asia/Kolkata",
			want:     "2025-2026",
		},
		{
			name:     "first instant of April in India",
			at:       time.Date(2026, time.March, 31, 18, 30, 0, 0, time.UTC),
			timezone: "Asia/Kolkata",
			want:     "2026-2027",
		},
		{
			name:     "empty timezone defaults to India",
			at:       time.Date(2026, time.March, 31, 18, 30, 0, 0, time.UTC),
			timezone: "",
			want:     "2026-2027",
		},
		{
			name:     "business timezone controls local boundary",
			at:       time.Date(2026, time.April, 1, 0, 30, 0, 0, time.UTC),
			timezone: "America/New_York",
			want:     "2025-2026",
		},
	}

	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			got, err := FinancialYear(fixture.at, fixture.timezone)
			if err != nil {
				t.Fatalf("financial year: %v", err)
			}
			if got != fixture.want {
				t.Fatalf("financial year = %q, want %q", got, fixture.want)
			}
		})
	}
}

func TestFinancialYearRejectsInvalidTimezone(t *testing.T) {
	_, err := FinancialYear(time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC), "Mars/Olympus")
	var invalid *InvalidTimezoneError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %T %v, want *InvalidTimezoneError", err, err)
	}
}

func TestFormatNumberValidatesSeriesDocumentTypeAndExhaustion(t *testing.T) {
	tests := []struct {
		name          string
		documentType  string
		series        string
		financialYear string
		number        int
		want          string
		wantErr       string
	}{
		{name: "tax invoice", documentType: DocumentTypeTaxInvoice, series: "INV", financialYear: "2026-2027", number: 1, want: "INV/26-27/000001"},
		{name: "bill of supply", documentType: DocumentTypeBillOfSupply, series: "BOS", financialYear: "2026-2027", number: 1, want: "BOS/26-27/000001"},
		{name: "custom series", documentType: DocumentTypeTaxInvoice, series: "A", financialYear: "2026-2027", number: 42, want: "A/26-27/000042"},
		{name: "last number", documentType: DocumentTypeTaxInvoice, series: "INV", financialYear: "2026-2027", number: 999999, want: "INV/26-27/999999"},
		{name: "lowercase series", documentType: DocumentTypeTaxInvoice, series: "inv", financialYear: "2026-2027", number: 1, wantErr: "series"},
		{name: "empty series", documentType: DocumentTypeTaxInvoice, series: "", financialYear: "2026-2027", number: 1, wantErr: "series"},
		{name: "long series", documentType: DocumentTypeTaxInvoice, series: "ABCD", financialYear: "2026-2027", number: 1, wantErr: "series"},
		{name: "punctuated series", documentType: DocumentTypeTaxInvoice, series: "A-1", financialYear: "2026-2027", number: 1, wantErr: "series"},
		{name: "unsupported document", documentType: "credit_note", series: "INV", financialYear: "2026-2027", number: 1, wantErr: "document"},
		{name: "exhausted", documentType: DocumentTypeTaxInvoice, series: "INV", financialYear: "2026-2027", number: 1000000, wantErr: "exhausted"},
	}

	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			got, err := FormatNumber(fixture.documentType, fixture.series, fixture.financialYear, fixture.number)
			if fixture.wantErr != "" {
				matched := false
				switch fixture.wantErr {
				case "series":
					var target *InvalidSeriesError
					matched = errors.As(err, &target)
				case "document":
					var target *InvalidDocumentTypeError
					matched = errors.As(err, &target)
				case "exhausted":
					var target *SequenceExhaustedError
					matched = errors.As(err, &target)
				}
				if !matched {
					t.Fatalf("error = %T %v, want %s error", err, err, fixture.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("format number: %v", err)
			}
			if got != fixture.want {
				t.Fatalf("number = %q, want %q", got, fixture.want)
			}
			if len(got) > 16 {
				t.Fatalf("identifier length = %d, want <= 16", len(got))
			}
		})
	}
}
