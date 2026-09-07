package razorpay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSubscriptionLifecycleUsesOfficialRazorpayContracts(t *testing.T) {
	requests := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/subscriptions":
			var input SubscriptionCreateParams
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.PlanID != "plan_pro_test_01" || input.TotalCount != 1200 || input.Quantity != 1 || input.CustomerNotify {
				t.Fatalf("unexpected create input: %#v", input)
			}
			if input.Notes["business_id"] != "business-a" || input.Notes["subscription_id"] != "local-sub-a" || input.Notes["provider_mode"] != "test" {
				t.Fatalf("missing tenant metadata: %#v", input.Notes)
			}
			requests = append(requests, "create")
			_, _ = w.Write([]byte(`{"id":"sub_provider_a","entity":"subscription","plan_id":"plan_pro_test_01","status":"created","short_url":"https://rzp.io/i/test","created_at":1788278400}`))
		case r.Method == http.MethodGet && r.URL.Path == "/subscriptions/sub_provider_a":
			requests = append(requests, "fetch")
			_, _ = w.Write([]byte(`{"id":"sub_provider_a","entity":"subscription","plan_id":"plan_pro_test_01","customer_id":"cust_provider_a","status":"active","current_start":1788278400,"current_end":1790870400,"charge_at":1790870400,"paid_count":1,"notes":{"business_id":"business-a","subscription_id":"local-sub-a","provider_mode":"test"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/subscriptions/sub_provider_a":
			var input SubscriptionUpdateParams
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.PlanID != "plan_rise_test_01" || input.ScheduleChangeAt != "cycle_end" || input.CustomerNotify {
				t.Fatalf("unexpected update input: %#v", input)
			}
			requests = append(requests, "update")
			_, _ = w.Write([]byte(`{"id":"sub_provider_a","entity":"subscription","plan_id":"plan_pro_test_01","status":"active","has_scheduled_changes":true,"schedule_change_at":"cycle_end","change_scheduled_at":1790870400}`))
		case r.Method == http.MethodPost && r.URL.Path == "/subscriptions/sub_provider_a/cancel":
			var input SubscriptionCancelParams
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if !input.CancelAtCycleEnd {
				t.Fatalf("cancel must be scheduled at cycle end: %#v", input)
			}
			requests = append(requests, "cancel")
			_, _ = w.Write([]byte(`{"id":"sub_provider_a","entity":"subscription","plan_id":"plan_pro_test_01","status":"active","current_end":1790870400}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(Config{KeyID: "key", KeySecret: "secret", BaseURL: server.URL}, nil)

	created, err := client.CreateSubscription(context.Background(), SubscriptionCreateParams{
		PlanID: "plan_pro_test_01", TotalCount: 1200, Quantity: 1, CustomerNotify: false,
		Notes: map[string]string{"business_id": "business-a", "subscription_id": "local-sub-a", "provider_mode": "test"},
	})
	if err != nil || created.ID != "sub_provider_a" || created.ShortURL == "" {
		t.Fatalf("CreateSubscription() = %#v, %v", created, err)
	}
	fetched, err := client.FetchSubscription(context.Background(), created.ID)
	if err != nil || fetched.PaidCount != 1 || fetched.CurrentEnd != 1790870400 {
		t.Fatalf("FetchSubscription() = %#v, %v", fetched, err)
	}
	if _, err := client.UpdateSubscription(context.Background(), created.ID, SubscriptionUpdateParams{
		PlanID: "plan_rise_test_01", ScheduleChangeAt: "cycle_end", CustomerNotify: false,
	}); err != nil {
		t.Fatalf("UpdateSubscription() error = %v", err)
	}
	if _, err := client.CancelSubscription(context.Background(), created.ID, SubscriptionCancelParams{CancelAtCycleEnd: true}); err != nil {
		t.Fatalf("CancelSubscription() error = %v", err)
	}
	if got := strings.Join(requests, ","); got != "create,fetch,update,cancel" {
		t.Fatalf("request sequence = %s", got)
	}
}

func TestRazorpayEndpointClassNeverExposesProviderIdentifiers(t *testing.T) {
	tests := map[string]string{
		"/subscriptions/sub_sensitive":        "/subscriptions/:subscription",
		"/subscriptions/sub_sensitive/cancel": "/subscriptions/:subscription/cancel",
		"/payments/pay_sensitive":             "/payments/:resource",
		"/orders?receipt=internal-sensitive":  "/orders",
	}
	for input, want := range tests {
		if got := razorpayEndpointClass(input); got != want {
			t.Fatalf("razorpayEndpointClass(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClientHTTPErrorPreservesStatusWithoutRawProviderBody(t *testing.T) {
	const rawBody = `{"error":"account acct_123 secret credential rejected"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(rawBody))
	}))
	defer server.Close()
	client := NewClient(Config{KeyID: "key", KeySecret: "secret", BaseURL: server.URL}, nil)

	_, err := client.FetchOrdersByReceipt(context.Background(), "health-check")

	var statusError interface{ HTTPStatusCode() int }
	if !errors.As(err, &statusError) {
		t.Fatalf("error = %T %v, want typed HTTP status error", err, err)
	}
	if statusError.HTTPStatusCode() != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", statusError.HTTPStatusCode(), http.StatusTooManyRequests)
	}
	if strings.Contains(err.Error(), "acct_123") || strings.Contains(err.Error(), "credential") {
		t.Fatalf("error leaked raw provider body: %v", err)
	}
}

func TestClientProbeUsesBoundedReadOnlyOrderList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/orders" || r.URL.Query().Get("count") != "1" {
			t.Fatalf("probe request = %s %s, want GET /orders?count=1", r.Method, r.URL.String())
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "key" || password != "secret" {
			t.Fatalf("probe did not use configured authentication")
		}
		_, _ = w.Write([]byte(`{"entity":"collection","count":0,"items":[]}`))
	}))
	defer server.Close()
	client := NewClient(Config{KeyID: "key", KeySecret: "secret", BaseURL: server.URL}, nil)

	if err := client.Probe(context.Background()); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
}
