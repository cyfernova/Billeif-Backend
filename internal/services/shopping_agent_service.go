package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

var (
	ErrCartMandateNotFound   = errors.New("cart mandate not found")
	ErrInvalidProduct        = errors.New("invalid product")
	ErrInsufficientStock     = errors.New("insufficient stock")
	ErrMandateExpired        = errors.New("mandate has expired")
	ErrCartTotalMismatch     = errors.New("cart total does not match line items")
	ErrIntentMandateNotFound = errors.New("intent mandate not found")
	ErrCartMandateUnsigned   = errors.New("cart mandate has not been signed by the merchant")
	ErrCartSignatureInvalid  = errors.New("cart mandate signature is invalid")
	ErrCartAlreadyProcessed  = errors.New("cart mandate has already been processed")
	ErrCartVersionConflict   = errors.New("cart version conflict")
	ErrCartNotEditable       = errors.New("cart is not editable")
	ErrCartCurrencyMismatch  = errors.New("cart products use different currencies")
)

type ShoppingAgentService struct {
	users               ReportUserRepository
	ap2Repo             interfaces.AP2Repository
	agentSvc            *AgentService
	intentSvc           *IntentProcessingService
	signer              *ap2.SignatureService
	mandateSvc          *ap2.MandateService
	verifier            *ap2.MandateVerifier
	a2aClient           *a2a.A2AClient
	a2aMessageEndpoint  string
	agentCardHTTPClient *http.Client
	log                 *logger.Logger
}

type versionedCartRepository interface {
	GetCartMandateForScope(ctx context.Context, id, userID, businessID string) (*models.CartMandate, error)
	UpdateCartMandateVersioned(ctx context.Context, mandate *models.CartMandate, expectedVersion int64) error
}

func (s *ShoppingAgentService) WithUserRepository(users ReportUserRepository) *ShoppingAgentService {
	s.users = users
	return s
}

func NewShoppingAgentService(
	ap2Repo interfaces.AP2Repository,
	agentSvc *AgentService,
	intentSvc *IntentProcessingService,
	signer *ap2.SignatureService,
	mandateSvc *ap2.MandateService,
	a2aClient *a2a.A2AClient,
	a2aMessageEndpoint string,
	log *logger.Logger,
) *ShoppingAgentService {
	if a2aMessageEndpoint == "" {
		a2aMessageEndpoint = "/api/v1/a2a"
	}

	return &ShoppingAgentService{
		ap2Repo:             ap2Repo,
		agentSvc:            agentSvc,
		intentSvc:           intentSvc,
		signer:              signer,
		mandateSvc:          mandateSvc,
		a2aClient:           a2aClient,
		a2aMessageEndpoint:  a2aMessageEndpoint,
		agentCardHTTPClient: newWebhookDeliveryHTTPClient(10 * time.Second),
		verifier:            ap2.NewMandateVerifier(),
		log:                 log,
	}
}

type ShoppingIntentRequest struct {
	UserID          string
	BusinessID      string
	ShoppingAgentID string
	Query           string
	ProductIDs      []string
	MaxAmount       *float64
	Expiration      int
}

