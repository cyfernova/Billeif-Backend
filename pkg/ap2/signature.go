package ap2

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type MandateSigner struct {
	signatureService *SignatureService
}

func NewMandateSigner(signatureService *SignatureService) *MandateSigner {
	return &MandateSigner{
		signatureService: signatureService,
	}
}

func (ms *MandateSigner) SignIntentMandate(mandate interface{}) (string, error) {
	data, err := json.Marshal(mandate)
	if err != nil {
		return "", fmt.Errorf("failed to marshal mandate: %w", err)
	}

	return ms.signatureService.SignData(data)
}

func (ms *MandateSigner) SignCartMandate(mandate interface{}) (string, error) {
	data, err := json.Marshal(mandate)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cart mandate: %w", err)
	}

	return ms.signatureService.SignData(data)
}

func (ms *MandateSigner) SignPaymentMandate(mandate interface{}) (string, error) {
	data, err := json.Marshal(mandate)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payment mandate: %w", err)
	}

	return ms.signatureService.SignData(data)
}

type MandateVerifier struct{}

func NewMandateVerifier() *MandateVerifier {
	return &MandateVerifier{}
}

func (mv *MandateVerifier) VerifyIntentMandate(mandate interface{}, signature, publicKeyStr string) error {
	data, err := json.Marshal(mandate)
	if err != nil {
		return fmt.Errorf("failed to marshal mandate: %w", err)
	}

	signatureService, err := NewSignatureService()
	if err != nil {
		return fmt.Errorf("failed to create signature service: %w", err)
	}

	return signatureService.VerifySignatureWithPublicKey(data, signature, publicKeyStr)
}

func (mv *MandateVerifier) VerifyCartMandate(mandate interface{}, signature, publicKeyStr string) error {
	data, err := json.Marshal(mandate)
	if err != nil {
		return fmt.Errorf("failed to marshal cart mandate: %w", err)
	}

	signatureService, err := NewSignatureService()
	if err != nil {
		return fmt.Errorf("failed to create signature service: %w", err)
	}

	return signatureService.VerifySignatureWithPublicKey(data, signature, publicKeyStr)
}

func (mv *MandateVerifier) VerifyPaymentMandate(mandate interface{}, signature, publicKeyStr string) error {
	data, err := json.Marshal(mandate)
	if err != nil {
		return fmt.Errorf("failed to marshal payment mandate: %w", err)
	}

	signatureService, err := NewSignatureService()
	if err != nil {
		return fmt.Errorf("failed to create signature service: %w", err)
	}

	return signatureService.VerifySignatureWithPublicKey(data, signature, publicKeyStr)
}

func HashMandate(mandate interface{}) ([32]byte, error) {
	data, err := json.Marshal(mandate)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to marshal mandate: %w", err)
	}

	return sha256.Sum256(data), nil
}

func VerifySignatureWithKey(publicKey *ecdsa.PublicKey, data []byte, signature string) error {
	hash := sha256.Sum256(data)
	sigBytes, err := DecodeSignature(signature)
	if err != nil {
		return err
	}

	valid := ecdsa.VerifyASN1(publicKey, hash[:], sigBytes)
	if !valid {
		return ErrInvalidSignature
	}

	return nil
}

func DecodeSignature(signature string) ([]byte, error) {
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return nil, ErrInvalidSignature
	}
	return sigBytes, nil
}

func EncodeSignature(signature []byte) string {
	return base64.StdEncoding.EncodeToString(signature)
}
