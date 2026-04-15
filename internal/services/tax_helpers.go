package services

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"invoice-backend/internal/gst"
)

func readFloatCandidate(data map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			switch typed := value.(type) {
			case float64:
				return typed
			case float32:
				return float64(typed)
			case int:
				return float64(typed)
			case int64:
				return float64(typed)
			case json.Number:
				if number, err := typed.Float64(); err == nil {
					return number
				}
			case string:
				if number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
					return number
				}
			}
		}
	}
	return 0
}

func readBoolCandidate(data map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			switch typed := value.(type) {
			case bool:
				return typed
			case string:
				if parsed, err := strconv.ParseBool(strings.TrimSpace(typed)); err == nil {
					return parsed
				}
			}
		}
	}
	return false
}

func readMapSlice(data map[string]interface{}, key string) []map[string]interface{} {
	raw, ok := data[key]
	if !ok {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if typed, ok := item.(map[string]interface{}); ok {
			result = append(result, typed)
		}
	}
	return result
}

func parsePANFromGSTIN(gstin string) string {
	gstin = strings.ToUpper(strings.TrimSpace(gstin))
	if len(gstin) >= 12 {
		return gstin[2:12]
	}
	return ""
}

func normalizeUQCCode(code string) string {
	return gst.NormalizeReportUQC(code)
}

func isValidHSNCode(code string) bool {
	return gst.IsValidHSNCode(code)
}

func almostEqualFloat(a, b float64) bool {
	return math.Abs(a-b) < 0.01
}

func mapSliceToWithholdings(items []map[string]interface{}) []WithholdingInput {
	result := make([]WithholdingInput, 0, len(items))
	for _, item := range items {
		withholding := mapToWithholdingInput(item)
		if withholding != nil {
			result = append(result, *withholding)
		}
	}
	return result
}