func (s *ShoppingAgentService) ProcessShoppingIntent(ctx context.Context, req *ShoppingIntentRequest) (*models.CartMandate, error) {
	shoppingAgent, err := s.ap2Repo.GetAgentByID(ctx, req.ShoppingAgentID)
	if err != nil {
		return nil, fmt.Errorf("shopping agent not found: %w", err)
	}

	if shoppingAgent.Type != "shopping" {
		return nil, errors.New("agent is not a shopping agent")
	}
	if shoppingAgent.BusinessID != req.BusinessID {
		return nil, ErrCartMandateNotFound
	}

	intentMandate, err := s.createIntentMandate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create intent mandate: %w", err)
	}

	var cartItems []ap2.CartItem
	totalAmount := 0.0

	for _, productID := range req.ProductIDs {
		product, err := s.ap2Repo.GetMarketplaceProductByID(ctx, productID)
		if err != nil {
			s.log.Error("product not found", "product_id", productID)
			continue
		}

		if !product.IsAvailable || product.InventoryCount <= 0 {
			s.log.Warn("product not available", "product_id", productID)
			continue
		}

		item, err := s.authoritativeCartItem(ctx, product, 1)
		if err != nil {
			return nil, err
		}
		cartItems = append(cartItems, item)
		totalAmount += item.LineTotal
	}

	if req.MaxAmount != nil && totalAmount > *req.MaxAmount {
		return nil, fmt.Errorf("total amount %f exceeds maximum %f", totalAmount, *req.MaxAmount)
	}

	cartMandate, err := s.CreateCartMandate(ctx, &CreateCartMandateRequest{
		UserID:          req.UserID,
		BusinessID:      req.BusinessID,
		ShoppingAgentID: req.ShoppingAgentID,
		IntentMandateID: &intentMandate.ID,
		Items:           cartItems,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cart mandate: %w", err)
	}

	s.log.Info("created cart mandate", "cart_mandate_id", cartMandate.ID, "user_id", req.UserID)
	return cartMandate, nil
}

