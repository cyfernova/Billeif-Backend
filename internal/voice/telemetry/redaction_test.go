package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactDetailsRemovesSecretsProviderContentAndPersonalData(t *testing.T) {
	details := map[string]any{
		"Authorization":   "Bearer sarvam-super-secret",
		"sarvam_api_key":  "sarvam-key-value",
		"turn_username":   "turn-user",
		"turn_credential": "turn-pass",
		"provider_body":   `{"secret":"raw upstream response"}`,
		"transcript":      "please pay my invoice",
		"answer":          "the private answer",
		"nested":          map[string]any{"safe": "kept", "password": "private"},
		"email_value":     "customer@example.com",
		"phone_value":     "+91 98765 43210",
		"payment_card":    "4111 1111 1111 1111",
		"gst_value":       "27AAPFU0939F1ZV",
		"pan_value":       "ABCDE1234F",
		"aadhaar_value":   "1234 5678 9012",
		"safe":            "provider timed out before headers",
	}

	redacted := RedactDetails(details)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		"sarvam-super-secret", "sarvam-key-value", "turn-user", "turn-pass",
		"raw upstream response", "please pay my invoice", "the private answer",
		"customer@example.com", "98765", "4111", "AAPFU0939F1ZV", "ABCDE1234F", "5678 9012",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("redacted output contains %q: %s", forbidden, text)
		}
	}
	if got := redacted["safe"]; got != "provider timed out before headers" {
		t.Fatalf("safe detail = %#v", got)
	}
	nested, ok := redacted["nested"].(map[string]any)
	if !ok || nested["safe"] != "kept" || nested["password"] != RedactedValue {
		t.Fatalf("nested redaction = %#v", redacted["nested"])
	}
}

func TestRedactDetailsIsBoundedAndFailsClosedForUnsupportedValues(t *testing.T) {
	long := strings.Repeat("x", maxDetailStringBytes+100)
	redacted := RedactDetails(map[string]any{
		"long":        long,
		"unsupported": struct{ Secret string }{Secret: "must-not-render"},
		"bytes":       []byte("must-not-render"),
	})
	if got, ok := redacted["long"].(string); !ok || len(got) > maxDetailStringBytes {
		t.Fatalf("bounded long detail = %#v", redacted["long"])
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "must-not-render") {
		t.Fatalf("unsupported values leaked: %s", encoded)
	}
}

func TestSensitiveDetailKeyCoversRequiredCategories(t *testing.T) {
	for _, key := range []string{
		"authorization", "sarvamKey", "turn_username", "turnCredential",
		"providerResponseBody", "transcript", "final_answer", "phoneNumber",
		"emailAddress", "paymentToken", "gstin", "aadhaar", "governmentId",
	} {
		if !sensitiveDetailKey(key) {
			t.Errorf("sensitiveDetailKey(%q) = false", key)
		}
	}
}

func TestRedactDetailsDetectsCredentialValuesUnderInnocuousKeys(t *testing.T) {
	details := RedactDetails(map[string]any{
		"one":   "Bearer eyJhbGciOiJIUzI1NiJ9.private.signature",
		"two":   "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.signature",
		"three": "Ocp-Apim-Subscription-Key: abcdef0123456789",
		"four":  "AKIAIOSFODNN7EXAMPLE",
		"five":  "turn://voice-user:voice-pass@turn.example.test:443?transport=udp",
	})
	for key, value := range details {
		if value != RedactedValue {
			t.Errorf("credential value %q = %#v, want %q", key, value, RedactedValue)
		}
	}
}

func TestRedactDetailsAppliesOneGlobalNodeAndByteBudget(t *testing.T) {
	leaf := map[string]any{"value": strings.Repeat("safe-value-", 100)}
	tree := any(leaf)
	for depth := 0; depth < 7; depth++ {
		branch := make(map[string]any, 16)
		for index := 0; index < 16; index++ {
			branch[string(rune('a'+index))] = tree
		}
		tree = branch
	}

	redacted := RedactDetails(map[string]any{"tree": tree})
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if len(encoded) > maxRedactedDetailsBytes {
		t.Fatalf("redacted detail bytes = %d, want <= %d", len(encoded), maxRedactedDetailsBytes)
	}
	if nodes := countJSONNodes(redacted); nodes > maxDetailTotalNodes {
		t.Fatalf("redacted detail nodes = %d, want <= %d", nodes, maxDetailTotalNodes)
	}
}

func TestRedactDetailsHardBoundsManyTruncatedSlices(t *testing.T) {
	details := make(map[string]any, 8)
	for index := 0; index < 8; index++ {
		items := make([]any, maxDetailSliceItems*2)
		for item := range items {
			items[item] = item
		}
		details[string(rune('a'+index))] = items
	}

	redacted := RedactDetails(details)
	if nodes := countJSONNodes(redacted); nodes > maxDetailTotalNodes {
		t.Fatalf("redacted detail nodes = %d, want <= %d", nodes, maxDetailTotalNodes)
	}
}

func countJSONNodes(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		count := 1
		for _, child := range typed {
			count += countJSONNodes(child)
		}
		return count
	case []any:
		count := 1
		for _, child := range typed {
			count += countJSONNodes(child)
		}
		return count
	default:
		return 1
	}
}
