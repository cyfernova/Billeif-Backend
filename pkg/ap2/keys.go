package ap2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
)

var (
	ErrInvalidPEM     = errors.New("invalid PEM data")
	ErrInvalidKeyData = errors.New("invalid key data")
	ErrKeyGeneration  = errors.New("key generation failed")
)

type KeyPair struct {
	PrivateKey *ecdsa.PrivateKey
	PublicKey  *ecdsa.PublicKey
}

func GenerateKeyPair() (*KeyPair, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyGeneration, err)
	}

	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
	}, nil
}

func EncodePublicKey(publicKey *ecdsa.PublicKey) string {
	publicKeyBytes := elliptic.MarshalCompressed(publicKey, publicKey.X, publicKey.Y)
	return base64.StdEncoding.EncodeToString(publicKeyBytes)
}

func DecodePublicKey(publicKeyStr string) (*ecdsa.PublicKey, error) {
	publicKeyBytes, err := base64.StdEncoding.DecodeString(publicKeyStr)
	if err != nil {
		return nil, ErrInvalidKeyData
	}

	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), publicKeyBytes)
	if x == nil {
		return nil, ErrInvalidKeyData
	}

	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}, nil
}

func EncodePrivateKey(privateKey *ecdsa.PrivateKey) (string, error) {
	derBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", err
	}

	pemBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: derBytes,
	}

	pemBytes := pem.EncodeToMemory(pemBlock)
	return string(pemBytes), nil
}

func DecodePrivateKey(privateKeyStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privateKeyStr))
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, ErrInvalidPEM
	}

	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return privateKey, nil
}

func PublicKeysMatch(pub1, pub2 *ecdsa.PublicKey) bool {
	if pub1 == nil || pub2 == nil {
		return false
	}

	return pub1.X.Cmp(pub2.X) == 0 && pub1.Y.Cmp(pub2.Y) == 0
}
