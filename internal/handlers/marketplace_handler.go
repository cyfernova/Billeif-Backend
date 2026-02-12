package handlers

import (
	"encoding/json"
	"net/http"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type MarketplaceHandler struct {
	svc     *services.MarketplaceService
	ap2Repo interfaces.AP2Repository
	log     *logger.Logger
}

func NewMarketplaceHandler(svc *services.MarketplaceService, ap2Repo interfaces.AP2Repository, log *logger.Logger) *MarketplaceHandler {
	return &MarketplaceHandler{svc: svc, ap2Repo: ap2Repo, log: log}
}

func (h *MarketplaceHandler) reqLog(c *gin.Context, operation string) *logger.Logger {
	return logger.FromContext(c.Request.Context()).Named("marketplace_handler").With("operation", operation)
}

type CreateProductRequest struct {
	Name           string   `json:"name" binding:"required"`
	Description    string   `json:"description"`
	Price          float64  `json:"price" binding:"required"`
	Currency       string   `json:"currency"`
	InventoryCount int      `json:"inventory_count"`
	IsAvailable    bool     `json:"is_available"`
	Images         []string `json:"images"`
	Categories     []string `json:"categories"`
}

type UpdateProductRequest struct {
	Name           *string  `json:"name"`
	Description    *string  `json:"description"`
	Price          *float64 `json:"price"`
	Currency       *string  `json:"currency"`
	InventoryCount *int     `json:"inventory_count"`
	IsAvailable    *bool    `json:"is_available"`
	Images         []string `json:"images"`
	Categories     []string `json:"categories"`
}

// ListProducts lists marketplace products
// @Summary List products
// @Description List marketplace products with optional category and agent filters.
// @Tags Marketplace
// @Produce json
// @Security BearerAuth
// @Param category query string false "Category filter"
// @Param agent_id query string false "Agent ID filter"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /marketplace/products [get]
func (h *MarketplaceHandler) ListProducts(c *gin.Context) {
	log := h.reqLog(c, "list_products")
	page, limit := utils.ParsePagination(c)
	category := c.Query("category")
	agentID := c.Query("agent_id")

	filters := make(map[string]interface{})
	if category != "" {
		filters["category"] = category
	}
	if agentID != "" {
		filters["agent_id"] = agentID
	}

	products, total, err := h.svc.ListProducts(c.Request.Context(), filters, page, limit)
	if err != nil {
		log.Error("failed to list marketplace products", "error", err, "filters", filters)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("marketplace products listed", "count", len(products), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// SearchProducts searches marketplace products
// @Summary Search products
// @Description Search marketplace products by query string.
// @Tags Marketplace
// @Produce json
// @Security BearerAuth
// @Param q query string false "Search query"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /marketplace/products/search [get]
func (h *MarketplaceHandler) SearchProducts(c *gin.Context) {
	log := h.reqLog(c, "search_products")
	query := c.Query("q")
	page, limit := utils.ParsePagination(c)

	products, total, err := h.svc.SearchProducts(c.Request.Context(), query, page, limit)
	if err != nil {
		log.Error("failed to search marketplace products", "error", err, "query", query)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("marketplace products search completed", "query", query, "count", len(products), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetProduct retrieves a product by ID
// @Summary Get product
// @Description Retrieve a specific marketplace product by its ID.
// @Tags Marketplace
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Success 200 {object} models.MarketplaceProduct
// @Failure 404 {object} map[string]string
// @Router /marketplace/products/{id} [get]
func (h *MarketplaceHandler) GetProduct(c *gin.Context) {
	log := h.reqLog(c, "get_product")
	id := c.Param("id")

	product, err := h.svc.GetProduct(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get marketplace product", "error", err, "product_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}

// GetAvailableProducts retrieves all available products
// @Summary Get available products
// @Description Retrieve all currently available marketplace products.
// @Tags Marketplace
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /marketplace/products/available [get]
func (h *MarketplaceHandler) GetAvailableProducts(c *gin.Context) {
	log := h.reqLog(c, "get_available_products")
	page, limit := utils.ParsePagination(c)

	products, total, err := h.svc.GetAvailableProducts(c.Request.Context(), page, limit)
	if err != nil {
		log.Error("failed to get available marketplace products", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("available marketplace products listed", "count", len(products), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetMerchantProducts retrieves products for a merchant agent
// @Summary Get merchant products
// @Description Retrieve all products belonging to a specific merchant agent.
// @Tags Marketplace
// @Produce json
// @Security BearerAuth
// @Param agent_id query string true "Agent ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /marketplace/merchant/products [get]
func (h *MarketplaceHandler) GetMerchantProducts(c *gin.Context) {
	log := h.reqLog(c, "get_merchant_products")
	agentID := c.Query("agent_id")
	if agentID == "" {
		log.Warn("missing agent_id query param")
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

	products, total, err := h.svc.GetMerchantProducts(c.Request.Context(), agentID, page, limit)
	if err != nil {
		log.Error("failed to get merchant products", "error", err, "agent_id", agentID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("merchant products listed", "agent_id", agentID, "count", len(products), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// AddProduct adds a new product to the marketplace
// @Summary Add product
// @Description Add a new product to the marketplace for a merchant agent.
// @Tags Marketplace
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param agent_id query string true "Agent ID"
// @Param input body CreateProductRequest true "Product details"
// @Success 201 {object} models.MarketplaceProduct
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /marketplace/merchant/products [post]
func (h *MarketplaceHandler) AddProduct(c *gin.Context) {
	log := h.reqLog(c, "add_product")
	agentID := c.Query("agent_id")
	if agentID == "" {
		log.Warn("missing agent_id query param")
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	var req CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warn("invalid add product payload", "error", err, "agent_id", agentID)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Currency == "" {
		req.Currency = "INR"
	}

	imagesJSON, _ := json.Marshal(req.Images)
	categoriesJSON, _ := json.Marshal(req.Categories)

	product := &models.MarketplaceProduct{
		AgentID:        agentID,
		Name:           req.Name,
		Description:    &req.Description,
		Price:          req.Price,
		Currency:       req.Currency,
		InventoryCount: req.InventoryCount,
		IsAvailable:    req.IsAvailable,
		Images:         string(imagesJSON),
		Categories:     string(categoriesJSON),
	}

	if err := h.ap2Repo.CreateMarketplaceProduct(c.Request.Context(), product); err != nil {
		log.Error("failed to create marketplace product", "error", err, "agent_id", agentID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("marketplace product created", "product_id", product.ID, "agent_id", agentID)

	c.JSON(http.StatusCreated, product)
}

// UpdateProduct updates an existing product
// @Summary Update product
// @Description Update an existing marketplace product.
// @Tags Marketplace
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param input body UpdateProductRequest true "Product update details"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /marketplace/merchant/products/{id} [put]
func (h *MarketplaceHandler) UpdateProduct(c *gin.Context) {
	log := h.reqLog(c, "update_product")
	id := c.Param("id")

	product, err := h.ap2Repo.GetMarketplaceProductByID(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to load marketplace product for update", "error", err, "product_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	var req UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warn("invalid update product payload", "error", err, "product_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		product.Name = *req.Name
	}
	if req.Description != nil {
		product.Description = req.Description
	}
	if req.Price != nil {
		product.Price = *req.Price
	}
	if req.Currency != nil {
		product.Currency = *req.Currency
	}
	if req.InventoryCount != nil {
		product.InventoryCount = *req.InventoryCount
	}
	if req.IsAvailable != nil {
		product.IsAvailable = *req.IsAvailable
	}
	if req.Images != nil {
		imagesJSON, _ := json.Marshal(req.Images)
		product.Images = string(imagesJSON)
	}
	if req.Categories != nil {
		categoriesJSON, _ := json.Marshal(req.Categories)
		product.Categories = string(categoriesJSON)
	}

	if err := h.ap2Repo.UpdateMarketplaceProduct(c.Request.Context(), product); err != nil {
		log.Error("failed to update marketplace product", "error", err, "product_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("marketplace product updated", "product_id", id)

	c.JSON(http.StatusOK, gin.H{"message": "product updated successfully"})
}

// GetUserOrders retrieves orders for the authenticated user
// @Summary Get user orders
// @Description Retrieve orders for the authenticated user, optionally filtered by status.
// @Tags Marketplace
// @Produce json
// @Security BearerAuth
// @Param status query string false "Order status filter"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /marketplace/orders [get]
func (h *MarketplaceHandler) GetUserOrders(c *gin.Context) {
	log := h.reqLog(c, "get_user_orders")
	userID := c.GetString("user_id")
	page, limit := utils.ParsePagination(c)
	status := c.Query("status")

	var orders []*models.MarketplaceOrder
	var total int64
	var err error

	if status != "" {
		orders, total, err = h.svc.GetOrdersByStatus(c.Request.Context(), userID, status, page, limit)
	} else {
		orders, total, err = h.svc.GetUserOrders(c.Request.Context(), userID, page, limit)
	}

	if err != nil {
		log.Error("failed to get user marketplace orders", "error", err, "user_id", userID, "status", status)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("user marketplace orders listed", "user_id", userID, "count", len(orders), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  orders,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetOrdersByStatus retrieves orders filtered by status
// @Summary Get orders by status
// @Description Retrieve orders for the authenticated user filtered by a specific status.
// @Tags Marketplace
// @Produce json
// @Security BearerAuth
// @Param status path string true "Order status"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /marketplace/orders/status/{status} [get]
func (h *MarketplaceHandler) GetOrdersByStatus(c *gin.Context) {
	log := h.reqLog(c, "get_orders_by_status")
	userID := c.GetString("user_id")
	status := c.Param("status")
	page, limit := utils.ParsePagination(c)

	orders, total, err := h.svc.GetOrdersByStatus(c.Request.Context(), userID, status, page, limit)
	if err != nil {
		log.Error("failed to get orders by status", "error", err, "user_id", userID, "status", status)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("orders by status listed", "status", status, "count", len(orders), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  orders,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GetMarketplaceStats retrieves marketplace statistics
// @Summary Get marketplace stats
// @Description Retrieve overall marketplace statistics.
// @Tags Marketplace
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /marketplace/stats [get]
func (h *MarketplaceHandler) GetMarketplaceStats(c *gin.Context) {
	log := h.reqLog(c, "get_marketplace_stats")
	stats, err := h.svc.GetMarketplaceStats(c.Request.Context())
	if err != nil {
		log.Error("failed to get marketplace stats", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("marketplace stats fetched")

	c.JSON(http.StatusOK, stats)
}

func (h *MarketplaceHandler) GetProductDetails(c *gin.Context) {
	log := h.reqLog(c, "get_product_details")
	id := c.Param("id")

	product, err := h.svc.GetProduct(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get marketplace product details", "error", err, "product_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	log.Debug("marketplace product details fetched", "product_id", id)

	c.JSON(http.StatusOK, product)
}
