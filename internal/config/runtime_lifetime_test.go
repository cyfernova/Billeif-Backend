package config

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

func TestSecretIdentifierValuesIncludesSarvamExactlyOnce(t *testing.T) {
	cfg := &Config{Secrets: SecretIdentifiers{
		Database:             "database-secret",
		CredentialEncryption: "credential-secret",
		Razorpay:             "razorpay-secret",
		LLM:                  "llm-secret",
		Exa:                  "exa-secret",
		GSTLookup:            "gst-lookup-secret",
		GSTProvider:          "gst-provider-secret",
		DeepSeek:             "deepseek-secret",
		Sarvam:               "sarvam-secret",
		InvoiceCursorHMAC:    "cursor-secret",
	}}

	got := cfg.SecretIdentifierValues()

	count := 0
	for _, identifier := range got {
		if identifier == "sarvam-secret" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Sarvam secret identifier count = %d in %#v, want exactly 1", count, got)
	}
}

func TestSecretKindsForEntrypointAreExact(t *testing.T) {
	tests := map[string][]SecretKind{
		"http":           {SecretCredentialEncryption, SecretRazorpay, SecretLLM, SecretExa, SecretGSTLookup, SecretGSTProvider, SecretDeepSeek},
		"a2a-stream":     {SecretCredentialEncryption, SecretRazorpay, SecretLLM, SecretExa, SecretGSTLookup, SecretGSTProvider, SecretDeepSeek},
		"sqs-invoice":    {SecretCredentialEncryption},
		"sqs-gst":        {SecretCredentialEncryption, SecretGSTProvider},
		"sqs-bargaining": {SecretLLM, SecretExa},
		"outbox":         nil,
		"ws":             nil,
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

type routedSecretsClient struct {
	mu     sync.Mutex
	values map[string][]string
	calls  map[string]int
}

func (f *routedSecretsClient) GetSecretValue(_ context.Context, input *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	identifier := aws.ToString(input.SecretId)
	f.calls[identifier]++
	values := f.values[identifier]
	value := values[0]
	if len(values) > 1 {
		f.values[identifier] = values[1:]
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(value)}, nil
}

func TestRuntimeResolverResolvesOneProviderLazilyAndRefreshesItAfterTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client := &routedSecretsClient{
		values: map[string][]string{
			"llm-secret": {`{"api_key":"llm-v1"}`, `{"api_key":"llm-v2"}`},
			"exa-secret": {`{"api_key":"exa-key"}`},
		},
		calls: map[string]int{},
	}
	resolver, err := NewRuntimeResolver(RuntimeResolverOptions{
		Clients:           RuntimeResolvers{Secrets: client},
		SecretIdentifiers: []string{"llm-secret", "exa-secret"},
		TTL:               time.Minute,
		Now:               func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new runtime resolver: %v", err)
	}
	cfg := &Config{
		Environment: "production",
		Secrets: SecretIdentifiers{
			LLM: "llm-secret",
			Exa: "exa-secret",
		},
	}
	if len(client.calls) != 0 {
		t.Fatalf("resolver construction fetched providers: %#v", client.calls)
	}

	first, err := resolver.ResolveProvider(context.Background(), cfg, SecretLLM)
	if err != nil {
		t.Fatalf("first LLM use: %v", err)
	}
	if first.LLM.APIKey != "llm-v1" {
		t.Fatalf("first LLM key = %q", first.LLM.APIKey)
	}
	if cfg.LLM.APIKey != "" {
		t.Fatal("provider resolution mutated shared bootstrap config")
	}
	if _, err := resolver.ResolveProvider(context.Background(), cfg, SecretLLM); err != nil {
		t.Fatalf("warm LLM reuse: %v", err)
	}
	if client.calls["llm-secret"] != 1 || client.calls["exa-secret"] != 0 {
		t.Fatalf("warm or unrelated provider fetch count = %#v", client.calls)
	}

	now = now.Add(61 * time.Second)
	refreshed, err := resolver.ResolveProvider(context.Background(), cfg, SecretLLM)
	if err != nil {
		t.Fatalf("LLM use after TTL: %v", err)
	}
	if refreshed.LLM.APIKey != "llm-v2" {
		t.Fatalf("refreshed LLM key = %q", refreshed.LLM.APIKey)
	}
	if client.calls["llm-secret"] != 2 || client.calls["exa-secret"] != 0 {
		t.Fatalf("TTL or unrelated provider fetch count = %#v", client.calls)
	}
}
