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

func (h *MarketplaceHandler) ListProducts(c *gin.Context) {
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

func (h *MarketplaceHandler) SearchProducts(c *gin.Context) {
	query := c.Query("q")
	page, limit := utils.ParsePagination(c)

	products, total, err := h.svc.SearchProducts(c.Request.Context(), query, page, limit)
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

func (h *MarketplaceHandler) GetProduct(c *gin.Context) {
	id := c.Param("id")

	product, err := h.svc.GetProduct(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}

func (h *MarketplaceHandler) GetAvailableProducts(c *gin.Context) {
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

func (h *MarketplaceHandler) GetMerchantProducts(c *gin.Context) {
	agentID := c.Query("agent_id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

	products, total, err := h.svc.GetMerchantProducts(c.Request.Context(), agentID, page, limit)
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

func (h *MarketplaceHandler) AddProduct(c *gin.Context) {
	agentID := c.Query("agent_id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	var req CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, product)
}

func (h *MarketplaceHandler) UpdateProduct(c *gin.Context) {
	id := c.Param("id")

	product, err := h.ap2Repo.GetMarketplaceProductByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	var req UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "product updated successfully"})
}

func (h *MarketplaceHandler) GetUserOrders(c *gin.Context) {
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

func (h *MarketplaceHandler) GetOrdersByStatus(c *gin.Context) {
	userID := c.GetString("user_id")
	status := c.Param("status")
	page, limit := utils.ParsePagination(c)

	orders, total, err := h.svc.GetOrdersByStatus(c.Request.Context(), userID, status, page, limit)
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

func (h *MarketplaceHandler) GetMarketplaceStats(c *gin.Context) {
	stats, err := h.svc.GetMarketplaceStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

func (h *MarketplaceHandler) GetProductDetails(c *gin.Context) {
	id := c.Param("id")

	product, err := h.svc.GetProduct(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}
