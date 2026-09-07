package handlers

import (
	"errors"
	"net/http"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type ShoppingAgentHandler struct {
	svc     *services.ShoppingAgentService
	ap2Repo interfaces.AP2Repository
	log     *logger.Logger
}

func NewShoppingAgentHandler(svc *services.ShoppingAgentService, ap2Repo interfaces.AP2Repository, log *logger.Logger) *ShoppingAgentHandler {
	return &ShoppingAgentHandler{svc: svc, ap2Repo: ap2Repo, log: log}
}

type SearchProductsRequest struct {
	Query    string `json:"query"`
	Page     int    `json:"page"`
	Limit    int    `json:"limit"`
	Category string `json:"category"`
}

type CreateCartRequest struct {
	ProductIDs []string `json:"product_ids" binding:"required"`
	MaxAmount  *float64 `json:"max_amount"`
	Query      string   `json:"query"`
	Expiration int      `json:"expiration"`
}

type AddToCartRequest struct {
	ProductID string `json:"product_id" binding:"required"`
}

type CartItemMutationRequest struct {
	Version  int64 `json:"version" binding:"required,gte=1"`
	Quantity int   `json:"quantity,omitempty" binding:"omitempty,gte=1,lte=10000"`
}

type CartVersionRequest struct {
	Version int64 `json:"version" binding:"required,gte=1"`
}

type CheckoutRequest struct {
	CartMandateID   string  `json:"cart_mandate_id" binding:"required"`
	PaymentMethodID *string `json:"payment_method_id"`
}

// SearchProducts searches for products in the marketplace
// @Summary Search marketplace products
// @Description Searches for products in the marketplace
// @Tags Shopping
// @Produce json
// @Security BearerAuth
// @Param q query string false "Search query"
// @Param category query string false "Category filter"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/search [get]
func (h *ShoppingAgentHandler) SearchProducts(c *gin.Context) {
	query := c.Query("q")
	page, limit := utils.ParsePagination(c)
	category := c.Query("category")

	var products []*models.MarketplaceProduct
	var total int64
	var err error

	if category != "" {
		products, total, err = h.svc.SearchProducts(c.Request.Context(), query+" category:"+category, page, limit)
	} else {
		products, total, err = h.svc.SearchProducts(c.Request.Context(), query, page, limit)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// CreateCart creates a new shopping cart
// @Summary Create shopping cart
// @Description Creates a new shopping cart with agent
// @Tags Shopping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param agent_id query string true "Shopping agent ID"
// @Param input body CreateCartRequest true "Cart details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/cart [post]
func (h *ShoppingAgentHandler) CreateCart(c *gin.Context) {
	userID := c.GetString("user_id")
	shoppingAgentID := c.Query("agent_id")

	if shoppingAgentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}
	agent, ok := requireOwnedAgent(c, h.ap2Repo, shoppingAgentID)
	if !ok {
		return
	}
	if agent.Type != "shopping" {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if agent.BusinessID != businessID {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var req CreateCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Expiration == 0 {
		req.Expiration = 24
	}

	shoppingReq := &services.ShoppingIntentRequest{
		UserID:          userID,
		BusinessID:      businessID,
		ShoppingAgentID: shoppingAgentID,
		Query:           req.Query,
		ProductIDs:      req.ProductIDs,
		MaxAmount:       req.MaxAmount,
		Expiration:      req.Expiration,
	}

	cartMandate, err := h.svc.ProcessShoppingIntent(c.Request.Context(), shoppingReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, cartMandate)
}

// AddToCart adds a product to an existing cart
// @Summary Add to cart
// @Description Adds a product to an existing shopping cart
// @Tags Shopping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param agent_id query string true "Shopping agent ID"
// @Param input body AddToCartRequest true "Product to add"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/cart/add [post]
func (h *ShoppingAgentHandler) AddToCart(c *gin.Context) {
	userID := c.GetString("user_id")
	shoppingAgentID := c.Query("agent_id")

	if shoppingAgentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}
	agent, ok := requireOwnedAgent(c, h.ap2Repo, shoppingAgentID)
	if !ok {
		return
	}
	if agent.Type != "shopping" {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if agent.BusinessID != businessID {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var req AddToCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cartMandate, err := h.svc.AddToCart(c.Request.Context(), userID, businessID, shoppingAgentID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, cartMandate)
}

// AddCartItem adds an authoritative product snapshot to an editable cart.
// @Summary Add item to editable cart
// @Tags Shopping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Cart ID"
// @Param product_id path string true "Marketplace product ID"
// @Param input body CartItemMutationRequest true "Expected version and quantity"
// @Success 200 {object} models.CartMandate
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /agents/shopping/cart/{id}/items/{product_id} [post]
func (h *ShoppingAgentHandler) AddCartItem(c *gin.Context) {
	h.mutateCartItem(c, "add")
}

// UpdateCartItem replaces an item quantity and refreshes all price, availability and tax snapshots.
// @Summary Update editable cart item
// @Tags Shopping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Cart ID"
// @Param product_id path string true "Marketplace product ID"
// @Param input body CartItemMutationRequest true "Expected version and quantity"
// @Success 200 {object} models.CartMandate
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /agents/shopping/cart/{id}/items/{product_id} [patch]
func (h *ShoppingAgentHandler) UpdateCartItem(c *gin.Context) {
	h.mutateCartItem(c, "update")
}

func (h *ShoppingAgentHandler) mutateCartItem(c *gin.Context, operation string) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input CartItemMutationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mutation := services.CartMutationInput{Version: input.Version, Quantity: input.Quantity}
	var cart *models.CartMandate
	var err error
	if operation == "add" {
		cart, err = h.svc.AddCartItem(c.Request.Context(), c.Param("id"), middleware.GetUserID(c), businessID, c.Param("product_id"), mutation)
	} else {
		cart, err = h.svc.UpdateCartItem(c.Request.Context(), c.Param("id"), middleware.GetUserID(c), businessID, c.Param("product_id"), mutation)
	}
	if err != nil {
		writeCartMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, cart)
}

// RemoveCartItem removes an item from an editable cart.
// @Summary Remove editable cart item
// @Tags Shopping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Cart ID"
// @Param product_id path string true "Marketplace product ID"
// @Param input body CartVersionRequest true "Expected cart version"
// @Success 200 {object} models.CartMandate
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /agents/shopping/cart/{id}/items/{product_id} [delete]
func (h *ShoppingAgentHandler) RemoveCartItem(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input CartVersionRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cart, err := h.svc.RemoveCartItem(c.Request.Context(), c.Param("id"), middleware.GetUserID(c), businessID, c.Param("product_id"), input.Version)
	if err != nil {
		writeCartMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, cart)
}

// ClearCart removes every item from an editable cart.
// @Summary Clear editable cart
// @Tags Shopping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Cart ID"
// @Param input body CartVersionRequest true "Expected cart version"
// @Success 200 {object} models.CartMandate
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /agents/shopping/cart/{id}/items [delete]
func (h *ShoppingAgentHandler) ClearCart(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input CartVersionRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cart, err := h.svc.ClearCart(c.Request.Context(), c.Param("id"), middleware.GetUserID(c), businessID, input.Version)
	if err != nil {
		writeCartMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, cart)
}

func writeCartMutationError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, services.ErrCartMandateNotFound):
		status = http.StatusNotFound
	case errors.Is(err, services.ErrCartVersionConflict), errors.Is(err, services.ErrCartNotEditable),
		errors.Is(err, services.ErrInsufficientStock), errors.Is(err, services.ErrMandateExpired):
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"error": err.Error()})
}