func (s *ShoppingAgentService) AddToCart(ctx context.Context, userID, businessID, shoppingAgentID, productID string) (*models.CartMandate, error) {
	agent, err := s.ap2Repo.GetAgentByID(ctx, shoppingAgentID)
	if err != nil || agent.Type != "shopping" || agent.BusinessID != businessID {
		return nil, ErrCartMandateNotFound
	}
	product, err := s.ap2Repo.GetMarketplaceProductByID(ctx, productID)
	if err != nil {
		return nil, ErrInvalidProduct
	}

	if !product.IsAvailable || product.InventoryCount <= 0 {
		return nil, ErrInsufficientStock
	}

	item, err := s.authoritativeCartItem(ctx, product, 1)
	if err != nil {
		return nil, err
	}
	cartItems := []ap2.CartItem{item}

	cartMandate, err := s.CreateCartMandate(ctx, &CreateCartMandateRequest{
		UserID:          userID,
		BusinessID:      businessID,
		ShoppingAgentID: shoppingAgentID,
		Items:           cartItems,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cart mandate: %w", err)
	}

	return cartMandate, nil
}

type CheckoutRequest struct {
	UserID          string
	BusinessID      string
	CartMandateID   string
	PaymentMethodID *string
}

type CreateCartMandateRequest struct {
	UserID          string
	BusinessID      string
	ShoppingAgentID string
	MerchantID      *string
	IntentMandateID *string
	Items           []ap2.CartItem
}

func (s *ShoppingAgentService) CompleteCheckout(ctx context.Context, req *CheckoutRequest) (*models.PaymentMandate, error) {
	if req == nil || strings.TrimSpace(req.BusinessID) == "" {
		return nil, ErrCartMandateNotFound
	}
	cartMandate, err := s.getCartForScope(ctx, req.CartMandateID, req.UserID, req.BusinessID)
	if err != nil {
		return nil, ErrCartMandateNotFound
	}
	resolvedRequest := *req
	resolvedRequest.UserID = cartMandate.UserID
	req = &resolvedRequest

	// CRITICAL: Verify mandate is not expired
	if err := s.verifyMandateExpiration(cartMandate); err != nil {
		return nil, err
	}

	if cartMandate.Status != "signed" || cartMandate.MerchantSignature == nil || cartMandate.MerchantSignaturePublicKey == nil {
		return nil, ErrCartMandateUnsigned
	}
	trustedKey := s.signer.GetPublicKey()
	if cartMandate.SignaturePublicKey != trustedKey || *cartMandate.MerchantSignaturePublicKey != trustedKey {
		return nil, fmt.Errorf("%w: untrusted signing key", ErrCartSignatureInvalid)
	}
	if err := ap2.VerifyCartMandateSignature(cartMandate, "buyer", cartMandate.Signature, cartMandate.SignaturePublicKey); err != nil {
		s.log.Warn("buyer cart mandate verification failed", "mandate_id", cartMandate.ID, "error", err)
		return nil, fmt.Errorf("%w: buyer signature", ErrCartSignatureInvalid)
	}
	if err := ap2.VerifyCartMandateSignature(cartMandate, "merchant", *cartMandate.MerchantSignature, *cartMandate.MerchantSignaturePublicKey); err != nil {
		s.log.Warn("merchant cart mandate verification failed", "mandate_id", cartMandate.ID, "error", err)
		return nil, fmt.Errorf("%w: merchant signature", ErrCartSignatureInvalid)
	}
	if len(cartMandate.PaymentMandates) != 0 {
		return nil, ErrCartAlreadyProcessed
	}

	// CRITICAL: Verify cart total matches line items
	if err := s.validateCartTotal(ctx, cartMandate); err != nil {
		return nil, err
	}

	// Verify intent mandate if linked
	if cartMandate.IntentMandateID != nil {
		// TODO: Verify intent mandate is still valid once signature verification is implemented
		if _, err := s.ap2Repo.GetIntentMandateByID(ctx, *cartMandate.IntentMandateID, req.UserID); err != nil {
			s.log.Warn("intent mandate not found", "mandate_id", *cartMandate.IntentMandateID, "error", err)
		}
	}

	paymentReq := &ap2.PaymentMandateRequest{
		CartMandateID:   req.CartMandateID,
		UserID:          req.UserID,
		PaymentMethodID: req.PaymentMethodID,
		Signature:       s.generatePaymentSignature(req),
	}

	paymentMandate, err := s.mandateSvc.CreatePaymentMandate(paymentReq, cartMandate.TotalAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment mandate: %w", err)
	}

	if err := s.ap2Repo.CreatePaymentMandate(ctx, paymentMandate); err != nil {
		return nil, fmt.Errorf("failed to save payment mandate: %w", err)
	}

	s.log.Info("created payment mandate", "payment_mandate_id", paymentMandate.ID, "user_id", req.UserID)
	return paymentMandate, nil
}

func (s *ShoppingAgentService) GetCartMandate(ctx context.Context, cartMandateID, userID string) (*models.CartMandate, error) {
	userID, err := resolveDatabaseUserID(ctx, s.users, userID)
	if err != nil {
		return nil, err
	}
	cartMandate, err := s.ap2Repo.GetCartMandateByID(ctx, cartMandateID, userID)
	if err != nil {
		return nil, ErrCartMandateNotFound
	}

	var items []ap2.CartItem
	if err := json.Unmarshal([]byte(cartMandate.Items), &items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cart items: %w", err)
	}

	return cartMandate, nil
}

func (s *ShoppingAgentService) GetCartMandateForScope(ctx context.Context, cartMandateID, userID, businessID string) (*models.CartMandate, error) {
	cart, err := s.getCartForScope(ctx, cartMandateID, userID, businessID)
	if err != nil {
		return nil, ErrCartMandateNotFound
	}
	return cart, nil
}

func (s *ShoppingAgentService) GetUserCarts(ctx context.Context, userID, businessID string, page, limit int) ([]*models.CartMandate, int64, error) {
	userID, err := resolveDatabaseUserID(ctx, s.users, userID)
	if err != nil {
		return nil, 0, err
	}
	if repo, ok := s.ap2Repo.(interface {
		GetCartMandatesForScope(context.Context, string, string, int, int) ([]*models.CartMandate, int64, error)
	}); ok {
		return repo.GetCartMandatesForScope(ctx, userID, businessID, page, limit)
	}
	return nil, 0, fmt.Errorf("business-scoped cart repository unavailable")
}

func (s *ShoppingAgentService) GetUserOrders(ctx context.Context, userID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	userID, err := resolveDatabaseUserID(ctx, s.users, userID)
	if err != nil {
		return nil, 0, err
	}
	return s.ap2Repo.GetOrdersByUser(ctx, userID, page, limit)
}

func (s *ShoppingAgentService) GetOrderDetails(ctx context.Context, orderID string) (*models.MarketplaceOrder, error) {
	return s.ap2Repo.GetOrderByID(ctx, orderID)
}

func (s *ShoppingAgentService) CreateOrderFromCart(ctx context.Context, cartMandateID, userID string) (*models.MarketplaceOrder, error) {
	userID, err := resolveDatabaseUserID(ctx, s.users, userID)
	if err != nil {
		return nil, err
	}
	cartMandate, err := s.ap2Repo.GetCartMandateByID(ctx, cartMandateID, userID)
	if err != nil {
		return nil, ErrCartMandateNotFound
	}

	order := &models.MarketplaceOrder{
		UserID:          cartMandate.UserID,
		ShoppingAgentID: cartMandate.AgentID,
		MerchantAgentID: *cartMandate.MerchantID,
		CartMandateID:   cartMandate.ID,
		TotalAmount:     cartMandate.TotalAmount,
		Currency:        cartMandate.Currency,
		Status:          "pending",
	}

	if err := s.ap2Repo.CreateOrder(ctx, order); err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	s.log.Info("created order", "order_id", order.ID, "cart_mandate_id", cartMandateID)
	return order, nil
}

func (s *ShoppingAgentService) SearchProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	return s.ap2Repo.SearchMarketplaceProducts(ctx, query, page, limit)
}

