package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/ratelimit"
	"invoice-backend/pkg/logger"

	proxycore "github.com/awslabs/aws-lambda-go-api-proxy/core"
	"github.com/gin-gonic/gin"
)

const maxAuthRateLimitBodyBytes int64 = 64 * 1024

type ClientIdentityResolver struct {
	production   bool
	trustedProxy *net.IPNet
}

func NewClientIdentityResolver(environment, trustedProxyCIDR string) (*ClientIdentityResolver, error) {
	resolver := &ClientIdentityResolver{production: isProductionEnvironment(environment)}
	trustedProxyCIDR = strings.TrimSpace(trustedProxyCIDR)
	if trustedProxyCIDR == "" {
		return resolver, nil
	}
	if resolver.production {
		return nil, fmt.Errorf("trusted proxy CIDR is local-only")
	}
	_, network, err := net.ParseCIDR(trustedProxyCIDR)
	if err != nil {
		return nil, fmt.Errorf("parse trusted proxy CIDR: %w", err)
	}
	ones, _ := network.Mask.Size()
	if ones == 0 {
		return nil, fmt.Errorf("trusted proxy CIDR cannot trust every address")
	}
	resolver.trustedProxy = network
	return resolver, nil
}

func (resolver *ClientIdentityResolver) ClientIP(request *http.Request) (string, error) {
	if resolver == nil || request == nil {
		return "", fmt.Errorf("request identity resolver is unavailable")
	}
	if resolver.production {
		gatewayContext, ok := proxycore.GetAPIGatewayContextFromContext(request.Context())
		if !ok {
			return "", fmt.Errorf("API Gateway request context is unavailable")
		}
		return canonicalIP(gatewayContext.Identity.SourceIP)
	}

	peer, err := remoteAddressIP(request.RemoteAddr)
	if err != nil {
		return "", err
	}
	if resolver.trustedProxy == nil || !resolver.trustedProxy.Contains(net.ParseIP(peer)) {
		return peer, nil
	}

	forwarded := strings.TrimSpace(request.Header.Get("X-Forwarded-For"))
	if forwarded == "" {
		return peer, nil
	}
	first, _, _ := strings.Cut(forwarded, ",")
	return canonicalIP(first)
}

type RateLimitPolicy struct {
	Namespace   string
	Limit       int64
	Window      time.Duration
	IP          bool
	User        bool
	AuthTarget  bool
	RequireUser bool
	Message     string
}

