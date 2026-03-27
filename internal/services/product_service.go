package services

import (
	"context"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type ProductService struct {
	repo interfaces.ProductRepository
	s3   *S3Service
	log  *logger.Logger
}

func NewProductService(repo interfaces.ProductRepository, s3 *S3Service, log *logger.Logger) *ProductService {
	return &ProductService{repo: repo, s3: s3, log: log}
}

type CreateProductInput struct {
	BusinessID      string  `json:"business_id,omitempty"`
	Name            string  `json:"name" binding:"required,min=2"`
	SKU             string  `json:"sku" binding:"required"`
	Description     string  `json:"description"`
	Price           float64 `json:"price" binding:"required,gt=0"`
	CostPrice       float64 `json:"cost_price"`
	ValuationMethod string  `json:"valuation_method"`
	HSNSACCode      string  `json:"hsn_sac_code"`
	IsService       bool    `json:"is_service"`
	Currency        string  `json:"currency"`
	Unit            string  `json:"unit"`
	StockLevel      int64   `json:"stock_level"`
	MinStock        int64   `json:"min_stock"`
}

func (s *ProductService) Create(ctx context.Context, input CreateProductInput) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "create", "business_id", input.BusinessID, "sku", input.SKU)
	existing, _ := s.repo.GetBySKU(ctx, input.BusinessID, input.SKU)
	if existing != nil {
		log.Warn("duplicate SKU rejected")
		return nil, fmt.Errorf("product with SKU %s already exists", input.SKU)
	}

	product := &models.Product{
		BusinessID:      input.BusinessID,
		Name:            input.Name,
		SKU:             input.SKU,
		Description:     input.Description,
		Price:           input.Price,
		CostPrice:       input.CostPrice,
		ValuationMethod: input.ValuationMethod,
		HSNSACCode:      input.HSNSACCode,
		IsService:       input.IsService,
		Currency:        input.Currency,
		Unit:            input.Unit,
		StockLevel:      input.StockLevel,
		MinStock:        input.MinStock,
		IsActive:        true,
	}

	if product.Currency == "" {
		product.Currency = "USD"
	}
	if product.Unit == "" {
		product.Unit = "PCS"
	}
	if product.ValuationMethod == "" {
		product.ValuationMethod = "last_purchase"
	}

	if err := s.repo.Create(ctx, product); err != nil {
		log.Error("failed to create product", "error", err)
		return nil, fmt.Errorf("failed to create product: %w", err)
	}

	log.Info("product created", "product_id", product.ID)
	return product, nil
}

func (s *ProductService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Product, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

func (s *ProductService) GetBySKU(ctx context.Context, businessID, sku string) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "get_by_sku", "business_id", businessID, "sku", sku)
	product, err := s.repo.GetBySKU(ctx, businessID, sku)
	if err != nil {
		log.Error("failed to get product by SKU", "error", err)
		return nil, err
	}
	return product, nil
}

func (s *ProductService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "list", "business_id", businessID, "page", page, "limit", limit)
	products, total, err := s.repo.GetByBusinessID(ctx, businessID, page, limit)
	if err != nil {
		log.Error("failed to list products", "error", err)
		return nil, 0, err
	}
	log.Debug("listed products", "count", len(products), "total", total)
	return products, total, nil
}

type UpdateProductInput struct {
	Name            string   `json:"name"`
	SKU             string   `json:"sku"`
	Description     string   `json:"description"`
	Price           float64  `json:"price"`
	CostPrice       *float64 `json:"cost_price"`
	ValuationMethod string   `json:"valuation_method"`
	HSNSACCode      string   `json:"hsn_sac_code"`
	IsService       *bool    `json:"is_service"`
	Currency        string   `json:"currency"`
	Unit            string   `json:"unit"`
	MinStock        int64    `json:"min_stock"`
}

