package middleware

import (
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

const (
	maxTokenSizeBytes = 8192 // 8KB max token size to prevent DoS
	minHeaderParts  = 2      // "Bearer <token>"
)

// ValidateJWTFormat performs basic format validation before attempting cryptographic validation
// This helps prevent DoS attacks and catches malformed tokens early
func ValidateJWTFormat(tokenString string) error {
	if tokenString == "" {
		return fmt.Errorf("token is empty")
	}

	// Check token size to prevent excessive memory allocation (CVE-2025-30204)
	if len(tokenString) > maxTokenSizeBytes {
		return fmt.Errorf("token exceeds maximum size of %d bytes", maxTokenSizeBytes)
	}

	// JWT has 3 parts separated by dots
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Check that all parts are non-empty
	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("invalid JWT format: part %d is empty", i)
		}
	}

	return nil
}

// ValidateTokenSigningMethod ensures the token uses RSA signing
func ValidateTokenSigningMethod(token *jwt.Token) error {
	if token.Method == nil {
		return fmt.Errorf("token method is nil")
	}

	signingMethod, ok := token.Method.(*jwt.SigningMethodRSA)
	if !ok {
		return fmt.Errorf("unexpected signing method: %v, expected RSA", token.Header["alg"])
	}

	if signingMethod == nil {
		return fmt.Errorf("RSA signing method is nil")
	}

	return nil
}

// ValidateTokenHeader checks required header fields
func ValidateTokenHeader(header map[string]interface{}) error {
	if header == nil {
		return fmt.Errorf("token header is nil")
	}

	// Check for alg claim
	alg, exists := header["alg"]
	if !exists || alg == nil {
		return fmt.Errorf("missing or nil 'alg' in token header")
	}

	algStr, ok := alg.(string)
	if !ok {
		return fmt.Errorf("'alg' in token header is not a string")
	}

	if algStr != "RS256" && algStr != "RS384" && algStr != "RS512" {
		return fmt.Errorf("unsupported algorithm: %s", algStr)
	}

	// Check for typ claim (optional but good practice)
	if typ, exists := header["typ"]; exists {
		typStr, ok := typ.(string)
		if !ok {
			return fmt.Errorf("'typ' in token header is not a string")
		}

		if typStr != "" && typStr != "JWT" {
			return fmt.Errorf("unexpected typ: %s, expected JWT", typStr)
		}
	}

	// Check for kid (required for JWKS lookup)
	kid, exists := header["kid"]
	if !exists || kid == nil {
		return fmt.Errorf("missing or nil 'kid' in token header")
	}

	_, ok := kid.(string)
	if !ok {
		return fmt.Errorf("'kid' in token header is not a string")
	}

	return nil
}

// ValidateClaims ensures required claims are present and valid
func ValidateClaims(claims *CognitoClaims) error {
	if claims == nil {
		return fmt.Errorf("claims are nil")
	}

	// Validate Subject (user ID)
	if claims.Subject == "" {
		return fmt.Errorf("missing 'sub' claim")
	}

	// Validate Token Use
	if claims.TokenUse == "" {
		return fmt.Errorf("missing 'token_use' claim")
	}

	if claims.TokenUse != "access" && claims.TokenUse != "id" {
		return fmt.Errorf("invalid token_use: %s, expected 'access' or 'id'", claims.TokenUse)
	}

	// Validate Email (required for our app)
	if claims.Email == "" {
		return fmt.Errorf("missing 'email' claim")
	}

	// Validate Username
	if claims.Username == "" {
		return fmt.Errorf("missing 'cognito:username' claim")
	}

	// Validate expiration (jwt.RegisteredClaims includes this)
	if claims.ExpiresAt == nil {
		return fmt.Errorf("missing 'exp' claim")
	}

	// Validate Issued At (good practice)
	if claims.IssuedAt == nil {
		return fmt.Errorf("missing 'iat' claim")
	}

	return nil
}
