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
	mu               sync.RWMutex
	keys             map[string]*rsa.PublicKey
	lastFetch        time.Time
	ttl              time.Duration
	jwksURL          string
	refreshing       bool
	refreshDone      chan struct{}
	lastRefreshError error
	lastMissRefresh  time.Time
}

const jwksUnknownKeyRefreshCooldown = 5 * time.Second

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
	return c.GetKeyContext(context.Background(), kid)
}

func (c *JWKSCache) GetKeyContext(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if c == nil || ctx == nil || kid == "" || len(kid) > 256 {
		return nil, fmt.Errorf("invalid JWKS key request")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	key, ok := c.keys[kid]
	wasMissing := !ok
	stale := time.Since(c.lastFetch) >= c.ttl
	missCoolingDown := !ok && !c.lastMissRefresh.IsZero() && time.Since(c.lastMissRefresh) < jwksUnknownKeyRefreshCooldown
	c.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}
	if missCoolingDown {
		return nil, fmt.Errorf("JWKS key not found")
	}

	if err := c.refreshContext(ctx, !ok); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	key, ok = c.keys[kid]
	c.mu.RUnlock()
	if ok {
		if wasMissing {
			c.clearMissRefresh()
		}
		return key, nil
	}
	// A successful forced refresh that still did not contain the requested
	// key proves this kid is currently unknown. Record one global short
	// cooldown so sequential random-kid JWTs cannot amplify JWKS traffic.
	return nil, fmt.Errorf("JWKS key not found")
}

func (c *JWKSCache) clearMissRefresh() {
	c.mu.Lock()
	c.lastMissRefresh = time.Time{}
	c.mu.Unlock()
}

func (c *JWKSCache) refreshContext(ctx context.Context, force bool) error {
	if c == nil || ctx == nil {
		return fmt.Errorf("invalid JWKS refresh request")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()

	if !force && time.Since(c.lastFetch) < c.ttl && len(c.keys) > 0 {
		c.mu.Unlock()
		return nil
	}
	if force && !c.lastMissRefresh.IsZero() && time.Since(c.lastMissRefresh) < jwksUnknownKeyRefreshCooldown {
		c.mu.Unlock()
		return nil
	}
	if c.refreshing {
		done := c.refreshDone
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			c.mu.RLock()
			err := c.lastRefreshError
			c.mu.RUnlock()
			return err
		}
	}
	if force {
		// Reserve the short forced-refresh window before network I/O. This
		// closes the handoff race between single-flight completion and the
		// requesting goroutine's key recheck.
		c.lastMissRefresh = time.Now()
	}
	c.refreshing = true
	c.refreshDone = make(chan struct{})
	done := c.refreshDone
	c.mu.Unlock()

	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return c.finishRefresh(done, nil, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.finishRefresh(done, nil, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return c.finishRefresh(done, nil, fmt.Errorf("fetch JWKS: unexpected status %d", resp.StatusCode))
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return c.finishRefresh(done, nil, err)
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
		return c.finishRefresh(done, nil, fmt.Errorf("fetch JWKS: no valid RSA keys found"))
	}
	return c.finishRefresh(done, refreshedKeys, nil)
}

func (c *JWKSCache) finishRefresh(done chan struct{}, refreshedKeys map[string]*rsa.PublicKey, refreshErr error) error {
	c.mu.Lock()
	if refreshErr == nil {
		// Replace the entire key map atomically so retired keys are dropped.
		c.keys = refreshedKeys
		c.lastFetch = time.Now()
	}
	c.lastRefreshError = refreshErr
	c.refreshing = false
	c.refreshDone = nil
	close(done)
	c.mu.Unlock()
	return refreshErr
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
	return ValidateCognitoTokenContext(context.Background(), cfg, tokenString)
}

func ValidateCognitoTokenContext(ctx context.Context, cfg config.CognitoConfig, tokenString string) (*CognitoClaims, error) {
	return validateCognitoTokenWithAllowedTokenUsesContext(ctx, cfg, tokenString, defaultAllowedTokenUses())
}

func ValidateCognitoAuthorization(cfg config.CognitoConfig, authHeader string) (*CognitoClaims, error) {
	return ValidateCognitoAuthorizationContext(context.Background(), cfg, authHeader)
}

func ValidateCognitoAuthorizationContext(ctx context.Context, cfg config.CognitoConfig, authHeader string) (*CognitoClaims, error) {
	if ctx == nil {
		return nil, fmt.Errorf("invalid authorization context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tokenString, err := parseAuthorizationHeader(authHeader)
	if err != nil {
		return nil, err
	}
	return ValidateCognitoTokenContext(ctx, cfg, tokenString)
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
		claims, err := validateCognitoTokenWithAllowedTokenUsesContext(c.Request.Context(), cfg, tokenString, allowedUses)
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

func validateCognitoTokenWithAllowedTokenUsesContext(ctx context.Context, cfg config.CognitoConfig, tokenString string, allowedTokenUses map[string]struct{}) (*CognitoClaims, error) {
	claims, _, err := validateCognitoTokenWithPoolsContext(ctx, tokenString, cognitoPoolsFromConfig(cfg), allowedTokenUses)
	return claims, err
}

func validateCognitoTokenWithPoolsContext(ctx context.Context, tokenString string, pools []cognitoPool, allowedTokenUses map[string]struct{}) (*CognitoClaims, cognitoPool, error) {
	if ctx == nil {
		return nil, cognitoPool{}, fmt.Errorf("invalid token validation context")
	}
	if err := ctx.Err(); err != nil {
		return nil, cognitoPool{}, err
	}
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
		return cache.GetKeyContext(ctx, kid)
	})
	if err != nil {
		return nil, cognitoPool{}, err
	}
	if err := ctx.Err(); err != nil {
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
