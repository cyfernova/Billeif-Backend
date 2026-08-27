package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/ratelimit"

	"github.com/aws/aws-lambda-go/events"
	proxycore "github.com/awslabs/aws-lambda-go-api-proxy/core"
	"github.com/gin-gonic/gin"
)

type recordingDistributedLimiter struct {
	mu       sync.Mutex
	buckets  [][]ratelimit.Bucket
	decision ratelimit.Decision
	err      error
}

func (limiter *recordingDistributedLimiter) Decide(_ context.Context, buckets []ratelimit.Bucket) (ratelimit.Decision, error) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	copyOfBuckets := append([]ratelimit.Bucket(nil), buckets...)
	limiter.buckets = append(limiter.buckets, copyOfBuckets)
	return limiter.decision, limiter.err
}

func TestGetClientIPIgnoresUntrustedForwardingHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx.Request.RemoteAddr = "198.51.100.24:443"
	ctx.Request.Header.Set("Forwarded", "for=203.0.113.10")
	ctx.Request.Header.Set("X-Forwarded-For", "203.0.113.11")
	ctx.Request.Header.Set("X-Real-IP", "203.0.113.12")
	ctx.Request.Header.Set("CF-Connecting-IP", "203.0.113.13")
	ctx.Request.Header.Set("True-Client-IP", "203.0.113.14")

	if got := GetClientIP(ctx); got != "198.51.100.24" {
		t.Fatalf("GetClientIP() = %q, want server-observed peer 198.51.100.24", got)
	}
}

func TestGetClientIPPrefersAPIGatewayServerContext(t *testing.T) {
	accessor := proxycore.RequestAccessor{}
	req, err := accessor.EventToRequestWithContext(context.Background(), events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       "/api/v1/auth/login",
		RequestContext: events.APIGatewayProxyRequestContext{
			DomainName: "api.example.test",
			Identity: events.APIGatewayRequestIdentity{
				SourceIP: "198.51.100.42",
			},
		},
	})
	if err != nil {
		t.Fatalf("convert API Gateway request: %v", err)
	}
	req.RemoteAddr = "203.0.113.99:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.98")

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = req

	if got := GetClientIP(ctx); got != "198.51.100.42" {
		t.Fatalf("GetClientIP() = %q, want API Gateway source 198.51.100.42", got)
	}
}

func TestDistributedRateLimitAtomicallySubmitsIPUserAndNormalizedAuthTarget(t *testing.T) {
	resolver, err := NewClientIdentityResolver("production", "")
	if err != nil {
		t.Fatalf("NewClientIdentityResolver() error = %v", err)
	}
	limiter := &recordingDistributedLimiter{decision: ratelimit.Decision{Allowed: true}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "user-123")
		c.Next()
	})
	router.POST("/login", DistributedRateLimit(limiter, resolver, RateLimitPolicy{
		Namespace:  "auth/login",
		Limit:      5,
		Window:     time.Minute,
		IP:         true,
		User:       true,
		AuthTarget: true,
	}), func(c *gin.Context) {
		var body map[string]string
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	request := apiGatewayHTTPRequest(t, http.MethodPost, "/login", "198.51.100.42", []byte(`{"email":"  Person@Example.COM ","password":"not-a-real-password"}`))
	request.RemoteAddr = "203.0.113.99:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.98")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", response.Code, response.Body.String())
	}

	if len(limiter.buckets) != 1 {
		t.Fatalf("backend calls = %d, want one atomic decision", len(limiter.buckets))
	}
	want := []ratelimit.Bucket{
		{Namespace: "auth/login/ip", Identity: "198.51.100.42", Limit: 5, Window: time.Minute},
		{Namespace: "auth/login/user", Identity: "user-123", Limit: 5, Window: time.Minute},
		{Namespace: "auth/login/auth-target", Identity: "email:person@example.com", Limit: 5, Window: time.Minute},
	}
	if len(limiter.buckets[0]) != len(want) {
		t.Fatalf("atomic buckets = %#v, want %#v", limiter.buckets[0], want)
	}
	for index := range want {
		if limiter.buckets[0][index] != want[index] {
			t.Fatalf("bucket %d = %#v, want %#v", index, limiter.buckets[0][index], want[index])
		}
	}
}

