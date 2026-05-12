package services

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestGenerateWebhookSecretFailsClosedWhenEntropyUnavailable(t *testing.T) {
	original := webhookSecretReader
	webhookSecretReader = failingReader{}
	t.Cleanup(func() { webhookSecretReader = original })

	if secret, err := generateWebhookSecret(); err == nil || secret != "" {
		t.Fatalf("expected entropy failure to return no secret, got secret=%q err=%v", secret, err)
	}
}

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
	client := newWebhookDeliveryHTTPClient(2 * time.Second)
	err := client.CheckRedirect(&http.Request{}, []*http.Request{{}})
	if err != http.ErrUseLastResponse {
		t.Fatalf("expected redirect policy to stop redirects, got %v", err)
	}
}
