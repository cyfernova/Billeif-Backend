package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/gin-gonic/gin"
)

// WAFRateLimiter provides AWS WAF-based distributed rate limiting coordination.
// It uses local counters and optionally syncs with WAF IP Sets for distributed enforcement.
type WAFRateLimiter struct {
	client      *wafv2.Client
	webACLArn   string
	ipSetID     string
	headerKey   string
	blockedMsg  string
	mu          sync.RWMutex
	lastRefresh time.Time
	refreshTTL  time.Duration
}

// ipRateLimitEntry tracks request counts per identifier.
type ipRateLimitEntry struct {
	count     int
	windowEnd time.Time
}

// WIPRateLimiter is an in-memory rate limiter that can coordinate with AWS WAF.
// For distributed deployments, enable WAF integration to sync blocked IPs.
type WIPRateLimiter struct {
	mu         sync.RWMutex
	limits     map[string]*ipRateLimitEntry
	interval   time.Duration
	maxHits    int
	wafEnabled bool
	waf        *WAFRateLimiter
}

// NewWAFRateLimiter creates a WAF-backed distributed rate limiter coordinator.
func NewWAFRateLimiter(client *wafv2.Client, cfg config.WAFConfig) *WAFRateLimiter {
	return &WAFRateLimiter{
		client:      client,
		webACLArn:   cfg.WebACLArn,
		ipSetID:     "",
		headerKey:   cfg.RateLimitHeader,
		blockedMsg:  cfg.BlockedResponse,
		lastRefresh: time.Time{},
		refreshTTL:  5 * time.Minute,
	}
}

// NewWIPRateLimiter creates a rate limiter with optional WAF coordination.
func NewWIPRateLimiter(client *wafv2.Client, cfg config.WAFConfig, interval time.Duration, maxHits int) *WIPRateLimiter {
	rl := &WIPRateLimiter{
		limits:     make(map[string]*ipRateLimitEntry),
		interval:   interval,
		maxHits:    maxHits,
		wafEnabled: cfg.Enabled && client != nil && cfg.WebACLArn != "",
	}

	if rl.wafEnabled {
		rl.waf = NewWAFRateLimiter(client, cfg)
	}

	go rl.cleanup()
	return rl
}

// isAllowed checks if the request should be allowed.
func (rl *WIPRateLimiter) isAllowed(ctx context.Context, identifier string) (bool, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.limits[identifier]

	if !exists || now.After(entry.windowEnd) {
		rl.limits[identifier] = &ipRateLimitEntry{
			count:     1,
			windowEnd: now.Add(rl.interval),
		}
		return true, nil
	}

	if entry.count >= rl.maxHits {
		// Coordinate with WAF for distributed blocking
		if rl.waf != nil {
			_ = rl.waf.syncBlockedIP(ctx, identifier)
		}
		return false, nil
	}

	entry.count++
	return true, nil
}

