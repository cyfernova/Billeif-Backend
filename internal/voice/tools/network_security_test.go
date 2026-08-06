package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolverRejectsEveryNonPublicOrMixedAnswerBeforeDial(t *testing.T) {
	tests := []struct {
		name    string
		answers []net.IPAddr
	}{
		{name: "unspecified IPv4", answers: []net.IPAddr{{IP: net.ParseIP("0.0.0.0")}}},
		{name: "private IPv4", answers: []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}},
		{name: "loopback IPv4", answers: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}},
		{name: "metadata IPv4", answers: []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}},
		{name: "carrier grade NAT", answers: []net.IPAddr{{IP: net.ParseIP("100.64.0.1")}}},
		{name: "IPv4 mapped loopback", answers: []net.IPAddr{{IP: net.ParseIP("::ffff:127.0.0.1")}}},
		{name: "unspecified IPv6", answers: []net.IPAddr{{IP: net.ParseIP("::")}}},
		{name: "loopback IPv6", answers: []net.IPAddr{{IP: net.ParseIP("::1")}}},
		{name: "private IPv6", answers: []net.IPAddr{{IP: net.ParseIP("fd00:ec2::254")}}},
		{name: "link local IPv6", answers: []net.IPAddr{{IP: net.ParseIP("fe80::1")}}},
		{name: "multicast IPv6", answers: []net.IPAddr{{IP: net.ParseIP("ff02::1")}}},
		{name: "zoned IPv6", answers: []net.IPAddr{{IP: net.ParseIP("2606:4700:4700::1111"), Zone: "en0"}}},
		{name: "mixed public and private", answers: []net.IPAddr{{IP: net.ParseIP(publicTestIP)}, {IP: net.ParseIP("127.0.0.1")}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeResolver{answers: [][]net.IPAddr{test.answers}}
			dialer := &countingRejectDialer{}
			source := &countingAuthorizationSource{value: testBearer}
			binding, err := NewSessionBinding(source, testBusinessID, "")
			if err != nil {
				t.Fatalf("NewSessionBinding() error = %v", err)
			}
			registry, err := NewRegistry(Config{
				Origin: "https://example.com", Timeout: 100 * time.Millisecond, Resolver: resolver, Dialer: dialer,
			}, binding)
			if err != nil {
				t.Fatalf("NewRegistry() error = %v", err)
			}
			t.Cleanup(func() { _ = registry.Close() })

			result, executeErr := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
			if result != nil || !errors.Is(executeErr, ErrToolUnavailable) {
				t.Fatalf("Execute() = (%s, %v), want SSRF fail-closed", result, executeErr)
			}
			if got := dialer.calls.Load(); got != 0 {
				t.Fatalf("underlying dialer called %d times for unsafe DNS answer", got)
			}
			if got := resolver.Calls(); got != 1 {
				t.Fatalf("resolver calls = %d, want 1", got)
			}
		})
	}
}

func TestResolverRevalidatesEachNewConnectionAndStopsRebinding(t *testing.T) {
	var requests atomic.Int32
	server, origin, roots, _, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Connection", "close")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[],"next_cursor":null}`))
	}))
	defer server.Close()
	resolver := &fakeResolver{answers: [][]net.IPAddr{
		{{IP: net.ParseIP(publicTestIP)}},
		{{IP: net.ParseIP("127.0.0.1")}},
	}}
	source := &countingAuthorizationSource{value: testBearer}
	binding, err := NewSessionBinding(source, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin, Timeout: time.Second, Resolver: resolver, Dialer: dialer, RootCAs: roots,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	if _, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	result, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
	if result != nil || !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("second Execute() = (%s, %v), want rebinding rejection", result, err)
	}
	if got := resolver.Calls(); got != 2 {
		t.Fatalf("resolver calls = %d, want one per new connection", got)
	}
	if got := dialer.Calls(); got != 1 {
		t.Fatalf("underlying dialer calls = %d, want unsafe second answer blocked", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("server requests = %d, want only pre-rebinding request", got)
	}
}

func TestRedirectIsNeverFollowedAndCredentialIsNotForwarded(t *testing.T) {
	var redirectTargetCalls atomic.Int32
	var redirectTargetAuthorization atomic.Value
	redirectTarget := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		redirectTargetCalls.Add(1)
		redirectTargetAuthorization.Store(request.Header.Get("Authorization"))
	}))
	defer redirectTarget.Close()

	server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", redirectTarget.URL+"/credential-target")
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	token := "Bearer redirect-secret-canary"
	binding, err := NewSessionBinding(staticAuthorizationSource{value: token}, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin, Timeout: time.Second, Resolver: resolver, Dialer: dialer, RootCAs: roots,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	result, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
	if result != nil || !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("Execute() = (%s, %v), want redirect rejection", result, err)
	}
	if got := redirectTargetCalls.Load(); got != 0 {
		t.Fatalf("redirect target reached %d times", got)
	}
	if value := redirectTargetAuthorization.Load(); value != nil {
		t.Fatalf("redirect target received Authorization %q", value)
	}
	if stringsContainAny(err.Error(), token, redirectTarget.URL) {
		t.Fatalf("redirect error exposed sensitive transport detail: %v", err)
	}
}

type countingRejectDialer struct {
	calls atomic.Int32
}

func (dialer *countingRejectDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	dialer.calls.Add(1)
	return nil, errors.New("dial detail must stay internal")
}

func stringsContainAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
