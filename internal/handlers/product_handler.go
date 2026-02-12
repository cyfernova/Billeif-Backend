package handlers

import (
	"invoice-backend/internal/models"
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type ProductHandler struct {
	svc *services.ProductService
	log *logger.Logger
}

func NewProductHandler(svc *services.ProductService, log *logger.Logger) *ProductHandler {
	return &ProductHandler{svc: svc, log: log}
}

// Create creates a new product
// @Summary Create product
// @Description Create a new product for a business.
// @Tags Products
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateProductInput true "Product details"
// @Success 201 {object} models.Product
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /products [post]
func (h *ProductHandler) Create(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "create")
	var input services.CreateProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid create product payload", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var product *models.Product
	product, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		log.Error("failed to create product", "error", err, "business_id", input.BusinessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product created", "product_id", product.ID, "business_id", product.BusinessID)

	c.JSON(http.StatusCreated, product)
}

// Get retrieves a product by ID
// @Summary Get product
// @Description Returns the details of a specific product.
// @Tags Products
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Success 200 {object} models.Product
// @Failure 404 {object} map[string]string
// @Router /products/{id} [get]
func (h *ProductHandler) Get(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "get")
	id := c.Param("id")
	var product *models.Product
	product, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		log.Error("failed to get product", "error", err, "product_id", id)
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}

// List retrieves all products for a business
// @Summary List products
// @Description Returns a list of products belonging to a specific business.
// @Tags Products
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /products [get]
func (h *ProductHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "list")
	businessID := c.Query("business_id")
	if businessID == "" {
		log.Warn("missing business_id query param")
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

	products, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		log.Error("failed to list products", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("products listed", "business_id", businessID, "count", len(products), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  products,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Update updates a product
// @Summary Update product
// @Description Update the details of a specific product.
// @Tags Products
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param input body services.UpdateProductInput true "Product updates"
// @Success 200 {object} models.Product
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /products/{id} [put]
func (h *ProductHandler) Update(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "update")
	id := c.Param("id")
	var input services.UpdateProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid update product payload", "error", err, "product_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var product *models.Product
	product, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		log.Error("failed to update product", "error", err, "product_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product updated", "product_id", product.ID)

	c.JSON(http.StatusOK, product)
}

// Delete deletes a product
// @Summary Delete product
// @Description Remove a specific product.
// @Tags Products
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Success 204 "No Content"
// @Failure 500 {object} map[string]string
// @Router /products/{id} [delete]
func (h *ProductHandler) Delete(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "delete")
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		log.Error("failed to delete product", "error", err, "product_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product deleted", "product_id", id)

	c.JSON(http.StatusNoContent, nil)
}

// UploadImage generates a presigned URL for product image upload
// @Summary Upload product image
// @Description Returns a presigned S3 URL to upload a product image.
// @Tags Products
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param Content-Type header string false "MIME type (default: image/png)"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /products/{id}/image [post]
func (h *ProductHandler) UploadImage(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "upload_image")
	id := c.Param("id")
	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}

	url, err := h.svc.GetImageUploadURL(c.Request.Context(), id, contentType)
	if err != nil {
		log.Error("failed to generate product image upload URL", "error", err, "product_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product image upload URL generated", "product_id", id)

	c.JSON(http.StatusOK, gin.H{"upload_url": url})
}

// AdjustStock adjusts the stock level of a product
// @Summary Adjust stock
// @Description Add or remove stock for a specific product.
// @Tags Products
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param input body services.StockAdjustmentInput true "Stock adjustment details"
// @Success 200 {object} models.Product
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /products/{id}/stock [post]
func (h *ProductHandler) AdjustStock(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "adjust_stock")
	id := c.Param("id")
	var input services.StockAdjustmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid stock adjustment payload", "error", err, "product_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	product, err := h.svc.AdjustStock(c.Request.Context(), id, input)
	if err != nil {
		log.Error("failed to adjust stock", "error", err, "product_id", id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product stock adjusted", "product_id", id, "stock_level", product.StockLevel)

	c.JSON(http.StatusOK, product)
}
