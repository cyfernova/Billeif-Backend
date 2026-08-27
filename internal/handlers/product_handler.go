package handlers

import (
	"context"
	"invoice-backend/internal/models"
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type ProductHandler struct {
	svc productService
	log *logger.Logger
}

type productService interface {
	Create(context.Context, services.CreateProductInput) (*models.Product, error)
	GetByBusiness(context.Context, string, string) (*models.Product, error)
	ListWithFilters(context.Context, string, services.ProductListFilter, int, int) ([]*models.Product, int64, error)
	UpdateByBusiness(context.Context, string, string, services.UpdateProductInput) (*models.Product, error)
	CloneByBusiness(context.Context, string, string) (*models.Product, error)
	DeleteByBusiness(context.Context, string, string) error
	GetImageUploadURLByBusiness(context.Context, string, string, string) (string, error)
	AdjustStockByBusiness(context.Context, string, string, services.StockAdjustmentInput) (*models.Product, error)
}

func NewProductHandler(svc productService, log *logger.Logger) *ProductHandler {
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
	if input.BusinessID == "" {
		input.BusinessID = c.Query("business_id")
	}
	businessID, ok := requireEffectiveBusinessScope(c, input.BusinessID)
	if !ok {
		return
	}
	input.BusinessID = businessID

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
// @Param business_id query string false "Business ID"
// @Success 200 {object} models.Product
// @Failure 404 {object} map[string]string
// @Router /products/{id} [get]
func (h *ProductHandler) Get(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "get")

	businessID, ok := requireEffectiveBusinessScope(c, "")
	if !ok {
		return
	}

	id := c.Param("id")
	var product *models.Product
	product, err := h.svc.GetByBusiness(c.Request.Context(), businessID, id)
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

	businessID, ok := requireEffectiveBusinessScope(c, "")
	if !ok {
		return
	}

	page, limit := utils.ParsePagination(c)
	filter := services.ProductListFilter{
		CategoryID:   c.Query("category_id"),
		WarehouseID:  c.Query("warehouse_id"),
		Query:        c.Query("query"),
		LowStockOnly: c.Query("low_stock") == "true",
	}

	products, total, err := h.svc.ListWithFilters(c.Request.Context(), businessID, filter, page, limit)
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
// @Param business_id query string false "Business ID"
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

	businessID, ok := requireEffectiveBusinessScope(c, "")
	if !ok {
		return
	}

	var product *models.Product
	product, err := h.svc.UpdateByBusiness(c.Request.Context(), businessID, id, input)
	if err != nil {
		log.Error("failed to update product", "error", err, "product_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product updated", "product_id", product.ID)

	c.JSON(http.StatusOK, product)
}

func (h *ProductHandler) Clone(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "clone")

	businessID, ok := requireEffectiveBusinessScope(c, "")
	if !ok {
		return
	}

	product, err := h.svc.CloneByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		log.Error("failed to clone product", "error", err)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, product)
}

// Delete deletes a product
// @Summary Delete product
// @Description Remove a specific product.
// @Tags Products
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param business_id query string false "Business ID"
// @Success 204 "No Content"
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /products/{id} [delete]
func (h *ProductHandler) Delete(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "delete")

	businessID, ok := requireEffectiveBusinessScope(c, "")
	if !ok {
		return
	}

	id := c.Param("id")
	if err := h.svc.DeleteByBusiness(c.Request.Context(), businessID, id); err != nil {
		log.Error("failed to delete product", "error", err, "product_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product deleted", "product_id", id)

	c.JSON(http.StatusNoContent, nil)
}

// UploadImage generates a presigned URL for product image upload
// @Summary Upload product image
// @Description Returns a presigned S3 URL and the exact headers required to upload a product image. Uploads are limited to 5 MiB.
// @Tags Products
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param Content-Type header string false "MIME type (default: image/png)"
// @Param size_bytes query int true "Exact upload size in bytes" minimum(1) maximum(5242880)
// @Success 200 {object} services.PresignedUpload
// @Failure 400 {object} map[string]string
// @Failure 413 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /products/{id}/image [post]
func (h *ProductHandler) UploadImage(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("product_handler").With("operation", "upload_image")

	businessID, ok := requireEffectiveBusinessScope(c, "")
	if !ok {
		return
	}

	id := c.Param("id")
	contentType, ok2 := validateImageContentType(c)
	if !ok2 {
		return
	}
	sizeBytes, ok := requireUploadSizeBytes(c, services.MaxProductImageUploadBytes)
	if !ok {
		return
	}

	upload, err := h.svc.GetImageUploadURLByBusiness(c.Request.Context(), businessID, id, contentType, sizeBytes)
	if err != nil {
		log.Error("failed to generate product image upload URL", "error", err, "product_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product image upload URL generated", "product_id", id)

	c.JSON(http.StatusOK, upload)
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

	businessID, ok := requireEffectiveBusinessScope(c, "")
	if !ok {
		return
	}

	id := c.Param("id")
	var input services.StockAdjustmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		log.Warn("invalid stock adjustment payload", "error", err, "product_id", id)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	product, err := h.svc.AdjustStockByBusiness(c.Request.Context(), businessID, id, input)
	if err != nil {
		log.Error("failed to adjust stock", "error", err, "product_id", id)
		if isNotFoundErr(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("product stock adjusted", "product_id", id, "stock_level", product.StockLevel)

	c.JSON(http.StatusOK, product)
}