func DistributedRateLimit(
	limiter ratelimit.Limiter,
	identities *ClientIdentityResolver,
	policies ...RateLimitPolicy,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context()).Named("rate_limit")
		buckets, status, err := rateLimitBuckets(c, identities, policies)
		if err != nil {
			log.Error("rate limit identity decision failed", "error", err)
			writeRateLimitUnavailable(c)
			return
		}
		if status != 0 {
			c.JSON(status, gin.H{"error": "user not authenticated"})
			c.Abort()
			return
		}
		if limiter == nil {
			log.Error("rate limit backend is unavailable")
			writeRateLimitUnavailable(c)
			return
		}

		decision, err := limiter.Decide(c.Request.Context(), buckets)
		if err != nil {
			log.Error("distributed rate limit decision failed", "error", err)
			writeRateLimitUnavailable(c)
			return
		}
		if !decision.Allowed {
			if decision.RetryAfter > 0 {
				seconds := (decision.RetryAfter + time.Second - 1) / time.Second
				c.Header("Retry-After", strconv.FormatInt(int64(seconds), 10))
			}
			message := "rate limit exceeded. please try again later"
			if len(policies) > 0 && strings.TrimSpace(policies[0].Message) != "" {
				message = policies[0].Message
			}
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limit_exceeded",
				"message": message,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func rateLimitBuckets(
	c *gin.Context,
	identities *ClientIdentityResolver,
	policies []RateLimitPolicy,
) ([]ratelimit.Bucket, int, error) {
	if c == nil || c.Request == nil {
		return nil, 0, fmt.Errorf("request is unavailable")
	}
	if len(policies) == 0 {
		return nil, 0, fmt.Errorf("rate limit policy is required")
	}

	needsIP := false
	needsTarget := false
	for _, policy := range policies {
		if strings.TrimSpace(policy.Namespace) == "" || policy.Limit <= 0 || policy.Window <= 0 {
			return nil, 0, fmt.Errorf("invalid rate limit policy")
		}
		needsIP = needsIP || policy.IP
		needsTarget = needsTarget || policy.AuthTarget
	}

	clientIP := ""
	var err error
	if needsIP {
		clientIP, err = identities.ClientIP(c.Request)
		if err != nil {
			return nil, 0, err
		}
	}
	userID := strings.TrimSpace(c.GetString("user_id"))
	authTarget := ""
	if needsTarget {
		authTarget, err = normalizedAuthTarget(c.Request)
		if err != nil {
			return nil, 0, err
		}
	}

	buckets := make([]ratelimit.Bucket, 0, len(policies)*3)
	for _, policy := range policies {
		if policy.IP {
			buckets = append(buckets, policyBucket(policy, "ip", clientIP))
		}
		if policy.User {
			if userID == "" && policy.RequireUser {
				return nil, http.StatusUnauthorized, nil
			}
			if userID != "" {
				buckets = append(buckets, policyBucket(policy, "user", userID))
			}
		}
		if policy.AuthTarget && authTarget != "" {
			buckets = append(buckets, policyBucket(policy, "auth-target", authTarget))
		}
	}
	if len(buckets) == 0 {
		return nil, 0, fmt.Errorf("rate limit policy produced no identity buckets")
	}
	return buckets, 0, nil
}

func policyBucket(policy RateLimitPolicy, dimension, identity string) ratelimit.Bucket {
	return ratelimit.Bucket{
		Namespace: strings.TrimSuffix(policy.Namespace, "/") + "/" + dimension,
		Identity:  identity,
		Limit:     policy.Limit,
		Window:    policy.Window,
	}
}

func normalizedAuthTarget(request *http.Request) (string, error) {
	if request.Body == nil || request.Body == http.NoBody {
		return "", nil
	}

	originalBody := request.Body
	prefix, err := io.ReadAll(io.LimitReader(originalBody, maxAuthRateLimitBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("read authentication target: %w", err)
	}
	if int64(len(prefix)) > maxAuthRateLimitBodyBytes {
		request.Body = &joinedReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix), originalBody), Closer: originalBody}
		return "", fmt.Errorf("authentication request body exceeds the safe normalization limit")
	}
	if err := originalBody.Close(); err != nil {
		return "", fmt.Errorf("close authentication request body: %w", err)
	}
	request.Body = io.NopCloser(bytes.NewReader(prefix))

	var payload struct {
		Email       string `json:"email"`
		PhoneNumber string `json:"phone_number"`
	}
	if err := json.Unmarshal(prefix, &payload); err != nil {
		return "", nil
	}
	if email := strings.ToLower(strings.TrimSpace(payload.Email)); email != "" {
		return "email:" + email, nil
	}
	if phone := normalizePhoneTarget(payload.PhoneNumber); phone != "" {
		return "phone:" + phone, nil
	}
	return "", nil
}

func normalizePhoneTarget(value string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
	value = replacer.Replace(strings.TrimSpace(value))
	switch {
	case strings.HasPrefix(value, "+91"):
		return "+91" + strings.TrimPrefix(value, "+91")
	case strings.HasPrefix(value, "91") && len(value) == 12:
		return "+91" + strings.TrimPrefix(value, "91")
	case len(value) == 10:
		return "+91" + value
	default:
		return value
	}
}

func writeRateLimitUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error":   "rate_limit_unavailable",
		"message": "request could not be safely evaluated. please try again shortly",
	})
	c.Abort()
}

func remoteAddressIP(remoteAddress string) (string, error) {
	remoteAddress = strings.TrimSpace(remoteAddress)
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return canonicalIP(host)
	}
	return canonicalIP(remoteAddress)
}

func canonicalIP(value string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return "", fmt.Errorf("invalid client IP address")
	}
	return ip.String(), nil
}

func isProductionEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "prod", "production":
		return true
	default:
		return false
	}
}

// GetClientIP returns only a server-observed identity. Production rate-limit
// decisions use ClientIdentityResolver, which additionally requires API Gateway
// context; this helper intentionally never consumes caller forwarding headers.
func GetClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if gatewayContext, ok := proxycore.GetAPIGatewayContextFromContext(c.Request.Context()); ok {
		if ip, err := canonicalIP(gatewayContext.Identity.SourceIP); err == nil {
			return ip
		}
	}
	if ip, err := remoteAddressIP(c.Request.RemoteAddr); err == nil {
		return ip
	}
	return ""
}

type joinedReadCloser struct {
	io.Reader
	io.Closer
}
