package ratelimit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestBucketKeyKeepsAtomicDecisionInOneSlotAndHidesIdentity(t *testing.T) {
	key, err := bucketKey(Bucket{
		Namespace: "auth-login/ip",
		Identity:  "person@example.com",
		Limit:     5,
		Window:    time.Minute,
	})
	if err != nil {
		t.Fatalf("bucketKey() error = %v", err)
	}

	if !strings.HasPrefix(key, "{billeif-rate-limit}:auth-login:ip:") {
		t.Fatalf("bucketKey() = %q, want stable cluster hash tag and namespace", key)
	}
	if strings.Contains(key, "person@example.com") {
		t.Fatalf("bucketKey() exposed raw identity: %q", key)
	}
	if got := len(strings.TrimPrefix(key, "{billeif-rate-limit}:auth-login:ip:")); got != 64 {
		t.Fatalf("bucket identity digest length = %d, want 64", got)
	}
}

func TestBucketKeyRejectsInvalidPolicy(t *testing.T) {
	tests := []Bucket{
		{Identity: "ip", Limit: 1, Window: time.Second},
		{Namespace: "auth-login/ip", Limit: 1, Window: time.Second},
		{Namespace: "auth-login/ip", Identity: "ip", Window: time.Second},
		{Namespace: "auth-login/ip", Identity: "ip", Limit: 1},
		{Namespace: "auth login/ip", Identity: "ip", Limit: 1, Window: time.Second},
	}

	for _, bucket := range tests {
		if _, err := bucketKey(bucket); err == nil {
			t.Fatalf("bucketKey(%#v) error = nil, want validation failure", bucket)
		}
	}
}

func TestIAMCredentialsProviderSignsShortLivedElastiCacheToken(t *testing.T) {
	provider, err := NewIAMCredentialsProvider(IAMCredentialsOptions{
		Region:    "ap-south-1",
		UserID:    "billeif-http",
		CacheName: "billeif-production-rate-limit",
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     "AKIDEXAMPLE",
				SecretAccessKey: "not-a-real-secret",
				SessionToken:    "not-a-real-session-token",
			}, nil
		}),
		Now: func() time.Time {
			return time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewIAMCredentialsProvider() error = %v", err)
	}

	username, token, err := provider(context.Background())
	if err != nil {
		t.Fatalf("provider() error = %v", err)
	}
	if username != "billeif-http" {
		t.Fatalf("provider() username = %q, want billeif-http", username)
	}
	for _, want := range []string{
		"billeif-production-rate-limit/?",
		"Action=connect",
		"User=billeif-http",
		"ResourceType=ServerlessCache",
		"X-Amz-Expires=900",
		"X-Amz-Signature=",
	} {
		if !strings.Contains(token, want) {
			t.Fatalf("provider() token is missing %q", want)
		}
	}
	if strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") {
		t.Fatalf("provider() token must omit URL scheme, got %q", token)
	}
}
