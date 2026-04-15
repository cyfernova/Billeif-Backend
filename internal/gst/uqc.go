package gst

import (
	"fmt"
	"sort"
	"strings"
)

type UQCCode struct {
	Code        string
	Name        string
	Description string
}

var UQCList = []UQCCode{
	{Code: "BAG", Name: "Bags", Description: "BAG-BAGS"},
	{Code: "BAL", Name: "Bale", Description: "BAL-BALE"},
	{Code: "BDL", Name: "Bundles", Description: "BDL-BUNDLES"},
	{Code: "BKL", Name: "Buckles", Description: "BKL-BUCKLES"},
	{Code: "BOU", Name: "Billion Of Units", Description: "BOU-BILLION OF UNITS"},
	{Code: "BOX", Name: "Box", Description: "BOX-BOX"},
	{Code: "BTL", Name: "Bottles", Description: "BTL-BOTTLES"},
	{Code: "BUN", Name: "Bunches", Description: "BUN-BUNCHES"},
	{Code: "CAN", Name: "Cans", Description: "CAN-CANS"},
	{Code: "CBM", Name: "Cubic Meters", Description: "CBM-CUBIC METERS"},
	{Code: "CCM", Name: "Cubic Centimeters", Description: "CCM-CUBIC CENTIMETERS"},
	{Code: "CMS", Name: "Centimeters", Description: "CMS-CENTIMETERS"},
	{Code: "CTN", Name: "Cartons", Description: "CTN-CARTONS"},
	{Code: "DOZ", Name: "Dozens", Description: "DOZ-DOZENS"},
	{Code: "DRM", Name: "Drums", Description: "DRM-DRUMS"},
	{Code: "GGK", Name: "Great Gross", Description: "GGK-GREAT GROSS"},
	{Code: "GMS", Name: "Grammes", Description: "GMS-GRAMMES"},
	{Code: "GRS", Name: "Gross", Description: "GRS-GROSS"},
	{Code: "GYD", Name: "Gross Yards", Description: "GYD-GROSS YARDS"},
	{Code: "KGS", Name: "Kilograms", Description: "KGS-KILOGRAMS"},
	{Code: "KLR", Name: "Kilolitre", Description: "KLR-KILOLITRE"},
	{Code: "KME", Name: "Kilometre", Description: "KME-KILOMETRE"},
	{Code: "LTR", Name: "Litres", Description: "LTR-LITRES"},
	{Code: "MLT", Name: "Mililitre", Description: "MLT-MILILITRE"},
	{Code: "MTR", Name: "Meters", Description: "MTR-METERS"},
	{Code: "MTS", Name: "Metric Ton", Description: "MTS-METRIC TON"},
	{Code: "NOS", Name: "Numbers", Description: "NOS-NUMBERS"},
	{Code: "PAC", Name: "Packs", Description: "PAC-PACKS"},
	{Code: "PCS", Name: "Pieces", Description: "PCS-PIECES"},
	{Code: "PRS", Name: "Pairs", Description: "PRS-PAIRS"},
	{Code: "QTL", Name: "Quintal", Description: "QTL-QUINTAL"},
	{Code: "ROL", Name: "Rolls", Description: "ROL-ROLLS"},
	{Code: "SET", Name: "Sets", Description: "SET-SETS"},
	{Code: "SQF", Name: "Square Feet", Description: "SQF-SQUARE FEET"},
	{Code: "SQM", Name: "Square Meters", Description: "SQM-SQUARE METERS"},
	{Code: "SQY", Name: "Square Yards", Description: "SQY-SQUARE YARDS"},
	{Code: "TBS", Name: "Tablets", Description: "TBS-TABLETS"},
	{Code: "TGM", Name: "Ten Gross", Description: "TGM-TEN GROSS"},
	{Code: "THD", Name: "Thousands", Description: "THD-THOUSANDS"},
	{Code: "TON", Name: "Tonnes", Description: "TON-TONNES"},
	{Code: "TUB", Name: "Tubes", Description: "TUB-TUBES"},
	{Code: "UGS", Name: "US Gallons", Description: "UGS-US GALLONS"},
	{Code: "UNT", Name: "Units", Description: "UNT-UNITS"},
	{Code: "YDS", Name: "Yards", Description: "YDS-YARDS"},
	{Code: "OTH", Name: "Others", Description: "OTH-OTHERS"},
}

const (
	DefaultProductUQC = "PCS"
	DefaultReportUQC  = "OTH"
)

var (
	validUQCCodes = func() map[string]struct{} {
		values := make(map[string]struct{}, len(UQCList))
		for _, item := range UQCList {
			values[item.Code] = struct{}{}
		}
		return values
	}()
	uqcNames = func() map[string]string {
		values := make(map[string]string, len(UQCList))
		for _, item := range UQCList {
			values[item.Code] = item.Name
		}
		return values
	}()
)

func ValidUQCCodes() []string {
	codes := make([]string, 0, len(validUQCCodes))
	for code := range validUQCCodes {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func IsValidUQC(code string) bool {
	_, ok := validUQCCodes[strings.ToUpper(strings.TrimSpace(code))]
	return ok
}

func NormalizeProductUQC(raw string) string {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return DefaultProductUQC
	}
	if IsValidUQC(code) {
		return code
	}
	return DefaultReportUQC
}

func NormalizeReportUQC(raw string) string {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return DefaultReportUQC
	}
	if IsValidUQC(code) {
		return code
	}
	return DefaultReportUQC
}

func CanonicalProductUQC(unit string, legacyUQC string) string {
	if strings.TrimSpace(unit) != "" {
		return NormalizeProductUQC(unit)
	}
	if strings.TrimSpace(legacyUQC) != "" {
		return NormalizeProductUQC(legacyUQC)
	}
	return DefaultProductUQC
}

func CanonicalSnapshotUQC(unit string, legacyUQC string) string {
	if strings.TrimSpace(unit) != "" {
		return NormalizeReportUQC(unit)
	}
	if strings.TrimSpace(legacyUQC) != "" {
		return NormalizeReportUQC(legacyUQC)
	}
	return DefaultReportUQC
}

func FormatUQC(code string) string {
	normalized := NormalizeReportUQC(code)
	if name, ok := uqcNames[normalized]; ok {
		return fmt.Sprintf("%s-%s", normalized, strings.ToUpper(name))
	}
	return normalized
}