func (s *ProductService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateProductInput) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "update", "product_id", id, "business_id", businessID)
	product, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load product for scoped update", "error", err)
		return nil, err
	}

	if input.Name != "" {
		product.Name = input.Name
	}
	if input.SKU != "" {
		product.SKU = input.SKU
	}
	if input.Description != "" {
		product.Description = input.Description
	}
	if input.Price > 0 {
		product.Price = input.Price
	}
	if input.CostPrice != nil {
		product.CostPrice = *input.CostPrice
	}
	if input.ValuationMethod != "" {
		product.ValuationMethod = input.ValuationMethod
	}
	if input.HSNSACCode != "" {
		product.HSNSACCode = input.HSNSACCode
	}
	if input.IsService != nil {
		product.IsService = *input.IsService
	}
	if input.Currency != "" {
		product.Currency = input.Currency
	}
	if input.Unit != "" {
		product.Unit = input.Unit
	}
	if input.MinStock >= 0 {
		product.MinStock = input.MinStock
	}

	if err := s.repo.Update(ctx, product); err != nil {
		log.Error("failed to update product", "error", err)
		return nil, err
	}

	log.Info("product updated", "product_id", product.ID)
	return product, nil
}

func (s *ProductService) Delete(ctx context.Context, id string) error {
	log := logger.FromContext(ctx).With("service", "product", "operation", "delete", "product_id", id)
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Error("failed to delete product", "error", err)
		return err
	}
	log.Info("product deleted", "product_id", id)
	return nil
}

func (s *ProductService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	log := logger.FromContext(ctx).With("service", "product", "operation", "delete", "product_id", id, "business_id", businessID)
	product, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load product for scoped delete", "error", err)
		return err
	}
	if err := s.repo.Delete(ctx, product.ID); err != nil {
		log.Error("failed to delete product", "error", err)
		return err
	}
	log.Info("product deleted", "product_id", product.ID)
	return nil
}

type StockAdjustmentInput struct {
	Quantity int64  `json:"quantity" binding:"required"`
	Reason   string `json:"reason"`
}

func (s *ProductService) AdjustStockByBusiness(ctx context.Context, businessID, productID string, input StockAdjustmentInput) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "adjust_stock", "product_id", productID, "quantity", input.Quantity, "business_id", businessID)
	product, err := s.GetByBusiness(ctx, businessID, productID)
	if err != nil {
		log.Error("failed to load product for stock adjustment", "error", err)
		return nil, err
	}
	if err := s.repo.AdjustStock(ctx, product.ID, input.Quantity); err != nil {
		log.Error("failed to adjust stock", "error", err)
		return nil, err
	}
	product.StockLevel += input.Quantity
	log.Info("product stock adjusted", "product_id", productID, "stock_level", product.StockLevel)
	return product, nil
}

func (s *ProductService) GetImageUploadURL(ctx context.Context, productID, contentType string) (string, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "get_image_upload_url", "product_id", productID)
	key := fmt.Sprintf("products/%s/image", productID)
	url, err := s.s3.GeneratePresignedUploadURL(ctx, "product-images", key, contentType, 3600)
	if err != nil {
		log.Error("failed to generate image upload URL", "error", err)
		return "", err
	}
	log.Debug("generated image upload URL", "product_id", productID)
	return url, nil
}

func (s *ProductService) GetImageUploadURLByBusiness(ctx context.Context, businessID, productID, contentType string) (string, error) {
	if _, err := s.GetByBusiness(ctx, businessID, productID); err != nil {
		return "", err
	}
	return s.GetImageUploadURL(ctx, productID, contentType)
}

func (s *ProductService) UpdateImageURL(ctx context.Context, businessID, productID, imageURL string) error {
	log := logger.FromContext(ctx).With("service", "product", "operation", "update_image_url", "product_id", productID)
	product, err := s.repo.GetByID(ctx, productID, businessID)
	if err != nil {
		log.Error("failed to load product for image update", "error", err)
		return err
	}
	product.ImageURL = imageURL
	if err := s.repo.Update(ctx, product); err != nil {
		log.Error("failed to persist product image URL", "error", err)
		return err
	}
	log.Info("product image URL updated", "product_id", productID)
	return nil
}

// ProductServiceTestable is a test-friendly version of ProductService
type ProductServiceTestable struct {
	repo ProductRepositoryTestable
	s3   S3ServiceTestable
	log  *logger.Logger
}

// S3ServiceTestable is the testable interface for S3 operations
type S3ServiceTestable interface {
	GeneratePresignedUploadURL(ctx context.Context, bucket, key, contentType string, expiresIn int64) (string, error)
}