func (s *ShoppingAgentService) GetAvailableProducts(ctx context.Context, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	return s.ap2Repo.GetAvailableProducts(ctx, page, limit)
}

func (s *ShoppingAgentService) GetProductDetails(ctx context.Context, productID string) (*models.MarketplaceProduct, error) {
	return s.ap2Repo.GetMarketplaceProductByID(ctx, productID)
}

func (s *ShoppingAgentService) TrackOrder(ctx context.Context, orderID, userID string) (*models.MarketplaceOrder, error) {
	userID, err := resolveDatabaseUserID(ctx, s.users, userID)
	if err != nil {
		return nil, err
	}
	return s.ap2Repo.GetOrderByIDForUser(ctx, orderID, userID)
}

func (s *ShoppingAgentService) CreateIntentMandate(ctx context.Context, req *ShoppingIntentRequest) (*models.IntentMandate, error) {
	return s.createIntentMandate(ctx, req)
}

func (s *ShoppingAgentService) createIntentMandate(ctx context.Context, req *ShoppingIntentRequest) (*models.IntentMandate, error) {
	userID, err := resolveDatabaseUserID(ctx, s.users, req.UserID)
	if err != nil {
		return nil, err
	}
	constraints := ap2.MandateConstraints{
		Currency:    "INR",
		MaxAmount:   req.MaxAmount,
		TimeWindows: []ap2.TimeWindow{},
	}

	intentReq := &ap2.IntentMandateRequest{
		UserID:                userID,
		AgentID:               req.ShoppingAgentID,
		NaturalLanguageIntent: req.Query,
		Constraints:           constraints,
		ExpirationDuration:    s.getDefaultExpiration(),
	}

	publicKey := s.signer.GetPublicKey()
	intentMandate, err := s.mandateSvc.CreateIntentMandate(intentReq, publicKey)
	if err != nil {
		return nil, err
	}

	if err := s.ap2Repo.CreateIntentMandate(ctx, intentMandate); err != nil {
		return nil, fmt.Errorf("failed to save intent mandate: %w", err)
	}

	return intentMandate, nil
}

func (s *ShoppingAgentService) generatePaymentSignature(req *CheckoutRequest) string {
	signature, _ := s.signer.SignData([]byte(fmt.Sprintf("%v", req)))
	return signature
}

func (s *ShoppingAgentService) getDefaultExpiration() time.Duration {
	return 24 * time.Hour
}

func (s *ShoppingAgentService) GetShoppingAgentCapabilities(ctx context.Context, agentID string) ([]*models.AgentCapability, error) {
	return s.ap2Repo.GetCapabilitiesByAgent(ctx, agentID)
}

