package verify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPJourneyProbeCleanupAndSecretSafety(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Header.Get("Authorization") != "Bearer journey-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"invoice-1","provider_token":"must-not-appear"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	probe := &HTTPJourneyProbe{
		ID: "journey.invoice", Endpoint: server.URL + "/create", BearerToken: "journey-secret",
		Body: `{}`, ResourceKind: "synthetic-invoice",
		CleanupURL: server.URL + "/delete/{reference}", HTTPClient: server.Client(),
	}
	outcome := probe.Probe(context.Background())
	if outcome.Status != StatusPassed || outcome.Cleanup == nil || len(outcome.Resources) != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if _, err := outcome.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if strings.Contains(sanitizeEvidenceToString(outcome.Evidence), "must-not-appear") || strings.Contains(sanitizeEvidenceToString(outcome.Evidence), "journey-secret") {
		t.Fatal("journey response or credential leaked")
	}
	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodDelete {
		t.Fatalf("methods = %v", methods)
	}
}

func TestHTTPJourneyProbePreservesUnknownOutcomeWithoutUncorrelatedCleanup(t *testing.T) {
	cleanupCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			cleanupCalls++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	check := &HTTPJourneyProbe{ID: "journey.unknown", Endpoint: server.URL + "/create", CleanupURL: server.URL + "/cleanup/{reference}", HTTPClient: server.Client()}
	opts := optionsForRun()
	opts.Mode = ModeWrite
	opts.AllowWrites = true
	result := resultFor(t, executeForTest(t, opts, []Check{check}), check.ID)
	if result.Status != StatusFailed || result.Attempts != 1 || result.Cleanup.Status != CleanupPreserved || cleanupCalls != 0 {
		t.Fatalf("result=%+v cleanup_calls=%d", result, cleanupCalls)
	}
}

func TestProbeURLsRequireHTTPSOutsideLoopback(t *testing.T) {
	if err := validateProbeURL("http://provider.example/probe"); err == nil {
		t.Fatal("external plain HTTP must be refused")
	}
	for _, raw := range []string{"https://provider.example/probe", "http://127.0.0.1:8080/probe", "http://localhost:8080/probe"} {
		if err := validateProbeURL(raw); err != nil {
			t.Fatalf("validateProbeURL(%q): %v", raw, err)
		}
	}
}

func TestCredentialedProbesRefuseRedirects(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	outcome := (&GSTSandboxProbe{BaseURL: redirect.URL, Sandbox: true, APIToken: "secret", HTTPClient: redirect.Client()}).Probe(context.Background())
	if outcome.Status == StatusPassed || targetCalls != 0 {
		t.Fatalf("redirect outcome=%s target_calls=%d", outcome.Status, targetCalls)
	}
}

