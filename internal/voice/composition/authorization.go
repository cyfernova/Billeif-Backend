package composition

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"

	"invoice-backend/internal/config"
	"invoice-backend/internal/middleware"
	"invoice-backend/internal/voice/webrtc"
)

const maxForwardedAuthorizationBytes = 8 << 10

type TokenIdentity struct {
	UserID     string
	BusinessID string
	ClientID   string
}

// TokenValidator is the context-aware cryptographic validation boundary. A
// production resolver uses the same Cognito issuer, key, token-use, and client
// binding checks as the authenticated HTTP API.
type TokenValidator interface {
	Validate(context.Context, string) (TokenIdentity, error)
}

type TokenValidatorFunc func(context.Context, string) (TokenIdentity, error)

func (validator TokenValidatorFunc) Validate(ctx context.Context, authorization string) (TokenIdentity, error) {
	if validator == nil {
		return TokenIdentity{}, webrtc.ErrSignalingUnauthorized
	}
	return validator(ctx, authorization)
}

type AuthorizationResolver struct {
	validator TokenValidator
}

func NewAuthorizationResolver(validator TokenValidator) (*AuthorizationResolver, error) {
	if nilAuthorizationInterface(validator) {
		return nil, ErrInvalidFactoryConfig
	}
	return &AuthorizationResolver{validator: validator}, nil
}

func NewCognitoAuthorizationResolver(cfg config.CognitoConfig) (*AuthorizationResolver, error) {
	if !validCognitoPool(cfg.Region, cfg.UserPoolID, cfg.ClientID) &&
		!validCognitoPool(cfg.Phone.Region, cfg.Phone.UserPoolID, cfg.Phone.ClientID) {
		return nil, ErrInvalidFactoryConfig
	}
	return NewAuthorizationResolver(&cognitoTokenValidator{config: cfg})
}

func (resolver *AuthorizationResolver) Resolve(ctx context.Context, authorization string) (identity webrtc.TrustedIdentity, resultErr error) {
	if ctx == nil {
		return webrtc.TrustedIdentity{}, webrtc.ErrSignalingUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return webrtc.TrustedIdentity{}, err
	}
	if resolver == nil || nilAuthorizationInterface(resolver.validator) || !validForwardedAuthorization(authorization) {
		return webrtc.TrustedIdentity{}, webrtc.ErrSignalingUnauthorized
	}
	defer func() {
		if recover() != nil {
			identity = webrtc.TrustedIdentity{}
			resultErr = webrtc.ErrSignalingUnauthorized
		}
	}()
	validated, err := resolver.validator.Validate(ctx, authorization)
	if contextErr := ctx.Err(); contextErr != nil {
		return webrtc.TrustedIdentity{}, contextErr
	}
	if err != nil || !validTokenIdentity(validated) {
		return webrtc.TrustedIdentity{}, webrtc.ErrSignalingUnauthorized
	}
	return webrtc.TrustedIdentity{
		UserID: validated.UserID, BusinessID: validated.BusinessID, ClientID: validated.ClientID,
	}, nil
}

func (*AuthorizationResolver) String() string   { return "voice authorization resolver{redacted}" }
func (*AuthorizationResolver) GoString() string { return "voice authorization resolver{redacted}" }
func (*AuthorizationResolver) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{}{})
}

type cognitoTokenValidator struct{ config config.CognitoConfig }

func (validator *cognitoTokenValidator) Validate(ctx context.Context, authorization string) (TokenIdentity, error) {
	if ctx == nil {
		return TokenIdentity{}, webrtc.ErrSignalingUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return TokenIdentity{}, err
	}
	claims, err := middleware.ValidateCognitoAuthorizationContext(ctx, validator.config, authorization)
	if err != nil {
		return TokenIdentity{}, webrtc.ErrSignalingUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return TokenIdentity{}, err
	}
	return TokenIdentity{
		UserID: claims.Subject, BusinessID: claims.BusinessID, ClientID: claims.ClientID,
	}, nil
}

func validForwardedAuthorization(value string) bool {
	if len(value) < len("Bearer ")+1 || len(value) > maxForwardedAuthorizationBytes || !utf8.ValidString(value) {
		return false
	}
	separator := strings.IndexByte(value, ' ')
	if separator <= 0 || !strings.EqualFold(value[:separator], "Bearer") {
		return false
	}
	token := value[separator+1:]
	if token == "" {
		return false
	}
	for index := 0; index < len(token); index++ {
		if token[index] < 0x21 || token[index] > 0x7e {
			return false
		}
	}
	return true
}

func validTokenIdentity(identity TokenIdentity) bool {
	return safeAuthorizationClaim(identity.UserID) && safeAuthorizationClaim(identity.BusinessID) && safeAuthorizationClaim(identity.ClientID)
}

func safeAuthorizationClaim(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func validCognitoPool(region, poolID, clientID string) bool {
	return safeAuthorizationClaim(region) && safeAuthorizationClaim(poolID) && safeAuthorizationClaim(clientID)
}

func nilAuthorizationInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ webrtc.AuthorizationResolver = (*AuthorizationResolver)(nil)
