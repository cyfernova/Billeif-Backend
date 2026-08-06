package tools

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testInvoiceID  = "33333333-3333-4333-8333-3333333333cc"
	testCustomerID = "44444444-4444-4444-8444-4444444444dd"
	publicTestIP   = "93.184.216.34"
	testCursor     = "v1.eyJidXNpbmVzc19pZCI6IjExMTExMTExLTExMTEtNDExMS04MTExLTExMTExMTExMTFhYSIsImNyZWF0ZWRfYXQiOiIyMDI2LTA4LTAxVDAwOjAwOjAwWiIsImlkIjoiMzMzMzMzMzMtMzMzMy00MzMzLTgzMzMtMzMzMzMzMzMzM2NjIn0.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

func TestRegistryExecutesExactRoutesWithFreshAuthorizationAndProjectsSafeFields(t *testing.T) {
	invoiceResponse := `{"id":"` + testInvoiceID + `","business_id":"` + testBusinessID + `",` +
		`"invoice_no":"INV-2026-001","status":"issued","invoice_date":"2026-08-01T00:00:00Z",` +
		`"issued_at":"2026-08-02T00:00:00Z","due_date":"2026-08-31T00:00:00Z","currency":"INR",` +
		`"subtotal":100,"tax":18,"discount":2,"total":116,"paid_amount":16,"balance_due":100,` +
		`"buyer_snapshot":{"name":"Acme Retail","email":"nested-sensitive@example.com"},` +
		`"notes":"invoice-sensitive-canary","custom_fields":"invoice-custom-canary",` +
		`"sign_metadata":"invoice-signature-canary","pdf_url":"https://private.invalid/invoice"}`
	customerResponse := `{"id":"` + testCustomerID + `","business_id":"` + testBusinessID + `",` +
		`"name":"Ada Buyer","company_name":"Ada Stores","email":"ada@example.com","phone":"+91-555-0100",` +
		`"address":"customer-address-canary","gstin":"customer-gstin-canary","pan":"customer-pan-canary",` +
		`"tax_id":"customer-tax-canary","notes":"customer-notes-canary","credit_limit":999999}`

	tokens := []string{
		"Bearer rotated-token-1",
		"Bearer rotated-token-2",
		"Bearer rotated-token-3",
		"Bearer rotated-token-4",
	}
	tests := []struct {
		name      string
		tool      string
		arguments json.RawMessage
		path      string
		query     url.Values
		response  string
		want      string
	}{
		{
			name: "list invoices", tool: "list_invoices", arguments: json.RawMessage(`{}`), path: "/api/v1/invoices",
			query:    url.Values{"business_id": {testBusinessID}, "limit": {"10"}},
			response: `{"items":[` + invoiceResponse + `],"next_cursor":"` + testCursor + `"}`,
			want: `{"items":[{"id":"` + testInvoiceID + `","invoice_number":"INV-2026-001","status":"issued",` +
				`"invoice_date":"2026-08-01T00:00:00Z","issued_at":"2026-08-02T00:00:00Z",` +
				`"due_date":"2026-08-31T00:00:00Z","currency":"INR","subtotal":100,"tax":18,"discount":2,` +
				`"total":116,"paid_amount":16,"balance_due":100,"customer_name":"Acme Retail"}],"next_cursor":"` + testCursor + `"}`,
		},
		{
			name: "get invoice", tool: "get_invoice", arguments: json.RawMessage(`{"id":"` + testInvoiceID + `"}`),
			path: "/api/v1/invoices/" + testInvoiceID, query: url.Values{"business_id": {testBusinessID}}, response: invoiceResponse,
			want: `{"id":"` + testInvoiceID + `","invoice_number":"INV-2026-001","status":"issued",` +
				`"invoice_date":"2026-08-01T00:00:00Z","issued_at":"2026-08-02T00:00:00Z",` +
				`"due_date":"2026-08-31T00:00:00Z","currency":"INR","subtotal":100,"tax":18,"discount":2,` +
				`"total":116,"paid_amount":16,"balance_due":100,"customer_name":"Acme Retail"}`,
		},
		{
			name: "list customers", tool: "list_customers", arguments: json.RawMessage(`{}`), path: "/api/v1/customers",
			query:    url.Values{"business_id": {testBusinessID}, "page": {"1"}, "limit": {"10"}},
			response: `{"data":[` + customerResponse + `],"total":1,"page":1,"limit":10}`,
			want:     `{"data":[{"id":"` + testCustomerID + `","name":"Ada Buyer","company_name":"Ada Stores","email":"ada@example.com","phone":"+91-555-0100"}],"total":1,"page":1,"limit":10}`,
		},
		{
			name: "get customer", tool: "get_customer", arguments: json.RawMessage(`{"id":"` + testCustomerID + `"}`),
			path: "/api/v1/customers/" + testCustomerID, query: url.Values{"business_id": {testBusinessID}}, response: customerResponse,
			want: `{"id":"` + testCustomerID + `","name":"Ada Buyer","company_name":"Ada Stores",` +
				`"email":"ada@example.com","phone":"+91-555-0100"}`,
		},
	}

	var requestIndex atomic.Int32
	var origin string
	server, configuredOrigin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		index := int(requestIndex.Add(1)) - 1
		if index >= len(tests) {
			t.Errorf("unexpected extra request: %s %s", request.Method, request.URL)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		test := tests[index]
		if request.Method != http.MethodGet || request.URL.Path != test.path || request.URL.RawQuery != test.query.Encode() {
			t.Errorf("request[%d] = %s %s?%s, want GET %s?%s", index, request.Method, request.URL.Path, request.URL.RawQuery, test.path, test.query.Encode())
		}
		if request.Host != strings.TrimPrefix(origin, "https://") {
			t.Errorf("request[%d] Host = %q, want exact immutable origin host", index, request.Host)
		}
		if got := request.Header.Get("Authorization"); got != tokens[index] {
			t.Errorf("request[%d] Authorization = %q, want fresh value %q", index, got, tokens[index])
		}
		if got := request.Header.Get("Accept"); got != "application/json" {
			t.Errorf("request[%d] Accept = %q, want application/json", index, got)
		}
		if request.Body != nil && request.ContentLength != 0 {
			t.Errorf("GET request[%d] unexpectedly has a body", index)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(test.response))
	}))
	origin = configuredOrigin
	defer server.Close()

	source := &sequenceAuthorizationSource{values: tokens}
	binding, err := NewSessionBinding(source, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin, Timeout: time.Second, Resolver: resolver, Dialer: dialer, RootCAs: roots, EnableCustomerTools: true,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := registry.Execute(context.Background(), test.tool, test.arguments)
			if err != nil {
				t.Fatalf("Execute(%s) error = %v", test.tool, err)
			}
			assertJSONEqual(t, got, json.RawMessage(test.want))
			for _, canary := range []string{
				"nested-sensitive@example.com", "invoice-sensitive-canary", "invoice-custom-canary",
				"invoice-signature-canary", "private.invalid", "customer-address-canary", "customer-gstin-canary",
				"customer-pan-canary", "customer-tax-canary", "customer-notes-canary", "999999",
			} {
				if strings.Contains(string(got), canary) {
					t.Fatalf("Execute(%s) exposed sensitive field canary %q in %s", test.tool, canary, got)
				}
			}
		})
	}
	if got := source.Calls(); got != len(tests) {
		t.Fatalf("AuthorizationSource calls = %d, want one immediately before each of %d requests", got, len(tests))
	}
	if got := requestIndex.Load(); got != int32(len(tests)) {
		t.Fatalf("HTTP request count = %d, want %d", got, len(tests))
	}
	if resolver.Calls() == 0 || dialer.Calls() == 0 {
		t.Fatal("injected resolver/dialer did not carry the real HTTPS request")
	}
}

