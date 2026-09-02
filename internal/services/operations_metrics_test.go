package services

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/operationsmetrics"
)

func newTestOperationsEmitter(t *testing.T) (*operationsmetrics.Emitter, *bytes.Buffer) {
	t.Helper()
	output := &bytes.Buffer{}
	emitter, err := operationsmetrics.NewEmitter(output, "test", time.Now)
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	return emitter, output
}

func TestConfiguredGSTProviderEmitsBoundedProviderLatency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	emitter, output := newTestOperationsEmitter(t)

	provider := &configuredGSTProvider{
		cfg:        &config.Config{GST: config.GSTConfig{BaseURL: server.URL}},
		httpClient: server.Client(),
		log:        logger.New(),
	}
	provider.WithOperationsMetrics(emitter)

	payload, err := provider.doJSON(context.Background(), http.MethodPost, "/einvoice", map[string]string{"k": "v"}, nil)
	if err != nil {
		t.Fatalf("doJSON: %v", err)
	}
	if payload == nil {
		t.Fatalf("expected a decoded payload")
	}
	var sample map[string]any
	if err := json.Unmarshal(output.Bytes(), &sample); err != nil {
		t.Fatalf("decode EMF: %v", err)
	}
	if sample["Category"] != string(operationsmetrics.CategoryProvider) {
		t.Fatalf("category = %#v", sample["Category"])
	}
	latency, ok := sample["ProviderLatencyMilliseconds"].(float64)
	if !ok || latency <= 0 {
		t.Fatalf("ProviderLatencyMilliseconds = %#v", sample["ProviderLatencyMilliseconds"])
	}
	for _, forbidden := range []string{"apitoken", "clientsecret", "credential", "password", "authorization"} {
		if strings.Contains(strings.ToLower(output.String()), forbidden) {
			t.Fatalf("EMF leaked sensitive field %q: %s", forbidden, output.String())
		}
	}
}

func TestConfiguredGSTProviderEmitsLatencyForFailedProviderCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	emitter, output := newTestOperationsEmitter(t)

	provider := &configuredGSTProvider{
		cfg:        &config.Config{GST: config.GSTConfig{BaseURL: server.URL}},
		httpClient: server.Client(),
		log:        logger.New(),
	}
	provider.WithOperationsMetrics(emitter)

	if _, err := provider.doJSON(context.Background(), http.MethodPost, "/einvoice", map[string]string{}, nil); err == nil {
		t.Fatalf("expected a provider HTTP failure")
	}
	var sample map[string]any
	if err := json.Unmarshal(output.Bytes(), &sample); err != nil {
		t.Fatalf("decode EMF: %v", err)
	}
	latency, ok := sample["ProviderLatencyMilliseconds"].(float64)
	if !ok || latency < 0 {
		t.Fatalf("ProviderLatencyMilliseconds = %#v", sample["ProviderLatencyMilliseconds"])
	}
}

func TestConfiguredGSTProviderWithoutEmitterStillSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	provider := &configuredGSTProvider{
		cfg:        &config.Config{GST: config.GSTConfig{BaseURL: server.URL}},
		httpClient: server.Client(),
		log:        logger.New(),
	}
	payload, err := provider.doJSON(context.Background(), http.MethodPost, "/einvoice", map[string]string{}, nil)
	if err != nil || payload == nil {
		t.Fatalf("nil emitter must not fail provider calls: %v, %v", payload, err)
	}
}

func TestRazorpayWebhookFailuresEmitBoundedWebhookMetric(t *testing.T) {
	svc, _, _ := newRazorpayPaymentTestService(t)
	emitter, output := newTestOperationsEmitter(t)
	svc.WithOperationsMetrics(emitter)

	raw := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_x","order_id":"order_x","amount":1,"currency":"INR","status":"captured"}}}}`)
	if _, err := svc.HandleWebhook(context.Background(), "invalid-signature", "evt_bad_signature", raw); err == nil {
		t.Fatalf("expected an invalid signature rejection")
	}
	if _, err := svc.HandleWebhook(context.Background(), hmacHex(string(raw), "webhook_secret"), "evt_bad_payload", []byte(`{not-json`)); err == nil {
		t.Fatalf("expected an invalid payload rejection")
	}

	events := decodeWebhookFailureSamples(t, output.String())
	if len(events) != 2 {
		t.Fatalf("expected one metric per rejected webhook, got %d: %s", len(events), output.String())
	}
	for _, event := range events {
		if event["Category"] != string(operationsmetrics.CategoryWebhook) || event["WebhookFailures"] != float64(1) {
			t.Fatalf("webhook sample = %#v", event)
		}
	}
	for _, forbidden := range []string{"invalid-signature", "not-json", "payload"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("EMF leaked webhook detail %q: %s", forbidden, output.String())
		}
	}
}

func TestRazorpayWebhookReconciliationEmitsBoundedWebhookMetric(t *testing.T) {
	svc, db, _ := newRazorpayPaymentTestService(t)
	emitter, output := newTestOperationsEmitter(t)
	svc.WithOperationsMetrics(emitter)

	businessID := "11111111-1111-4111-8111-111111111111"
	if err := db.Create(&models.PaymentAttempt{
		ID:              "22222222-2222-4222-8222-222222222222",
		UserID:          "user_1",
		BusinessID:      businessID,
		TargetType:      models.PaymentAttemptTargetStoreOrder,
		TargetID:        "33333333-3333-4333-8333-333333333333",
		AmountPaise:     12345,
		Currency:        "INR",
		RazorpayOrderID: "order_reconciliation",
		Status:          models.PaymentAttemptStatusCreated,
		IdempotencyKey:  "idem-reconciliation",
	}).Error; err != nil {
		t.Fatalf("create payment attempt: %v", err)
	}

	raw := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_rec","order_id":"order_reconciliation","amount":500,"currency":"INR","status":"captured","captured":true}}}}`)
	signature := hmacHex(string(raw), "webhook_secret")
	if _, err := svc.HandleWebhook(context.Background(), signature, "evt_reconciliation", raw); err == nil {
		t.Fatalf("expected a reconciliation-required outcome")
	}

	events := decodeWebhookFailureSamples(t, output.String())
	if len(events) != 1 || events[0]["WebhookFailures"] != float64(1) {
		t.Fatalf("expected one reconciliation webhook failure sample, got %s", output.String())
	}
}

func decodeWebhookFailureSamples(t *testing.T, output string) []map[string]any {
	t.Helper()
	events := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode EMF: %v", err)
		}
		if _, present := payload["WebhookFailures"]; present {
			events = append(events, payload)
		}
	}
	return events
}
