package config

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

const sentinelSecret = "sentinel-secret-must-not-leak"

type fakeSecretsManager struct {
	calls  atomic.Int32
	mu     sync.Mutex
	values []string
	err    error
}

func (f *fakeSecretsManager) GetSecretValue(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	value := f.values[0]
	if len(f.values) > 1 {
		f.values = f.values[1:]
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(value)}, nil
}

func TestSecretResolverCachesConcurrentReads(t *testing.T) {
	client := &fakeSecretsManager{values: []string{`{"password":"` + sentinelSecret + `"}`}}
	resolver, err := NewSecretResolver(client, []string{"db-secret-arn"}, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	const readers = 32
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := resolver.JSONField(context.Background(), "db-secret-arn", "password")
			if err != nil {
				errs <- err
				return
			}
			if got != sentinelSecret {
				errs <- errors.New("unexpected resolved value")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("expected one upstream fetch, got %d", got)
	}
}

func TestSecretResolverRefreshesAfterTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client := &fakeSecretsManager{values: []string{"first", "second"}}
	resolver, err := NewSecretResolver(client, []string{"provider-secret"}, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	first, err := resolver.String(context.Background(), "provider-secret")
	if err != nil || first != "first" {
		t.Fatalf("first read = %q, %v", first, err)
	}
	now = now.Add(59 * time.Second)
	cached, err := resolver.String(context.Background(), "provider-secret")
	if err != nil || cached != "first" {
		t.Fatalf("cached read = %q, %v", cached, err)
	}
	now = now.Add(2 * time.Second)
	refreshed, err := resolver.String(context.Background(), "provider-secret")
	if err != nil || refreshed != "second" {
		t.Fatalf("refreshed read = %q, %v", refreshed, err)
	}
	if got := client.calls.Load(); got != 2 {
		t.Fatalf("expected two upstream fetches, got %d", got)
	}
}

func TestSecretResolverRejectsUnknownIdentifiersAndMissingFields(t *testing.T) {
	client := &fakeSecretsManager{values: []string{`{"username":"invoice"}`}}
	resolver, err := NewSecretResolver(client, []string{"db-secret"}, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	if _, err := resolver.String(context.Background(), "not-configured"); !IsConfigurationError(err) {
		t.Fatalf("expected typed configuration error for identifier, got %T: %v", err, err)
	}
	if _, err := resolver.JSONField(context.Background(), "db-secret", "password"); !IsConfigurationError(err) {
		t.Fatalf("expected typed configuration error for field, got %T: %v", err, err)
	}
}

func TestSecretResolverErrorsDoNotLeakSecretMaterial(t *testing.T) {
	cases := []struct {
		name   string
		client *fakeSecretsManager
		field  string
	}{
		{
			name:   "upstream",
			client: &fakeSecretsManager{err: errors.New("provider failed with " + sentinelSecret)},
		},
		{
			name:   "invalid JSON",
			client: &fakeSecretsManager{values: []string{`{"password":"` + sentinelSecret + `"`}},
			field:  "password",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver, err := NewSecretResolver(tc.client, []string{"configured-secret"}, time.Minute, time.Now)
			if err != nil {
				t.Fatalf("new resolver: %v", err)
			}
			var resolveErr error
			if tc.field == "" {
				_, resolveErr = resolver.String(context.Background(), "configured-secret")
			} else {
				_, resolveErr = resolver.JSONField(context.Background(), "configured-secret", tc.field)
			}
			if resolveErr == nil {
				t.Fatal("expected resolution error")
			}
			if containsSecret(resolveErr.Error()) {
				t.Fatalf("error leaked secret material: %v", resolveErr)
			}
		})
	}
}

func containsSecret(value string) bool {
	return value == sentinelSecret || len(value) >= len(sentinelSecret) && findSubstring(value, sentinelSecret)
}

func findSubstring(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
