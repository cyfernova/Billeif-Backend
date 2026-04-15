package gst

import (
	"fmt"
	"sort"
	"strings"
)

type HSNSummaryLine struct {
	HSNSACCode   string
	Unit         string
	LegacyUQC    string
	Description  string
	TaxRate      float64
	Quantity     float64
	TaxableValue float64
	IGSTAmount   float64
	CGSTAmount   float64
	SGSTAmount   float64
	TotalValue   float64
	Sign         float64
	WarningRef   string
}

type HSNSummaryRow struct {
	HSNSACCode   string  `json:"hsn_sac_code"`
	Description  string  `json:"description"`
	Unit         string  `json:"unit"`
	UQCCode      string  `json:"uqc_code"`
	TaxRate      float64 `json:"tax_rate"`
	Quantity     float64 `json:"quantity"`
	TaxableValue float64 `json:"taxable_value"`
	IGSTAmount   float64 `json:"igst_amount"`
	CGSTAmount   float64 `json:"cgst_amount"`
	SGSTAmount   float64 `json:"sgst_amount"`
	TaxAmount    float64 `json:"tax_amount"`
	TotalValue   float64 `json:"total_value"`
}

type HSNSummaryTotals struct {
	Quantity     float64
	TaxableValue float64
	IGSTAmount   float64
	CGSTAmount   float64
	SGSTAmount   float64
	TaxAmount    float64
	TotalValue   float64
}

type HSNSummaryResult struct {
	Rows     []HSNSummaryRow
	Totals   HSNSummaryTotals
	Warnings []string
}

type hsnSummaryAccumulator struct {
	row          HSNSummaryRow
	descriptions map[string]struct{}
}

func AggregateHSNSummary(lines []HSNSummaryLine) HSNSummaryResult {
	rows := make(map[string]*hsnSummaryAccumulator)
	warningSet := map[string]struct{}{}
	totals := HSNSummaryTotals{}

	for _, line := range lines {
		uqc := CanonicalSnapshotUQC(line.Unit, line.LegacyUQC)
		rawUQC := strings.ToUpper(strings.TrimSpace(firstNonBlank(line.Unit, line.LegacyUQC)))
		if rawUQC != "" && rawUQC != "OTH" && !IsValidUQC(rawUQC) {
			warningSet[fmt.Sprintf("invalid UQC %q downgraded to OTH for %s", rawUQC, line.WarningRef)] = struct{}{}
		}

		hsn := strings.TrimSpace(line.HSNSACCode)
		if !IsValidHSNCode(hsn) {
			warningSet[fmt.Sprintf("excluded invalid HSN for %s", line.WarningRef)] = struct{}{}
			continue
		}

		sign := line.Sign
		if sign == 0 {
			sign = 1
		}
		key := fmt.Sprintf("%s|%s|%.3f", hsn, uqc, line.TaxRate)
		acc, ok := rows[key]
		if !ok {
			acc = &hsnSummaryAccumulator{
				row: HSNSummaryRow{
					HSNSACCode: hsn,
					Unit:       uqc,
					UQCCode:    uqc,
					TaxRate:    round3(line.TaxRate),
				},
				descriptions: map[string]struct{}{},
			}
			rows[key] = acc
		}

		description := strings.TrimSpace(line.Description)
		if description != "" {
			acc.descriptions[description] = struct{}{}
		}

		acc.row.Quantity = round3(acc.row.Quantity + sign*line.Quantity)
		acc.row.TaxableValue = round2(acc.row.TaxableValue + sign*line.TaxableValue)
		acc.row.IGSTAmount = round2(acc.row.IGSTAmount + sign*line.IGSTAmount)
		acc.row.CGSTAmount = round2(acc.row.CGSTAmount + sign*line.CGSTAmount)
		acc.row.SGSTAmount = round2(acc.row.SGSTAmount + sign*line.SGSTAmount)
		acc.row.TaxAmount = round2(acc.row.TaxAmount + sign*(line.IGSTAmount+line.CGSTAmount+line.SGSTAmount))
		acc.row.TotalValue = round2(acc.row.TotalValue + sign*line.TotalValue)

		totals.Quantity = round3(totals.Quantity + sign*line.Quantity)
		totals.TaxableValue = round2(totals.TaxableValue + sign*line.TaxableValue)
		totals.IGSTAmount = round2(totals.IGSTAmount + sign*line.IGSTAmount)
		totals.CGSTAmount = round2(totals.CGSTAmount + sign*line.CGSTAmount)
		totals.SGSTAmount = round2(totals.SGSTAmount + sign*line.SGSTAmount)
		totals.TaxAmount = round2(totals.TaxAmount + sign*(line.IGSTAmount+line.CGSTAmount+line.SGSTAmount))
		totals.TotalValue = round2(totals.TotalValue + sign*line.TotalValue)
	}

	result := make([]HSNSummaryRow, 0, len(rows))
	for _, acc := range rows {
		acc.row.Description = summaryDescription(acc.descriptions)
		result = append(result, acc.row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].HSNSACCode != result[j].HSNSACCode {
			return result[i].HSNSACCode < result[j].HSNSACCode
		}
		if result[i].Unit != result[j].Unit {
			return result[i].Unit < result[j].Unit
		}
		if result[i].TaxRate != result[j].TaxRate {
			return result[i].TaxRate < result[j].TaxRate
		}
		return result[i].Description < result[j].Description
	})

	warnings := make([]string, 0, len(warningSet))
	for warning := range warningSet {
		warnings = append(warnings, warning)
	}
	sort.Strings(warnings)

	return HSNSummaryResult{
		Rows:     result,
		Totals:   totals,
		Warnings: warnings,
	}
}

func IsValidHSNCode(code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 4 && len(code) != 6 && len(code) != 8 {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func summaryDescription(values map[string]struct{}) string {
	if len(values) == 1 {
		for value := range values {
			return value
		}
	}
	if len(values) == 0 {
		return ""
	}
	return "Mixed items"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func round2(value float64) float64 {
	return roundTo(value, 100)
}

func round3(value float64) float64 {
	return roundTo(value, 1000)
}

func roundTo(value float64, scale float64) float64 {
	if value >= 0 {
		return float64(int64(value*scale+0.5)) / scale
	}
	return float64(int64(value*scale-0.5)) / scale
}