func (s *ShoppingAgentService) CreateCartMandate(ctx context.Context, req *CreateCartMandateRequest) (*models.CartMandate, error) {
	userID, err := resolveDatabaseUserID(ctx, s.users, req.UserID)
	if err != nil {
		return nil, err
	}
	cartReq := &ap2.CartMandateRequest{
		IntentMandateID:    req.IntentMandateID,
		UserID:             userID,
		AgentID:            req.ShoppingAgentID,
		MerchantID:         req.MerchantID,
		Items:              req.Items,
		Signature:          "pending-signature",
		ExpirationDuration: s.getDefaultExpiration(),
	}

	cartMandate, err := s.mandateSvc.CreateCartMandate(cartReq)
	if err != nil {
		return nil, err
	}
	cartMandate.ID = uuid.NewString()
	cartMandate.BusinessID = req.BusinessID
	cartMandate.Version = 1
	subtotal, tax, total := cartAmounts(req.Items)
	cartMandate.SubtotalAmount = subtotal
	cartMandate.TaxAmount = tax
	cartMandate.TotalAmount = total
	if len(req.Items) > 0 {
		if product, productErr := s.ap2Repo.GetMarketplaceProductByID(ctx, req.Items[0].ProductID); productErr == nil && product.Currency != "" {
			cartMandate.Currency = product.Currency
		}
	}
	signature, publicKey, err := ap2.SignCartMandate(s.signer, cartMandate, "buyer")
	if err != nil {
		return nil, fmt.Errorf("failed to sign cart mandate: %w", err)
	}
	cartMandate.Signature = signature
	cartMandate.SignaturePublicKey = publicKey

	if err := s.ap2Repo.CreateCartMandate(ctx, cartMandate); err != nil {
		return nil, fmt.Errorf("failed to save cart mandate: %w", err)
	}

	return cartMandate, nil
}

type CartMutationInput struct {
	Version  int64 `json:"version" binding:"required,gte=1"`
	Quantity int   `json:"quantity,omitempty" binding:"omitempty,gte=1,lte=10000"`
}

func (s *ShoppingAgentService) AddCartItem(ctx context.Context, cartID, userID, businessID, productID string, input CartMutationInput) (*models.CartMandate, error) {
	return s.mutateCart(ctx, cartID, userID, businessID, input.Version, func(items []ap2.CartItem) ([]ap2.CartItem, error) {
		quantity := input.Quantity
		if quantity == 0 {
			quantity = 1
		}
		for i := range items {
			if items[i].ProductID == productID {
				items[i].Quantity += quantity
				return items, nil
			}
		}
		return append(items, ap2.CartItem{ProductID: productID, Quantity: quantity}), nil
	})
}

func (s *ShoppingAgentService) UpdateCartItem(ctx context.Context, cartID, userID, businessID, productID string, input CartMutationInput) (*models.CartMandate, error) {
	if input.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	return s.mutateCart(ctx, cartID, userID, businessID, input.Version, func(items []ap2.CartItem) ([]ap2.CartItem, error) {
		for i := range items {
			if items[i].ProductID == productID {
				items[i].Quantity = input.Quantity
				return items, nil
			}
		}
		return nil, ErrInvalidProduct
	})
}

func (s *ShoppingAgentService) RemoveCartItem(ctx context.Context, cartID, userID, businessID, productID string, version int64) (*models.CartMandate, error) {
	return s.mutateCart(ctx, cartID, userID, businessID, version, func(items []ap2.CartItem) ([]ap2.CartItem, error) {
		for i := range items {
			if items[i].ProductID == productID {
				return append(items[:i], items[i+1:]...), nil
			}
		}
		return nil, ErrInvalidProduct
	})
}

func (s *ShoppingAgentService) ClearCart(ctx context.Context, cartID, userID, businessID string, version int64) (*models.CartMandate, error) {
	return s.mutateCart(ctx, cartID, userID, businessID, version, func([]ap2.CartItem) ([]ap2.CartItem, error) {
		return []ap2.CartItem{}, nil
	})
}

