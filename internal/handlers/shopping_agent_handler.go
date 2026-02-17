package handlers

import (
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

type CheckoutRequest struct {
	CartMandateID   string  `json:"cart_mandate_id" binding:"required"`
	PaymentMethodID *string `json:"payment_method_id"`
}

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

	var req AddToCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cartMandate, err := h.svc.AddToCart(c.Request.Context(), userID, shoppingAgentID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, cartMandate)
}

func (h *ShoppingAgentHandler) Checkout(c *gin.Context) {
	userID := c.GetString("user_id")

	var req CheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	checkoutReq := &services.CheckoutRequest{
		UserID:          userID,
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

func (h *ShoppingAgentHandler) GetCart(c *gin.Context) {
	cartID := c.Param("id")
	userID := middleware.GetUserID(c)

	cartMandate, err := h.svc.GetCartMandate(c.Request.Context(), cartID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cart not found"})
		return
	}

	c.JSON(http.StatusOK, cartMandate)
}

func (h *ShoppingAgentHandler) ListCarts(c *gin.Context) {
	userID := c.GetString("user_id")
	page, limit := utils.ParsePagination(c)

	carts, total, err := h.svc.GetUserCarts(c.Request.Context(), userID, page, limit)
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

func (h *ShoppingAgentHandler) TrackOrder(c *gin.Context) {
	orderID := c.Param("id")

	order, err := h.svc.TrackOrder(c.Request.Context(), orderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	c.JSON(http.StatusOK, order)
}

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

func (h *ShoppingAgentHandler) GetProductDetails(c *gin.Context) {
	productID := c.Param("id")

	product, err := h.svc.GetProductDetails(c.Request.Context(), productID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}

func (h *ShoppingAgentHandler) GetAgentCapabilities(c *gin.Context) {
	agentID := c.Param("id")

	capabilities, err := h.svc.GetShoppingAgentCapabilities(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, capabilities)
}

func (h *ShoppingAgentHandler) GenerateIdeas(c *gin.Context) {
	var req struct {
		Input string `json:"input" binding:"required"`
	}

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