// Checkout completes checkout for a shopping cart
// @Summary Checkout
// @Description Completes checkout for a shopping cart
// @Tags Shopping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body CheckoutRequest true "Checkout details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/checkout [post]
func (h *ShoppingAgentHandler) Checkout(c *gin.Context) {
	userID := c.GetString("user_id")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	var req CheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	checkoutReq := &services.CheckoutRequest{
		UserID:          userID,
		BusinessID:      businessID,
		CartMandateID:   req.CartMandateID,
		PaymentMethodID: req.PaymentMethodID,
	}

	paymentMandate, err := h.svc.CompleteCheckout(c.Request.Context(), checkoutReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, paymentMandate)
}

// GetCart retrieves a cart by ID
// @Summary Get cart
// @Description Returns a shopping cart by ID
// @Tags Shopping
// @Produce json
// @Security BearerAuth
// @Param id path string true "Cart ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/cart/{id} [get]
func (h *ShoppingAgentHandler) GetCart(c *gin.Context) {
	cartID := c.Param("id")
	userID := middleware.GetUserID(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	cartMandate, err := h.svc.GetCartMandateForScope(c.Request.Context(), cartID, userID, businessID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cart not found"})
		return
	}

	c.JSON(http.StatusOK, cartMandate)
}

// ListCarts lists all carts for the user
// @Summary List carts
// @Description Returns all shopping carts for the user
// @Tags Shopping
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/carts [get]
func (h *ShoppingAgentHandler) ListCarts(c *gin.Context) {
	userID := c.GetString("user_id")
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)

	carts, total, err := h.svc.GetUserCarts(c.Request.Context(), userID, businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  carts,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// ListOrders lists all orders for the user
// @Summary List orders
// @Description Returns all marketplace orders for the user
// @Tags Shopping
// @Produce json
// @Security BearerAuth
// @Param status query string false "Filter by status"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/orders [get]
func (h *ShoppingAgentHandler) ListOrders(c *gin.Context) {
	userID := c.GetString("user_id")
	page, limit := utils.ParsePagination(c)
	status := c.Query("status")

	var orders []*models.MarketplaceOrder
	var total int64
	var err error

	if status != "" {
		orders, total, err = h.svc.GetUserOrders(c.Request.Context(), userID, page, limit)
	} else {
		orders, total, err = h.svc.GetUserOrders(c.Request.Context(), userID, page, limit)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  orders,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// TrackOrder tracks an order by ID
// @Summary Track order
// @Description Returns tracking information for an order
// @Tags Shopping
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/orders/{id} [get]
func (h *ShoppingAgentHandler) TrackOrder(c *gin.Context) {
	orderID := c.Param("id")
	userID := c.GetString("user_id")

	order, err := h.svc.TrackOrder(c.Request.Context(), orderID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	c.JSON(http.StatusOK, order)
}

// GetAvailableProducts retrieves available products
// @Summary Get available products
// @Description Returns all available marketplace products
// @Tags Shopping
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/products/available [get]
func (h *ShoppingAgentHandler) GetAvailableProducts(c *gin.Context) {
	page, limit := utils.ParsePagination(c)

	products, total, err := h.svc.GetAvailableProducts(c.Request.Context(), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetProductDetails retrieves product details
// @Summary Get product details
// @Description Returns details for a specific marketplace product
// @Tags Shopping
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/products/{id} [get]
func (h *ShoppingAgentHandler) GetProductDetails(c *gin.Context) {
	productID := c.Param("id")

	product, err := h.svc.GetProductDetails(c.Request.Context(), productID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}

// GetAgentCapabilities retrieves shopping agent capabilities
// @Summary Get shopping agent capabilities
// @Description Returns capabilities for a shopping agent
// @Tags Shopping
// @Produce json
// @Security BearerAuth
// @Param id path string true "Agent ID"
// @Success 200 {object} interface{}
// @Failure 500 {object} map[string]string
// @Router /agents/shopping/{id}/capabilities [get]
func (h *ShoppingAgentHandler) GetAgentCapabilities(c *gin.Context) {
	agentID := c.Param("id")

	capabilities, err := h.svc.GetShoppingAgentCapabilities(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, capabilities)
}

// GenerateIdeasRequest represents generate ideas request
type GenerateIdeasRequest struct {
	Input string `json:"input" binding:"required"`
}

// GenerateIdeas generates ideas using LLM
// @Summary Generate shopping ideas
// @Description Generates shopping ideas using LLM
// @Tags Shopping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body GenerateIdeasRequest true "Idea request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /agents/ideate [post]
func (h *ShoppingAgentHandler) GenerateIdeas(c *gin.Context) {
	var req GenerateIdeasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.svc.GenerateIdeas(c.Request.Context(), req.Input)
	if err != nil {
		h.log.Error("failed to generate ideas", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate ideas"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"response": response})
}