func (s *ShoppingAgentService) mutateCart(
	ctx context.Context,
	cartID, userID, businessID string,
	expectedVersion int64,
	change func([]ap2.CartItem) ([]ap2.CartItem, error),
) (*models.CartMandate, error) {
	if expectedVersion < 1 {
		return nil, ErrCartVersionConflict
	}
	cart, err := s.getCartForScope(ctx, cartID, userID, businessID)
	if err != nil {
		return nil, ErrCartMandateNotFound
	}
	if cart.Status != "pending" || len(cart.PaymentMandates) != 0 {
		return nil, ErrCartNotEditable
	}
	if err := s.verifyMandateExpiration(cart); err != nil {
		return nil, err
	}
	if cart.Version != expectedVersion {
		return nil, ErrCartVersionConflict
	}
	var items []ap2.CartItem
	if err := json.Unmarshal([]byte(cart.Items), &items); err != nil {
		return nil, fmt.Errorf("decode cart items: %w", err)
	}
	items, err = change(items)
	if err != nil {
		return nil, err
	}
	currency := ""
	for i := range items {
		product, productErr := s.ap2Repo.GetMarketplaceProductByID(ctx, items[i].ProductID)
		if productErr != nil {
			return nil, ErrInvalidProduct
		}
		items[i], err = s.authoritativeCartItem(ctx, product, items[i].Quantity)
		if err != nil {
			return nil, err
		}
		if currency != "" && !strings.EqualFold(currency, product.Currency) {
			return nil, ErrCartCurrencyMismatch
		}
		currency = product.Currency
		cart.Currency = product.Currency
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("encode cart items: %w", err)
	}
	cart.Items = string(encoded)
	cart.SubtotalAmount, cart.TaxAmount, cart.TotalAmount = cartAmounts(items)
	cart.MerchantSignature = nil
	cart.MerchantSignaturePublicKey = nil
	signature, publicKey, err := ap2.SignCartMandate(s.signer, cart, "buyer")
	if err != nil {
		return nil, fmt.Errorf("sign updated cart: %w", err)
	}
	cart.Signature = signature
	cart.SignaturePublicKey = publicKey
	repo, ok := s.ap2Repo.(versionedCartRepository)
	if !ok {
		return nil, fmt.Errorf("versioned cart repository unavailable")
	}
	if err := repo.UpdateCartMandateVersioned(ctx, cart, expectedVersion); err != nil {
		return nil, ErrCartVersionConflict
	}
	return cart, nil
}

func (s *ShoppingAgentService) getCartForScope(ctx context.Context, cartID, userID, businessID string) (*models.CartMandate, error) {
	userID, err := resolveDatabaseUserID(ctx, s.users, userID)
	if err != nil {
		return nil, err
	}
	if repo, ok := s.ap2Repo.(versionedCartRepository); ok {
		return repo.GetCartMandateForScope(ctx, cartID, userID, businessID)
	}
	cart, err := s.ap2Repo.GetCartMandateByID(ctx, cartID, userID)
	if err != nil || cart.BusinessID != businessID {
		return nil, ErrCartMandateNotFound
	}
	return cart, nil
}

func (s *ShoppingAgentService) authoritativeCartItem(ctx context.Context, product *models.MarketplaceProduct, quantity int) (ap2.CartItem, error) {
	if product == nil || !product.IsAvailable || quantity <= 0 || product.InventoryCount-product.ReservedInventoryCount < quantity {
		return ap2.CartItem{}, ErrInsufficientStock
	}
	taxRate := 0.0
	if product.ProductID != nil && s.agentSvc != nil && s.agentSvc.productRepo != nil {
		agent, err := s.ap2Repo.GetAgentByID(ctx, product.AgentID)
		if err != nil {
			return ap2.CartItem{}, ErrInvalidProduct
		}
		backing, err := s.agentSvc.productRepo.GetByID(ctx, *product.ProductID, agent.BusinessID)
		if err != nil || !backing.IsActive {
			return ap2.CartItem{}, ErrInvalidProduct
		}
		taxRate = floatValue(unmarshalJSONMap(backing.GSTMetadata)["tax_rate"])
		if taxRate < 0 || taxRate > 100 {
			return ap2.CartItem{}, fmt.Errorf("invalid authoritative product tax rate")
		}
	}
	subtotal := roundMoney(float64(quantity) * product.Price)
	taxAmount := roundMoney(subtotal * taxRate / 100)
	return ap2.CartItem{
		ProductID: product.ID,
		Name:      product.Name,
		Quantity:  quantity,
		UnitPrice: roundMoney(product.Price),
		TaxRate:   taxRate,
		TaxAmount: taxAmount,
		LineTotal: roundMoney(subtotal + taxAmount),
	}, nil
}

