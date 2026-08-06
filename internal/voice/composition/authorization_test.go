package composition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"invoice-backend/internal/voice/webrtc"
)

func TestAuthorizationResolverReturnsOnlyValidatedIdentity(t *testing.T) {
	validator := &recordingTokenValidator{identity: TokenIdentity{
		UserID: "ap-south-1_phonepool:subject", BusinessID: "11111111-1111-4111-8111-111111111111", ClientID: "phone-client",
	}}
	resolver, err := NewAuthorizationResolver(validator)
	if err != nil {
		t.Fatalf("NewAuthorizationResolver() error = %v", err)
	}
	identity, err := resolver.Resolve(context.Background(), "Bearer validated-token")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := webrtc.TrustedIdentity{
		UserID: "ap-south-1_phonepool:subject", BusinessID: "11111111-1111-4111-8111-111111111111", ClientID: "phone-client",
	}
	if identity != want {
		t.Fatalf("identity = %#v, want %#v", identity, want)
	}
	if validator.authorization != "Bearer validated-token" {
		t.Fatalf("validator authorization = %q", validator.authorization)
	}
	if encoded, err := json.Marshal(resolver); err != nil || string(encoded) != "{}" {
		t.Fatalf("MarshalJSON() = (%s, %v), want ({}, nil)", encoded, err)
	}
	for _, rendered := range []string{fmt.Sprint(resolver), fmt.Sprintf("%#v", resolver)} {
		if strings.Contains(rendered, "validated-token") {
			t.Fatalf("resolver rendering leaked authorization: %s", rendered)
		}
	}
}

func TestAuthorizationResolverFailsClosedWithoutLeakingValidatorErrors(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		validator     TokenValidator
	}{
		{name: "missing bearer", authorization: "validated-token", validator: &recordingTokenValidator{}},
		{name: "space in token", authorization: "Bearer token value", validator: &recordingTokenValidator{}},
		{name: "validator error", authorization: "Bearer sensitive-token", validator: &recordingTokenValidator{err: errors.New("upstream-sensitive-canary")}},
		{name: "validator panic", authorization: "Bearer sensitive-token", validator: TokenValidatorFunc(func(context.Context, string) (TokenIdentity, error) { panic("panic-sensitive-canary") })},
		{name: "invalid claims", authorization: "Bearer sensitive-token", validator: &recordingTokenValidator{identity: TokenIdentity{UserID: "user", BusinessID: "", ClientID: "client"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver, err := NewAuthorizationResolver(test.validator)
			if err != nil {
				t.Fatalf("NewAuthorizationResolver() error = %v", err)
			}
			identity, err := resolver.Resolve(context.Background(), test.authorization)
			if identity != (webrtc.TrustedIdentity{}) || !errors.Is(err, webrtc.ErrSignalingUnauthorized) {
				t.Fatalf("Resolve() = (%#v, %v), want empty/ErrSignalingUnauthorized", identity, err)
			}
			for _, secret := range []string{"sensitive-token", "upstream-sensitive-canary", "panic-sensitive-canary"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("Resolve() error leaked %q: %v", secret, err)
				}
			}
		})
	}
}

func TestAuthorizationResolverHonorsCanceledContextBeforeValidation(t *testing.T) {
	validator := &recordingTokenValidator{identity: TokenIdentity{UserID: "user", BusinessID: "business", ClientID: "client"}}
	resolver, err := NewAuthorizationResolver(validator)
	if err != nil {
		t.Fatalf("NewAuthorizationResolver() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = resolver.Resolve(ctx, "Bearer canceled-token")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v, want context.Canceled", err)
	}
	if validator.calls != 0 {
		t.Fatalf("validator calls = %d, want 0", validator.calls)
	}
}

type recordingTokenValidator struct {
	identity      TokenIdentity
	err           error
	authorization string
	calls         int
}

func (validator *recordingTokenValidator) Validate(_ context.Context, authorization string) (TokenIdentity, error) {
	validator.calls++
	validator.authorization = authorization
	return validator.identity, validator.err
}
