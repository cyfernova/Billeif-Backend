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

func NewJWKSCache(poolID, region string, ttl time.Duration) *JWKSCache {
	jwksURL := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s/.well-known/jwks.json", region, poolID)
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
	Email      string   `json:"email"`
	Username   string   `json:"cognito:username"`
	Groups     []string `json:"cognito:groups"`
	TokenUse   string   `json:"token_use"`
	BusinessID string   `json:"custom:businessId"`
	Role       string   `json:"custom:role"`
	Picture    string   `json:"picture"`
	Name       string   `json:"name"`
}

var jwksCache *JWKSCache

func Auth(cfg config.CognitoConfig, log *logger.Logger) gin.HandlerFunc {
	if jwksCache == nil {
		jwksCache = NewJWKSCache(cfg.UserPoolID, cfg.Region, cfg.JWKSRefreshRate)
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			return
		}

		tokenString := parts[1]
		token, err := jwt.ParseWithClaims(tokenString, &CognitoClaims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			kid, ok := token.Header["kid"].(string)
			if !ok {
				return nil, fmt.Errorf("kid not found in token header")
			}
			return jwksCache.GetKey(kid)
		})

		if err != nil {
			log.Warn("token validation failed", "error", err, "request_id", GetRequestID(c))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		claims, ok := token.Claims.(*CognitoClaims)
		if !ok || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
			return
		}

		if claims.TokenUse != "access" && claims.TokenUse != "id" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token type"})
			return
		}

		c.Set("user_id", claims.Subject)
		c.Set("email", claims.Email)
		c.Set("username", claims.Username)
		c.Set("groups", claims.Groups)
		c.Set("business_id", claims.BusinessID)
		c.Set("role", claims.Role)
		c.Set("picture", claims.Picture)
		c.Set("name", claims.Name)

		c.Next()
	}
}

func GetUserID(c *gin.Context) string {
	if id, exists := c.Get("user_id"); exists {
		if str, ok := id.(string); ok {
			return str
		}
	}
	return ""
}

func GetEmail(c *gin.Context) string {
	if email, exists := c.Get("email"); exists {
		if str, ok := email.(string); ok {
			return str
		}
	}
	return ""
}

func GetBusinessID(c *gin.Context) string {
	if id, exists := c.Get("business_id"); exists {
		if str, ok := id.(string); ok {
			return str
		}
	}
	return ""
}

func GetRole(c *gin.Context) string {
	if role, exists := c.Get("role"); exists {
		if str, ok := role.(string); ok {
			return str
		}
	}
	return "viewer"
}

func GetGroups(c *gin.Context) []string {
	if groups, exists := c.Get("groups"); exists {
		if groupSlice, ok := groups.([]string); ok {
			return groupSlice
		}
	}
	return nil
}

func GetPicture(c *gin.Context) string {
	if picture, exists := c.Get("picture"); exists {
		if str, ok := picture.(string); ok {
			return str
		}
	}
	return ""
}

func GetName(c *gin.Context) string {
	if name, exists := c.Get("name"); exists {
		if str, ok := name.(string); ok {
			return str
		}
	}
	return ""
}
