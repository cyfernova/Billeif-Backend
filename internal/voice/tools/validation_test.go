package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecuteRejectsInvalidArgumentsBeforeAuthorizationOrNetwork(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		arguments json.RawMessage
		wantErr   error
	}{
		{name: "unknown tool", tool: "fetch_url", arguments: json.RawMessage(`{}`), wantErr: ErrUnknownTool},
		{name: "missing JSON", tool: "list_invoices", wantErr: ErrInvalidToolArguments},
		{name: "null", tool: "list_invoices", arguments: json.RawMessage(`null`), wantErr: ErrInvalidToolArguments},
		{name: "array", tool: "list_invoices", arguments: json.RawMessage(`[]`), wantErr: ErrInvalidToolArguments},
		{name: "trailing document", tool: "list_invoices", arguments: json.RawMessage(`{} {}`), wantErr: ErrInvalidToolArguments},
		{name: "unknown argument", tool: "list_invoices", arguments: json.RawMessage(`{"url":"https://attacker.invalid"}`), wantErr: ErrInvalidToolArguments},
		{name: "host argument", tool: "list_invoices", arguments: json.RawMessage(`{"host":"attacker.invalid"}`), wantErr: ErrInvalidToolArguments},
		{name: "method argument", tool: "list_invoices", arguments: json.RawMessage(`{"method":"POST"}`), wantErr: ErrInvalidToolArguments},
		{name: "path argument", tool: "list_invoices", arguments: json.RawMessage(`{"path":"/admin"}`), wantErr: ErrInvalidToolArguments},
		{name: "header argument", tool: "list_invoices", arguments: json.RawMessage(`{"headers":{"Authorization":"Bearer attacker"}}`), wantErr: ErrInvalidToolArguments},
		{name: "business argument", tool: "list_invoices", arguments: json.RawMessage(`{"business_id":"` + testOtherBusinessID + `"}`), wantErr: ErrInvalidToolArguments},
		{name: "branch argument", tool: "list_invoices", arguments: json.RawMessage(`{"branch_id":"` + testBranchID + `"}`), wantErr: ErrInvalidToolArguments},
		{name: "duplicate argument", tool: "list_invoices", arguments: json.RawMessage(`{"limit":1,"limit":20}`), wantErr: ErrInvalidToolArguments},
		{name: "zero invoice limit", tool: "list_invoices", arguments: json.RawMessage(`{"limit":0}`), wantErr: ErrInvalidToolArguments},
		{name: "over invoice limit", tool: "list_invoices", arguments: json.RawMessage(`{"limit":21}`), wantErr: ErrInvalidToolArguments},
		{name: "fractional invoice limit", tool: "list_invoices", arguments: json.RawMessage(`{"limit":1.5}`), wantErr: ErrInvalidToolArguments},
		{name: "string invoice limit", tool: "list_invoices", arguments: json.RawMessage(`{"limit":"10"}`), wantErr: ErrInvalidToolArguments},
		{name: "empty cursor", tool: "list_invoices", arguments: json.RawMessage(`{"cursor":""}`), wantErr: ErrInvalidToolArguments},
		{name: "control cursor", tool: "list_invoices", arguments: json.RawMessage(`{"cursor":"bad\nvalue"}`), wantErr: ErrInvalidToolArguments},
		{name: "arbitrary cursor", tool: "list_invoices", arguments: json.RawMessage(`{"cursor":"opaque-next"}`), wantErr: ErrInvalidToolArguments},
		{name: "unicode cursor", tool: "list_invoices", arguments: json.RawMessage(`{"cursor":"v1.payload.署名"}`), wantErr: ErrInvalidToolArguments},
		{name: "padded cursor", tool: "list_invoices", arguments: json.RawMessage(`{"cursor":"v1.e30=.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`), wantErr: ErrInvalidToolArguments},
		{name: "cross tenant cursor", tool: "list_invoices", arguments: json.RawMessage(`{"cursor":"` + cursorTokenForTest(testOtherBusinessID, testInvoiceID) + `"}`), wantErr: ErrInvalidToolArguments},
		{name: "missing invoice id", tool: "get_invoice", arguments: json.RawMessage(`{}`), wantErr: ErrInvalidToolArguments},
		{name: "noncanonical invoice id", tool: "get_invoice", arguments: json.RawMessage(`{"id":"` + strings.ToUpper(testInvoiceID) + `"}`), wantErr: ErrInvalidToolArguments},
		{name: "zero customer page", tool: "list_customers", arguments: json.RawMessage(`{"page":0}`), wantErr: ErrInvalidToolArguments},
		{name: "over customer page", tool: "list_customers", arguments: json.RawMessage(`{"page":1001}`), wantErr: ErrInvalidToolArguments},
		{name: "over customer limit", tool: "list_customers", arguments: json.RawMessage(`{"limit":21}`), wantErr: ErrInvalidToolArguments},
		{name: "missing customer id", tool: "get_customer", arguments: json.RawMessage(`{}`), wantErr: ErrInvalidToolArguments},
		{name: "oversized args", tool: "list_invoices", arguments: json.RawMessage(`{"cursor":"` + strings.Repeat("x", maximumToolArgumentBytes) + `"}`), wantErr: ErrInvalidToolArguments},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &countingAuthorizationSource{value: testBearer}
			registry := newPanicNetworkRegistry(t, source, "")
			result, err := registry.Execute(context.Background(), test.tool, test.arguments)
			if result != nil || !errors.Is(err, test.wantErr) {
				t.Fatalf("Execute() = (%s, %v), want (nil, %v)", result, err, test.wantErr)
			}
			if got := source.calls.Load(); got != 0 {
				t.Fatalf("AuthorizationSource called %d times for rejected arguments", got)
			}
		})
	}
}

