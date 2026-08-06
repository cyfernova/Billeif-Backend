package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestExecuteValidatesEveryAuthorizationByteUsingSharedBearerPolicy(t *testing.T) {
	server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[],"next_cursor":null}`))
	}))
	defer server.Close()

	tests := []struct {
		name    string
		value   string
		wantErr error
	}{
		{name: "canonical scheme", value: "Bearer valid-token"},
		{name: "lowercase scheme", value: "bearer valid-token"},
		{name: "maximum length", value: "Bearer " + strings.Repeat("x", maximumAuthorizationBytes-len("Bearer "))},
		{name: "missing", wantErr: ErrAuthorizationUnavailable},
		{name: "wrong scheme", value: "Basic valid-token", wantErr: ErrAuthorizationUnavailable},
		{name: "missing token", value: "Bearer ", wantErr: ErrAuthorizationUnavailable},
		{name: "embedded space", value: "Bearer token with spaces", wantErr: ErrAuthorizationUnavailable},
		{name: "tab", value: "Bearer token\tvalue", wantErr: ErrAuthorizationUnavailable},
		{name: "carriage return", value: "Bearer token\rvalue", wantErr: ErrAuthorizationUnavailable},
		{name: "newline", value: "Bearer token\nvalue", wantErr: ErrAuthorizationUnavailable},
		{name: "delete control", value: "Bearer token\x7fvalue", wantErr: ErrAuthorizationUnavailable},
		{name: "over limit", value: "Bearer " + strings.Repeat("x", maximumAuthorizationBytes-len("Bearer ")+1), wantErr: ErrAuthorizationUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &countingAuthorizationSource{value: test.value}
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

			result, executeErr := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
			if test.wantErr != nil {
				if result != nil || !errors.Is(executeErr, test.wantErr) {
					t.Fatalf("Execute() = (%s, %v), want (nil, %v)", result, executeErr, test.wantErr)
				}
			} else if executeErr != nil {
				t.Fatalf("Execute() error = %v", executeErr)
			}
			if got := source.calls.Load(); got != 1 {
				t.Fatalf("AuthorizationSource calls = %d, want 1", got)
			}
		})
	}
}

func TestExecutePreservesCancellationFromAuthorizationSource(t *testing.T) {
	source := &waitingAuthorizationSource{}
	registry := newPanicNetworkRegistry(t, source, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	result, err := registry.Execute(ctx, "list_invoices", json.RawMessage(`{}`))
	if result != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() = (%s, %v), want caller deadline", result, err)
	}
	if source.calls != 1 {
		t.Fatalf("AuthorizationSource calls = %d, want 1", source.calls)
	}
}

func TestAuthorizationRefreshErrorFromSourceRetainsOrchestratorMarker(t *testing.T) {
	source := &countingAuthorizationSource{err: &AuthorizationRefreshRequiredError{}}
	registry := newPanicNetworkRegistry(t, source, "")
	result, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
	if result != nil || !errors.Is(err, ErrAuthorizationRefreshRequired) {
		t.Fatalf("Execute() = (%s, %v), want refresh required", result, err)
	}
	marker, ok := err.(interface{ AuthorizationRefreshRequired() bool })
	if !ok || !marker.AuthorizationRefreshRequired() {
		t.Fatalf("error %T does not expose refresh marker", err)
	}
}

type waitingAuthorizationSource struct {
	calls int
}

func (source *waitingAuthorizationSource) Authorization(ctx context.Context) (string, error) {
	source.calls++
	<-ctx.Done()
	return "", ctx.Err()
}
