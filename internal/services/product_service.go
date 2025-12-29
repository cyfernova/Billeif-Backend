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
	BusinessID  string  `json:"business_id" binding:"required,uuid"`
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

func (s *ProductService) Get(ctx context.Context, id string) (*models.Product, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *ProductService) GetBySKU(ctx context.Context, businessID, sku string) (*models.Product, error) {
	return s.repo.GetBySKU(ctx, businessID, sku)
}

func (s *ProductService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Product, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
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
	product, err := s.repo.GetByID(ctx, id)
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

func (s *ProductService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

type StockAdjustmentInput struct {
	Quantity int64  `json:"quantity" binding:"required"`
	Reason   string `json:"reason"`
}

func (s *ProductService) AdjustStock(ctx context.Context, productID string, input StockAdjustmentInput) (*models.Product, error) {
	if err := s.repo.AdjustStock(ctx, productID, input.Quantity); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, productID)
}

func (s *ProductService) GetImageUploadURL(ctx context.Context, productID, contentType string) (string, error) {
	key := fmt.Sprintf("products/%s/image", productID)
	return s.s3.GeneratePresignedUploadURL(ctx, "product-images", key, contentType, 3600)
}

func (s *ProductService) UpdateImageURL(ctx context.Context, productID, imageURL string) error {
	product, err := s.repo.GetByID(ctx, productID)
	if err != nil {
		return err
	}
	product.ImageURL = imageURL
	return s.repo.Update(ctx, product)
}