func TestDistributedRateLimitFailsClosedWhenBackendDecisionFails(t *testing.T) {
	resolver, err := NewClientIdentityResolver("production", "")
	if err != nil {
		t.Fatalf("NewClientIdentityResolver() error = %v", err)
	}
	limiter := &recordingDistributedLimiter{err: errors.New("backend unavailable")}
	downstreamCalled := false
	router := gin.New()
	router.GET("/catalog", DistributedRateLimit(limiter, resolver, RateLimitPolicy{
		Namespace: "public/storefront-catalog",
		Limit:     120,
		Window:    time.Minute,
		IP:        true,
	}), func(c *gin.Context) {
		downstreamCalled = true
		c.Status(http.StatusOK)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, apiGatewayHTTPRequest(t, http.MethodGet, "/catalog", "198.51.100.50", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", response.Code, response.Body.String())
	}
	if downstreamCalled {
		t.Fatal("downstream handler ran after backend decision error")
	}
}

func TestProductionIdentityFailsClosedWithoutAPIGatewayContext(t *testing.T) {
	resolver, err := NewClientIdentityResolver("production", "")
	if err != nil {
		t.Fatalf("NewClientIdentityResolver() error = %v", err)
	}
	limiter := &recordingDistributedLimiter{decision: ratelimit.Decision{Allowed: true}}
	router := gin.New()
	router.GET("/catalog", DistributedRateLimit(limiter, resolver, RateLimitPolicy{
		Namespace: "public/storefront-catalog",
		Limit:     120,
		Window:    time.Minute,
		IP:        true,
	}), func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	request.RemoteAddr = "198.51.100.60:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.61")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", response.Code, response.Body.String())
	}
	if len(limiter.buckets) != 0 {
		t.Fatal("backend was called without a trusted production network identity")
	}
}

func TestAuthenticationTargetFailsClosedWhenBodyCannotBeSafelyNormalized(t *testing.T) {
	resolver, err := NewClientIdentityResolver("production", "")
	if err != nil {
		t.Fatalf("NewClientIdentityResolver() error = %v", err)
	}
	limiter := &recordingDistributedLimiter{decision: ratelimit.Decision{Allowed: true}}
	router := gin.New()
	router.POST("/login", DistributedRateLimit(limiter, resolver, RateLimitPolicy{
		Namespace:  "auth/login",
		Limit:      5,
		Window:     time.Minute,
		IP:         true,
		AuthTarget: true,
	}), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	body := []byte(`{"email":"person@example.com","padding":"` + strings.Repeat("x", int(maxAuthRateLimitBodyBytes)) + `"}`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, apiGatewayHTTPRequest(t, http.MethodPost, "/login", "198.51.100.50", body))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", response.Code, response.Body.String())
	}
	if len(limiter.buckets) != 0 {
		t.Fatal("backend received an incomplete authentication-target decision")
	}
}

func TestNormalizePhoneTargetCollapsesAcceptedIndianNumberFormats(t *testing.T) {
	for _, input := range []string{"9876543210", "91 98765-43210", "+91 (98765) 43210"} {
		if got := normalizePhoneTarget(input); got != "+919876543210" {
			t.Fatalf("normalizePhoneTarget(%q) = %q, want +919876543210", input, got)
		}
	}
}

func TestLocalIdentityAcceptsForwardingHeaderOnlyFromExactTrustedProxyCIDR(t *testing.T) {
	resolver, err := NewClientIdentityResolver("development", "127.0.0.1/32")
	if err != nil {
		t.Fatalf("NewClientIdentityResolver() error = %v", err)
	}

	trustedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	trustedRequest.RemoteAddr = "127.0.0.1:54321"
	trustedRequest.Header.Set("X-Forwarded-For", "198.51.100.70")
	if got, err := resolver.ClientIP(trustedRequest); err != nil || got != "198.51.100.70" {
		t.Fatalf("trusted proxy ClientIP() = %q, %v, want 198.51.100.70", got, err)
	}

	untrustedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	untrustedRequest.RemoteAddr = "127.0.0.2:54321"
	untrustedRequest.Header.Set("X-Forwarded-For", "198.51.100.71")
	if got, err := resolver.ClientIP(untrustedRequest); err != nil || got != "127.0.0.2" {
		t.Fatalf("untrusted peer ClientIP() = %q, %v, want direct peer 127.0.0.2", got, err)
	}

	if _, err := NewClientIdentityResolver("development", "0.0.0.0/0"); err == nil {
		t.Fatal("NewClientIdentityResolver() accepted a trust-everyone proxy CIDR")
	}
}

func apiGatewayHTTPRequest(t *testing.T, method, path, sourceIP string, body []byte) *http.Request {
	t.Helper()
	accessor := proxycore.RequestAccessor{}
	request, err := accessor.EventToRequestWithContext(context.Background(), events.APIGatewayProxyRequest{
		HTTPMethod: method,
		Path:       path,
		Body:       string(body),
		Headers:    map[string]string{"Content-Type": "application/json"},
		RequestContext: events.APIGatewayProxyRequestContext{
			DomainName: "api.example.test",
			Identity:   events.APIGatewayRequestIdentity{SourceIP: sourceIP},
		},
	})
	if err != nil {
		t.Fatalf("convert API Gateway request: %v", err)
	}
	if body != nil {
		request.Body = http.NoBody
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
	}
	return request
}
