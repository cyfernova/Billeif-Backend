package composition

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	voicesession "invoice-backend/internal/voice/session"
	"invoice-backend/internal/voice/webrtc"
)

const (
	testCurrentSessionID        = "voice_01KTEST"
	testCurrentRuntimeSessionID = "voice-session-01KTEST00000000000000000000"
	testCurrentBranchID         = "cbd6e793-62e6-4c32-a106-065709caf460"
	testCurrentAuthorization    = "Bearer current-session-authorization-canary"
)

func TestHTTPSessionAuthorizerReturnsOnlyCurrentServerAuthorizedBranch(t *testing.T) {
	tests := []struct {
		name     string
		branchID string
	}{
		{name: "branch bound", branchID: testCurrentBranchID},
		{name: "branchless for current all-branch caller"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var receivedAuthorization string
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet || request.URL.EscapedPath() != "/test/api/v1/voice/sessions/"+testCurrentSessionID {
					t.Errorf("request = %s %s", request.Method, request.URL.EscapedPath())
				}
				if request.Header.Get("Accept") != "application/json" || request.Header.Get("Accept-Encoding") != "identity" {
					t.Errorf("unsafe content negotiation: %#v", request.Header)
				}
				receivedAuthorization = request.Header.Get("Authorization")
				writer.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(writer).Encode(currentSessionResponse(test.branchID))
			}))
			defer server.Close()

			authorizer, err := NewHTTPSessionAuthorizer(HTTPSessionAuthorizerConfig{
				Origin: server.URL + "/test", Timeout: time.Second, Transport: server.Client().Transport,
			})
			if err != nil {
				t.Fatalf("NewHTTPSessionAuthorizer() error = %v", err)
			}
			defer authorizer.Close()

			result, err := authorizer.Authorize(context.Background(), testCurrentAuthorization, testCurrentSessionID)
			if err != nil || result != (webrtc.CurrentSessionAuthorization{BranchID: test.branchID}) {
				t.Fatalf("Authorize() = (%#v, %v)", result, err)
			}
			if receivedAuthorization != testCurrentAuthorization {
				t.Fatalf("Authorization = %q", receivedAuthorization)
			}
			encoded, err := json.Marshal(authorizer)
			if err != nil || string(encoded) != "{}" || strings.Contains(string(encoded), testCurrentAuthorization) {
				t.Fatalf("authorizer serialization = (%s, %v)", encoded, err)
			}
		})
	}
}

func TestHTTPSessionAuthorizerMapsCurrentAccessDenialsWithoutReadingBodies(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte("upstream-denial-body-canary"))
			}))
			defer server.Close()
			authorizer := newTestHTTPSessionAuthorizer(t, server, time.Second)

			result, err := authorizer.Authorize(context.Background(), testCurrentAuthorization, testCurrentSessionID)
			if result != (webrtc.CurrentSessionAuthorization{}) || !errors.Is(err, webrtc.ErrCurrentSessionUnauthorized) {
				t.Fatalf("Authorize(status=%d) = (%#v, %v)", status, result, err)
			}
			assertCurrentSessionErrorSanitized(t, err)
		})
	}
}

