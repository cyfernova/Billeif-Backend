package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testBusinessID = "11111111-1111-4111-8111-1111111111aa"
	testBranchID   = "22222222-2222-4222-8222-2222222222bb"
	testBearer     = "Bearer header.payload.signature"
)

func TestRegistryDefinitionsAreExactlyFourStrictToolsAndDefensive(t *testing.T) {
	binding, err := NewSessionBinding(staticAuthorizationSource{value: testBearer}, testBusinessID, testBranchID)
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin:              "https://example.com",
		Timeout:             time.Second,
		Resolver:            panicResolver{},
		Dialer:              panicDialer{},
		EnableCustomerTools: true,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	want := []struct {
		name       string
		parameters string
	}{
		{
			name: "list_invoices",
			parameters: `{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":20,"default":10},` +
				`"cursor":{"type":"string","minLength":1,"maxLength":1024,"pattern":"^v1\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$"}},"additionalProperties":false}`,
		},
		{
			name:       "get_invoice",
			parameters: `{"type":"object","properties":{"id":{"type":"string","format":"uuid"}},"required":["id"],"additionalProperties":false}`,
		},
		{
			name: "list_customers",
			parameters: `{"type":"object","properties":{"page":{"type":"integer","minimum":1,"maximum":1000,"default":1},` +
				`"limit":{"type":"integer","minimum":1,"maximum":20,"default":10}},"additionalProperties":false}`,
		},
		{
			name:       "get_customer",
			parameters: `{"type":"object","properties":{"id":{"type":"string","format":"uuid"}},"required":["id"],"additionalProperties":false}`,
		},
	}

	definitions := registry.Definitions()
	if len(definitions) != len(want) {
		t.Fatalf("Definitions() count = %d, want %d", len(definitions), len(want))
	}
	for index, expected := range want {
		definition := definitions[index]
		if definition.Name != expected.name {
			t.Fatalf("Definitions()[%d].Name = %q, want %q", index, definition.Name, expected.name)
		}
		if definition.Description == "" {
			t.Fatalf("Definitions()[%d].Description is empty", index)
		}
		var gotSchema, wantSchema any
		if err := json.Unmarshal(definition.Parameters, &gotSchema); err != nil {
			t.Fatalf("Definitions()[%d].Parameters invalid JSON: %v", index, err)
		}
		if err := json.Unmarshal([]byte(expected.parameters), &wantSchema); err != nil {
			t.Fatalf("invalid hand-written expected schema: %v", err)
		}
		if !reflect.DeepEqual(gotSchema, wantSchema) {
			t.Fatalf("Definitions()[%d].Parameters = %s, want %s", index, definition.Parameters, expected.parameters)
		}
	}

	definitions[0].Name = "mutated"
	definitions[0].Parameters[0] = '['
	again := registry.Definitions()
	if again[0].Name != "list_invoices" || !json.Valid(again[0].Parameters) {
		t.Fatalf("Definitions() shares mutable state: %#v", again[0])
	}
}

func TestSessionBindingRequiresCanonicalScopeAndNeverExposesOrOwnsAuthorization(t *testing.T) {
	token := "Bearer do-not-expose-this-token"
	source := &closableAuthorizationSource{value: token}
	binding, err := NewSessionBinding(source, testBusinessID, testBranchID)
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}

	formatted := fmt.Sprintf("%v|%+v|%#v", binding, binding, binding)
	document, err := json.Marshal(binding)
	if err != nil {
		t.Fatalf("json.Marshal(binding) error = %v", err)
	}
	for _, secret := range []string{token, testBusinessID, testBranchID} {
		if strings.Contains(formatted, secret) || strings.Contains(string(document), secret) {
			t.Fatalf("binding diagnostics exposed scoped credential material: %q / %s", formatted, document)
		}
	}
	if string(document) != "{}" {
		t.Fatalf("json.Marshal(binding) = %s, want {}", document)
	}

	if err := binding.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := binding.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := source.closeCalls.Load(); got != 0 {
		t.Fatalf("binding closed signaling-owned AuthorizationSource %d times", got)
	}
}

func TestNewSessionBindingRejectsTypedNilAndNoncanonicalScope(t *testing.T) {
	var typedNil *closableAuthorizationSource
	tests := []struct {
		name       string
		source     AuthorizationSource
		businessID string
		branchID   string
		wantErr    error
	}{
		{name: "nil source", businessID: testBusinessID, branchID: testBranchID, wantErr: ErrAuthorizationSourceRequired},
		{name: "typed nil source", source: typedNil, businessID: testBusinessID, branchID: testBranchID, wantErr: ErrAuthorizationSourceRequired},
		{name: "missing business", source: staticAuthorizationSource{value: testBearer}, branchID: testBranchID, wantErr: ErrInvalidSessionScope},
		{name: "noncanonical business", source: staticAuthorizationSource{value: testBearer}, businessID: strings.ToUpper(testBusinessID), branchID: testBranchID, wantErr: ErrInvalidSessionScope},
		{name: "invalid branch", source: staticAuthorizationSource{value: testBearer}, businessID: testBusinessID, branchID: "../other", wantErr: ErrInvalidSessionScope},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding, err := NewSessionBinding(test.source, test.businessID, test.branchID)
			if binding != nil || !errors.Is(err, test.wantErr) {
				t.Fatalf("NewSessionBinding() = (%v, %v), want (nil, %v)", binding, err, test.wantErr)
			}
			for _, value := range []string{test.businessID, test.branchID} {
				if value != "" && strings.Contains(err.Error(), value) {
					t.Fatalf("constructor error exposed rejected scope %q: %v", value, err)
				}
			}
		})
	}

	if binding, err := NewSessionBinding(staticAuthorizationSource{value: testBearer}, testBusinessID, ""); err != nil || binding == nil {
		t.Fatalf("NewSessionBinding(optional branch) = (%v, %v), want success", binding, err)
	} else {
		_ = binding.Close()
	}
}

type staticAuthorizationSource struct {
	value string
	err   error
}

func (source staticAuthorizationSource) Authorization(context.Context) (string, error) {
	return source.value, source.err
}

type closableAuthorizationSource struct {
	value      string
	closeCalls atomic.Int32
}

func (source *closableAuthorizationSource) Authorization(context.Context) (string, error) {
	return source.value, nil
}

func (source *closableAuthorizationSource) Close() error {
	source.closeCalls.Add(1)
	return nil
}

type panicResolver struct{}

func (panicResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	panic("unexpected network resolution")
}

type panicDialer struct{}

func (panicDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	panic("unexpected network dial")
}
