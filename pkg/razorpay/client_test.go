package razorpay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