func TestHTTPSessionAuthorizerFailsClosedForUnavailableAndUnsafeResponses(t *testing.T) {
	validResponse, err := json.Marshal(currentSessionResponse(testCurrentBranchID))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name            string
		status          int
		contentType     string
		contentEncoding string
		body            []byte
	}{
		{name: "server failure", status: http.StatusInternalServerError, body: []byte("upstream-response-body-canary")},
		{name: "rate limited", status: http.StatusTooManyRequests, body: []byte("upstream-response-body-canary")},
		{name: "redirect", status: http.StatusFound, body: []byte("upstream-response-body-canary")},
		{name: "missing content type", status: http.StatusOK, body: validResponse},
		{name: "non JSON content type", status: http.StatusOK, contentType: "text/plain", body: validResponse},
		{name: "compressed", status: http.StatusOK, contentType: "application/json", contentEncoding: "gzip", body: validResponse},
		{name: "malformed JSON", status: http.StatusOK, contentType: "application/json", body: []byte(`{"session_id":`)},
		{name: "duplicate branch", status: http.StatusOK, contentType: "application/json", body: []byte(`{"session_id":"` + testCurrentSessionID + `","runtime_session_id":"` + testCurrentRuntimeSessionID + `","branch_id":"` + testCurrentBranchID + `","branch_id":""}`)},
		{name: "alternate case branch", status: http.StatusOK, contentType: "application/json", body: []byte(`{"session_id":"` + testCurrentSessionID + `","runtime_session_id":"` + testCurrentRuntimeSessionID + `","BRANCH_ID":"` + testCurrentBranchID + `"}`)},
		{name: "null branch", status: http.StatusOK, contentType: "application/json", body: []byte(`{"session_id":"` + testCurrentSessionID + `","runtime_session_id":"` + testCurrentRuntimeSessionID + `","branch_id":null}`)},
		{name: "trailing JSON", status: http.StatusOK, contentType: "application/json", body: append(validResponse, []byte(`{}`)...)},
		{name: "wrong session", status: http.StatusOK, contentType: "application/json", body: []byte(`{"session_id":"voice_other","runtime_session_id":"` + testCurrentRuntimeSessionID + `"}`)},
		{name: "missing runtime", status: http.StatusOK, contentType: "application/json", body: []byte(`{"session_id":"` + testCurrentSessionID + `"}`)},
		{name: "invalid branch", status: http.StatusOK, contentType: "application/json", body: []byte(`{"session_id":"` + testCurrentSessionID + `","runtime_session_id":"` + testCurrentRuntimeSessionID + `","branch_id":"../other"}`)},
		{name: "oversized", status: http.StatusOK, contentType: "application/json", body: bytes.Repeat([]byte(" "), maxCurrentSessionResponseBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.contentType != "" {
					writer.Header().Set("Content-Type", test.contentType)
				}
				if test.contentEncoding != "" {
					writer.Header().Set("Content-Encoding", test.contentEncoding)
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write(test.body)
			}))
			defer server.Close()
			authorizer := newTestHTTPSessionAuthorizer(t, server, time.Second)

			result, err := authorizer.Authorize(context.Background(), testCurrentAuthorization, testCurrentSessionID)
			if result != (webrtc.CurrentSessionAuthorization{}) || !errors.Is(err, webrtc.ErrCurrentSessionUnavailable) {
				t.Fatalf("Authorize() = (%#v, %v)", result, err)
			}
			assertCurrentSessionErrorSanitized(t, err)
		})
	}
}

