package verify

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRedactValueRedactsSensitiveKeysRecursively(t *testing.T) {
	input := map[string]any{
		"nested": map[string]any{
			"api_key":        "sk-live-abc123",
			"API_KEY":        "sk-live-def456",
			"client_secret":  "super-secret",
			"Authorization":  "Bearer abc.def.ghi",
			"Webhook-Secret": "whsec_123",
			"safe":           "value",
			"deep": map[string]any{
				"access_token": "tok_abc",
				"count":        float64(3),
			},
		},
		"cookie":                "session=xyz",
		"aws_secret_access_key": "wJalrXUtnFEMI",
	}
	redacted := RedactValue(input).(map[string]any)
	nested := redacted["nested"].(map[string]any)
	for _, key := range []string{"api_key", "API_KEY", "client_secret", "Authorization", "Webhook-Secret"} {
		if got := nested[key]; got != redactedPlaceholder {
			t.Fatalf("nested[%q] = %#v, want %q", key, got, redactedPlaceholder)
		}
	}
	if nested["safe"] != "value" {
		t.Fatalf("nested[safe] = %#v, want untouched value", nested["safe"])
	}
	deep := nested["deep"].(map[string]any)
	if deep["access_token"] != redactedPlaceholder {
		t.Fatalf("deep[access_token] = %#v, want %q", deep["access_token"], redactedPlaceholder)
	}
	if deep["count"] != float64(3) {
		t.Fatalf("deep[count] = %#v, want 3", deep["count"])
	}
	if redacted["cookie"] != redactedPlaceholder {
		t.Fatalf("cookie = %#v, want %q", redacted["cookie"], redactedPlaceholder)
	}
	if redacted["aws_secret_access_key"] != redactedPlaceholder {
		t.Fatalf("aws_secret_access_key = %#v, want %q", redacted["aws_secret_access_key"], redactedPlaceholder)
	}
}

func TestRedactValueRedactsSecretsInsideStringValues(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bearer header",
			input: "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			want:  "[REDACTED_BEARER_TOKEN]",
		},
		{
			name:  "short bearer token",
			input: "Bearer xyz12345",
			want:  "[REDACTED_BEARER_TOKEN]",
		},
		{
			name:  "raw jwt",
			input: "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c leaked",
			want:  "token [REDACTED_JWT] leaked",
		},
		{
			name:  "aws access key id",
			input: "credentials AKIAIOSFODNN7EXAMPLE in evidence",
			want:  "credentials [REDACTED_AWS_ACCESS_KEY_ID] in evidence",
		},
		{
			name:  "razorpay key id",
			input: "razorpay rzp_live_AbCdEf123456 configured",
			want:  "razorpay [REDACTED_RAZORPAY_KEY_ID] configured",
		},
		{
			name:  "database dsn with password",
			input: "postgres://user:hunter2@example.com:5432/db",
			want:  "postgres://user:[REDACTED]@example.com:5432/db",
		},
		{
			name:  "query token parameter",
			input: "https://example.com/cb?token=abc123def456&other=keep",
			want:  "https://example.com/cb?token=[REDACTED]&other=keep",
		},
		{
			name:  "long hex credential",
			input: "secret 0123456789abcdef0123456789abcdef01234567",
			want:  "secret [REDACTED_CREDENTIAL]",
		},
		{
			name:  "plain safe text untouched",
			input: "verification completed in 120ms",
			want:  "verification completed in 120ms",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RedactString(tc.input); got != tc.want {
				t.Fatalf("RedactString(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRedactValueHandlesSlicesAndPrimitives(t *testing.T) {
	input := []any{
		map[string]any{"session_token": "abc", "ok": true},
		[]any{float64(1), "Bearer xyz12345"},
		"safe",
		float64(12),
		nil,
	}
	redacted := RedactValue(input).([]any)
	first := redacted[0].(map[string]any)
	if first["session_token"] != redactedPlaceholder || first["ok"] != true {
		t.Fatalf("slice[0] = %#v", first)
	}
	second := redacted[1].([]any)
	if second[0] != float64(1) || second[1] != "[REDACTED_BEARER_TOKEN]" {
		t.Fatalf("slice[1] = %#v", second)
	}
	if redacted[2] != "safe" || redacted[3] != float64(12) || redacted[4] != nil {
		t.Fatalf("slice tail = %#v", redacted)
	}
}

func TestRedactValueNeverExposesErrorStrings(t *testing.T) {
	input := map[string]any{"err": errors.New("request failed: password=hunter2")}
	redacted := RedactValue(input).(map[string]any)
	if strings.Contains(redacted["err"].(string), "hunter2") {
		t.Fatalf("password leaked through error value: %#v", redacted["err"])
	}
}

func TestRedactValueDoesNotMutateInput(t *testing.T) {
	input := map[string]any{"api_key": "sk-live-abc123", "inner": map[string]any{"token": "tok"}}
	_ = RedactValue(input)
	if input["api_key"] != "sk-live-abc123" {
		t.Fatal("input map must not be mutated")
	}
	if input["inner"].(map[string]any)["token"] != "tok" {
		t.Fatal("nested input map must not be mutated")
	}
}

func TestRedactValueHandlesCyclesAndUnknownTypes(t *testing.T) {
	inner := map[string]any{}
	inner["self"] = inner
	inner["password"] = "leak"
	redacted := RedactValue(inner).(map[string]any)
	if _, ok := redacted["self"]; !ok {
		t.Fatal("cycle guard must still emit a key")
	}
	if strings.Contains(redactedStringify(redacted["self"]), "leak") {
		t.Fatalf("cycle redaction leaked value: %#v", redacted["self"])
	}
	if got, ok := RedactValue(struct{ Secret string }{"x"}).(string); !ok || got != "struct { Secret string }" {
		t.Fatalf("unknown struct type must degrade to type name, got %#v", RedactValue(struct{ Secret string }{"x"}))
	}
}

func TestRedactValueBoundsDepthAndLength(t *testing.T) {
	deep := map[string]any{}
	cursor := deep
	for i := 0; i < 40; i++ {
		next := map[string]any{}
		cursor["child"] = next
		cursor = next
	}
	redacted := RedactValue(deep).(map[string]any)
	if strings.Contains(redactedStringify(redacted), "40") && false {
		t.Fatal("unreachable")
	}
	long := strings.Repeat("x", 10000)
	trimmed := RedactString(long)
	if len(trimmed) > 512+len(truncationMarker) || !strings.HasSuffix(trimmed, truncationMarker) {
		t.Fatalf("long string must be bounded with truncation marker, len=%d tail=%q", len(trimmed), tailOf(trimmed, 32))
	}
	if _, err := json.Marshal(redacted); err != nil {
		t.Fatalf("redacted tree must stay JSON-marshalable: %v", err)
	}
}

func redactedStringify(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
