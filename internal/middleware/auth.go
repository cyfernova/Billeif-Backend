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

type TokenUse string

const (
	TokenUseAccess TokenUse = "access"
	TokenUseID     TokenUse = "id"
)

func NewJWKSCache(jwksURL string, ttl time.Duration) *JWKSCache {
	return &JWKSCache{
		keys:    make(map[string]*rsa.PublicKey),
		ttl:     ttl,
		jwksURL: jwksURL,
	}
}

func (c *JWKSCache) GetKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	stale := time.Since(c.lastFetch) >= c.ttl
	c.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}

	if err := c.refresh(!ok); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key not found: %s", kid)
}

func (c *JWKSCache) refresh(force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !force && time.Since(c.lastFetch) < c.ttl && len(c.keys) > 0 {
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

	refreshedKeys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pubKey, err := parseRSAPublicKey(k)
		if err != nil {
			continue
		}
		refreshedKeys[k.Kid] = pubKey
	}

	if len(refreshedKeys) == 0 {
		return fmt.Errorf("fetch JWKS: no valid RSA keys found")
	}

	// Replace the entire key map atomically so retired keys are dropped.
	c.keys = refreshedKeys
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
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	PhoneNumber   string   `json:"phone_number"`
	Username      string   `json:"cognito:username"`
	Groups        []string `json:"cognito:groups"`
	TokenUse      string   `json:"token_use"`
	ClientID      string   `json:"client_id"`
	BusinessID    string   `json:"custom:businessId"`
	Role          string   `json:"custom:role"`
	Picture       string   `json:"picture"`
	Name          string   `json:"name"`
}

type cognitoPool struct {
	userPoolID          string
	region              string
	issuer              string
	jwksURL             string
	ttl                 time.Duration
	canonicalizeSubject bool
	allowedClientIDs    map[string]struct{}
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
	return validateCognitoTokenWithAllowedTokenUses(cfg, tokenString, defaultAllowedTokenUses())
}

func ValidateCognitoAuthorization(cfg config.CognitoConfig, authHeader string) (*CognitoClaims, error) {
	tokenString, err := parseAuthorizationHeader(authHeader)
	if err != nil {
		return nil, err
	}
	return ValidateCognitoToken(cfg, tokenString)
}

func Auth(cfg config.CognitoConfig, log *logger.Logger) gin.HandlerFunc {
	return AuthWithTokenUse(cfg, log, TokenUseAccess)
}

