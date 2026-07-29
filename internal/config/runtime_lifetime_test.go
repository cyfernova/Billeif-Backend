package config

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestSecretKindsForEntrypointAreExact(t *testing.T) {
	tests := map[string][]SecretKind{
		"http":           {SecretCredentialEncryption, SecretRazorpay, SecretLLM, SecretExa, SecretGSTLookup, SecretGSTProvider},
		"a2a-stream":     {SecretCredentialEncryption, SecretRazorpay, SecretLLM, SecretExa, SecretGSTLookup, SecretGSTProvider},
		"sqs-invoice":    {SecretCredentialEncryption},
		"sqs-payment":    nil,
		"sqs-gst":        {SecretCredentialEncryption, SecretGSTProvider},
		"sqs-bargaining": {SecretCredentialEncryption, SecretLLM, SecretExa},
		"ws":             nil,
		"voice-session":  {SecretDeepgram, SecretDeepSeek},
	}
	for entrypoint, want := range tests {
		t.Run(entrypoint, func(t *testing.T) {
			if got := SecretKindsForEntrypoint(entrypoint); !reflect.DeepEqual(got, want) {
				t.Fatalf("secret kinds = %#v, want %#v", got, want)
			}
		})
	}
}

func TestRuntimeResolverRetainsCachesAndRefreshesDatabaseCredentialsAfterTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	secrets := &fakeSecretsManager{values: []string{
		`{"username":"invoice_v1","password":"password_v1"}`,
		`{"username":"invoice_v2","password":"password_v2"}`,
	}}
	parameters := &fakeSSM{values: map[string]string{"/invoice/database/host": "db.internal"}}

	resolver, err := NewRuntimeResolver(RuntimeResolverOptions{
		Clients:           RuntimeResolvers{Secrets: secrets, SSM: parameters},
		SecretIdentifiers: []string{"rds-secret"},
		ParameterNames:    []string{"/invoice/database/host"},
		TTL:               time.Minute,
		Now:               func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new runtime resolver: %v", err)
	}
	cfg := &Config{
		Database: DatabaseConfig{Port: 5432, Name: "invoice", SSLMode: "require"},
		SSM:      SSMConfig{DatabaseHostParam: "/invoice/database/host"},
		Secrets:  SecretIdentifiers{Database: "rds-secret"},
	}

	first, err := resolver.Database(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first database access: %v", err)
	}
	if first.Host != "db.internal" || first.User != "invoice_v1" || first.Password != "password_v1" {
		t.Fatalf("unexpected initial database credentials: %#v", first)
	}

	now = now.Add(61 * time.Second)
	second, err := resolver.Database(context.Background(), cfg)
	if err != nil {
		t.Fatalf("database access after TTL: %v", err)
	}
	if second.User != "invoice_v2" || second.Password != "password_v2" {
		t.Fatalf("database credentials did not rotate: %#v", second)
	}
	if got := secrets.calls.Load(); got != 2 {
		t.Fatalf("expected retained cache to fetch once per TTL window, got %d calls", got)
	}
}

func TestRuntimeResolverGSTProviderScopeMapsEveryCredentialField(t *testing.T) {
	secrets := &fakeSecretsManager{values: []string{`{
		"client_id":"gst-client",
		"client_secret":"gst-client-secret",
		"username":"gst-user",
		"password":"gst-password",
		"api_token":"gst-token"
	}`}}
	resolver, err := NewRuntimeResolver(RuntimeResolverOptions{
		Clients:           RuntimeResolvers{Secrets: secrets},
		SecretIdentifiers: []string{"gst-provider-secret"},
		TTL:               time.Minute,
	})
	if err != nil {
		t.Fatalf("new runtime resolver: %v", err)
	}
	cfg := &Config{Secrets: SecretIdentifiers{
		GSTProvider: "gst-provider-secret",
		LLM:         "must-not-be-fetched",
	}}

	if err := resolver.Resolve(context.Background(), cfg, []SecretKind{SecretGSTProvider}); err != nil {
		t.Fatalf("resolve GST provider: %v", err)
	}
	if cfg.GST.ClientID != "gst-client" ||
		cfg.GST.ClientSecret != "gst-client-secret" ||
		cfg.GST.Username != "gst-user" ||
		cfg.GST.Password != "gst-password" ||
		cfg.GST.APIToken != "gst-token" {
		t.Fatalf("GST provider fields were not completely mapped: %#v", cfg.GST)
	}
	if got := secrets.calls.Load(); got != 1 {
		t.Fatalf("expected only GST provider secret to be fetched, got %d calls", got)
	}
}