func cartAmounts(items []ap2.CartItem) (float64, float64, float64) {
	var subtotal, tax float64
	for _, item := range items {
		subtotal += roundMoney(float64(item.Quantity) * item.UnitPrice)
		tax += item.TaxAmount
	}
	subtotal = roundMoney(subtotal)
	tax = roundMoney(tax)
	return subtotal, tax, roundMoney(subtotal + tax)
}

// SECURITY: Verify mandate is not expired
func (s *ShoppingAgentService) verifyMandateExpiration(mandate *models.CartMandate) error {
	if time.Now().After(mandate.ExpiresAt) {
		s.log.Warn("mandate expired", "mandate_id", mandate.ID, "expired_at", mandate.ExpiresAt)
		return fmt.Errorf("%w: expired at %v", ErrMandateExpired, mandate.ExpiresAt)
	}
	return nil
}

// SECURITY: Validate cart total matches line items (prevents tampering with amounts)
func (s *ShoppingAgentService) validateCartTotal(ctx context.Context, cartMandate *models.CartMandate) error {
	var items []ap2.CartItem
	if err := json.Unmarshal([]byte(cartMandate.Items), &items); err != nil {
		return fmt.Errorf("failed to parse cart items: %w", err)
	}

	calculatedTotal := 0.0
	for _, item := range items {
		// Verify product exists
		_, err := s.ap2Repo.GetMarketplaceProductByID(ctx, item.ProductID)
		if err != nil {
			return fmt.Errorf("product not found: %s", item.ProductID)
		}

		// Verify the signed authoritative snapshot including its tax component.
		linePrice := item.LineTotal
		if linePrice <= 0 {
			linePrice = float64(item.Quantity)*item.UnitPrice + item.TaxAmount
		}
		calculatedTotal += linePrice
	}

	// Allow small floating point differences (0.01 = 1 paise)
	diff := calculatedTotal - cartMandate.TotalAmount
	if diff < -0.01 || diff > 0.01 {
		s.log.Error("cart total mismatch detected",
			"cart_mandate_id", cartMandate.ID,
			"calculated_total", calculatedTotal,
			"provided_total", cartMandate.TotalAmount,
			"difference", diff)
		return fmt.Errorf("%w: calculated=%.2f, provided=%.2f, difference=%.2f",
			ErrCartTotalMismatch, calculatedTotal, cartMandate.TotalAmount, diff)
	}

	return nil
}

// QueryMerchantAgentsA2A fetches latest A2A Agent Cards from merchant agent well-known endpoints.
func (s *ShoppingAgentService) QueryMerchantAgentsA2A(ctx context.Context, merchantAgentEndpoints []string) (map[string]*a2a.AgentCard, error) {
	cards := make(map[string]*a2a.AgentCard)
	client := s.agentCardHTTPClient
	if client == nil {
		client = newWebhookDeliveryHTTPClient(10 * time.Second)
	}
	for _, endpoint := range merchantAgentEndpoints {
		cardURL := strings.TrimRight(endpoint, "/") + "/.well-known/agent-card.json"
		if err := validateWebhookURL(ctx, cardURL); err != nil {
			s.log.Warn("unsafe merchant agent card URL rejected", "endpoint", endpoint, "error", err)
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build agent card request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			s.log.Warn("failed to fetch merchant agent card", "endpoint", endpoint, "error", err)
			continue
		}
		body, err := a2a.ReadResponseBody(resp)
		if err != nil {
			resp.Body.Close()
			s.log.Warn("failed to decode merchant agent card", "endpoint", endpoint, "error", err)
			continue
		}
		var card a2a.AgentCard
		if err := json.Unmarshal(body, &card); err != nil {
			resp.Body.Close()
			s.log.Warn("failed to decode merchant agent card", "endpoint", endpoint, "error", err)
			continue
		}
		resp.Body.Close()
		cards[endpoint] = &card
	}

	return cards, nil
}

