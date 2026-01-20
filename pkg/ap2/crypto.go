package ap2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

var (
	ErrInvalidSignature = errors.New("invalid signature")
	ErrInvalidKey       = errors.New("invalid key")
	ErrSigningFailed    = errors.New("signing operation failed")
)

type SignatureService struct {
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey
}

func NewSignatureService() (*SignatureService, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, ErrSigningFailed
	}

	return &SignatureService{
		privateKey: privateKey,
		publicKey:  &privateKey.PublicKey,
	}, nil
}

func NewSignatureServiceFromKey(privateKey *ecdsa.PrivateKey) (*SignatureService, error) {
	if privateKey == nil {
		return nil, ErrInvalidKey
	}

	return &SignatureService{
		privateKey: privateKey,
		publicKey:  &privateKey.PublicKey,
	}, nil
}

func (s *SignatureService) SignData(data []byte) (string, error) {
	hash := sha256.Sum256(data)
	signature, err := ecdsa.SignASN1(rand.Reader, s.privateKey, hash[:])
	if err != nil {
		return "", ErrSigningFailed
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

func (s *SignatureService) VerifySignature(data []byte, signature string) error {
	hash := sha256.Sum256(data)
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return ErrInvalidSignature
	}

	valid := ecdsa.VerifyASN1(s.publicKey, hash[:], sigBytes)
	if !valid {
		return ErrInvalidSignature
	}

	return nil
}

func (s *SignatureService) VerifySignatureWithPublicKey(data []byte, signature string, publicKeyStr string) error {
	publicKey, err := DecodePublicKey(publicKeyStr)
	if err != nil {
		return err
	}

	hash := sha256.Sum256(data)
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return ErrInvalidSignature
	}

	valid := ecdsa.VerifyASN1(publicKey, hash[:], sigBytes)
	if !valid {
		return ErrInvalidSignature
	}

	return nil
}

func (s *SignatureService) GetPrivateKey() *ecdsa.PrivateKey {
	return s.privateKey
}

func (s *SignatureService) GetPublicKey() string {
	publicKeyBytes := elliptic.MarshalCompressed(s.publicKey, s.publicKey.X, s.publicKey.Y)
	return base64.StdEncoding.EncodeToString(publicKeyBytes)
}

// SignDataCanonical signs data after canonicalizing it per RFC 8785
// This ensures deterministic signatures across different JSON representations
func (s *SignatureService) SignDataCanonical(data interface{}) (string, error) {
	canonical, err := CanonicalizeJSON(data)
	if err != nil {
		return "", fmt.Errorf("canonicalization failed: %w", err)
	}

	return s.SignData(canonical)
}

// VerifySignatureCanonical verifies a signature against canonicalized data
func (s *SignatureService) VerifySignatureCanonical(data interface{}, signature string) error {
	canonical, err := CanonicalizeJSON(data)
	if err != nil {
		return fmt.Errorf("canonicalization failed: %w", err)
	}

	return s.VerifySignature(canonical, signature)
}

// VerifySignatureCanonicalWithPublicKey verifies a signature with a public key against canonicalized data
func (s *SignatureService) VerifySignatureCanonicalWithPublicKey(data interface{}, signature string, publicKeyStr string) error {
	canonical, err := CanonicalizeJSON(data)
	if err != nil {
		return fmt.Errorf("canonicalization failed: %w", err)
	}

	return s.VerifySignatureWithPublicKey(canonical, signature, publicKeyStr)
}
