package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

type JWKS struct {
	Keys []JWK `json:"keys"`
}

type JWK struct {
	Alg string `json:"alg"`
	E   string `json:"e"`
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	Use string `json:"use"`
}

type JWKSCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	lastFetch time.Time
	ttl       time.Duration
	jwksURL   string
}

func NewJWKSCache(jwksURL string, ttl time.Duration) *JWKSCache {
	return &JWKSCache{
		keys:    make(map[string]*rsa.PublicKey),
		ttl:     ttl,
		jwksURL: jwksURL,
	}
}

func (c *JWKSCache) GetKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	if key, ok := c.keys[kid]; ok && time.Since(c.lastFetch) < c.ttl {
		c.mu.RUnlock()
		return key, nil
	}
	c.mu.RUnlock()

	if err := c.refresh(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key not found: %s", kid)
}

func (c *JWKSCache) refresh() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.lastFetch) < c.ttl && len(c.keys) > 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch JWKS: unexpected status %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}

	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pubKey, err := parseRSAPublicKey(k)
		if err != nil {
			continue
		}
		c.keys[k.Kid] = pubKey
	}

	c.lastFetch = time.Now()
	return nil
}

func parseRSAPublicKey(k JWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

type CognitoClaims struct {
	jwt.RegisteredClaims
	Email       string   `json:"email"`
	PhoneNumber string   `json:"phone_number"`
	Username    string   `json:"cognito:username"`
	Groups      []string `json:"cognito:groups"`
	TokenUse    string   `json:"token_use"`
	BusinessID  string   `json:"custom:businessId"`
	Role        string   `json:"custom:role"`
	Picture     string   `json:"picture"`
	Name        string   `json:"name"`
}

type cognitoPool struct {
	userPoolID          string
	region              string
	issuer              string
	jwksURL             string
	ttl                 time.Duration
	canonicalizeSubject bool
}

var (
	jwksCachesMu sync.Mutex
	jwksCaches   = map[string]*JWKSCache{}
)

func parseAuthorizationHeader(authHeader string) (string, error) {
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 1 {
		return parts[0], nil
	}
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", fmt.Errorf("invalid authorization header format")
	}
	return parts[1], nil
}

func ValidateCognitoToken(cfg config.CognitoConfig, tokenString string) (*CognitoClaims, error) {
	claims, _, err := validateCognitoTokenWithPools(tokenString, cognitoPoolsFromConfig(cfg))
	return claims, err
}

func ValidateCognitoAuthorization(cfg config.CognitoConfig, authHeader string) (*CognitoClaims, error) {
	tokenString, err := parseAuthorizationHeader(authHeader)
	if err != nil {
		return nil, err
	}
	return ValidateCognitoToken(cfg, tokenString)
}

