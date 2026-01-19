package middleware

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

type RateLimiter interface {
	Allow(key string) bool
}

type InMemoryLimiter struct {
	mu       sync.Mutex
	tokens   map[string]int
	lastTime map[string]time.Time
	rate     int
	interval time.Duration
}

func NewInMemoryLimiter(rate int, interval time.Duration) *InMemoryLimiter {
	return &InMemoryLimiter{
		tokens:   make(map[string]int),
		lastTime: make(map[string]time.Time),
		rate:     rate,
		interval: interval,
	}
}

func (l *InMemoryLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	last, exists := l.lastTime[key]
	if !exists || now.Sub(last) >= l.interval {
		l.tokens[key] = l.rate
		l.lastTime[key] = now
	}

	if l.tokens[key] > 0 {
		l.tokens[key]--
		return true
	}
	return false
}

// CircuitState represents the state of the circuit breaker
type CircuitState int32

const (
	CircuitClosed CircuitState = iota // Normal operation
	CircuitOpen                       // Failing, reject requests
	CircuitHalfOpen                   // Testing if service recovered
)

// RedisLimiter implements rate limiting with Redis and a circuit breaker pattern
type RedisLimiter struct {
	client   *redis.Client
	rate     int
	interval time.Duration
	fallback *InMemoryLimiter

	// Circuit breaker state
	state           atomic.Int32
	failures        atomic.Int32
	lastFailure     atomic.Int64
	failureThreshold int
	recoveryTimeout  time.Duration
}

func NewRedisLimiter(client *redis.Client, rate int, interval time.Duration) *RedisLimiter {
	limiter := &RedisLimiter{
		client:           client,
		rate:             rate,
		interval:         interval,
		fallback:         NewInMemoryLimiter(rate, interval),
		failureThreshold: 5,                // Open circuit after 5 consecutive failures
		recoveryTimeout:  30 * time.Second, // Try to recover after 30 seconds
	}
	limiter.state.Store(int32(CircuitClosed))
	return limiter
}

func (l *RedisLimiter) Allow(key string) bool {
	state := CircuitState(l.state.Load())

	switch state {
	case CircuitOpen:
		// Check if recovery timeout has passed
		lastFail := time.Unix(0, l.lastFailure.Load())
		if time.Since(lastFail) > l.recoveryTimeout {
			// Transition to half-open state
			l.state.CompareAndSwap(int32(CircuitOpen), int32(CircuitHalfOpen))
			return l.tryRedis(key)
		}
		// Circuit is open - use fallback (fail-closed with in-memory limiting)
		return l.fallback.Allow(key)

	case CircuitHalfOpen:
		// Test if Redis is working again
		return l.tryRedis(key)

	default: // CircuitClosed
		return l.tryRedis(key)
	}
}

func (l *RedisLimiter) tryRedis(key string) bool {
	ctx := context.Background()
	redisKey := "ratelimit:" + key

	count, err := l.client.Incr(ctx, redisKey).Result()
	if err != nil {
		l.recordFailure()
		// On Redis failure, use in-memory fallback (fail-closed with backup)
		return l.fallback.Allow(key)
	}

	// Success - reset failure count and close circuit
	l.failures.Store(0)
	l.state.Store(int32(CircuitClosed))

	if count == 1 {
		l.client.Expire(ctx, redisKey, l.interval)
	}

	return count <= int64(l.rate)
}

func (l *RedisLimiter) recordFailure() {
	failures := l.failures.Add(1)
	l.lastFailure.Store(time.Now().UnixNano())

	if failures >= int32(l.failureThreshold) {
		l.state.Store(int32(CircuitOpen))
	}
}

func RateLimit(limiter RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		if userID := GetUserID(c); userID != "" {
			key = userID
		}

		if !limiter.Allow(key) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}
		c.Next()
	}
}