func TestHTTPSessionAuthorizerAllowsAdditiveNonAuthoritativeMetadata(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"session_id":"` + testCurrentSessionID + `",
			"runtime_session_id":"` + testCurrentRuntimeSessionID + `",
			"branch_id":"` + testCurrentBranchID + `",
			"future_metadata":{"safe":true}
		}`))
	}))
	defer server.Close()
	authorizer := newTestHTTPSessionAuthorizer(t, server, time.Second)

	result, err := authorizer.Authorize(context.Background(), testCurrentAuthorization, testCurrentSessionID)
	if err != nil || result != (webrtc.CurrentSessionAuthorization{BranchID: testCurrentBranchID}) {
		t.Fatalf("Authorize(additive metadata) = (%#v, %v)", result, err)
	}
}

func TestHTTPSessionAuthorizerBoundsTimeoutAndHonorsCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	authorizer := newTestHTTPSessionAuthorizer(t, server, 20*time.Millisecond)

	started := time.Now()
	result, err := authorizer.Authorize(context.Background(), testCurrentAuthorization, testCurrentSessionID)
	if result != (webrtc.CurrentSessionAuthorization{}) || !errors.Is(err, webrtc.ErrCurrentSessionUnavailable) {
		t.Fatalf("Authorize(timeout) = (%#v, %v)", result, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Authorize timeout elapsed %v", elapsed)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authorizer.Authorize(canceled, testCurrentAuthorization, testCurrentSessionID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Authorize(canceled) error = %v", err)
	}
}

func TestNewHTTPSessionAuthorizerRejectsUnsafeConfigurationAndInputWithoutNetwork(t *testing.T) {
	var typedNilTransport *http.Transport
	invalidConfigs := []HTTPSessionAuthorizerConfig{
		{},
		{Origin: "http://api.example.com/test", Timeout: time.Second},
		{Origin: "https://user@example.com/test", Timeout: time.Second},
		{Origin: "https://api.example.com/test?query=1", Timeout: time.Second},
		{Origin: "https://api.example.com/test", Timeout: -time.Second},
		{Origin: "https://api.example.com/test", Timeout: maximumSessionAuthorizationTimeout + time.Millisecond},
		{Origin: "https://api.example.com/test", Timeout: time.Second, Transport: typedNilTransport},
	}
	for _, config := range invalidConfigs {
		if value, err := NewHTTPSessionAuthorizer(config); value != nil || !errors.Is(err, ErrInvalidFactoryConfig) {
			t.Fatalf("NewHTTPSessionAuthorizer(%#v) = (%#v, %v)", config, value, err)
		}
	}

	roundTrips := 0
	authorizer, err := NewHTTPSessionAuthorizer(HTTPSessionAuthorizerConfig{
		Origin: "https://api.example.com/test", Timeout: time.Second,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			roundTrips++
			return nil, errors.New("network should not be reached")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer authorizer.Close()
	for _, input := range []struct {
		ctx           context.Context
		authorization string
		sessionID     string
	}{
		{ctx: nil, authorization: testCurrentAuthorization, sessionID: testCurrentSessionID},
		{ctx: context.Background(), authorization: "Basic unsafe", sessionID: testCurrentSessionID},
		{ctx: context.Background(), authorization: testCurrentAuthorization, sessionID: "../other"},
	} {
		if _, err := authorizer.Authorize(input.ctx, input.authorization, input.sessionID); err == nil {
			t.Fatalf("Authorize(%#v) unexpectedly succeeded", input)
		}
	}
	if roundTrips != 0 {
		t.Fatalf("unsafe inputs made %d network calls", roundTrips)
	}
}

func TestHTTPSessionAuthorizerProductionTransportIsBoundedProxyFreeAndTLSBound(t *testing.T) {
	authorizer, err := NewHTTPSessionAuthorizer(HTTPSessionAuthorizerConfig{
		Origin: "https://api123.execute-api.ap-south-1.amazonaws.com/test",
	})
	if err != nil {
		t.Fatalf("NewHTTPSessionAuthorizer() error = %v", err)
	}
	transport := authorizer.ownedTransport
	if transport == nil || authorizer.client.Transport != transport || transport.Proxy != nil || !transport.DisableCompression {
		t.Fatalf("unsafe production transport: %#v", transport)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 ||
		transport.TLSClientConfig.ServerName != "api123.execute-api.ap-south-1.amazonaws.com" {
		t.Fatalf("TLS policy = %#v", transport.TLSClientConfig)
	}
	if authorizer.client.Timeout != defaultSessionAuthorizationTimeout || transport.ResponseHeaderTimeout != defaultSessionAuthorizationTimeout ||
		transport.TLSHandshakeTimeout != defaultSessionAuthorizationTimeout || transport.MaxResponseHeaderBytes != maxCurrentSessionHeaderBytes {
		t.Fatalf("transport bounds = client=%v header=%v tls=%v max-header=%d", authorizer.client.Timeout, transport.ResponseHeaderTimeout, transport.TLSHandshakeTimeout, transport.MaxResponseHeaderBytes)
	}
	if err := authorizer.client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy error = %v", err)
	}
	if err := authorizer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := authorizer.Authorize(context.Background(), testCurrentAuthorization, testCurrentSessionID); !errors.Is(err, webrtc.ErrCurrentSessionUnavailable) {
		t.Fatalf("Authorize(after close) error = %v", err)
	}
}

func currentSessionResponse(branchID string) voicesession.SessionResponse {
	return voicesession.SessionResponse{
		SessionID: testCurrentSessionID, RuntimeSessionID: testCurrentRuntimeSessionID, BranchID: branchID,
		Status: voicesession.StatusActive, ProtocolVersion: 1, KVSChannelIndex: 7,
		PreferredLanguage: "en-IN", FallbackLanguage: "hi-IN", CurrentLanguage: "en-IN",
		CreatedAt:      time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, time.August, 6, 12, 1, 0, 0, time.UTC),
		LeaseExpiresAt: time.Date(2026, time.August, 6, 12, 3, 0, 0, time.UTC),
		ExpiresAt:      time.Date(2026, time.August, 6, 12, 55, 0, 0, time.UTC),
		RotateAt:       time.Date(2026, time.August, 6, 12, 52, 0, 0, time.UTC),
		Resumable:      true,
	}
}

func newTestHTTPSessionAuthorizer(t *testing.T, server *httptest.Server, timeout time.Duration) *HTTPSessionAuthorizer {
	t.Helper()
	authorizer, err := NewHTTPSessionAuthorizer(HTTPSessionAuthorizerConfig{
		Origin: server.URL, Timeout: timeout, Transport: server.Client().Transport,
	})
	if err != nil {
		t.Fatalf("NewHTTPSessionAuthorizer() error = %v", err)
	}
	t.Cleanup(func() { _ = authorizer.Close() })
	return authorizer
}

func assertCurrentSessionErrorSanitized(t *testing.T, err error) {
	t.Helper()
	for _, canary := range []string{testCurrentAuthorization, "upstream-denial-body-canary", "upstream-response-body-canary"} {
		if strings.Contains(err.Error(), canary) {
			t.Fatalf("error leaked %q: %v", canary, err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