func TestRazorpayTestModeProbeClassifications(t *testing.T) {
	if got := (&RazorpayTestModeProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing keys status = %q, want not_configured", got)
	}

	var sawUser, sawPass string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plans" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		sawUser, sawPass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entity":"collection","count":1,"items":[{"id":"plan_x","product_name":"Pro"}]}`))
	}))
	defer server.Close()

	passed := (&RazorpayTestModeProbe{
		KeyID:      "rzp_test_unit123456",
		KeySecret:  "unit-secret",
		Mode:       "test",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	if sawUser != "rzp_test_unit123456" || sawPass != "unit-secret" {
		t.Fatalf("basic auth = %q/%q", sawUser, sawPass)
	}
	serialized := sanitizeEvidenceToString(passed.Evidence)
	if strings.Contains(serialized, "unit-secret") || strings.Contains(serialized, "rzp_test_unit123456") {
		t.Fatalf("razorpay credentials leaked: %s", serialized)
	}
	if !strings.Contains(serialized, "rzp_test_") {
		t.Fatalf("key id prefix evidence missing: %s", serialized)
	}

	blocked := (&RazorpayTestModeProbe{
		KeyID:      "rzp_live_unit123456",
		KeySecret:  "live-secret",
		Mode:       "live",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}).Probe(context.Background())
	if blocked.Status != StatusBlocked {
		t.Fatalf("live mode status = %q, want blocked", blocked.Status)
	}
	if reason, _ := evidenceMap(blocked.Evidence)["reason"].(string); reason != "test_mode_required" {
		t.Fatalf("blocked reason = %q, want test_mode_required", reason)
	}
	if blocked.Err != nil {
		t.Fatal("blocked outcome must not execute the provider call")
	}

	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"description":"backend down secret-abc123456789"}}`))
	}))
	defer failingServer.Close()
	failed := (&RazorpayTestModeProbe{
		KeyID:      "rzp_test_unit123456",
		KeySecret:  "unit-secret",
		Mode:       "test",
		BaseURL:    failingServer.URL,
		HTTPClient: failingServer.Client(),
	}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("500 status = %q, want failed", failed.Status)
	}
	if strings.Contains(sanitizeEvidenceToString(failed.Evidence), "secret-abc123456789") {
		t.Fatal("provider error payload leaked into evidence")
	}
}

func TestGSTSandboxProbeClassifications(t *testing.T) {
	if got := (&GSTSandboxProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing config status = %q, want not_configured", got)
	}
	blocked := (&GSTSandboxProbe{BaseURL: "https://gst.example", Sandbox: false, APIToken: "tok"}).Probe(context.Background())
	if blocked.Status != StatusBlocked {
		t.Fatalf("non-sandbox status = %q, want blocked", blocked.Status)
	}

	var headers http.Header
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":1,"token":"secret-gst-token-123456789"}`))
	}))
	defer server.Close()

	passed := (&GSTSandboxProbe{
		BaseURL:      server.URL,
		ValidatePath: "/validate",
		Sandbox:      true,
		APIToken:     "bearer-token",
		ClientID:     "client",
		ClientSecret: "client-secret-value",
		GSPName:      "billeif",
		HTTPClient:   server.Client(),
	}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	if headers.Get("X-Client-Id") != "client" || headers.Get("Authorization") != "Bearer bearer-token" {
		t.Fatalf("probe must reuse the adapter transport contract: %v", headers)
	}
	if body == "" {
		t.Fatal("probe must send a JSON body")
	}
	serialized := sanitizeEvidenceToString(passed.Evidence)
	for _, secret := range []string{"bearer-token", "client-secret-value", "secret-gst-token-123456789"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("gst secret %q leaked into evidence", secret)
		}
	}

	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer failingServer.Close()
	failed := (&GSTSandboxProbe{
		BaseURL:      failingServer.URL,
		ValidatePath: "/validate",
		Sandbox:      true,
		Username:     "u",
		Password:     "p",
		HTTPClient:   failingServer.Client(),
	}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("401 status = %q, want failed", failed.Status)
	}
}

func TestLLMGatewayProbeClassifications(t *testing.T) {
	if got := (&LLMGatewayProbe{Family: "claude"}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing config status = %q, want not_configured", got)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer llm-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"claude-test-model"},{"id":"gemini-2.5-pro"}]}`))
	}))
	defer server.Close()

	base := LLMGatewayProbe{APIURL: server.URL + "/v1/chat/completions", APIKey: "llm-key", HTTPClient: server.Client()}

	claude := base
	claude.Family = "claude"
	claude.Model = "claude-test-model"
	if got := claude.Probe(context.Background()); got.Status != StatusPassed {
		t.Fatalf("claude status = %q (%v)", got.Status, got.Err)
	}

	gemini := base
	gemini.Family = "gemini"
	gemini.Model = "gemini-test-model"
	if got := gemini.Probe(context.Background()); got.Status != StatusPassed {
		t.Fatalf("gemini status = %q (%v)", got.Status, got.Err)
	}

	// Gemini not offered by the gateway: truthful not_configured, not failure.
	geminiMissing := base
	geminiMissing.Family = "gemini"
	geminiMissing.Model = "claude-only-model"
	missing := geminiMissing.Probe(context.Background())
	if missing.Status != StatusNotConfigured {
		t.Fatalf("no gemini family status = %q, want not_configured", missing.Status)
	}

	// Claude family absent: truthful failure.
	claudeMissing := base
	claudeMissing.Family = "claude"
	claudeMissing.Model = "gemini-only-model"
	unavailable := claudeMissing.Probe(context.Background())
	if unavailable.Status != StatusFailed {
		t.Fatalf("no claude family status = %q, want failed", unavailable.Status)
	}
}

