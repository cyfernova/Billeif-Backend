package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthAcceptsPrimaryAndPhonePools(t *testing.T) {
	gin.SetMode(gin.TestMode)

	primaryKey := mustGenerateRSAKey(t)
	phoneKey := mustGenerateRSAKey(t)
	primaryServer, primaryJWKSURL := startJWKSServer(t, primaryKey, "primary-kid")
	defer primaryServer.Close()
	phoneServer, phoneJWKSURL := startJWKSServer(t, phoneKey, "phone-kid")
	defer phoneServer.Close()

	cfg := config.CognitoConfig{
		UserPoolID:      "us-east-1_primary",
		ClientID:        "primary-client-id",
		Region:          "us-east-1",
		JWKSRefreshRate: time.Minute,
		Phone: config.CognitoPhoneConfig{
			UserPoolID: "ap-south-1_phone",
			ClientID:   "phone-client-id",
			Region:     "ap-south-1",
		},
	}

	seedJWKSCache(cognitoIssuer(cfg.Region, cfg.UserPoolID)+"/.well-known/jwks.json", primaryJWKSURL)
	seedJWKSCache(cognitoIssuer(cfg.Phone.Region, cfg.Phone.UserPoolID)+"/.well-known/jwks.json", phoneJWKSURL)
	t.Cleanup(func() {
		jwksCachesMu.Lock()
		defer jwksCachesMu.Unlock()
		jwksCaches = map[string]*JWKSCache{}
	})

	router := gin.New()
	router.Use(Auth(cfg, logger.New()))
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id": GetUserID(c),
		})
	})

	t.Run("primary pool keeps raw subject", func(t *testing.T) {
		token := mustSignToken(t, primaryKey, "primary-kid", &CognitoClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "primary-subject",
				Issuer:    cognitoIssuer(cfg.Region, cfg.UserPoolID),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			TokenUse: "access",
			ClientID: "primary-client-id",
			Email:    "primary@example.com",
		})

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "primary-subject", body["user_id"])
	})

	t.Run("phone pool uses canonical subject", func(t *testing.T) {
		token := mustSignToken(t, phoneKey, "phone-kid", &CognitoClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "phone-subject",
				Issuer:    cognitoIssuer(cfg.Phone.Region, cfg.Phone.UserPoolID),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			TokenUse:    "access",
			ClientID:    "phone-client-id",
			PhoneNumber: "+919876543210",
		})

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "ap-south-1_phone:phone-subject", body["user_id"])
	})
}

func TestAuth_DefaultRequiresAccessToken_AndGooglePathAllowsID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	primaryKey := mustGenerateRSAKey(t)
	primaryServer, primaryJWKSURL := startJWKSServer(t, primaryKey, "primary-kid")
	defer primaryServer.Close()

	cfg := config.CognitoConfig{
		UserPoolID:      "us-east-1_primary",
		ClientID:        "primary-client-id",
		Region:          "us-east-1",
		JWKSRefreshRate: time.Minute,
	}

	seedJWKSCache(cognitoIssuer(cfg.Region, cfg.UserPoolID)+"/.well-known/jwks.json", primaryJWKSURL)
	t.Cleanup(func() {
		jwksCachesMu.Lock()
		defer jwksCachesMu.Unlock()
		jwksCaches = map[string]*JWKSCache{}
	})

	idToken := mustSignToken(t, primaryKey, "primary-kid", &CognitoClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "id-subject",
			Issuer:    cognitoIssuer(cfg.Region, cfg.UserPoolID),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Audience:  []string{"primary-client-id"},
		},
		TokenUse:      "id",
		Email:         "primary@example.com",
		EmailVerified: true,
	})

	t.Run("default auth rejects id token", func(t *testing.T) {
		router := gin.New()
		router.Use(Auth(cfg, logger.New()))
		router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+idToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("id-only auth accepts id token", func(t *testing.T) {
		router := gin.New()
		router.Use(AuthWithTokenUse(cfg, logger.New(), TokenUseID))
		router.POST("/auth/google", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"email_verified": GetEmailVerified(c)})
		})

		req := httptest.NewRequest(http.MethodPost, "/auth/google", nil)
		req.Header.Set("Authorization", "Bearer "+idToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]bool
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.True(t, body["email_verified"])
	})
}

func TestAuth_RejectsTokenWithWrongClientBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	primaryKey := mustGenerateRSAKey(t)
	primaryServer, primaryJWKSURL := startJWKSServer(t, primaryKey, "primary-kid")
	defer primaryServer.Close()

	cfg := config.CognitoConfig{
		UserPoolID:      "us-east-1_primary",
		ClientID:        "expected-client-id",
		Region:          "us-east-1",
		JWKSRefreshRate: time.Minute,
	}

	seedJWKSCache(cognitoIssuer(cfg.Region, cfg.UserPoolID)+"/.well-known/jwks.json", primaryJWKSURL)
	t.Cleanup(func() {
		jwksCachesMu.Lock()
		defer jwksCachesMu.Unlock()
		jwksCaches = map[string]*JWKSCache{}
	})

	router := gin.New()
	router.Use(Auth(cfg, logger.New()))
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })

	accessToken := mustSignToken(t, primaryKey, "primary-kid", &CognitoClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    cognitoIssuer(cfg.Region, cfg.UserPoolID),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TokenUse: "access",
		ClientID: "unexpected-client-id",
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestJWKSRefresh_RemovesRetiredKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)

	primaryKey := mustGenerateRSAKey(t)
	rotatedKey := mustGenerateRSAKey(t)
	server, jwksURL, setJWKS := startMutableJWKSServer(t)
	defer server.Close()

	cfg := config.CognitoConfig{
		UserPoolID:      "us-east-1_primary",
		ClientID:        "primary-client-id",
		Region:          "us-east-1",
		JWKSRefreshRate: 5 * time.Millisecond,
	}

	cacheKey := cognitoIssuer(cfg.Region, cfg.UserPoolID) + "/.well-known/jwks.json"
	seedJWKSCacheWithTTL(cacheKey, jwksURL, cfg.JWKSRefreshRate)
	t.Cleanup(func() {
		jwksCachesMu.Lock()
		defer jwksCachesMu.Unlock()
		jwksCaches = map[string]*JWKSCache{}
	})

	setJWKS([]map[string]string{jwkForKey(primaryKey, "initial-kid")})

	initialToken := mustSignToken(t, primaryKey, "initial-kid", &CognitoClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    cognitoIssuer(cfg.Region, cfg.UserPoolID),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TokenUse: "access",
		ClientID: "primary-client-id",
	})

	_, err := ValidateCognitoToken(cfg, initialToken)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	setJWKS([]map[string]string{jwkForKey(rotatedKey, "rotated-kid")})

	_, err = ValidateCognitoToken(cfg, initialToken)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func mustGenerateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func startJWKSServer(t *testing.T, key *rsa.PrivateKey, kid string) (*httptest.Server, string) {
	t.Helper()

	jwks := map[string]any{"keys": []map[string]string{jwkForKey(key, kid)}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/jwks.json", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(jwks))
	}))

	return server, server.URL + "/jwks.json"
}

func startMutableJWKSServer(t *testing.T) (*httptest.Server, string, func(keys []map[string]string)) {
	t.Helper()

	var mu sync.RWMutex
	current := []map[string]string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/jwks.json", r.URL.Path)
		mu.RLock()
		payload := map[string]any{"keys": current}
		mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(payload))
	}))

	setJWKS := func(keys []map[string]string) {
		mu.Lock()
		defer mu.Unlock()
		current = keys
	}

	return server, server.URL + "/jwks.json", setJWKS
}

func jwkForKey(key *rsa.PrivateKey, kid string) map[string]string {
	return map[string]string{
		"alg": "RS256",
		"e":   base64.RawURLEncoding.EncodeToString(bigEndianBytes(key.PublicKey.E)),
		"kid": kid,
		"kty": "RSA",
		"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"use": "sig",
	}
}

func mustSignToken(t *testing.T, key *rsa.PrivateKey, kid string, claims *CognitoClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

func seedJWKSCache(cacheKey, serverJWKSURL string) {
	seedJWKSCacheWithTTL(cacheKey, serverJWKSURL, time.Minute)
}

func seedJWKSCacheWithTTL(cacheKey, serverJWKSURL string, ttl time.Duration) {
	jwksCachesMu.Lock()
	defer jwksCachesMu.Unlock()
	jwksCaches[cacheKey] = NewJWKSCache(serverJWKSURL, ttl)
}

func bigEndianBytes(exponent int) []byte {
	if exponent == 0 {
		return []byte{0}
	}

	var out []byte
	for exponent > 0 {
		out = append([]byte{byte(exponent & 0xff)}, out...)
		exponent >>= 8
	}
	return out
}

func Example_cognitoIssuer() {
	fmt.Println(cognitoIssuer("ap-south-1", "pool"))
	// Output: https://cognito-idp.ap-south-1.amazonaws.com/pool
}
