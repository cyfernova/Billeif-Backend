package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// RateLimiter implements a simple token bucket rate limiter
type RateLimiter struct {
	mu       sync.Mutex
	limits   map[string]*userLimit
	interval time.Duration
	maxHits  int
}

type userLimit struct {
	hits      int
	resetTime time.Time
}

// NewRateLimiter creates a new rate limiter
// interval: time window (e.g., time.Hour)
// maxHits: maximum requests per interval
func NewRateLimiter(interval time.Duration, maxHits int) *RateLimiter {
	rl := &RateLimiter{
		limits:   make(map[string]*userLimit),
		interval: interval,
		maxHits:  maxHits,
	}

	// Start cleanup goroutine to remove old entries
	go rl.cleanup()

	return rl
}

// isAllowed checks if a user has exceeded rate limit
func (rl *RateLimiter) isAllowed(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	limit, exists := rl.limits[userID]

	// Create new limit if doesn't exist or window has passed
	if !exists || now.After(limit.resetTime) {
		rl.limits[userID] = &userLimit{
			hits:      1,
			resetTime: now.Add(rl.interval),
		}
		return true
	}

	// Check if hit limit
	if limit.hits >= rl.maxHits {
		return false
	}

	limit.hits++
	return true
}

// cleanup removes old entries periodically
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for userID, limit := range rl.limits {
			if now.After(limit.resetTime) {
				delete(rl.limits, userID)
			}
		}
		rl.mu.Unlock()
	}
}

// AgentCreationRateLimit returns middleware that limits agent creation per IP address
// 10 agents per hour per IP (works without auth)
func AgentCreationRateLimit() gin.HandlerFunc {
	limiter := NewRateLimiter(time.Hour, 10)

	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context()).Named("rate_limit")
		clientIP := c.ClientIP()
		if clientIP == "" {
			clientIP = "unknown"
		}

		if !limiter.isAllowed(clientIP) {
			log.Warn("agent creation rate limit exceeded", "client_ip", clientIP)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "agent creation rate limit exceeded",
				"message": fmt.Sprintf("maximum 10 agents per hour. Please try again in an hour"),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ShoppingIntentRateLimit limits shopping intent processing per user
// 100 requests per minute per user
func ShoppingIntentRateLimit() gin.HandlerFunc {
	limiter := NewRateLimiter(time.Minute, 100)

	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context()).Named("rate_limit")
		userID := c.GetString("user_id")
		if userID == "" {
			log.Warn("shopping intent rate limit check failed: unauthenticated")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			c.Abort()
			return
		}

		if !limiter.isAllowed(userID) {
			log.Warn("shopping intent rate limit exceeded", "user_id", userID)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "too many requests",
				"message": "you have exceeded the request limit. please try again shortly",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// PaymentRateLimit limits payment processing per user
// 20 payments per minute per user (prevent spam)
func PaymentRateLimit() gin.HandlerFunc {
	limiter := NewRateLimiter(time.Minute, 20)

	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context()).Named("rate_limit")
		userID := c.GetString("user_id")
		if userID == "" {
			log.Warn("payment rate limit check failed: unauthenticated")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			c.Abort()
			return
		}

		if !limiter.isAllowed(userID) {
			log.Warn("payment rate limit exceeded", "user_id", userID)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "payment processing rate limit exceeded",
				"message": "you are processing payments too quickly. please wait a moment before trying again",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
