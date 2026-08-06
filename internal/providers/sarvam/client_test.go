package sarvam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientPostJSONInjectsOnlyCanonicalAuthenticationAndKeepsConfigImmutable(t *testing.T) {
	const originalKey = "sarvam-test-key-5f9e"
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/text-to-speech/stream" {
			t.Errorf("path = %q, want /text-to-speech/stream", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		if got := r.Header.Values("api-subscription-key"); len(got) != 1 || got[0] != originalKey {
			t.Errorf("api-subscription-key = %#v, want one canonical credential", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Accept"); got != "audio/mpeg" {
			t.Errorf("Accept = %q, want audio/mpeg", got)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request JSON: %v", err)
		}
		if len(payload) != 1 || payload["text"] != "hello" {
			t.Errorf("payload = %#v, want map[text:hello]", payload)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("x-request-id", "request-123")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "audio")
		received <- struct{}{}
	}))
	defer server.Close()

	cfg := Config{APIKey: originalKey, BaseURL: server.URL}
	client, err := NewClient(cfg, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	cfg.APIKey = "mutated-key"
	response, err := client.PostJSON(context.Background(), JSONRequest{
		Path:          "/text-to-speech/stream",
		Payload:       map[string]string{"text": "hello"},
		Accept:        "audio/mpeg",
		ResponseLimit: 16,
	})
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	<-received
	if string(response.Body) != "audio" {
		t.Fatalf("response body = %q, want audio", response.Body)
	}
	if response.ContentType != "audio/mpeg" || response.RequestID != "request-123" {
		t.Fatalf("response metadata = %#v, want audio/mpeg and request-123", response)
	}
}

func TestClientPostJSONPreservesConfiguredBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider-gateway/text-to-speech/stream" {
			t.Errorf("path = %q, want /provider-gateway/text-to-speech/stream", r.URL.Path)
		}
		_, _ = io.WriteString(w, "audio")
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "base-path-test-key", BaseURL: server.URL + "/provider-gateway"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.PostJSON(context.Background(), JSONRequest{
		Path:          "/text-to-speech/stream",
		Payload:       map[string]string{"text": "hello"},
		ResponseLimit: 16,
	})
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
}

func TestClientRefusesCrossOriginRedirectBeforeCredentialCanBeForwarded(t *testing.T) {
	const apiKey = "redirect-test-api-key"
	var redirectedCalls atomic.Int32
	var redirectedCredential atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedCalls.Add(1)
		if r.Header.Get("api-subscription-key") != "" {
			redirectedCredential.Store(true)
		}
		_, _ = io.WriteString(w, "redirected")
	}))
	defer destination.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL+"/credential-target")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client, err := NewClient(Config{APIKey: apiKey, BaseURL: origin.URL}, origin.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.PostJSON(context.Background(), JSONRequest{
		Path:          "/redirect",
		Payload:       map[string]string{"text": "hello"},
		ResponseLimit: 32,
	})

	if got := redirectedCalls.Load(); got != 0 {
		t.Fatalf("redirected host received %d requests, want 0", got)
	}
	if redirectedCredential.Load() {
		t.Fatal("redirected host received api-subscription-key")
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("error = %v, want safe provider status 307", err)
	}
}

func TestClientPostJSONPreservesCallerDeadlineAndCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "deadline-test-key", BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	startedAt := time.Now()

	_, err = client.PostJSON(ctx, JSONRequest{Path: "/wait", Payload: map[string]bool{"wait": true}})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("provider request never started")
	}
	if elapsed := time.Since(startedAt); elapsed >= 250*time.Millisecond {
		t.Fatalf("provider call returned after %s, want caller deadline to cancel it before 250ms", elapsed)
	}
}

func TestClientPostJSONBoundsRequestsAndResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "123456")
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "bounds-test-key", BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.PostJSON(context.Background(), JSONRequest{
		Path:    "/bounded",
		Payload: map[string]string{"value": strings.Repeat("x", 1<<20)},
	})
	if !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("oversized request error = %v, want ErrRequestTooLarge", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("oversized request reached provider %d times, want 0", got)
	}

	_, err = client.PostJSON(context.Background(), JSONRequest{
		Path:          "/bounded",
		Payload:       map[string]string{"value": "ok"},
		ResponseLimit: 5,
	})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("oversized response error = %v, want ErrResponseTooLarge", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("bounded response calls = %d, want 1", got)
	}
}

func TestClientProviderErrorExposesOnlySafeStatusAndRequestID(t *testing.T) {
	const apiKey = "never-log-this-api-key"
	const providerBody = "never-log-this-provider-body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-request-id", "request-safe-429")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, providerBody)
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: apiKey, BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.PostJSON(context.Background(), JSONRequest{Path: "/limited", Payload: map[string]string{"text": "hello"}})
	if err == nil {
		t.Fatal("PostJSON error = nil, want provider error")
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if providerErr.StatusCode != 429 || providerErr.RequestID != "request-safe-429" {
		t.Fatalf("provider error = %#v, want status 429 and safe request ID", providerErr)
	}

	var logOutput bytes.Buffer
	log.New(&logOutput, "", 0).Printf("provider call failed: %v", err)
	combined := err.Error() + "\n" + logOutput.String()
	for _, forbidden := range []string{apiKey, providerBody} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("error/log output leaked %q: %q", forbidden, combined)
		}
	}
	if !strings.Contains(combined, "429") || !strings.Contains(combined, "request-safe-429") {
		t.Fatalf("safe diagnostics missing from %q", combined)
	}
}

func TestClientDropsRequestIDThatReflectsAPIKey(t *testing.T) {
	const apiKey = "reflected-api-key-912"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-request-id", apiKey)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: apiKey, BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.PostJSON(context.Background(), JSONRequest{Path: "/auth", Payload: map[string]string{"text": "hello"}})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if providerErr.RequestID != "" {
		t.Fatalf("unsafe request ID = %q, want empty", providerErr.RequestID)
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Fatalf("provider error reflected API key: %q", err)
	}
}

type errorDoer struct {
	err error
}

func (d errorDoer) Do(*http.Request) (*http.Response, error) {
	return nil, d.err
}

func TestClientRedactsTransportErrorsAndRequiresCallerContext(t *testing.T) {
	const apiKey = "transport-secret-key"
	const providerDetail = "upstream-secret-detail"
	client, err := NewClient(Config{APIKey: apiKey, BaseURL: "https://sarvam.example.test"}, errorDoer{
		err: fmt.Errorf("dial failed with %s and %s", apiKey, providerDetail),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.PostJSON(context.Background(), JSONRequest{Path: "/chat", Payload: map[string]string{"text": "hello"}})
	if !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("transport error = %v, want ErrRequestFailed", err)
	}
	if strings.Contains(err.Error(), apiKey) || strings.Contains(err.Error(), providerDetail) {
		t.Fatalf("transport error leaked sensitive details: %q", err)
	}

	var missingContext context.Context
	_, err = client.PostJSON(missingContext, JSONRequest{Path: "/chat", Payload: map[string]string{"text": "hello"}})
	if !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil-context error = %v, want ErrContextRequired", err)
	}
}

type trackingBody struct {
	reader *strings.Reader
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

type responseDoer struct {
	response *http.Response
}

func (d responseDoer) Do(*http.Request) (*http.Response, error) { return d.response, nil }

func TestClientClosesProviderResponseBodies(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader("failure")}
	client, err := NewClient(Config{APIKey: "close-test-key", BaseURL: "https://sarvam.example.test"}, responseDoer{
		response: &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       body,
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.PostJSON(context.Background(), JSONRequest{Path: "/chat", Payload: map[string]string{"text": "hello"}})
	if err == nil {
		t.Fatal("PostJSON error = nil, want provider error")
	}
	if !body.closed {
		t.Fatal("provider response body was not closed")
	}
}
