package services

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveAllowedWebhookIPs_BlocksPrivateDNSResolution(t *testing.T) {
	lookup := func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.10")}}, nil
	}

	_, err := resolveAllowedWebhookIPs(context.Background(), "safe.example.com", lookup)
	if err == nil {
		t.Fatal("expected error for private DNS resolution")
	}
}

func TestDialWebhookAddress_UsesResolvedPublicAddress(t *testing.T) {
	lookup := func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}

	dialedAddress := ""
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialedAddress = address
		c1, c2 := net.Pipe()
		go c2.Close()
		return c1, nil
	}

	conn, err := dialWebhookAddress(context.Background(), "tcp", "safe.example.com:443", lookup, dial)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	_ = conn.Close()

	if dialedAddress != "93.184.216.34:443" {
		t.Fatalf("expected dial to resolved public IP, got %q", dialedAddress)
	}
}

func TestWebhookDeliveryClient_DoesNotFollowRedirects(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Location", "https://127.0.0.1/private")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := newWebhookDeliveryHTTPClient(2 * time.Second)
	// Keep redirect behavior, but use default transport so this test can use httptest loopback.
	client.Transport = http.DefaultTransport

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected redirect response, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 response, got %d", resp.StatusCode)
	}
	if requestCount != 1 {
		t.Fatalf("expected a single request without redirect follow, got %d", requestCount)
	}
}