func AuthWithTokenUse(cfg config.CognitoConfig, log *logger.Logger, allowedTokenUses ...TokenUse) gin.HandlerFunc {
	allowedUses := normalizeAllowedTokenUses(allowedTokenUses)
	return func(c *gin.Context) {
		reqLog := logger.FromContext(c.Request.Context()).Named("auth_middleware")
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" && websocketUpgradeRequested(c) {
			authHeader = c.Query("authorization")
			if authHeader == "" {
				if token := c.Query("access_token"); token != "" {
					authHeader = "Bearer " + token
				}
			}
		}
		tokenString, err := parseAuthorizationHeader(authHeader)
		if err != nil {
			reqLog.Warn("token validation failed", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		claims, err := validateCognitoTokenWithAllowedTokenUses(cfg, tokenString, allowedUses)
		if err != nil {
			reqLog.Warn("token validation failed", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Set("user_id", claims.Subject)
		c.Set("email", claims.Email)
		c.Set("email_verified", claims.EmailVerified)
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

func websocketUpgradeRequested(c *gin.Context) bool {
	return strings.EqualFold(c.GetHeader("Upgrade"), "websocket")
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

func GetEmailVerified(c *gin.Context) bool {
	if emailVerified, exists := c.Get("email_verified"); exists {
		return emailVerified.(bool)
	}
	return false
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
		newCognitoPool(cfg.Region, cfg.UserPoolID, cfg.ClientID, ttl, false),
	}

	if cfg.Phone.UserPoolID != "" && cfg.Phone.Region != "" {
		pools = append(pools, newCognitoPool(cfg.Phone.Region, cfg.Phone.UserPoolID, cfg.Phone.ClientID, ttl, true))
	}

	return pools
}

func newCognitoPool(region, userPoolID, clientID string, ttl time.Duration, canonicalizeSubject bool) cognitoPool {
	allowedClientIDs := map[string]struct{}{}
	if trimmed := strings.TrimSpace(clientID); trimmed != "" {
		allowedClientIDs[trimmed] = struct{}{}
	}
	return cognitoPool{
		userPoolID:          userPoolID,
		region:              region,
		issuer:              cognitoIssuer(region, userPoolID),
		jwksURL:             fmt.Sprintf("%s/.well-known/jwks.json", cognitoIssuer(region, userPoolID)),
		ttl:                 ttl,
		canonicalizeSubject: canonicalizeSubject,
		allowedClientIDs:    allowedClientIDs,
	}
}

func validateCognitoTokenWithAllowedTokenUses(cfg config.CognitoConfig, tokenString string, allowedTokenUses map[string]struct{}) (*CognitoClaims, error) {
	claims, _, err := validateCognitoTokenWithPools(tokenString, cognitoPoolsFromConfig(cfg), allowedTokenUses)
	return claims, err
}

func validateCognitoTokenWithPools(tokenString string, pools []cognitoPool, allowedTokenUses map[string]struct{}) (*CognitoClaims, cognitoPool, error) {
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
	if err := validateTokenUse(claims, allowedTokenUses); err != nil {
		return nil, cognitoPool{}, err
	}
	if err := validateClientBinding(claims, pool); err != nil {
		return nil, cognitoPool{}, err
	}
	if pool.canonicalizeSubject {
		claims.Subject = canonicalPhoneCognitoID(pool.userPoolID, claims.Subject)
	}

	return claims, pool, nil
}

func defaultAllowedTokenUses() map[string]struct{} {
	return normalizeAllowedTokenUses([]TokenUse{TokenUseAccess})
}

func normalizeAllowedTokenUses(allowedTokenUses []TokenUse) map[string]struct{} {
	allowed := make(map[string]struct{}, len(allowedTokenUses))
	for _, tokenUse := range allowedTokenUses {
		normalized := strings.ToLower(strings.TrimSpace(string(tokenUse)))
		if normalized == "" {
			continue
		}
		allowed[normalized] = struct{}{}
	}
	if len(allowed) == 0 {
		allowed[string(TokenUseAccess)] = struct{}{}
	}
	return allowed
}

func validateTokenUse(claims *CognitoClaims, allowedTokenUses map[string]struct{}) error {
	tokenUse := strings.ToLower(strings.TrimSpace(claims.TokenUse))
	if tokenUse == "" {
		return fmt.Errorf("token_use is required")
	}
	if _, ok := allowedTokenUses[tokenUse]; !ok {
		return fmt.Errorf("token_use %q is not allowed", tokenUse)
	}
	return nil
}

func validateClientBinding(claims *CognitoClaims, pool cognitoPool) error {
	if len(pool.allowedClientIDs) == 0 {
		return fmt.Errorf("no cognito client_id configured for issuer")
	}
	for _, clientID := range tokenClientIDs(claims) {
		if _, ok := pool.allowedClientIDs[clientID]; ok {
			return nil
		}
	}
	return fmt.Errorf("token client binding is invalid")
}

func tokenClientIDs(claims *CognitoClaims) []string {
	unique := make(map[string]struct{})
	var ids []string
	appendID := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, exists := unique[candidate]; exists {
			return
		}
		unique[candidate] = struct{}{}
		ids = append(ids, candidate)
	}
	appendID(claims.ClientID)
	for _, audience := range claims.Audience {
		appendID(audience)
	}
	return ids
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