func TestBranchBoundSessionsFailAllToolsBeforeAuthorizationOrNetwork(t *testing.T) {
	source := &countingAuthorizationSource{value: testBearer}
	registry := newPanicNetworkRegistry(t, source, testBranchID)
	for _, call := range []struct {
		name      string
		arguments json.RawMessage
	}{
		{name: "list_invoices", arguments: json.RawMessage(`{}`)},
		{name: "get_invoice", arguments: json.RawMessage(`{"id":"` + testInvoiceID + `"}`)},
		{name: "list_customers", arguments: json.RawMessage(`{}`)},
		{name: "get_customer", arguments: json.RawMessage(`{"id":"` + testCustomerID + `"}`)},
	} {
		t.Run(call.name, func(t *testing.T) {
			result, err := registry.Execute(context.Background(), call.name, call.arguments)
			if result != nil || !errors.Is(err, ErrBranchScopeUnsupported) {
				t.Fatalf("Execute() = (%s, %v), want branch fail-closed", result, err)
			}
		})
	}
	if got := source.calls.Load(); got != 0 {
		t.Fatalf("AuthorizationSource called %d times for branch-bound session", got)
	}
}

func TestCustomerToolsAreDisabledUnlessCompositionExplicitlyEnablesThem(t *testing.T) {
	source := &countingAuthorizationSource{value: testBearer}
	binding, err := NewSessionBinding(source, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: "https://example.com", Timeout: time.Second, Resolver: panicResolver{}, Dialer: panicDialer{},
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	definitions := registry.Definitions()
	if len(definitions) != 2 || definitions[0].Name != "list_invoices" || definitions[1].Name != "get_invoice" {
		t.Fatalf("Definitions() = %#v, want only invoice tools", definitions)
	}
	for _, name := range []string{"list_customers", "get_customer"} {
		result, executeErr := registry.Execute(context.Background(), name, json.RawMessage(`{}`))
		if result != nil || !errors.Is(executeErr, ErrUnknownTool) {
			t.Fatalf("Execute(%s) = (%s, %v), want unavailable", name, result, executeErr)
		}
	}
	if got := source.calls.Load(); got != 0 {
		t.Fatalf("AuthorizationSource called %d times for disabled tools", got)
	}

	enabledSource := &countingAuthorizationSource{value: testBearer}
	enabledRegistry := newPanicNetworkRegistry(t, enabledSource, testBranchID)
	if got := len(enabledRegistry.Definitions()); got != 4 {
		t.Fatalf("explicitly enabled Definitions() count = %d, want 4", got)
	}
}

func TestNewRegistryRejectsUnsafeOrAmbiguousConfiguration(t *testing.T) {
	var typedNilResolver *fakeResolver
	var typedNilDialer *mappingDialer
	tests := []struct {
		name   string
		config Config
	}{
		{name: "missing origin", config: Config{}},
		{name: "plaintext origin", config: Config{Origin: "http://example.com"}},
		{name: "userinfo", config: Config{Origin: "https://user:pass@example.com"}},
		{name: "path traversal", config: Config{Origin: "https://example.com/staging/../admin"}},
		{name: "double slash path", config: Config{Origin: "https://example.com/staging//admin"}},
		{name: "escaped path", config: Config{Origin: "https://example.com/staging%2Fadmin"}},
		{name: "query", config: Config{Origin: "https://example.com?target=other"}},
		{name: "fragment", config: Config{Origin: "https://example.com#other"}},
		{name: "loopback literal", config: Config{Origin: "https://127.0.0.1"}},
		{name: "metadata literal", config: Config{Origin: "https://169.254.169.254"}},
		{name: "localhost name", config: Config{Origin: "https://localhost"}},
		{name: "local suffix", config: Config{Origin: "https://api.local"}},
		{name: "underscore host", config: Config{Origin: "https://bad_host.example"}},
		{name: "empty host label", config: Config{Origin: "https://bad..example"}},
		{name: "leading host hyphen", config: Config{Origin: "https://-bad.example"}},
		{name: "trailing host hyphen", config: Config{Origin: "https://bad-.example"}},
		{name: "invalid port", config: Config{Origin: "https://example.com:70000"}},
		{name: "negative timeout", config: Config{Origin: "https://example.com", Timeout: -time.Second}},
		{name: "timeout above cap", config: Config{Origin: "https://example.com", Timeout: maximumToolTimeout + time.Nanosecond}},
		{name: "resolver only", config: Config{Origin: "https://example.com", Resolver: panicResolver{}}},
		{name: "dialer only", config: Config{Origin: "https://example.com", Dialer: panicDialer{}}},
		{name: "typed nil resolver with dialer", config: Config{Origin: "https://example.com", Resolver: typedNilResolver, Dialer: panicDialer{}}},
		{name: "resolver with typed nil dialer", config: Config{Origin: "https://example.com", Resolver: panicResolver{}, Dialer: typedNilDialer}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding, err := NewSessionBinding(staticAuthorizationSource{value: testBearer}, testBusinessID, "")
			if err != nil {
				t.Fatalf("NewSessionBinding() error = %v", err)
			}
			t.Cleanup(func() { _ = binding.Close() })
			registry, err := NewRegistry(test.config, binding)
			if registry != nil || !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("NewRegistry() = (%v, %v), want (nil, invalid configuration)", registry, err)
			}
		})
	}
}

func newPanicNetworkRegistry(t *testing.T, source AuthorizationSource, branchID string) *Registry {
	t.Helper()
	binding, err := NewSessionBinding(source, testBusinessID, branchID)
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: "https://example.com", Timeout: time.Second, Resolver: panicResolver{}, Dialer: panicDialer{}, EnableCustomerTools: true,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	return registry
}

type countingAuthorizationSource struct {
	value string
	err   error
	calls atomic.Int32
}

func (source *countingAuthorizationSource) Authorization(context.Context) (string, error) {
	source.calls.Add(1)
	return source.value, source.err
}
