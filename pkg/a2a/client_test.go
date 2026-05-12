package a2a

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSendMessageRejectsLocalhostEndpoint(t *testing.T) {
	client := NewA2AClient(nil, nil)

	_, err := client.SendMessage(context.Background(), "http://127.0.0.1:8080", validSendMessageRequest())
	if err == nil {
		t.Fatal("expected localhost A2A endpoint to be rejected")
	}
}

func TestSendMessageRejectsPrivateDNSResolution(t *testing.T) {
	client := &A2AClient{
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
			Transport: newSafeA2ATransport(
				func(ctx context.Context, host string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
				},
				func(ctx context.Context, network, address string) (net.Conn, error) {
					t.Fatalf("dial should not be called for private DNS result %q", address)
					return nil, nil
				},
			),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	_, err := client.SendMessage(context.Background(), "https://seller.example", validSendMessageRequest())
	if err == nil {
		t.Fatal("expected private DNS result to be rejected")
	}
	if !strings.Contains(err.Error(), "private or local IP addresses are not allowed") {
		t.Fatalf("expected private IP error, got %v", err)
	}
}

func TestSendMessageDoesNotFollowRedirects(t *testing.T) {
	transport := &redirectOnceTransport{}
	client := &A2AClient{
		httpClient: &http.Client{
			Timeout:   2 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	_, err := client.SendMessage(context.Background(), "https://seller.example", validSendMessageRequest())
	if err == nil {
		t.Fatal("expected redirect response to be returned as an A2A error")
	}
	if transport.count != 1 {
		t.Fatalf("expected redirect not to be followed, got %d requests", transport.count)
	}
}

func validSendMessageRequest() *SendMessageRequest {
	return &SendMessageRequest{
		Message: NewTextMessage(RoleUser, "hello"),
	}
}

type redirectOnceTransport struct {
	count int
}

func (t *redirectOnceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.count++
	return &http.Response{
		StatusCode: http.StatusFound,
		Header: http.Header{
			"Location": []string{"http://127.0.0.1:8080/private"},
		},
		Body:    io.NopCloser(strings.NewReader("redirect")),
		Request: req,
	}, nil
}
