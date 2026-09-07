package ap2

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"invoice-backend/internal/models"
)

const CartMandateSignatureVersion = "billeif-ap2-cart-mandate-v1"

var ErrInvalidCartMandate = errors.New("invalid cart mandate")

type CartMandateClaims struct {
	Version         string     `json:"version"`
	SignerRole      string     `json:"signer_role"`
	MandateID       string     `json:"mandate_id"`
	IntentMandateID *string    `json:"intent_mandate_id,omitempty"`
	UserID          string     `json:"user_id"`
	AgentID         string     `json:"agent_id"`
	MerchantID      *string    `json:"merchant_id,omitempty"`
	Items           []CartItem `json:"items"`
	TotalAmount     string     `json:"total_amount"`
	Currency        string     `json:"currency"`
	ExpiresAt       string     `json:"expires_at"`
}

func NewCartMandateClaims(mandate *models.CartMandate, signerRole string) (*CartMandateClaims, error) {
	if mandate == nil || mandate.ID == "" || (signerRole != "buyer" && signerRole != "merchant") {
		return nil, ErrInvalidCartMandate
	}

	var items []CartItem
	if err := json.Unmarshal([]byte(mandate.Items), &items); err != nil {
		return nil, fmt.Errorf("%w: decode items: %v", ErrInvalidCartMandate, err)
	}
	if mandate.TotalAmount < 0 || mandate.Currency == "" || mandate.ExpiresAt.IsZero() {
		return nil, ErrInvalidCartMandate
	}
	if signerRole == "merchant" && (len(items) == 0 || mandate.TotalAmount <= 0) {
		return nil, ErrInvalidCartMandate
	}
	if signerRole == "merchant" && (mandate.MerchantID == nil || *mandate.MerchantID == "") {
		return nil, ErrInvalidCartMandate
	}

	return &CartMandateClaims{
		Version:         CartMandateSignatureVersion,
		SignerRole:      signerRole,
		MandateID:       mandate.ID,
		IntentMandateID: mandate.IntentMandateID,
		UserID:          mandate.UserID,
		AgentID:         mandate.AgentID,
		MerchantID:      mandate.MerchantID,
		Items:           items,
		TotalAmount:     strconv.FormatFloat(mandate.TotalAmount, 'f', 2, 64),
		Currency:        mandate.Currency,
		ExpiresAt:       mandate.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func SignCartMandate(signer *SignatureService, mandate *models.CartMandate, signerRole string) (string, string, error) {
	if signer == nil {
		return "", "", ErrSigningFailed
	}
	claims, err := NewCartMandateClaims(mandate, signerRole)
	if err != nil {
		return "", "", err
	}
	signature, err := signer.SignDataCanonical(claims)
	if err != nil {
		return "", "", err
	}
	return signature, signer.GetPublicKey(), nil
}

func VerifyCartMandateSignature(mandate *models.CartMandate, signerRole, signature, publicKey string) error {
	if signature == "" || publicKey == "" {
		return ErrInvalidSignature
	}
	claims, err := NewCartMandateClaims(mandate, signerRole)
	if err != nil {
		return err
	}
	verifier, err := NewSignatureService()
	if err != nil {
		return err
	}
	return verifier.VerifySignatureCanonicalWithPublicKey(claims, signature, publicKey)
}