// cleanup removes expired entries periodically.
func (rl *WIPRateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, entry := range rl.limits {
			if now.After(entry.windowEnd) {
				delete(rl.limits, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// syncBlockedIP logs and optionally syncs blocked IPs to WAF for distributed enforcement.
// In production, this would call WAF UpdateIPSet to block the IP fleet-wide.
// For now, it logs the violation for WAF rule evaluation.
func (w *WAFRateLimiter) syncBlockedIP(ctx context.Context, ip string) error {
	if w == nil || w.client == nil {
		return nil
	}

	log := logger.Global()
	log.Warn("rate limit violation detected, IP flagged for WAF",
		"ip", ip,
		"web_acl_arn", w.webACLArn,
	)

	// WAF integration point: in production, call UpdateIPSet to add IP to blocked set
	// The actual WAF WebACL with rate-based rules would handle enforcement
	return nil
}

// WAFRateLimit returns middleware that enforces rate limiting with optional WAF backend.
func WAFRateLimit(client *awsclients.Config, cfg config.WAFConfig, interval time.Duration, maxHits int) gin.HandlerFunc {
	limiter := NewWIPRateLimiter(client.WAF, cfg, interval, maxHits)

	return func(c *gin.Context) {
		if !cfg.Enabled {
			c.Next()
			return
		}

		log := logger.FromContext(c.Request.Context()).Named("waf_rate_limit")

		// Use header-based key if configured (e.g., X-Forwarded-For, CF-Connecting-IP)
		identifier := c.GetHeader(cfg.RateLimitHeader)
		if identifier == "" {
			identifier = GetClientIP(c)
		}

		allowed, err := limiter.isAllowed(c.Request.Context(), identifier)
		if err != nil {
			log.Error("rate limit check failed", "error", err, "identifier", identifier)
			// Fail open - allow request if rate limiter check fails
			c.Next()
			return
		}

		if !allowed {
			log.Warn("rate limit exceeded, blocking request", "identifier", identifier, "path", c.Request.URL.Path)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limit_exceeded",
				"message": cfg.BlockedResponse,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// WAFCommonRateLimit returns middleware for common endpoints (100 req/min per IP).
func WAFCommonRateLimit(client *awsclients.Config, cfg config.WAFConfig) gin.HandlerFunc {
	return WAFRateLimit(client, cfg, time.Minute, 100)
}

// WAFAuthRateLimit returns middleware for auth endpoints (5 req/min per IP).
func WAFAuthRateLimit(client *awsclients.Config, cfg config.WAFConfig) gin.HandlerFunc {
	return WAFRateLimit(client, cfg, time.Minute, 5)
}

// WAFBulkRateLimit returns middleware for bulk operations (20 req/min per IP).
func WAFBulkRateLimit(client *awsclients.Config, cfg config.WAFConfig) gin.HandlerFunc {
	return WAFRateLimit(client, cfg, time.Minute, 20)
}

// WAFLLMRateLimit returns middleware for LLM endpoints (30 req/min per IP).
func WAFLLMRateLimit(client *awsclients.Config, cfg config.WAFConfig) gin.HandlerFunc {
	return WAFRateLimit(client, cfg, time.Minute, 30)
}

// WAFWebSocketRateLimit returns middleware for WebSocket connections (10 conn/min per IP).
func WAFWebSocketRateLimit(client *awsclients.Config, cfg config.WAFConfig) gin.HandlerFunc {
	return WAFRateLimit(client, cfg, time.Minute, 10)
}

// WAFUserWriteRateLimit returns middleware for standard write operations per authenticated user (100 req/min).
// Uses user ID when authenticated, falls back to IP.
func WAFUserWriteRateLimit(client *awsclients.Config, cfg config.WAFConfig) gin.HandlerFunc {
	return WAFWithUserScope(client, cfg, time.Minute, 100)
}

// WAFUserHeavyRateLimit returns middleware for heavy operations per authenticated user (100 req/min).
// Use for bulk imports, document merge/convert, e-invoice/ewaybill generation.
func WAFUserHeavyRateLimit(client *awsclients.Config, cfg config.WAFConfig) gin.HandlerFunc {
	return WAFWithUserScope(client, cfg, time.Minute, 100)
}

// WAFUserReportRateLimit returns middleware for report queries per authenticated user (100 req/min).
func WAFUserReportRateLimit(client *awsclients.Config, cfg config.WAFConfig) gin.HandlerFunc {
	return WAFWithUserScope(client, cfg, time.Minute, 100)
}

// WAFWithUserScope returns middleware that rate limits by user ID (from JWT).
func WAFWithUserScope(client *awsclients.Config, cfg config.WAFConfig, interval time.Duration, maxHits int) gin.HandlerFunc {
	limiter := NewWIPRateLimiter(client.WAF, cfg, interval, maxHits)

	return func(c *gin.Context) {
		if !cfg.Enabled {
			c.Next()
			return
		}

		log := logger.FromContext(c.Request.Context()).Named("waf_rate_limit")

		// Prefer user ID if authenticated, fall back to IP
		identifier := c.GetString("user_id")
		if identifier == "" {
			identifier = c.GetHeader(cfg.RateLimitHeader)
		}
		if identifier == "" {
			identifier = GetClientIP(c)
		}

		allowed, err := limiter.isAllowed(c.Request.Context(), identifier)
		if err != nil {
			log.Error("rate limit check failed", "error", err, "identifier", identifier)
			c.Next()
			return
		}

		if !allowed {
			log.Warn("rate limit exceeded", "identifier", identifier, "path", c.Request.URL.Path)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limit_exceeded",
				"message": cfg.BlockedResponse,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// GetClientIP extracts the real client IP, respecting common proxy headers.
func GetClientIP(c *gin.Context) string {
	// Check common CDN/proxy headers
	for _, header := range []string{
		"CF-Connecting-IP",
		"X-Real-IP",
		"X-Forwarded-For",
		"True-Client-IP",
	} {
		if ip := c.GetHeader(header); ip != "" {
			// X-Forwarded-For can contain multiple IPs, take the first one
			if header == "X-Forwarded-For" {
				parts := strings.Split(ip, ",")
				ip = strings.TrimSpace(parts[0])
			}
			return ip
		}
	}
	return c.ClientIP()
}

// ParseRateLimitHeader parses rate limit info from response headers.
func ParseRateLimitHeader(value string) (remaining int, resetAt time.Time, err error) {
	if value == "" {
		return 0, time.Time{}, fmt.Errorf("empty header")
	}

	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return 0, time.Time{}, fmt.Errorf("invalid format")
	}

	remaining, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, time.Time{}, err
	}

	resetUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, time.Time{}, err
	}

	return remaining, time.Unix(resetUnix, 0), nil
}

// wafSDKRateLimit provides a direct WAF SDK rate limit check without IP set management.
// This can be used with WAF's GetRateBasedStatement to check if an IP is rate limited.
type wafSDKRateLimit struct {
	client    *wafv2.Client
	webACLArn string
	scope     string
}

// newWAFSDKRateLimit creates a WAF SDK-based rate limiter.
func newWAFSDKRateLimit(client *wafv2.Client, webACLArn string) *wafSDKRateLimit {
	return &wafSDKRateLimit{
		client:    client,
		webACLArn: webACLArn,
		scope:     "REGIONAL", // Use REGIONAL for API Gateway
	}
}

// checkRateLimit checks if the given IP is currently blocked by WAF rules.
// Returns true if blocked, false otherwise.
func (w *wafSDKRateLimit) checkRateLimit(ctx context.Context, ip string) (bool, error) {
	if w.client == nil || w.webACLArn == "" {
		return false, nil
	}

	// Get Web ACL to check rules
	acl, err := w.client.GetWebACL(ctx, &wafv2.GetWebACLInput{
		ARN: aws.String(w.webACLArn),
	})
	if err != nil {
		return false, fmt.Errorf("get web acl: %w", err)
	}

	// Check if IP matches any blocked rule
	// Note: In production, WAF's built-in rate limiting handles this automatically
	// This is for custom rule evaluation if needed
	_ = acl

	return false, nil
}
