package telemetry

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	RedactedValue             = "[REDACTED]"
	maxDetailDepth            = 6
	maxDetailEntries          = 64
	maxDetailSliceItems       = 32
	maxDetailStringBytes      = 512
	maxDetailKeyBytes         = 96
	maxDetailTotalNodes       = 256
	maxDetailTotalStringBytes = 16 * 1024
	maxRedactedDetailsBytes   = 48 * 1024
)

var sensitiveValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[a-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`\b[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`(?i)\b(?:ocp-apim-subscription-key|api-subscription-key|authorization)\s*[:=]\s*[^\s,;]{8,}`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`(?i)\bturns?:(?://)?[^\s@]+@[^\s]+`),
	regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`),
	regexp.MustCompile(`(?i)\b[0-9]{2}[a-z]{5}[0-9]{4}[a-z][a-z0-9]z[a-z0-9]\b`),
	regexp.MustCompile(`(?i)\b[a-z]{5}[0-9]{4}[a-z]\b`),
	regexp.MustCompile(`(?i)\b[a-z][1-9][0-9]{6}\b`),
	regexp.MustCompile(`(?i)\b[a-z]{3}[0-9]{7}\b`),
	regexp.MustCompile(`(?i)\b[a-z]{4}0[a-z0-9]{6}\b`),
	regexp.MustCompile(`(?i)\b[a-z0-9._-]{2,}@[a-z]{2,}\b`),
	regexp.MustCompile(`(?:\+?[0-9][0-9 ()-]{8,}[0-9])`),
}

// RedactDetails returns a bounded copy suitable for a sampled trace. Unknown
// value types fail closed instead of invoking String methods that may expose
// secrets.
func RedactDetails(details map[string]any) map[string]any {
	if details == nil {
		return map[string]any{}
	}
	budget := redactionBudget{nodesRemaining: maxDetailTotalNodes, stringBytesRemaining: maxDetailTotalStringBytes}
	value, ok := redactMap(details, 0, &budget)
	if !ok {
		return map[string]any{}
	}
	return value
}

type redactionBudget struct {
	nodesRemaining       int
	stringBytesRemaining int
}

func (budget *redactionBudget) consumeNode() bool {
	if budget.nodesRemaining <= 0 {
		return false
	}
	budget.nodesRemaining--
	return true
}

func redactMap(input map[string]any, depth int, budget *redactionBudget) (map[string]any, bool) {
	if depth >= maxDetailDepth || !budget.consumeNode() {
		return nil, false
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	truncated := len(keys) > maxDetailEntries
	if truncated {
		keys = keys[:maxDetailEntries]
	}
	result := make(map[string]any, len(keys)+1)
	for _, key := range keys {
		safeKey := boundedDetailKey(key)
		if sensitiveDetailKey(key) {
			if !budget.consumeNode() {
				truncated = true
				break
			}
			result[safeKey] = RedactedValue
			continue
		}
		value, ok := redactValue(input[key], depth+1, budget)
		if !ok {
			truncated = true
			break
		}
		result[safeKey] = value
	}
	if truncated && budget.consumeNode() {
		result["_truncated"] = true
	}
	return result, true
}

func redactValue(value any, depth int, budget *redactionBudget) (any, bool) {
	if depth >= maxDetailDepth {
		if !budget.consumeNode() {
			return nil, false
		}
		return RedactedValue, true
	}
	switch typed := value.(type) {
	case map[string]any:
		return redactMap(typed, depth, budget)
	case []any:
		return redactSlice(typed, depth, budget)
	}
	if !budget.consumeNode() {
		return nil, false
	}
	switch typed := value.(type) {
	case nil:
		return nil, true
	case string:
		if containsSensitiveValue(typed) {
			return RedactedValue, true
		}
		return budgetDetailString(typed, budget), true
	case bool:
		return typed, true
	case int:
		return typed, true
	case int8:
		return typed, true
	case int16:
		return typed, true
	case int32:
		return typed, true
	case int64:
		return typed, true
	case uint:
		return typed, true
	case uint8:
		return typed, true
	case uint16:
		return typed, true
	case uint32:
		return typed, true
	case uint64:
		return typed, true
	case float32:
		return typed, true
	case float64:
		return typed, true
	default:
		return RedactedValue, true
	}
}

func redactSlice(input []any, depth int, budget *redactionBudget) ([]any, bool) {
	if !budget.consumeNode() {
		return nil, false
	}
	limit := min(len(input), maxDetailSliceItems)
	result := make([]any, 0, limit+1)
	truncated := len(input) > limit
	for _, item := range input[:limit] {
		value, ok := redactValue(item, depth+1, budget)
		if !ok {
			truncated = true
			break
		}
		result = append(result, value)
	}
	if truncated && budget.consumeNode() {
		result = append(result, RedactedValue)
	}
	return result, true
}

func sensitiveDetailKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(strings.ToLower(key))
	for _, marker := range []string{
		"authorization", "apikey", "sarvamkey", "sarvamsecret", "token", "secret", "password",
		"subscriptionkey",
		"turnusername", "turncredential", "turnpassword", "credential",
		"providerbody", "providerresponse", "rawresponse", "responsebody",
		"transcript", "utterance", "answer", "prompt",
		"phone", "email", "payment", "card", "cvv", "upi", "bankaccount",
		"gstin", "gstnumber", "aadhaar", "aadhar", "governmentid", "passport", "voterid", "pannumber", "panvalue",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func containsSensitiveValue(value string) bool {
	for _, pattern := range sensitiveValuePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func budgetDetailString(value string, budget *redactionBudget) string {
	if budget.stringBytesRemaining <= 0 {
		return RedactedValue
	}
	limit := min(maxDetailStringBytes, budget.stringBytesRemaining)
	result := truncateUTF8(value, limit)
	budget.stringBytesRemaining -= len(result)
	return result
}

func boundedDetailKey(key string) string {
	if containsSensitiveValue(key) {
		return "redacted_key"
	}
	return truncateUTF8(key, maxDetailKeyBytes)
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}
