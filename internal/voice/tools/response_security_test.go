package tools

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	testOtherBusinessID = "55555555-5555-4555-8555-5555555555ee"
	testOtherInvoiceID  = "66666666-6666-4666-8666-6666666666ff"
	testOtherCustomerID = "77777777-7777-4777-8777-7777777777aa"
)

func TestExecuteRejectsUntrustedStatusContentAndResponseContracts(t *testing.T) {
	invoice := safeInvoiceDocument(testInvoiceID, testBusinessID)
	otherTenantInvoice := safeInvoiceDocument(testInvoiceID, testOtherBusinessID)
	mismatchedInvoice := safeInvoiceDocument(testOtherInvoiceID, testBusinessID)
	customer := safeCustomerDocument(testCustomerID, testBusinessID)

	duplicateInvoiceRecord := strings.Replace(invoice, `"business_id"`, `"id":"`+testOtherInvoiceID+`","business_id"`, 1)
	nestedTenantMismatch := strings.Replace(invoice, `"buyer_snapshot":{"name":"Buyer"}`, `"buyer_snapshot":{"name":"Buyer","business_id":"`+testOtherBusinessID+`"}`, 1)
	missingInvoiceTenant := strings.Replace(invoice, `"business_id":"`+testBusinessID+`",`, "", 1)
	noncanonicalInvoiceID := strings.Replace(invoice, testInvoiceID, strings.ToUpper(testInvoiceID), 1)
	duplicateInvoiceList := `{"items":[` + invoice + `,` + invoice + `],"next_cursor":null}`
	duplicateCustomerList := `{"data":[` + customer + `,` + customer + `],"total":2,"page":1,"limit":10}`

	tooManyInvoices := make([]string, 0, defaultToolLimit+1)
	for index := 0; index < defaultToolLimit+1; index++ {
		id := fmt.Sprintf("80000000-0000-4000-8000-%012x", index)
		tooManyInvoices = append(tooManyInvoices, safeInvoiceDocument(id, testBusinessID))
	}

	gzipBody := new(bytes.Buffer)
	gzipWriter := gzip.NewWriter(gzipBody)
	_, _ = gzipWriter.Write([]byte(`{"items":[],"next_cursor":null}`))
	_ = gzipWriter.Close()

	tests := []struct {
		name            string
		tool            string
		arguments       json.RawMessage
		status          int
		contentType     string
		contentEncoding string
		body            []byte
		wantErr         error
		wantRefresh     bool
	}{
		{name: "unauthorized refresh", status: http.StatusUnauthorized, contentType: "text/plain", body: []byte("provider-body-canary"), wantErr: ErrAuthorizationRefreshRequired, wantRefresh: true},
		{name: "forbidden is not retried", status: http.StatusForbidden, contentType: "application/json", body: []byte(`{"error":"provider-body-canary"}`), wantErr: ErrAuthorizationUnavailable},
		{name: "not found", status: http.StatusNotFound, body: []byte("provider-body-canary"), wantErr: ErrToolNotFound},
		{name: "rate limited", status: http.StatusTooManyRequests, body: []byte("provider-body-canary"), wantErr: ErrToolUnavailable},
		{name: "provider failure", status: http.StatusInternalServerError, body: []byte("provider-body-canary"), wantErr: ErrToolUnavailable},
		{name: "manual redirect status", status: http.StatusFound, body: []byte("provider-body-canary"), wantErr: ErrToolUnavailable},
		{name: "missing content type", body: []byte(`{"items":[],"next_cursor":null}`), wantErr: ErrInvalidToolResponse},
		{name: "non JSON content type", contentType: "text/plain", body: []byte(`{"items":[],"next_cursor":null}`), wantErr: ErrInvalidToolResponse},
		{name: "malformed JSON", contentType: "application/json", body: []byte(`{"items":`), wantErr: ErrInvalidToolResponse},
		{name: "duplicate envelope key", contentType: "application/json", body: []byte(`{"items":[],"items":[],"next_cursor":null}`), wantErr: ErrInvalidToolResponse},
		{name: "duplicate nested record key", contentType: "application/json", body: []byte(`{"items":[` + duplicateInvoiceRecord + `],"next_cursor":null}`), wantErr: ErrInvalidToolResponse},
		{name: "extra invoice envelope key", contentType: "application/json", body: []byte(`{"items":[],"next_cursor":null,"extra":true}`), wantErr: ErrInvalidToolResponse},
		{name: "missing invoice envelope key", contentType: "application/json", body: []byte(`{"items":[]}`), wantErr: ErrInvalidToolResponse},
		{name: "oversized body", contentType: "application/json", body: bytes.Repeat([]byte(" "), maximumToolResponseBytes+1), wantErr: ErrToolResponseTooLarge},
		{name: "excessive JSON depth", contentType: "application/json", body: []byte(strings.Repeat("[", maximumJSONDepth+2) + "0" + strings.Repeat("]", maximumJSONDepth+2)), wantErr: ErrInvalidToolResponse},
		{name: "compressed response is not transparently expanded", contentType: "application/json", contentEncoding: "gzip", body: gzipBody.Bytes(), wantErr: ErrInvalidToolResponse},
		{name: "cross tenant invoice", tool: "get_invoice", arguments: json.RawMessage(`{"id":"` + testInvoiceID + `"}`), contentType: "application/json", body: []byte(otherTenantInvoice), wantErr: ErrInvalidToolResponse},
		{name: "missing invoice tenant", tool: "get_invoice", arguments: json.RawMessage(`{"id":"` + testInvoiceID + `"}`), contentType: "application/json", body: []byte(missingInvoiceTenant), wantErr: ErrInvalidToolResponse},
		{name: "noncanonical invoice id", tool: "get_invoice", arguments: json.RawMessage(`{"id":"` + testInvoiceID + `"}`), contentType: "application/json", body: []byte(noncanonicalInvoiceID), wantErr: ErrInvalidToolResponse},
		{name: "mismatched get invoice id", tool: "get_invoice", arguments: json.RawMessage(`{"id":"` + testInvoiceID + `"}`), contentType: "application/json", body: []byte(mismatchedInvoice), wantErr: ErrInvalidToolResponse},
		{name: "nested tenant mismatch", tool: "get_invoice", arguments: json.RawMessage(`{"id":"` + testInvoiceID + `"}`), contentType: "application/json", body: []byte(nestedTenantMismatch), wantErr: ErrInvalidToolResponse},
		{name: "duplicate projected invoice id", contentType: "application/json", body: []byte(duplicateInvoiceList), wantErr: ErrInvalidToolResponse},
		{name: "invoice count exceeds requested limit", contentType: "application/json", body: []byte(`{"items":[` + strings.Join(tooManyInvoices, ",") + `],"next_cursor":null}`), wantErr: ErrInvalidToolResponse},
		{name: "invalid next cursor", contentType: "application/json", body: []byte(`{"items":[],"next_cursor":"opaque-unicode-署名"}`), wantErr: ErrInvalidToolResponse},
		{name: "cross tenant next cursor", contentType: "application/json", body: []byte(`{"items":[],"next_cursor":"` + cursorTokenForTest(testOtherBusinessID, testInvoiceID) + `"}`), wantErr: ErrInvalidToolResponse},
		{name: "customer page mismatch", tool: "list_customers", arguments: json.RawMessage(`{}`), contentType: "application/json", body: []byte(`{"data":[],"total":0,"page":2,"limit":10}`), wantErr: ErrInvalidToolResponse},
		{name: "customer limit mismatch", tool: "list_customers", arguments: json.RawMessage(`{}`), contentType: "application/json", body: []byte(`{"data":[],"total":0,"page":1,"limit":20}`), wantErr: ErrInvalidToolResponse},
		{name: "customer total below records", tool: "list_customers", arguments: json.RawMessage(`{}`), contentType: "application/json", body: []byte(`{"data":[` + customer + `],"total":0,"page":1,"limit":10}`), wantErr: ErrInvalidToolResponse},
		{name: "duplicate projected customer id", tool: "list_customers", arguments: json.RawMessage(`{}`), contentType: "application/json", body: []byte(duplicateCustomerList), wantErr: ErrInvalidToolResponse},
		{name: "customer tenant mismatch", tool: "get_customer", arguments: json.RawMessage(`{"id":"` + testCustomerID + `"}`), contentType: "application/json", body: []byte(safeCustomerDocument(testCustomerID, testOtherBusinessID)), wantErr: ErrInvalidToolResponse},
		{name: "customer get id mismatch", tool: "get_customer", arguments: json.RawMessage(`{"id":"` + testCustomerID + `"}`), contentType: "application/json", body: []byte(safeCustomerDocument(testOtherCustomerID, testBusinessID)), wantErr: ErrInvalidToolResponse},
		{name: "extra customer envelope key", tool: "list_customers", arguments: json.RawMessage(`{}`), contentType: "application/json", body: []byte(`{"data":[],"total":0,"page":1,"limit":10,"extra":true}`), wantErr: ErrInvalidToolResponse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := test.status
			if status == 0 {
				status = http.StatusOK
			}
			server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Header.Get("Accept-Encoding") != "" {
					t.Errorf("request unexpectedly negotiated response compression: %q", request.Header.Get("Accept-Encoding"))
				}
				if test.contentType != "" {
					writer.Header().Set("Content-Type", test.contentType)
				}
				if test.contentEncoding != "" {
					writer.Header().Set("Content-Encoding", test.contentEncoding)
				}
				writer.WriteHeader(status)
				_, _ = writer.Write(test.body)
			}))
			defer server.Close()

			const authorizationCanary = "Bearer response-test-secret-canary"
			binding, err := NewSessionBinding(staticAuthorizationSource{value: authorizationCanary}, testBusinessID, "")
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

			tool := test.tool
			if tool == "" {
				tool = "list_invoices"
			}
			arguments := test.arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			result, executeErr := registry.Execute(context.Background(), tool, arguments)
			if result != nil || !errors.Is(executeErr, test.wantErr) {
				t.Fatalf("Execute() = (%s, %v), want (nil, %v)", result, executeErr, test.wantErr)
			}
			if stringsContainAny(executeErr.Error(), authorizationCanary, "provider-body-canary", origin, testOtherBusinessID) {
				t.Fatalf("error exposed credential, provider body, origin, or tenant detail: %v", executeErr)
			}
			marker, marked := executeErr.(interface{ AuthorizationRefreshRequired() bool })
			if test.wantRefresh != (marked && marker.AuthorizationRefreshRequired()) {
				t.Fatalf("refresh marker = %v, want %v; error=%T", marked, test.wantRefresh, executeErr)
			}
		})
	}
}

func safeInvoiceDocument(id, businessID string) string {
	return `{"id":"` + id + `","business_id":"` + businessID + `","invoice_no":"INV-SAFE",` +
		`"status":"issued","invoice_date":"2026-08-01T00:00:00Z","issued_at":"2026-08-02T00:00:00Z",` +
		`"due_date":"2026-08-31T00:00:00Z","currency":"INR","subtotal":100,"tax":18,"discount":2,` +
		`"total":116,"paid_amount":16,"balance_due":100,"buyer_snapshot":{"name":"Buyer"}}`
}

func safeCustomerDocument(id, businessID string) string {
	return `{"id":"` + id + `","business_id":"` + businessID + `","name":"Customer"}`
}

func cursorTokenForTest(businessID, id string) string {
	payload := `{"business_id":"` + businessID + `","created_at":"2026-08-01T00:00:00Z","id":"` + id + `"}`
	return "v1." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + strings.Repeat("A", 43)
}
