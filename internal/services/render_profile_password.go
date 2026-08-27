package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"invoice-backend/internal/config"
)

const (
	// v1 identifies the envelope and AAD format, not a key generation. Like the
	// repository's other credential-encryption consumers, key rotation requires
	// a coordinated re-encryption before replacing the single configured key.
	renderProfilePasswordEnvelopePrefix = "rpw:v1:"
	renderProfilePasswordDomain         = "invoice-backend/render-profile-password/v1"
)

func (s *DocumentService) encryptRenderProfilePassword(
	ctx context.Context,
	businessID, profileID, plaintext string,
) (string, error) {
	gcm, err := s.renderProfilePasswordGCM(ctx)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate render profile password nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), renderProfilePasswordAAD(businessID, profileID))
	payload := append(nonce, sealed...)
	return renderProfilePasswordEnvelopePrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (s *DocumentService) decryptRenderProfilePassword(
	ctx context.Context,
	businessID, profileID, envelope string,
) (string, error) {
	if !strings.HasPrefix(envelope, renderProfilePasswordEnvelopePrefix) {
		return "", errors.New("render profile password ciphertext has an unsupported version")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(envelope, renderProfilePasswordEnvelopePrefix))
	if err != nil {
		return "", errors.New("render profile password ciphertext is invalid")
	}
	gcm, err := s.renderProfilePasswordGCM(ctx)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("render profile password ciphertext is invalid")
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, renderProfilePasswordAAD(businessID, profileID))
	if err != nil {
		return "", errors.New("render profile password authentication failed")
	}
	return string(plaintext), nil
}

func (s *DocumentService) renderProfilePasswordGCM(ctx context.Context) (cipher.AEAD, error) {
	key, err := s.renderProfilePasswordKey(ctx)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("initialize render profile password cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("initialize render profile password authentication")
	}
	return gcm, nil
}

func (s *DocumentService) renderProfilePasswordKey(ctx context.Context) ([]byte, error) {
	if s.cfg == nil {
		return nil, errors.New("application config is required for render profile password encryption")
	}
	runtimeConfig := s.cfg
	if s.resolver != nil {
		resolved, err := s.resolver.ResolveProvider(ctx, s.cfg, config.SecretCredentialEncryption)
		if err != nil {
			return nil, fmt.Errorf("resolve render profile password encryption key: %w", err)
		}
		runtimeConfig = resolved
	}
	key, err := base64.StdEncoding.DecodeString(runtimeConfig.Credentials.EncryptionKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("render profile password encryption key is invalid")
	}
	return key, nil
}

func renderProfilePasswordAAD(businessID, profileID string) []byte {
	return []byte(fmt.Sprintf(
		"%s\x00%d:%s\x00%d:%s",
		renderProfilePasswordDomain,
		len(businessID),
		businessID,
		len(profileID),
		profileID,
	))
}
