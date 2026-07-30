package config

import (
	"context"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/invoicecursor"

	"github.com/google/uuid"
)

func TestProductionHTTPRequiresInvoiceCursorSecretButA2ADoesNot(t *testing.T) {
	httpConfig := validProductionHTTPConfigForCursorTest()
	httpConfig.Secrets.InvoiceCursorHMAC = ""
	err := ValidateForProfile(httpConfig, ProfileHTTP)
	if err == nil || !strings.Contains(err.Error(), "INVOICE_CURSOR_HMAC_SECRET_ARN") {
		t.Fatalf("HTTP validation error = %v, want cursor secret requirement", err)
	}

	a2aConfig := validProductionHTTPConfigForCursorTest()
	a2aConfig.Secrets.InvoiceCursorHMAC = ""
	if err := ValidateForProfile(a2aConfig, ProfileA2A); err != nil {
		t.Fatalf("A2A unexpectedly required cursor secret: %v", err)
	}
}

func TestRuntimeResolverConstructsInvoiceCursorCodecFromScalarWithoutRetainingKey(t *testing.T) {
	const identifier = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:billeif-cursor"
	const key = "0123456789abcdef0123456789abcdef"
	client := &fakeSecretsManager{values: []string{key}}
	resolver, err := NewRuntimeResolver(RuntimeResolverOptions{
		Clients:           RuntimeResolvers{Secrets: client},
		SecretIdentifiers: []string{identifier},
		TTL:               time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRuntimeResolver() error = %v", err)
	}

	codec, err := resolver.InvoiceCursorCodec(context.Background(), identifier)
	if err != nil {
		t.Fatalf("InvoiceCursorCodec() error = %v", err)
	}
	businessID := uuid.NewString()
	token, err := codec.Encode(businessID, invoicecursor.Position{
		CreatedAt: time.Date(2026, time.July, 30, 1, 2, 3, 4, time.UTC),
		ID:        uuid.NewString(),
	})
	if err != nil || token == "" {
		t.Fatalf("resolved codec Encode() token=%q error=%v", token, err)
	}
	if client.calls.Load() != 1 {
		t.Fatalf("secret resolutions = %d, want 1", client.calls.Load())
	}
}

func TestRuntimeResolverInvoiceCursorErrorsDoNotLeakSecretMaterial(t *testing.T) {
	const identifier = "billeif-cursor-secret"
	client := &fakeSecretsManager{err: errWithSecretMaterial{}}
	resolver, err := NewRuntimeResolver(RuntimeResolverOptions{
		Clients:           RuntimeResolvers{Secrets: client},
		SecretIdentifiers: []string{identifier},
		TTL:               time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRuntimeResolver() error = %v", err)
	}
	_, err = resolver.InvoiceCursorCodec(context.Background(), identifier)
	if err == nil || strings.Contains(err.Error(), sentinelSecret) {
		t.Fatalf("InvoiceCursorCodec() error = %v, want generic non-leaking error", err)
	}
}

type errWithSecretMaterial struct{}

func (errWithSecretMaterial) Error() string { return "provider failed with " + sentinelSecret }

func validProductionHTTPConfigForCursorTest() *Config {
	cfg := validConfigForTest()
	cfg.Environment = "production"
	cfg.Secrets.InvoiceCursorHMAC = "cursor-secret"
	cfg.LLM.ExaAPIKey = "exa-key"
	cfg.GSTLookup.APIKey = "gst-lookup-key"
	cfg.GST.APIToken = "gst-provider-token"
	cfg.Deepgram.APIKey = "deepgram-key"
	cfg.VoiceRealtime.DeepSeekAPIKey = "deepseek-key"
	return cfg
}
