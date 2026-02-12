package services

import (
	"context"
	"errors"
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
	BusinessID  string  `json:"business_id,omitempty"`
	Name        string  `json:"name" binding:"required,min=2"`
	SKU         string  `json:"sku" binding:"required"`
	Description string  `json:"description"`
	Price       float64 `json:"price" binding:"required,gt=0"`
	Currency    string  `json:"currency"`
	Unit        string  `json:"unit"`
	StockLevel  int64   `json:"stock_level"`
	MinStock    int64   `json:"min_stock"`
}

func (s *ProductService) Create(ctx context.Context, input CreateProductInput) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "create", "business_id", input.BusinessID, "sku", input.SKU)
	existing, _ := s.repo.GetBySKU(ctx, input.BusinessID, input.SKU)
	if existing != nil {
		log.Warn("duplicate SKU rejected")
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
		log.Error("failed to create product", "error", err)
		return nil, fmt.Errorf("failed to create product: %w", err)
	}

	log.Info("product created", "product_id", product.ID)
	return product, nil
}

func (s *ProductService) Get(ctx context.Context, id string) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "get", "product_id", id)
	product, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to get product", "error", err)
		return nil, err
	}
	return product, nil
}

func (s *ProductService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Product, error) {
	product, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if product.BusinessID != businessID {
		return nil, errors.New("product not found")
	}
	return product, nil
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
	Name        string  `json:"name"`
	SKU         string  `json:"sku"`
	Description string  `json:"description"`
	Price       float64 `json:"price"`
	Currency    string  `json:"currency"`
	Unit        string  `json:"unit"`
	MinStock    int64   `json:"min_stock"`
}

func (s *ProductService) Update(ctx context.Context, id string, input UpdateProductInput) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "update", "product_id", id)
	product, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to load product for update", "error", err)
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
		log.Error("failed to update product", "error", err)
		return nil, err
	}

	log.Info("product updated", "product_id", product.ID)
	return product, nil
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

func (s *ProductService) AdjustStock(ctx context.Context, productID string, input StockAdjustmentInput) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "adjust_stock", "product_id", productID, "quantity", input.Quantity)
	if err := s.repo.AdjustStock(ctx, productID, input.Quantity); err != nil {
		log.Error("failed to adjust stock", "error", err)
		return nil, err
	}
	product, err := s.repo.GetByID(ctx, productID)
	if err != nil {
		log.Error("failed to load product after stock adjustment", "error", err)
		return nil, err
	}
	log.Info("product stock adjusted", "product_id", productID, "stock_level", product.StockLevel)
	return product, nil
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
	updated, err := s.GetByBusiness(ctx, businessID, product.ID)
	if err != nil {
		log.Error("failed to load product after stock adjustment", "error", err)
		return nil, err
	}
	log.Info("product stock adjusted", "product_id", productID, "stock_level", updated.StockLevel)
	return updated, nil
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

func (s *ProductService) UpdateImageURL(ctx context.Context, productID, imageURL string) error {
	log := logger.FromContext(ctx).With("service", "product", "operation", "update_image_url", "product_id", productID)
	product, err := s.repo.GetByID(ctx, productID)
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