// ProcessCartWithMerchantA2A sends cart to merchant agent for processing via A2A
func (s *ShoppingAgentService) ProcessCartWithMerchantA2A(ctx context.Context, merchantEndpoint string, cartMandate *models.CartMandate, userID string) (*a2a.SendMessageResponse, error) {
	s.log.Info("sending cart to merchant agent", "endpoint", merchantEndpoint, "cart_id", cartMandate.ID)

	req := &a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: uuid.NewString(),
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{
				{Text: fmt.Sprintf("Process cart %s for user %s.", cartMandate.ID, userID)},
			},
			Metadata: map[string]interface{}{
				"taskType":      "merchant.process_cart",
				"cartMandateId": cartMandate.ID,
			},
		},
		Metadata: map[string]interface{}{
			"taskType":        "merchant.process_cart",
			"cartMandateId":   cartMandate.ID,
			"merchantAgentId": valueOrEmpty(cartMandate.MerchantID),
			"userId":          userID,
		},
	}

	response, err := s.a2aClient.SendMessageWithRetry(ctx, merchantEndpoint, req, 3)
	if err != nil {
		s.log.Error("failed to process cart with merchant", "error", err, "cart_id", cartMandate.ID)
		return nil, fmt.Errorf("merchant processing failed: %w", err)
	}

	s.log.Info("cart processed by merchant agent", "cart_id", cartMandate.ID)

	return response, nil
}

// RequestPaymentProcessingA2A sends payment request to payment processor via A2A
func (s *ShoppingAgentService) RequestPaymentProcessingA2A(ctx context.Context, paymentEndpoint string, paymentMandate *models.PaymentMandate) (*a2a.SendMessageResponse, error) {
	s.log.Info("requesting payment processing", "endpoint", paymentEndpoint, "payment_mandate_id", paymentMandate.ID)

	req := &a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: uuid.NewString(),
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{
				{Text: fmt.Sprintf("Process payment mandate %s.", paymentMandate.ID)},
			},
			Metadata: map[string]interface{}{
				"taskType": "payment.process",
			},
		},
		Metadata: map[string]interface{}{
			"taskType":         "payment.process",
			"paymentMandateId": paymentMandate.ID,
			"userId":           paymentMandate.UserID,
		},
	}

	response, err := s.a2aClient.SendMessageWithRetry(ctx, paymentEndpoint, req, 3)
	if err != nil {
		s.log.Error("failed to process payment", "error", err, "payment_mandate_id", paymentMandate.ID)
		return nil, fmt.Errorf("payment processing failed: %w", err)
	}

	s.log.Info("payment processed", "payment_mandate_id", paymentMandate.ID)

	return response, nil
}

// BroadcastCartStatusA2A broadcasts cart status updates to all interested agents via A2A
func (s *ShoppingAgentService) BroadcastCartStatusA2A(ctx context.Context, endpoints []string, cartMandate *models.CartMandate, status string) error {
	s.log.Info("broadcasting cart status", "status", status, "cart_id", cartMandate.ID, "recipient_count", len(endpoints))

	for _, endpoint := range endpoints {
		req := &a2a.SendMessageRequest{
			Message: a2a.Message{
				MessageID: uuid.NewString(),
				TaskID:    cartMandate.ID,
				Role:      a2a.RoleUser,
				Parts: []a2a.Part{
					{Text: fmt.Sprintf("Cart %s is now %s.", cartMandate.ID, status)},
				},
			},
			Metadata: map[string]interface{}{
				"taskType": "merchant.process_cart",
				"status":   status,
			},
		}
		if _, err := s.a2aClient.SendMessage(ctx, endpoint, req); err != nil {
			s.log.Warn("failed to broadcast cart status", "endpoint", endpoint, "error", err)
		}
	}

	return nil
}

// GenerateIdeas generates ideas for AI assistants
func (s *ShoppingAgentService) GenerateIdeas(ctx context.Context, userInput string) (string, error) {
	if s.intentSvc == nil {
		return "", fmt.Errorf("intent processing service not available")
	}
	return s.intentSvc.GenerateIdeas(ctx, userInput)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