// ProductRepositoryTestable is the testable interface for ProductRepository
type ProductRepositoryTestable interface {
	Create(ctx context.Context, product *models.Product) error
	GetByID(ctx context.Context, id, businessID string) (*models.Product, error)
	GetBySKU(ctx context.Context, businessID, sku string) (*models.Product, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error)
	Update(ctx context.Context, product *models.Product) error
	Delete(ctx context.Context, id string) error
	AdjustStock(ctx context.Context, productID string, quantity int64) error
}

// NewProductServiceForTesting creates a ProductServiceTestable for unit testing
func NewProductServiceForTesting(repo ProductRepositoryTestable, s3 S3ServiceTestable, log *logger.Logger) *ProductServiceTestable {
	return &ProductServiceTestable{
		repo: repo,
		s3:   s3,
		log:  log,
	}
}

// Create creates a product (testable version)
func (s *ProductServiceTestable) Create(ctx context.Context, input CreateProductInput) (*models.Product, error) {
	existing, _ := s.repo.GetBySKU(ctx, input.BusinessID, input.SKU)
	if existing != nil {
		return nil, fmt.Errorf("product with SKU %s already exists", input.SKU)
	}

	product := &models.Product{
		BusinessID:  input.BusinessID,
		Name:        input.Name,
		SKU:         input.SKU,
		Description: input.Description,
		Price:       input.Price,
		Currency:    input.Currency,
		Unit:        input.Unit,
		StockLevel:  input.StockLevel,
		MinStock:    input.MinStock,
		IsActive:    true,
	}

	if product.Currency == "" {
		product.Currency = "USD"
	}
	if product.Unit == "" {
		product.Unit = "PCS"
	}

	if err := s.repo.Create(ctx, product); err != nil {
		return nil, fmt.Errorf("failed to create product: %w", err)
	}

	return product, nil
}

// GetByBusiness retrieves a product by business ID and product ID
func (s *ProductServiceTestable) GetByBusiness(ctx context.Context, businessID, id string) (*models.Product, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

// GetBySKU retrieves a product by SKU
func (s *ProductServiceTestable) GetBySKU(ctx context.Context, businessID, sku string) (*models.Product, error) {
	return s.repo.GetBySKU(ctx, businessID, sku)
}

// List retrieves products for a business with pagination
func (s *ProductServiceTestable) List(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

// UpdateByBusiness updates a product (testable version)
func (s *ProductServiceTestable) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateProductInput) (*models.Product, error) {
	product, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if input.Name != "" {
		product.Name = input.Name
	}
	if input.SKU != "" {
		product.SKU = input.SKU
	}
	if input.Description != "" {
		product.Description = input.Description
	}
	if input.Price > 0 {
		product.Price = input.Price
	}
	if input.Currency != "" {
		product.Currency = input.Currency
	}
	if input.Unit != "" {
		product.Unit = input.Unit
	}
	if input.MinStock >= 0 {
		product.MinStock = input.MinStock
	}

	if err := s.repo.Update(ctx, product); err != nil {
		return nil, err
	}

	return product, nil
}

// DeleteByBusiness deletes a product (testable version)
func (s *ProductServiceTestable) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	product, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, product.ID)
}

// AdjustStockByBusiness adjusts product stock (testable version)
func (s *ProductServiceTestable) AdjustStockByBusiness(ctx context.Context, businessID, productID string, input StockAdjustmentInput) (*models.Product, error) {
	product, err := s.GetByBusiness(ctx, businessID, productID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.AdjustStock(ctx, product.ID, input.Quantity); err != nil {
		return nil, err
	}
	product.StockLevel += input.Quantity
	return product, nil
}

// GetImageUploadURLByBusiness generates presigned URL for product image
func (s *ProductServiceTestable) GetImageUploadURLByBusiness(ctx context.Context, businessID, productID, contentType string) (string, error) {
	if _, err := s.GetByBusiness(ctx, businessID, productID); err != nil {
		return "", err
	}
	key := fmt.Sprintf("products/%s/image", productID)
	return s.s3.GeneratePresignedUploadURL(ctx, "product-images", key, contentType, 3600)
}

// UpdateImageURL updates product image URL
func (s *ProductServiceTestable) UpdateImageURL(ctx context.Context, businessID, productID, imageURL string) error {
	product, err := s.repo.GetByID(ctx, productID, businessID)
	if err != nil {
		return err
	}
	product.ImageURL = imageURL
	return s.repo.Update(ctx, product)
}
