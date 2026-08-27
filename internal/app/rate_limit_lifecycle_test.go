package app

import (
	"context"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/ratelimit"
)

type closingRateLimiter struct {
	closeCalls int
}

func (*closingRateLimiter) Decide(context.Context, []ratelimit.Bucket) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: true}, nil
}

func (limiter *closingRateLimiter) Close() error {
	limiter.closeCalls++
	return nil
}

func TestRuntimeCloseClosesInjectedDistributedRateLimiter(t *testing.T) {
	limiter := &closingRateLimiter{}
	runtime := &Runtime{RateLimiter: limiter}

	runtime.Close()

	if limiter.closeCalls != 1 {
		t.Fatalf("distributed limiter Close() calls = %d, want 1", limiter.closeCalls)
	}
}

func TestInitializeRateLimiterNeverBypassesMissingProductionHTTPBackend(t *testing.T) {
	backend, err := initializeRateLimiter(context.Background(), &config.Config{Environment: "production"}, config.ProfileHTTP, nil)
	if backend != nil {
		_ = backend.Close()
	}
	if err == nil {
		t.Fatal("initializeRateLimiter() accepted a production HTTP runtime without the distributed backend")
	}
}
