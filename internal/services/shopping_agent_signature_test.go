package services

import (
	"context"
	"errors"
	"sync"
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
	mu      sync.Mutex
	cart    *models.CartMandate
	product *models.MarketplaceProduct
	agent   *models.Agent
}

type cartProductRepository struct {
	interfaces.ProductRepository
	product *models.Product
}

func (r *cartProductRepository) GetByID(context.Context, string, string) (*models.Product, error) {
	return r.product, nil
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
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *r.cart
	return &clone, nil
}

func (r *cartSecurityRepository) GetCartMandateForScope(_ context.Context, _, userID, businessID string) (*models.CartMandate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cart.UserID != userID || r.cart.BusinessID != businessID {
		return nil, ErrCartMandateNotFound
	}
	clone := *r.cart
	return &clone, nil
}

func (r *cartSecurityRepository) UpdateCartMandateVersioned(_ context.Context, cart *models.CartMandate, expectedVersion int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cart.Version != expectedVersion || r.cart.Status != "pending" {
		return errors.New("cart version or state conflict")
	}
	clone := *cart
	clone.Version = expectedVersion + 1
	r.cart = &clone
	cart.Version = expectedVersion + 1
	return nil
}

func (r *cartSecurityRepository) GetMarketplaceProductByID(context.Context, string) (*models.MarketplaceProduct, error) {
	if r.product == nil {
		return nil, errors.New("product not found")
	}
	return r.product, nil
}

func (r *cartSecurityRepository) GetAgentByID(context.Context, string) (*models.Agent, error) {
	if r.agent == nil {
		return nil, errors.New("agent not found")
	}
	return r.agent, nil
}

func TestCartMutationEnforcesTenantVersionAndAuthoritativeTotals(t *testing.T) {
	backingID := "77777777-7777-4777-8777-777777777777"
	repo := &cartSecurityRepository{
		cart: &models.CartMandate{
			ID: "33333333-3333-4333-8333-333333333333", BusinessID: "11111111-1111-4111-8111-111111111111",
			UserID: "44444444-4444-4444-8444-444444444444", AgentID: "55555555-5555-4555-8555-555555555555",
			Items:    `[{"product_id":"66666666-6666-4666-8666-666666666666","quantity":1}]`,
			Currency: "INR", Status: "pending", Version: 2, ExpiresAt: time.Now().Add(time.Hour),
		},
		product: &models.MarketplaceProduct{
			ID: "66666666-6666-4666-8666-666666666666", AgentID: "88888888-8888-4888-8888-888888888888",
			ProductID: &backingID, Name: "Authoritative widget", Price: 125, Currency: "INR",
			InventoryCount: 10, IsAvailable: true,
		},
		agent: &models.Agent{ID: "88888888-8888-4888-8888-888888888888", BusinessID: "99999999-9999-4999-8999-999999999999"},
	}
	signer, err := ap2.NewSignatureService()
	if err != nil {
		t.Fatal(err)
	}
	svc := &ShoppingAgentService{
		ap2Repo: repo, signer: signer, log: logger.New(),
		agentSvc: &AgentService{productRepo: &cartProductRepository{product: &models.Product{ID: backingID, IsActive: true, GSTMetadata: `{"tax_rate":18}`}}},
	}
	cartID, userID, businessID := repo.cart.ID, repo.cart.UserID, repo.cart.BusinessID

	if _, err := svc.UpdateCartItem(context.Background(), cartID, userID, "foreign-business", repo.product.ID, CartMutationInput{Version: 2, Quantity: 2}); !errors.Is(err, ErrCartMandateNotFound) {
		t.Fatalf("foreign tenant mutation error = %v", err)
	}
	start := make(chan struct{})
	results := make(chan *models.CartMandate, 2)
	errorsFound := make(chan error, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, mutationErr := svc.UpdateCartItem(context.Background(), cartID, userID, businessID, repo.product.ID, CartMutationInput{Version: 2, Quantity: 2})
			results <- result
			errorsFound <- mutationErr
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsFound)
	var updated *models.CartMandate
	successes, conflicts := 0, 0
	for mutationErr := range errorsFound {
		if mutationErr == nil {
			successes++
		} else if errors.Is(mutationErr, ErrCartVersionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent mutation error: %v", mutationErr)
		}
	}
	for result := range results {
		if result != nil {
			updated = result
		}
	}
	if successes != 1 || conflicts != 1 || updated == nil {
		t.Fatalf("concurrent results success=%d conflict=%d updated=%v", successes, conflicts, updated)
	}
	if updated.Version != 3 || updated.SubtotalAmount != 250 || updated.TaxAmount != 45 || updated.TotalAmount != 295 {
		t.Fatalf("unexpected authoritative totals/version: %#v", updated)
	}
	if err := ap2.VerifyCartMandateSignature(updated, "buyer", updated.Signature, updated.SignaturePublicKey); err != nil {
		t.Fatalf("updated buyer signature invalid: %v", err)
	}
	if _, err := svc.UpdateCartItem(context.Background(), updated.ID, updated.UserID, updated.BusinessID, repo.product.ID, CartMutationInput{Version: 2, Quantity: 3}); !errors.Is(err, ErrCartVersionConflict) {
		t.Fatalf("stale mutation error = %v", err)
	}
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
		BusinessID:    repo.cart.BusinessID,
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
	_, err = svc.CompleteCheckout(context.Background(), &CheckoutRequest{CartMandateID: cart.ID, UserID: cart.UserID, BusinessID: cart.BusinessID})
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
	_, err = svc.CompleteCheckout(context.Background(), &CheckoutRequest{CartMandateID: cart.ID, UserID: cart.UserID, BusinessID: cart.BusinessID})
	if !errors.Is(err, ErrCartSignatureInvalid) {
		t.Fatalf("expected untrusted key error, got %v", err)
	}
}

func securityTestCart(t *testing.T) *models.CartMandate {
	t.Helper()
	merchantID := "22222222-2222-2222-2222-222222222222"
	return &models.CartMandate{
		ID: "33333333-3333-3333-3333-333333333333", UserID: "44444444-4444-4444-4444-444444444444",
		BusinessID: "11111111-1111-4111-8111-111111111111",
		AgentID:    "55555555-5555-5555-5555-555555555555", MerchantID: &merchantID,
		Items:       `[{"product_id":"66666666-6666-6666-6666-666666666666","name":"Widget","quantity":1,"unit_price":10}]`,
		TotalAmount: 10, Currency: "INR", Status: "pending", ExpiresAt: time.Now().Add(time.Hour),
	}
}
