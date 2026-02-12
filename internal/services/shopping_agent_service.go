package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"
)

var (
	ErrCartMandateNotFound   = errors.New("cart mandate not found")
	ErrInvalidProduct        = errors.New("invalid product")
	ErrInsufficientStock     = errors.New("insufficient stock")
	ErrMandateExpired        = errors.New("mandate has expired")
	ErrCartTotalMismatch     = errors.New("cart total does not match line items")
	ErrIntentMandateNotFound = errors.New("intent mandate not found")
)

type ShoppingAgentService struct {
	ap2Repo    interfaces.AP2Repository
	agentSvc   *AgentService
	intentSvc  *IntentProcessingService
	signer     *ap2.SignatureService
	mandateSvc *ap2.MandateService
	verifier   *ap2.MandateVerifier
	a2aClient  *a2a.A2AClient
	log        *logger.Logger
}

func NewShoppingAgentService(
	ap2Repo interfaces.AP2Repository,
	agentSvc *AgentService,
	intentSvc *IntentProcessingService,
	signer *ap2.SignatureService,
	mandateSvc *ap2.MandateService,
	a2aClient *a2a.A2AClient,
	log *logger.Logger,
) *ShoppingAgentService {
	return &ShoppingAgentService{
		ap2Repo:    ap2Repo,
		agentSvc:   agentSvc,
		intentSvc:  intentSvc,
		signer:     signer,
		mandateSvc: mandateSvc,
		a2aClient:  a2aClient,
		verifier:   ap2.NewMandateVerifier(),
		log:        log,
	}
}

type ShoppingIntentRequest struct {
	UserID          string
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

		cartItems = append(cartItems, ap2.CartItem{
			ProductID: product.ID,
			Name:      product.Name,
			Quantity:  1,
			UnitPrice: product.Price,
		})

		totalAmount += product.Price
	}

	if req.MaxAmount != nil && totalAmount > *req.MaxAmount {
		return nil, fmt.Errorf("total amount %f exceeds maximum %f", totalAmount, *req.MaxAmount)
	}

	cartReq := &ap2.CartMandateRequest{
		IntentMandateID:    &intentMandate.ID,
		UserID:             req.UserID,
		AgentID:            req.ShoppingAgentID,
		Items:              cartItems,
		Signature:          s.signCartMandate(intentMandate, cartItems),
		ExpirationDuration: s.getDefaultExpiration(),
	}

	cartMandate, err := s.mandateSvc.CreateCartMandate(cartReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create cart mandate: %w", err)
	}

	s.log.Info("created cart mandate", "cart_mandate_id", cartMandate.ID, "user_id", req.UserID)
	return cartMandate, nil
}

func (s *ShoppingAgentService) AddToCart(ctx context.Context, userID, shoppingAgentID, productID string) (*models.CartMandate, error) {
	product, err := s.ap2Repo.GetMarketplaceProductByID(ctx, productID)
	if err != nil {
		return nil, ErrInvalidProduct
	}

	if !product.IsAvailable || product.InventoryCount <= 0 {
		return nil, ErrInsufficientStock
	}

	cartItems := []ap2.CartItem{
		{
			ProductID: product.ID,
			Name:      product.Name,
			Quantity:  1,
			UnitPrice: product.Price,
		},
	}

	cartReq := &ap2.CartMandateRequest{
		UserID:             userID,
		AgentID:            shoppingAgentID,
		Items:              cartItems,
		Signature:          s.generateSignature(cartItems),
		ExpirationDuration: s.getDefaultExpiration(),
	}

	cartMandate, err := s.mandateSvc.CreateCartMandate(cartReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create cart mandate: %w", err)
	}

	return cartMandate, nil
}

type CheckoutRequest struct {
	UserID          string
	CartMandateID   string
	PaymentMethodID *string
}

