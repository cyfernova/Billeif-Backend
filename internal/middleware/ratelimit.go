package middleware

import (
	"context"
	"net/http"
	"sync"
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

type RedisLimiter struct {
	client   *redis.Client
	rate     int
	interval time.Duration
}

func NewRedisLimiter(client *redis.Client, rate int, interval time.Duration) *RedisLimiter {
	return &RedisLimiter{
		client:   client,
		rate:     rate,
		interval: interval,
	}
}

func (l *RedisLimiter) Allow(key string) bool {
	ctx := context.Background()
	redisKey := "ratelimit:" + key

	count, err := l.client.Incr(ctx, redisKey).Result()
	if err != nil {
		return true
	}

	if count == 1 {
		l.client.Expire(ctx, redisKey, l.interval)
	}

	return count <= int64(l.rate)
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