func Auth(cfg config.CognitoConfig, log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqLog := logger.FromContext(c.Request.Context()).Named("auth_middleware")
		authHeader := c.GetHeader("Authorization")
		claims, err := ValidateCognitoAuthorization(cfg, authHeader)
		if err != nil {
			reqLog.Warn("token validation failed", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Set("user_id", claims.Subject)
		c.Set("email", claims.Email)
		c.Set("phone_number", claims.PhoneNumber)
		c.Set("username", claims.Username)
		c.Set("groups", claims.Groups)
		c.Set("business_id", claims.BusinessID)
		c.Set("role", claims.Role)
		c.Set("picture", claims.Picture)
		c.Set("name", claims.Name)
		c.Set("authorization_header", authHeader)
		c.Request = c.Request.WithContext(logger.ToContext(c.Request.Context(), reqLog.With(
			"user_id", claims.Subject,
			"business_id", claims.BusinessID,
			"role", claims.Role,
		)))
		c.Request = c.Request.WithContext(a2a.WithAuthorizationHeader(c.Request.Context(), authHeader))

		c.Next()
	}
}

func GetUserID(c *gin.Context) string {
	if id, exists := c.Get("user_id"); exists {
		return id.(string)
	}
	return ""
}

func GetEmail(c *gin.Context) string {
	if email, exists := c.Get("email"); exists {
		return email.(string)
	}
	return ""
}

func GetBusinessID(c *gin.Context) string {
	if id, exists := c.Get("business_id"); exists {
		return id.(string)
	}
	return ""
}

func GetRole(c *gin.Context) string {
	if role, exists := c.Get("role"); exists {
		return role.(string)
	}
	return "viewer"
}

func GetGroups(c *gin.Context) []string {
	if groups, exists := c.Get("groups"); exists {
		return groups.([]string)
	}
	return nil
}

func GetPicture(c *gin.Context) string {
	if picture, exists := c.Get("picture"); exists {
		return picture.(string)
	}
	return ""
}

func GetName(c *gin.Context) string {
	if name, exists := c.Get("name"); exists {
		return name.(string)
	}
	return ""
}

func cognitoPoolsFromConfig(cfg config.CognitoConfig) []cognitoPool {
	ttl := cfg.JWKSRefreshRate
	if ttl == 0 {
		ttl = 10 * time.Minute
	}

	pools := []cognitoPool{
		newCognitoPool(cfg.Region, cfg.UserPoolID, ttl, false),
	}

	if cfg.Phone.UserPoolID != "" && cfg.Phone.Region != "" {
		pools = append(pools, newCognitoPool(cfg.Phone.Region, cfg.Phone.UserPoolID, ttl, true))
	}

	return pools
}

func newCognitoPool(region, userPoolID string, ttl time.Duration, canonicalizeSubject bool) cognitoPool {
	return cognitoPool{
		userPoolID:          userPoolID,
		region:              region,
		issuer:              cognitoIssuer(region, userPoolID),
		jwksURL:             fmt.Sprintf("%s/.well-known/jwks.json", cognitoIssuer(region, userPoolID)),
		ttl:                 ttl,
		canonicalizeSubject: canonicalizeSubject,
	}
}

func validateCognitoTokenWithPools(tokenString string, pools []cognitoPool) (*CognitoClaims, cognitoPool, error) {
	unverifiedClaims, err := parseUnverifiedClaims(tokenString)
	if err != nil {
		return nil, cognitoPool{}, err
	}

	pool, ok := findCognitoPoolByIssuer(unverifiedClaims.Issuer, pools)
	if !ok {
		return nil, cognitoPool{}, fmt.Errorf("token issuer is not allowed")
	}

	cache := getJWKSCache(pool)
	token, err := jwt.ParseWithClaims(tokenString, &CognitoClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid not found in token header")
		}
		return cache.GetKey(kid)
	})
	if err != nil {
		return nil, cognitoPool{}, err
	}

	claims, ok := token.Claims.(*CognitoClaims)
	if !ok || !token.Valid {
		return nil, cognitoPool{}, fmt.Errorf("invalid token claims")
	}
	if claims.Issuer != pool.issuer {
		return nil, cognitoPool{}, fmt.Errorf("invalid token issuer")
	}
	if claims.TokenUse != "access" && claims.TokenUse != "id" {
		return nil, cognitoPool{}, fmt.Errorf("invalid token type: %s", claims.TokenUse)
	}
	if pool.canonicalizeSubject {
		claims.Subject = canonicalPhoneCognitoID(pool.userPoolID, claims.Subject)
	}

	return claims, pool, nil
}

func parseUnverifiedClaims(tokenString string) (*CognitoClaims, error) {
	parser := jwt.Parser{}
	claims := &CognitoClaims{}
	if _, _, err := parser.ParseUnverified(tokenString, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func findCognitoPoolByIssuer(issuer string, pools []cognitoPool) (cognitoPool, bool) {
	for _, pool := range pools {
		if pool.issuer == issuer {
			return pool, true
		}
	}
	return cognitoPool{}, false
}

func getJWKSCache(pool cognitoPool) *JWKSCache {
	jwksCachesMu.Lock()
	defer jwksCachesMu.Unlock()

	cache, ok := jwksCaches[pool.jwksURL]
	if ok {
		return cache
	}

	cache = NewJWKSCache(pool.jwksURL, pool.ttl)
	jwksCaches[pool.jwksURL] = cache
	return cache
}

func cognitoIssuer(region, userPoolID string) string {
	return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, userPoolID)
}

func canonicalPhoneCognitoID(userPoolID, subject string) string {
	return fmt.Sprintf("%s:%s", userPoolID, subject)
}
