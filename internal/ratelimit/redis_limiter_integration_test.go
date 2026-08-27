//go:build integration

package ratelimit

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRedisLimiterSharesQuotaAcrossIndependentClients(t *testing.T) {
	addr := os.Getenv("RATE_LIMIT_VALKEY_ADDR")
	if addr == "" {
		t.Skip("RATE_LIMIT_VALKEY_ADDR is required for the real Valkey integration test")
	}
	ctx := context.Background()
	clients := make([]*RedisLimiter, 4)
	for index := range clients {
		client, err := NewRedisLimiter(ctx, RedisOptions{Address: addr, DecisionTimeout: time.Second})
		if err != nil {
			t.Fatalf("NewRedisLimiter(client %d) error = %v", index+1, err)
		}
		clients[index] = client
		t.Cleanup(func() { _ = client.Close() })
	}

	buckets := []Bucket{{
		Namespace: "integration/concurrent/ip",
		Identity:  fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano()),
		Limit:     25,
		Window:    30 * time.Second,
	}}
	var allowed atomic.Int64
	var failures atomic.Int64
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			decision, err := clients[index%len(clients)].Decide(ctx, buckets)
			if err != nil {
				failures.Add(1)
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			}
		}(index)
	}
	wait.Wait()

	if failures.Load() != 0 {
		t.Fatalf("backend decision failures = %d, want 0", failures.Load())
	}
	if allowed.Load() != 25 {
		t.Fatalf("allowed decisions = %d, want exactly 25 across all clients", allowed.Load())
	}
}

func TestRedisLimiterDoesNotPartiallyDebitDeniedMultiBucketDecision(t *testing.T) {
	addr := os.Getenv("RATE_LIMIT_VALKEY_ADDR")
	if addr == "" {
		t.Skip("RATE_LIMIT_VALKEY_ADDR is required for the real Valkey integration test")
	}
	ctx := context.Background()
	first, err := NewRedisLimiter(ctx, RedisOptions{Address: addr, DecisionTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewRedisLimiter(first) error = %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := NewRedisLimiter(ctx, RedisOptions{Address: addr, DecisionTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewRedisLimiter(second) error = %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	unique := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	target := Bucket{Namespace: "integration/atomic/target", Identity: unique + "-target", Limit: 1, Window: 30 * time.Second}
	ipA := Bucket{Namespace: "integration/atomic/ip", Identity: unique + "-ip-a", Limit: 1, Window: 30 * time.Second}
	ipB := Bucket{Namespace: "integration/atomic/ip", Identity: unique + "-ip-b", Limit: 1, Window: 30 * time.Second}
	newTarget := target
	newTarget.Identity = unique + "-new-target"

	decision, err := first.Decide(ctx, []Bucket{ipA, target})
	if err != nil || !decision.Allowed {
		t.Fatalf("initial decision = %#v, %v", decision, err)
	}
	decision, err = second.Decide(ctx, []Bucket{ipB, target})
	if err != nil || decision.Allowed {
		t.Fatalf("exhausted target decision = %#v, %v, want denied", decision, err)
	}
	decision, err = first.Decide(ctx, []Bucket{ipB, newTarget})
	if err != nil || !decision.Allowed {
		t.Fatalf("denied transaction partially debited spare IP bucket: %#v, %v", decision, err)
	}
}