func TestRegistryFreezesConfiguredStagePrefixAheadOfFixedAPIRoute(t *testing.T) {
	var calls atomic.Int32
	server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/staging/api/v1/invoices" {
			t.Errorf("request = %s %s, want fixed staged invoice route", request.Method, request.URL.Path)
		}
		if request.URL.Query().Get("business_id") != testBusinessID || request.URL.Query().Get("limit") != "10" {
			t.Errorf("query = %s, want bound business/default limit", request.URL.RawQuery)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[],"next_cursor":null}`))
	}))
	defer server.Close()

	binding, err := NewSessionBinding(staticAuthorizationSource{value: testBearer}, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin + "/staging/", Timeout: time.Second, Resolver: resolver, Dialer: dialer, RootCAs: roots,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry(staged origin) error = %v", err)
	}
	defer registry.Close()

	result, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	assertJSONEqual(t, result, json.RawMessage(`{"items":[],"next_cursor":null}`))
	if got := calls.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestRegistryUsesOnlyBoundedToolPaginationArguments(t *testing.T) {
	wants := []struct {
		path     string
		query    url.Values
		response string
	}{
		{
			path:     "/api/v1/invoices",
			query:    url.Values{"business_id": {testBusinessID}, "limit": {"20"}, "cursor": {testCursor}},
			response: `{"items":[],"next_cursor":null}`,
		},
		{
			path:     "/api/v1/customers",
			query:    url.Values{"business_id": {testBusinessID}, "page": {"1000"}, "limit": {"20"}},
			response: `{"data":[],"total":0,"page":1000,"limit":20}`,
		},
	}
	var index atomic.Int32
	server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		position := int(index.Add(1)) - 1
		if position >= len(wants) {
			t.Errorf("unexpected extra request: %s", request.URL)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		want := wants[position]
		if request.URL.Path != want.path || request.URL.RawQuery != want.query.Encode() {
			t.Errorf("request = %s?%s, want %s?%s", request.URL.Path, request.URL.RawQuery, want.path, want.query.Encode())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(want.response))
	}))
	defer server.Close()

	binding, err := NewSessionBinding(staticAuthorizationSource{value: testBearer}, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin, Timeout: time.Second, Resolver: resolver, Dialer: dialer, RootCAs: roots, EnableCustomerTools: true,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()

	if _, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{"limit":20,"cursor":"`+testCursor+`"}`)); err != nil {
		t.Fatalf("list_invoices Execute() error = %v", err)
	}
	if _, err := registry.Execute(context.Background(), "list_customers", json.RawMessage(`{"page":1000,"limit":20}`)); err != nil {
		t.Fatalf("list_customers Execute() error = %v", err)
	}
	if got := index.Load(); got != int32(len(wants)) {
		t.Fatalf("request count = %d, want %d", got, len(wants))
	}
}

func assertJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("got invalid JSON %s: %v", got, err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("test has invalid expected JSON %s: %v", want, err)
	}
	gotDocument, _ := json.Marshal(gotValue)
	wantDocument, _ := json.Marshal(wantValue)
	if string(gotDocument) != string(wantDocument) {
		t.Fatalf("JSON = %s, want %s", gotDocument, wantDocument)
	}
}

type sequenceAuthorizationSource struct {
	mu     sync.Mutex
	values []string
	calls  int
}

func (source *sequenceAuthorizationSource) Authorization(context.Context) (string, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls >= len(source.values) {
		return "", fmt.Errorf("authorization sequence exhausted")
	}
	value := source.values[source.calls]
	source.calls++
	return value, nil
}

func (source *sequenceAuthorizationSource) Calls() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.calls
}

type fakeResolver struct {
	mu      sync.Mutex
	answers [][]net.IPAddr
	calls   int
}

func (resolver *fakeResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	index := resolver.calls
	resolver.calls++
	if len(resolver.answers) == 0 {
		return nil, fmt.Errorf("no fake resolution")
	}
	if index >= len(resolver.answers) {
		index = len(resolver.answers) - 1
	}
	return append([]net.IPAddr(nil), resolver.answers[index]...), nil
}

func (resolver *fakeResolver) Calls() int {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	return resolver.calls
}

type mappingDialer struct {
	mu        sync.Mutex
	want      string
	target    string
	addresses []string
}

func (dialer *mappingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer.mu.Lock()
	dialer.addresses = append(dialer.addresses, address)
	dialer.mu.Unlock()
	if address != dialer.want {
		return nil, fmt.Errorf("dialed unvalidated address")
	}
	return (&net.Dialer{}).DialContext(ctx, network, dialer.target)
}

func (dialer *mappingDialer) Calls() int {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return len(dialer.addresses)
}

func newInjectedTLSServer(t *testing.T, handler http.Handler) (*httptest.Server, string, *x509.CertPool, *fakeResolver, *mappingDialer) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		server.Close()
		t.Fatalf("split fake TLS address: %v", err)
	}
	origin := "https://example.com:" + port
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	resolver := &fakeResolver{answers: [][]net.IPAddr{{{IP: net.ParseIP(publicTestIP)}}}}
	dialer := &mappingDialer{
		want:   net.JoinHostPort(publicTestIP, port),
		target: server.Listener.Addr().String(),
	}
	return server, origin, roots, resolver, dialer
}
