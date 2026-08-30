package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"
)

type cartSecurityRepository struct {
	interfaces.AP2Repository
	cart *models.CartMandate
}

type merchantTaskAuthorizationRepository struct {
	interfaces.AP2Repository
}

func (*merchantTaskAuthorizationRepository) HasAgentOwnership(context.Context, string, string) (bool, error) {
	return false, nil
}

func TestMerchantCartTaskRejectsNonOwner(t *testing.T) {
	signer, err := ap2.NewSignatureService()
	if err != nil {
		t.Fatal(err)
	}
	repo := &merchantTaskAuthorizationRepository{}
	svc := &A2ATaskService{
		ap2Repo: repo, merchantSvc: &MerchantAgentService{}, signer: signer,
	}
	task := a2a.NewTask("", "attacker-user", "business")
	req := &a2a.SendMessageRequest{Metadata: map[string]interface{}{
		"taskType": "merchant.process_cart", "cartMandateId": "cart", "merchantAgentId": "merchant",
	}}

	_, _, err = svc.executeTask(context.Background(), task, req)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func (r *cartSecurityRepository) GetCartMandateByID(context.Context, string, string) (*models.CartMandate, error) {
	return r.cart, nil
}

func TestCompleteCheckoutRejectsUnsignedCartBeforePersistence(t *testing.T) {
	repo := &cartSecurityRepository{cart: securityTestCart(t)}
	signer, err := ap2.NewSignatureService()
	if err != nil {
		t.Fatal(err)
	}
	svc := &ShoppingAgentService{ap2Repo: repo, signer: signer, log: logger.New()}

	_, err = svc.CompleteCheckout(context.Background(), &CheckoutRequest{
		CartMandateID: repo.cart.ID,
		UserID:        repo.cart.UserID,
	})
	if !errors.Is(err, ErrCartMandateUnsigned) {
		t.Fatalf("expected unsigned error, got %v", err)
	}
}

func TestCompleteCheckoutRejectsReplayedCartBeforePersistence(t *testing.T) {
	cart := securityTestCart(t)
	signer, err := ap2.NewSignatureService()
	if err != nil {
		t.Fatal(err)
	}
	cart.Signature, cart.SignaturePublicKey, err = ap2.SignCartMandate(signer, cart, "buyer")
	if err != nil {
		t.Fatal(err)
	}
	merchantSignature, merchantKey, err := ap2.SignCartMandate(signer, cart, "merchant")
	if err != nil {
		t.Fatal(err)
	}
	cart.MerchantSignature = &merchantSignature
	cart.MerchantSignaturePublicKey = &merchantKey
	cart.Status = "signed"
	cart.PaymentMandates = []models.PaymentMandate{{ID: "existing-payment"}}

	repo := &cartSecurityRepository{cart: cart}
	svc := &ShoppingAgentService{ap2Repo: repo, signer: signer, log: logger.New()}
	_, err = svc.CompleteCheckout(context.Background(), &CheckoutRequest{CartMandateID: cart.ID, UserID: cart.UserID})
	if !errors.Is(err, ErrCartAlreadyProcessed) {
		t.Fatalf("expected replay error, got %v", err)
	}
}

func TestCompleteCheckoutRejectsSignaturesFromUntrustedKey(t *testing.T) {
	cart := securityTestCart(t)
	trustedSigner, err := ap2.NewSignatureService()
	if err != nil {
		t.Fatal(err)
	}
	rogueSigner, err := ap2.NewSignatureService()
	if err != nil {
		t.Fatal(err)
	}
	cart.Signature, cart.SignaturePublicKey, err = ap2.SignCartMandate(rogueSigner, cart, "buyer")
	if err != nil {
		t.Fatal(err)
	}
	merchantSignature, merchantKey, err := ap2.SignCartMandate(rogueSigner, cart, "merchant")
	if err != nil {
		t.Fatal(err)
	}
	cart.MerchantSignature = &merchantSignature
	cart.MerchantSignaturePublicKey = &merchantKey
	cart.Status = "signed"

	svc := &ShoppingAgentService{ap2Repo: &cartSecurityRepository{cart: cart}, signer: trustedSigner, log: logger.New()}
	_, err = svc.CompleteCheckout(context.Background(), &CheckoutRequest{CartMandateID: cart.ID, UserID: cart.UserID})
	if !errors.Is(err, ErrCartSignatureInvalid) {
		t.Fatalf("expected untrusted key error, got %v", err)
	}
}

func securityTestCart(t *testing.T) *models.CartMandate {
	t.Helper()
	merchantID := "22222222-2222-2222-2222-222222222222"
	return &models.CartMandate{
		ID: "33333333-3333-3333-3333-333333333333", UserID: "44444444-4444-4444-4444-444444444444",
		AgentID: "55555555-5555-5555-5555-555555555555", MerchantID: &merchantID,
		Items:       `[{"product_id":"66666666-6666-6666-6666-666666666666","name":"Widget","quantity":1,"unit_price":10}]`,
		TotalAmount: 10, Currency: "INR", Status: "pending", ExpiresAt: time.Now().Add(time.Hour),
	}
}
