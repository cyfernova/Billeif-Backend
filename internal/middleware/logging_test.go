package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestLoggerInjectedAndCompletionLogged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, observed := observer.New(zapcore.DebugLevel)
	log := logger.FromZap(zap.New(core))

	router := gin.New()
	router.Use(RequestID())
	router.Use(Logger(log))
	router.GET("/ping", func(c *gin.Context) {
		logger.FromContext(c.Request.Context()).Info("handler-executed")
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	entries := observed.AllUntimed()
	handlerEntry := findEntry(entries, "handler-executed")
	if handlerEntry == nil {
		t.Fatalf("expected handler-executed entry to be logged")
	}
	if _, ok := handlerEntry.ContextMap()["request_id"]; !ok {
		t.Fatalf("expected handler log to contain request_id")
	}

	completionEntry := findEntry(entries, "request completed")
	if completionEntry == nil {
		t.Fatalf("expected request completed entry to be logged")
	}
	fields := completionEntry.ContextMap()
	if fields["method"] != "GET" {
		t.Fatalf("expected method GET, got %v", fields["method"])
	}
	if fields["route"] != "/ping" {
		t.Fatalf("expected route /ping, got %v", fields["route"])
	}
	if fields["status"] != int64(200) {
		t.Fatalf("expected status 200, got %v", fields["status"])
	}
	if _, ok := fields["request_id"]; !ok {
		t.Fatalf("expected request completed entry to contain request_id")
	}
}

func TestAuthInvalidTokenLogsWarning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwksCachesMu.Lock()
	jwksCaches = map[string]*JWKSCache{}
	jwksCachesMu.Unlock()

	core, observed := observer.New(zapcore.DebugLevel)
	log := logger.FromZap(zap.New(core))

	router := gin.New()
	router.Use(RequestID())
	router.Use(Logger(log))
	router.Use(Auth(config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"}, log))
	router.GET("/secure", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer invalid.token")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}

	entry := findEntry(observed.AllUntimed(), "token validation failed")
	if entry == nil {
		t.Fatalf("expected token validation warning log")
	}
	if entry.Level != zapcore.WarnLevel {
		t.Fatalf("expected warn level, got %v", entry.Level)
	}
}

func TestAuthValidTokenEnrichesContextLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	cfg := config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123", JWKSRefreshRate: time.Hour}
	jwksCachesMu.Lock()
	jwksCaches = map[string]*JWKSCache{
		fmt.Sprintf("%s/.well-known/jwks.json", cognitoIssuer(cfg.Region, cfg.UserPoolID)): {
			keys: map[string]*rsa.PublicKey{
				"test-kid": &privateKey.PublicKey,
			},
			lastFetch: time.Now(),
			ttl:       time.Hour,
			jwksURL:   "memory",
		},
	}
	jwksCachesMu.Unlock()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &CognitoClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    cognitoIssuer(cfg.Region, cfg.UserPoolID),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TokenUse:   "access",
		BusinessID: "biz-123",
		Role:       "admin",
		Email:      "user@example.com",
		Username:   "user1",
	})
	token.Header["kid"] = "test-kid"
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("failed to sign JWT: %v", err)
	}

	core, observed := observer.New(zapcore.DebugLevel)
	log := logger.FromZap(zap.New(core))

	router := gin.New()
	router.Use(RequestID())
	router.Use(Logger(log))
	router.Use(Auth(cfg, log))
	router.GET("/secure", func(c *gin.Context) {
		logger.FromContext(c.Request.Context()).Info("inside-auth-handler")
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+signedToken)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	entry := findEntry(observed.AllUntimed(), "inside-auth-handler")
	if entry == nil {
		t.Fatalf("expected inside-auth-handler log")
	}
	fields := entry.ContextMap()
	if fields["user_id"] != "user-123" {
		t.Fatalf("expected user_id=user-123, got %v", fields["user_id"])
	}
	if fields["business_id"] != "biz-123" {
		t.Fatalf("expected business_id=biz-123, got %v", fields["business_id"])
	}
	if fields["role"] != "admin" {
		t.Fatalf("expected role=admin, got %v", fields["role"])
	}
	if _, ok := fields["request_id"]; !ok {
		t.Fatalf("expected request_id to be present in auth-enriched handler log")
	}
}

func findEntry(entries []observer.LoggedEntry, msg string) *observer.LoggedEntry {
	for i := range entries {
		if entries[i].Message == msg {
			return &entries[i]
		}
	}
	return nil
}