func (s *ShoppingAgentService) CompleteCheckout(ctx context.Context, req *CheckoutRequest) (*models.PaymentMandate, error) {
	cartMandate, err := s.ap2Repo.GetCartMandateByID(ctx, req.CartMandateID)
	if err != nil {
		return nil, ErrCartMandateNotFound
	}

	if cartMandate.UserID != req.UserID {
		return nil, errors.New("unauthorized: cart mandate does not belong to user")
	}

	// CRITICAL: Verify mandate is not expired
	if err := s.verifyMandateExpiration(cartMandate); err != nil {
		return nil, err
	}

	// TODO: Verify cart mandate signature and validity
	// if err := s.verifier.VerifyCartMandate(cartMandate, cartMandate.Signature, cartMandate.PublicKey); err != nil {
	//	s.log.Warn("cart mandate verification failed", "mandate_id", cartMandate.ID, "error", err)
	//	return nil, fmt.Errorf("cart mandate verification failed: %w", err)
	// }

	// CRITICAL: Verify cart total matches line items
	if err := s.validateCartTotal(ctx, cartMandate); err != nil {
		return nil, err
	}

	// Verify intent mandate if linked
	if cartMandate.IntentMandateID != nil {
		// TODO: Verify intent mandate is still valid once signature verification is implemented
		if _, err := s.ap2Repo.GetIntentMandateByID(ctx, *cartMandate.IntentMandateID); err != nil {
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

	s.log.Info("created payment mandate", "payment_mandate_id", paymentMandate.ID, "user_id", req.UserID)
	return paymentMandate, nil
}

func (s *ShoppingAgentService) GetCartMandate(ctx context.Context, cartMandateID string) (*models.CartMandate, error) {
	cartMandate, err := s.ap2Repo.GetCartMandateByID(ctx, cartMandateID)
	if err != nil {
		return nil, ErrCartMandateNotFound
	}

	var items []ap2.CartItem
	if err := json.Unmarshal([]byte(cartMandate.Items), &items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cart items: %w", err)
	}

	return cartMandate, nil
}

func (s *ShoppingAgentService) GetUserCarts(ctx context.Context, userID string, page, limit int) ([]*models.CartMandate, int64, error) {
	return s.ap2Repo.GetCartMandatesByUser(ctx, userID, page, limit)
}

func (s *ShoppingAgentService) GetUserOrders(ctx context.Context, userID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	return s.ap2Repo.GetOrdersByUser(ctx, userID, page, limit)
}

func (s *ShoppingAgentService) GetOrderDetails(ctx context.Context, orderID string) (*models.MarketplaceOrder, error) {
	return s.ap2Repo.GetOrderByID(ctx, orderID)
}

func (s *ShoppingAgentService) CreateOrderFromCart(ctx context.Context, cartMandateID string) (*models.MarketplaceOrder, error) {
	cartMandate, err := s.ap2Repo.GetCartMandateByID(ctx, cartMandateID)
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

func (s *ShoppingAgentService) TrackOrder(ctx context.Context, orderID string) (*models.MarketplaceOrder, error) {
	return s.ap2Repo.GetOrderByID(ctx, orderID)
}

func (s *ShoppingAgentService) createIntentMandate(ctx context.Context, req *ShoppingIntentRequest) (*models.IntentMandate, error) {
	constraints := ap2.MandateConstraints{
		Currency:    "INR",
		MaxAmount:   req.MaxAmount,
		TimeWindows: []ap2.TimeWindow{},
	}

	intentReq := &ap2.IntentMandateRequest{
		UserID:                req.UserID,
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

func (s *ShoppingAgentService) signCartMandate(intentMandate *models.IntentMandate, items []ap2.CartItem) string {
	signData := map[string]interface{}{
		"intent_mandate_id": intentMandate.ID,
		"items":             items,
		"timestamp":         intentMandate.ExpiresAt,
	}
	signature, _ := s.signer.SignData([]byte(fmt.Sprintf("%v", signData)))
	return signature
}

func (s *ShoppingAgentService) generateSignature(items []ap2.CartItem) string {
	signature, _ := s.signer.SignData([]byte(fmt.Sprintf("%v", items)))
	return signature
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

		// Use item's line price (from mandate, not product) for verification
		// This ensures cart total matches: sum of (quantity * unit_price)
		linePrice := float64(item.Quantity) * item.UnitPrice
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

// QueryMerchantAgentsA2A queries merchant agents for capabilities via A2A
func (s *ShoppingAgentService) QueryMerchantAgentsA2A(ctx context.Context, merchantAgentEndpoints []string) (map[string]*a2a.CapabilitiesPayload, error) {
	capabilities := make(map[string]*a2a.CapabilitiesPayload)

	for _, endpoint := range merchantAgentEndpoints {
		s.log.Info("querying merchant agent capabilities", "endpoint", endpoint)

		// Create capabilities query message
		msg := a2a.NewA2AMessage("shopping-agent", "merchant-agent", a2a.MessageTypeQueryCapabilities)

		// Send query and get response
		response, err := s.a2aClient.SendMessage(ctx, endpoint, msg)
		if err != nil {
			s.log.Error("failed to query merchant agent", "endpoint", endpoint, "error", err)
			continue
		}

		// Extract capabilities from response
		var caps a2a.CapabilitiesPayload
		if err := a2a.ExtractPayload(response, &caps); err != nil {
			s.log.Error("failed to extract capabilities", "endpoint", endpoint, "error", err)
			continue
		}

		capabilities[endpoint] = &caps
		s.log.Info("merchant agent capabilities received", "endpoint", endpoint, "capabilities", caps.Capabilities)
	}

	return capabilities, nil
}

// ProcessCartWithMerchantA2A sends cart to merchant agent for processing via A2A
func (s *ShoppingAgentService) ProcessCartWithMerchantA2A(ctx context.Context, merchantEndpoint string, cartMandate *models.CartMandate, userID string) (*a2a.A2AMessage, error) {
	s.log.Info("sending cart to merchant agent", "endpoint", merchantEndpoint, "cart_id", cartMandate.ID)

	// Create task.start message for cart processing
	msg := a2a.NewA2AMessage("shopping-agent", "merchant-agent", a2a.MessageTypeTaskStart)
	msg.WithEndpoints("http://localhost:8080/api/v1/a2a/message", merchantEndpoint)

	// Create task start payload
	payload := &a2a.TaskStartPayload{
		TaskName:    "merchant.process_cart",
		Description: "Process shopping cart and apply constraints",
		Parameters: map[string]interface{}{
			"cart_mandate_id": cartMandate.ID,
			"user_id":         userID,
			"items_count":     len(cartMandate.Items),
			"total_amount":    cartMandate.TotalAmount,
		},
		Priority: 5,
	}

	if _, err := msg.WithPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	// Send message with retry
	response, err := s.a2aClient.SendMessageWithRetry(ctx, merchantEndpoint, msg, 3)
	if err != nil {
		s.log.Error("failed to process cart with merchant", "error", err, "cart_id", cartMandate.ID)
		return nil, fmt.Errorf("merchant processing failed: %w", err)
	}

	s.log.Info("cart processed by merchant agent", "response_id", response.MessageID, "cart_id", cartMandate.ID)

	return response, nil
}

// RequestPaymentProcessingA2A sends payment request to payment processor via A2A
func (s *ShoppingAgentService) RequestPaymentProcessingA2A(ctx context.Context, paymentEndpoint string, paymentMandate *models.PaymentMandate) (*a2a.A2AMessage, error) {
	s.log.Info("requesting payment processing", "endpoint", paymentEndpoint, "payment_mandate_id", paymentMandate.ID)

	// Create task.start message for payment processing
	msg := a2a.NewA2AMessage("shopping-agent", "payment-processor", a2a.MessageTypeTaskStart)
	msg.WithEndpoints("http://localhost:8080/api/v1/a2a/message", paymentEndpoint)

	// Create task start payload
	payload := &a2a.TaskStartPayload{
		TaskName:    "payment.process",
		Description: "Process payment transaction",
		Parameters: map[string]interface{}{
			"payment_mandate_id": paymentMandate.ID,
			"amount":             paymentMandate.Amount,
			"currency":           paymentMandate.Currency,
			"user_id":            paymentMandate.UserID,
		},
		Priority: 8, // High priority for payments
	}

	if _, err := msg.WithPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	// Send message with retry
	response, err := s.a2aClient.SendMessageWithRetry(ctx, paymentEndpoint, msg, 3)
	if err != nil {
		s.log.Error("failed to process payment", "error", err, "payment_mandate_id", paymentMandate.ID)
		return nil, fmt.Errorf("payment processing failed: %w", err)
	}

	s.log.Info("payment processed", "response_id", response.MessageID, "payment_mandate_id", paymentMandate.ID)

	return response, nil
}

// BroadcastCartStatusA2A broadcasts cart status updates to all interested agents via A2A
func (s *ShoppingAgentService) BroadcastCartStatusA2A(ctx context.Context, endpoints []string, cartMandate *models.CartMandate, status string) error {
	s.log.Info("broadcasting cart status", "status", status, "cart_id", cartMandate.ID, "recipient_count", len(endpoints))

	// Create task.status message
	msg := a2a.NewA2AMessage("shopping-agent", "merchant-agent", a2a.MessageTypeTaskStatus)
	msg.WithTaskID(cartMandate.ID)

	// Calculate progress
	progress := 0
	switch status {
	case "pending":
		progress = 10
	case "validated":
		progress = 25
	case "confirmed":
		progress = 50
	case "processing":
		progress = 75
	case "completed":
		progress = 100
	default:
		progress = 0
	}

	// Create status payload
	statusPayload := &a2a.TaskStatusPayload{
		Status:      status,
		ProgressPct: progress,
		Message:     fmt.Sprintf("Cart %s is now %s", cartMandate.ID, status),
	}

	if _, err := msg.WithPayload(statusPayload); err != nil {
		return fmt.Errorf("failed to set status payload: %w", err)
	}

	// Broadcast to all endpoints
	errors := s.a2aClient.BroadcastMessage(ctx, endpoints, msg)
	if len(errors) > 0 {
		s.log.Warn("some broadcasts failed", "error_count", len(errors))
		for endpoint, err := range errors {
			s.log.Error("broadcast failed", "endpoint", endpoint, "error", err)
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
