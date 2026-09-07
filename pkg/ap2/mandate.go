package ap2

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"invoice-backend/internal/models"
)

var (
	ErrMandateExpired     = errors.New("mandate has expired")
	ErrMandateRevoked     = errors.New("mandate has been revoked")
	ErrInvalidConstraint  = errors.New("invalid constraint")
	ErrConstraintViolated = errors.New("constraint violated")
)

type MandateConstraints struct {
	MaxAmount         *float64     `json:"max_amount,omitempty"`
	Currency          string       `json:"currency"`
	AllowedCategories []string     `json:"allowed_categories,omitempty"`
	MaxItems          *int         `json:"max_items,omitempty"`
	AllowedMerchants  []string     `json:"allowed_merchants,omitempty"`
	TimeWindows       []TimeWindow `json:"time_windows,omitempty"`
}

type TimeWindow struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Days      []int  `json:"days"`
}

type IntentMandateRequest struct {
	UserID                string             `json:"user_id"`
	AgentID               string             `json:"agent_id"`
	NaturalLanguageIntent string             `json:"natural_language_intent"`
	Constraints           MandateConstraints `json:"constraints"`
	ExpirationDuration    time.Duration      `json:"expiration_duration"`
}

type CartMandateRequest struct {
	IntentMandateID    *string       `json:"intent_mandate_id,omitempty"`
	UserID             string        `json:"user_id"`
	AgentID            string        `json:"agent_id"`
	MerchantID         *string       `json:"merchant_id,omitempty"`
	Items              []CartItem    `json:"items"`
	Signature          string        `json:"signature"`
	ExpirationDuration time.Duration `json:"expiration_duration"`
}

type PaymentMandateRequest struct {
	CartMandateID   string  `json:"cart_mandate_id"`
	UserID          string  `json:"user_id"`
	PaymentMethodID *string `json:"payment_method_id,omitempty"`
	Signature       string  `json:"signature"`
}

type CartItem struct {
	ProductID   string  `json:"product_id"`
	Name        string  `json:"name"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	TaxRate     float64 `json:"tax_rate,omitempty"`
	TaxAmount   float64 `json:"tax_amount,omitempty"`
	LineTotal   float64 `json:"line_total,omitempty"`
	Description *string `json:"description,omitempty"`
}

type MandateService struct {
	signer   *MandateSigner
	verifier *MandateVerifier
}

func NewMandateService(signer *MandateSigner, verifier *MandateVerifier) *MandateService {
	return &MandateService{
		signer:   signer,
		verifier: verifier,
	}
}

func (ms *MandateService) CreateIntentMandate(req *IntentMandateRequest, publicKey string) (*models.IntentMandate, error) {
	if err := ms.validateConstraints(&req.Constraints); err != nil {
		return nil, err
	}

	constraintsJSON, err := json.Marshal(req.Constraints)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal constraints: %w", err)
	}

	signature, err := ms.signer.SignIntentMandate(map[string]interface{}{
		"user_id":                 req.UserID,
		"agent_id":                req.AgentID,
		"constraints":             req.Constraints,
		"natural_language_intent": req.NaturalLanguageIntent,
		"expires_at":              time.Now().Add(req.ExpirationDuration).UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sign mandate: %w", err)
	}

	intentMandate := &models.IntentMandate{
		UserID:                req.UserID,
		AgentID:               req.AgentID,
		Constraints:           string(constraintsJSON),
		NaturalLanguageIntent: req.NaturalLanguageIntent,
		Signature:             signature,
		PublicKey:             &publicKey,
		ExpiresAt:             time.Now().Add(req.ExpirationDuration).UTC(),
		Status:                "active",
	}

	return intentMandate, nil
}

func (ms *MandateService) CreateCartMandate(req *CartMandateRequest) (*models.CartMandate, error) {
	itemsJSON, err := json.Marshal(req.Items)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal items: %w", err)
	}

	var totalAmount float64
	for _, item := range req.Items {
		lineTotal := item.LineTotal
		if lineTotal <= 0 {
			lineTotal = float64(item.Quantity) * item.UnitPrice
		}
		totalAmount += lineTotal
	}

	cartMandate := &models.CartMandate{
		IntentMandateID: req.IntentMandateID,
		UserID:          req.UserID,
		AgentID:         req.AgentID,
		MerchantID:      req.MerchantID,
		Items:           string(itemsJSON),
		SubtotalAmount:  totalAmount,
		TotalAmount:     totalAmount,
		Currency:        "INR",
		Signature:       req.Signature,
		Status:          "pending",
		ExpiresAt:       time.Now().Add(req.ExpirationDuration).UTC(),
	}

	return cartMandate, nil
}

func (ms *MandateService) CreatePaymentMandate(req *PaymentMandateRequest, amount float64) (*models.PaymentMandate, error) {
	paymentMandate := &models.PaymentMandate{
		CartMandateID:   req.CartMandateID,
		UserID:          req.UserID,
		PaymentMethodID: req.PaymentMethodID,
		Amount:          amount,
		Currency:        "INR",
		Signature:       req.Signature,
		Status:          "pending",
	}

	return paymentMandate, nil
}

func (ms *MandateService) ValidateIntentMandate(mandate *models.IntentMandate) error {
	if time.Now().After(mandate.ExpiresAt) {
		return ErrMandateExpired
	}

	if mandate.Status != "active" {
		return ErrMandateRevoked
	}

	var constraints MandateConstraints
	if err := json.Unmarshal([]byte(mandate.Constraints), &constraints); err != nil {
		return fmt.Errorf("failed to unmarshal constraints: %w", err)
	}

	if err := ms.validateConstraints(&constraints); err != nil {
		return err
	}

	return nil
}

func (ms *MandateService) ValidateCartAgainstIntent(cart *models.CartMandate, intent *models.IntentMandate) error {
	var constraints MandateConstraints
	if err := json.Unmarshal([]byte(intent.Constraints), &constraints); err != nil {
		return fmt.Errorf("failed to unmarshal constraints: %w", err)
	}

	if constraints.MaxAmount != nil && cart.TotalAmount > *constraints.MaxAmount {
		return ErrConstraintViolated
	}

	var items []CartItem
	if err := json.Unmarshal([]byte(cart.Items), &items); err != nil {
		return fmt.Errorf("failed to unmarshal items: %w", err)
	}

	totalItems := 0
	for _, item := range items {
		totalItems += item.Quantity
	}

	if constraints.MaxItems != nil && totalItems > *constraints.MaxItems {
		return ErrConstraintViolated
	}

	return nil
}

func (ms *MandateService) validateConstraints(constraints *MandateConstraints) error {
	if constraints.Currency == "" {
		constraints.Currency = "INR"
	}

	if constraints.MaxAmount != nil && *constraints.MaxAmount <= 0 {
		return ErrInvalidConstraint
	}

	if constraints.MaxItems != nil && *constraints.MaxItems <= 0 {
		return ErrInvalidConstraint
	}

	return nil
}

func (ms *MandateService) IsTimeWindowValid() bool {
	return true
}
