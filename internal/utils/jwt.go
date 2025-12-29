package utils

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type JWKS struct {
	Keys []JWKKey `json:"keys"`
}

type JWKKey struct {
	Alg string `json:"alg"`
	E   string `json:"e"`
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	Use string `json:"use"`
}

type JWKSClient struct {
	jwksURL   string
	cache     map[string]*rsa.PublicKey
	mutex     sync.RWMutex
	ttl       time.Duration
	lastFetch time.Time
}

func NewJWKSClient(jwksURL string, ttl time.Duration) *JWKSClient {
	return &JWKSClient{
		jwksURL: jwksURL,
		cache:   make(map[string]*rsa.PublicKey),
		ttl:     ttl,
	}
}

func (c *JWKSClient) GetKey(kid string) (*rsa.PublicKey, error) {
	c.mutex.RLock()
	if key, ok := c.cache[kid]; ok && time.Since(c.lastFetch) < c.ttl {
		c.mutex.RUnlock()
		return key, nil
	}
	c.mutex.RUnlock()

	if err := c.refresh(); err != nil {
		return nil, err
	}

	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if key, ok := c.cache[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key with kid %s not found", kid)
}

func (c *JWKSClient) refresh() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if time.Since(c.lastFetch) < c.ttl && len(c.cache) > 0 {
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

	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}

		pubKey, err := parseRSAKey(key)
		if err != nil {
			continue
		}
		c.cache[key.Kid] = pubKey
	}

	c.lastFetch = time.Now()
	return nil
}

func parseRSAKey(key JWKKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, err
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
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

func ValidateToken(tokenString string, jwksClient *JWKSClient) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid not found in token header")
		}

		return jwksClient.GetKey(kid)
	})
}