func TestWhatsAppTestEnvProbeClassifications(t *testing.T) {
	if got := (&WhatsAppTestEnvProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing config status = %q, want not_configured", got)
	}

	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/1234567890" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"verified_name":"Billeif Test","quality_rating":"GREEN"}`))
	}))
	defer server.Close()

	probe := &WhatsAppTestEnvProbe{
		BaseURL:        server.URL,
		PhoneNumberID:  "1234567890",
		AccessTokenEnv: "WHA_ACCESS_TOKEN",
		PhoneNumberEnv: "WHA_PHONE_ID",
		EnvLookup:      func(string) string { return "" },
		HTTPClient:     server.Client(),
	}
	if got := probe.Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing env status = %q, want not_configured", got)
	}

	probe.EnvLookup = func(name string) string {
		if name == "WHA_ACCESS_TOKEN" {
			return "wa-token"
		}
		return "1234567890"
	}
	passed := probe.Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	if sawAuth != "Bearer wa-token" {
		t.Fatalf("probe must use Authorization header, got %q", sawAuth)
	}
	if strings.Contains(sanitizeEvidenceToString(passed.Evidence), "wa-token") {
		t.Fatal("whatsapp token leaked into evidence")
	}
}

func TestSarvamAPIProbeClassifications(t *testing.T) {
	if got := (&SarvamAPIProbe{}).Probe(context.Background()).Status; got != StatusNotConfigured {
		t.Fatalf("missing config status = %q, want not_configured", got)
	}

	var sawKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawKey = r.Header.Get("api-subscription-key")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer server.Close()

	passed := (&SarvamAPIProbe{APIKey: "sarvam-key", BaseURL: server.URL, HTTPClient: server.Client()}).Probe(context.Background())
	if passed.Status != StatusPassed {
		t.Fatalf("status = %q (%v)", passed.Status, passed.Err)
	}
	if sawKey != "sarvam-key" {
		t.Fatalf("probe did not send the sarvam api key header, got %q", sawKey)
	}
	if strings.Contains(sanitizeEvidenceToString(passed.Evidence), "sarvam-key") {
		t.Fatal("sarvam api key leaked into evidence")
	}

	failed := (&SarvamAPIProbe{APIKey: "sarvam-key", BaseURL: "http://127.0.0.1:1", HTTPClient: server.Client()}).Probe(context.Background())
	if failed.Status != StatusFailed {
		t.Fatalf("unreachable status = %q, want failed", failed.Status)
	}
}

// --- helpers -----------------------------------------------------------------

func sanitizeEvidenceToString(evidence []Evidence) string {
	redacted := SanitizeEvidence(evidence)
	var builder strings.Builder
	for _, item := range redacted {
		builder.WriteString(item.Key)
		builder.WriteString("=")
		builder.WriteString(stringifyEvidence(item.Value))
		builder.WriteString(";")
	}
	return builder.String()
}

func stringifyEvidence(value any) string {
	redacted := RedactValue(value)
	if s, ok := redacted.(string); ok {
		return s
	}
	raw, err := json.Marshal(redacted)
	if err != nil {
		return "<?>"
	}
	return string(raw)
}
