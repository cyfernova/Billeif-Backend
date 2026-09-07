package ap2

import (
	"testing"
	"time"

	"invoice-backend/internal/models"
)

func TestCartMandateSignatureBindsSecurityCriticalFields(t *testing.T) {
	merchantID := "22222222-2222-2222-2222-222222222222"
	mandate := &models.CartMandate{
		ID:          "33333333-3333-3333-3333-333333333333",
		UserID:      "44444444-4444-4444-4444-444444444444",
		AgentID:     "55555555-5555-5555-5555-555555555555",
		MerchantID:  &merchantID,
		Items:       `[{"product_id":"66666666-6666-6666-6666-666666666666","name":"Widget","quantity":2,"unit_price":50}]`,
		TotalAmount: 100,
		Currency:    "INR",
		ExpiresAt:   time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC),
	}
	signer, err := NewSignatureService()
	if err != nil {
		t.Fatal(err)
	}
	signature, publicKey, err := SignCartMandate(signer, mandate, "buyer")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCartMandateSignature(mandate, "buyer", signature, publicKey); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	tests := map[string]func(*models.CartMandate){
		"amount": func(m *models.CartMandate) { m.TotalAmount = 100.01 },
		"items": func(m *models.CartMandate) {
			m.Items = `[{"product_id":"66666666-6666-6666-6666-666666666666","name":"Widget","quantity":3,"unit_price":50}]`
		},
		"agent": func(m *models.CartMandate) { m.AgentID = "77777777-7777-7777-7777-777777777777" },
		"merchant": func(m *models.CartMandate) {
			changed := "88888888-8888-8888-8888-888888888888"
			m.MerchantID = &changed
		},
		"user":       func(m *models.CartMandate) { m.UserID = "99999999-9999-9999-9999-999999999999" },
		"expiration": func(m *models.CartMandate) { m.ExpiresAt = m.ExpiresAt.Add(time.Second) },
		"mandate ID": func(m *models.CartMandate) { m.ID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := *mandate
			mutate(&changed)
			if err := VerifyCartMandateSignature(&changed, "buyer", signature, publicKey); err == nil {
				t.Fatal("tampered mandate accepted")
			}
		})
	}
	if err := VerifyCartMandateSignature(mandate, "merchant", signature, publicKey); err == nil {
		t.Fatal("signature accepted for wrong signer role")
	}
}

func TestCartMandateSignatureRejectsMissingOrMalformedValues(t *testing.T) {
	merchantID := "22222222-2222-2222-2222-222222222222"
	mandate := &models.CartMandate{
		ID: "33333333-3333-3333-3333-333333333333", UserID: "user", AgentID: "agent",
		MerchantID: &merchantID, Items: `[{"product_id":"product","name":"Widget","quantity":1,"unit_price":1}]`,
		TotalAmount: 1, Currency: "INR", ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := VerifyCartMandateSignature(mandate, "buyer", "", ""); err == nil {
		t.Fatal("missing signature and key accepted")
	}
	if err := VerifyCartMandateSignature(mandate, "buyer", "not-base64", "not-a-key"); err == nil {
		t.Fatal("malformed signature and key accepted")
	}
}
