package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"invoice-backend/internal/gst"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ProductService struct {
	db        *gorm.DB
	repo      interfaces.ProductRepository
	s3        *S3Service
	inventory *InventoryService
	log       *logger.Logger
}

func NewProductService(db *gorm.DB, repo interfaces.ProductRepository, s3 *S3Service, inventory *InventoryService, log *logger.Logger) *ProductService {
	return &ProductService{db: db, repo: repo, s3: s3, inventory: inventory, log: log}
}

type CreateProductInput struct {
	BusinessID        string                 `json:"business_id,omitempty"`
	CategoryID        string                 `json:"category_id"`
	Name              string                 `json:"name" binding:"required,min=2"`
	SKU               string                 `json:"sku"`
	Barcode           string                 `json:"barcode"`
	Description       string                 `json:"description"`
	Price             float64                `json:"price" binding:"required,gt=0"`
	CostPrice         float64                `json:"cost_price"`
	ValuationMethod   string                 `json:"valuation_method"`
	HSNSACCode        string                 `json:"hsn_sac_code"`
	UQCCode           string                 `json:"uqc_code" swaggerignore:"true"`
	GSTMetadata       map[string]interface{} `json:"gst_metadata,omitempty"`
	IsService         bool                   `json:"is_service"`
	Currency          string                 `json:"currency"`
	Unit              string                 `json:"unit"`
	StockLevel        int64                  `json:"stock_level"`
	MinStock          int64                  `json:"min_stock"`
	LowStockThreshold int64                  `json:"low_stock_threshold"`
	Images            []ProductImageInput    `json:"images,omitempty"`
	CustomColumns     map[string]interface{} `json:"custom_columns,omitempty"`
	Variants          []ProductVariantInput  `json:"variants,omitempty"`
	Categories        []string               `json:"categories"` // e.g. ["electronics", "laptops", "computers"]
}

type ProductImageInput struct {
	URL       string `json:"url" binding:"required"`
	Key       string `json:"key"`
	AltText   string `json:"alt_text"`
	Position  int    `json:"position"`
	IsPrimary bool   `json:"is_primary"`
}

type ProductVariantInput struct {
	ID                string                 `json:"id,omitempty"`
	Name              string                 `json:"name"`
	SKU               string                 `json:"sku"`
	Barcode           string                 `json:"barcode"`
	Attributes        map[string]interface{} `json:"attributes,omitempty"`
	IsDefault         bool                   `json:"is_default"`
	TrackBatches      bool                   `json:"track_batches"`
	TrackSerials      bool                   `json:"track_serials"`
	Price             float64                `json:"price"`
	CostPrice         float64                `json:"cost_price"`
	StockLevel        float64                `json:"stock_level"`
	LowStockThreshold float64                `json:"low_stock_threshold"`
	Images            []ProductImageInput    `json:"images,omitempty"`
}

func (s *ProductService) Create(ctx context.Context, input CreateProductInput) (*models.Product, error) {
	// Auto-generate SKU if not provided
	if strings.TrimSpace(input.SKU) == "" {
		input.SKU = fmt.Sprintf("SKU-%s", uuid.New().String()[:8])
	}

	log := logger.FromContext(ctx).With("service", "product", "operation", "create", "business_id", input.BusinessID, "sku", input.SKU)
	if input.BusinessID == "" {
		log.Error("business_id is empty")
		return nil, fmt.Errorf("business_id is required")
	}
	existing, _ := s.repo.GetBySKU(ctx, input.BusinessID, input.SKU)
	if existing != nil {
		log.Warn("duplicate SKU rejected")
		return nil, fmt.Errorf("product with SKU %s already exists", input.SKU)
	}

	product := &models.Product{
		BusinessID:        input.BusinessID,
		CategoryID:        stringPointer(input.CategoryID),
		Name:              input.Name,
		SKU:               input.SKU,
		Barcode:           firstNonEmpty(strings.TrimSpace(input.Barcode), strings.TrimSpace(input.SKU)),
		Description:       input.Description,
		Price:             input.Price,
		CostPrice:         input.CostPrice,
		ValuationMethod:   input.ValuationMethod,
		HSNSACCode:        input.HSNSACCode,
		UQCCode:           input.UQCCode,
		GSTMetadata:       mustMarshalMap(input.GSTMetadata),
		IsService:         input.IsService,
		Currency:          input.Currency,
		Unit:              input.Unit,
		StockLevel:        0,
		MinStock:          input.MinStock,
		LowStockThreshold: input.LowStockThreshold,
		ExtraAttributes:   mustMarshalMap(nil),
		Categories:        input.Categories,
		IsActive:          true,
	}

	if product.Currency == "" {
		product.Currency = "USD"
	}
	product.Unit = gst.CanonicalProductUQC(input.Unit, input.UQCCode)
	product.UQCCode = product.Unit
	if product.ValuationMethod == "" {
		product.ValuationMethod = "last_purchase"
	}
	if product.LowStockThreshold == 0 {
		product.LowStockThreshold = product.MinStock
	}

	if s.db == nil {
		product.StockLevel = input.StockLevel
		log.Info("about to insert product", "product_business_id", product.BusinessID, "product_sku", product.SKU)
		if err := s.repo.Create(ctx, product); err != nil {
			log.Error("failed to create product", "error", err)
			return nil, fmt.Errorf("failed to create product: %w", err)
		}
		log.Info("product created", "product_id", product.ID)
		return product, nil
	}

	log.Info("about to begin transaction", "product_business_id", product.BusinessID, "input_business_id", input.BusinessID)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		log.Info("inside transaction", "product_business_id", product.BusinessID)
		if err := tx.Create(product).Error; err != nil {
			return err
		}
		if err := s.syncProductRelationsTx(tx, product, input.Images, input.CustomColumns, input.Variants); err != nil {
			return err
		}
		if input.StockLevel > 0 && s.inventory != nil && !product.IsService {
			defaultVariant, err := s.inventory.ensureDefaultVariantTx(tx, input.BusinessID, product)
			if err != nil {
				return err
			}
			if len(input.Variants) > 0 {
				variantRows, err := s.listProductVariantsTx(tx, product.ID)
				if err != nil {
					return err
				}
				if len(variantRows) > 0 {
					defaultVariant = variantRows[0]
					for _, row := range variantRows {
						if row.IsDefault {
							defaultVariant = row
							break
						}
					}
				}
			}
			_, err = s.inventory.applyInventoryMutationTx(tx, inventoryMutationInput{
				BusinessID:      input.BusinessID,
				Product:         product,
				Variant:         defaultVariant,
				WarehouseID:     "",
				Quantity:        float64(input.StockLevel),
				Reason:          "opening quantity",
				UnitCost:        product.CostPrice,
				TransactionType: models.InventoryTransactionTypeOpeningBalance,
			})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Error("failed to create product", "error", err)
		return nil, fmt.Errorf("failed to create product: %w", err)
	}

	product, _ = s.GetByBusiness(ctx, input.BusinessID, product.ID)
	log.Info("product created", "product_id", product.ID)
	return product, nil
}

func (s *ProductService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Product, error) {
	if s.db != nil {
		return s.getProductDetailed(ctx, businessID, id)
	}
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
	if s.db != nil {
		return s.listProductsDetailed(ctx, businessID, ProductListFilter{}, page, limit)
	}
	log := logger.FromContext(ctx).With("service", "product", "operation", "list", "business_id", businessID, "page", page, "limit", limit)
	products, total, err := s.repo.GetByBusinessID(ctx, businessID, page, limit)
	if err != nil {
		log.Error("failed to list products", "error", err)
		return nil, 0, err
	}
	log.Debug("listed products", "count", len(products), "total", total)
	return products, total, nil
}

func (s *ProductService) ListWithFilters(ctx context.Context, businessID string, filter ProductListFilter, page, limit int) ([]*models.Product, int64, error) {
	if s.db != nil {
		return s.listProductsDetailed(ctx, businessID, filter, page, limit)
	}
	return s.List(ctx, businessID, page, limit)
}

type UpdateProductInput struct {
	CategoryID        string                 `json:"category_id"`
	Name              string                 `json:"name"`
	SKU               string                 `json:"sku"`
	Barcode           string                 `json:"barcode"`
	Description       string                 `json:"description"`
	Price             float64                `json:"price"`
	CostPrice         *float64               `json:"cost_price"`
	ValuationMethod   string                 `json:"valuation_method"`
	HSNSACCode        string                 `json:"hsn_sac_code"`
	UQCCode           string                 `json:"uqc_code" swaggerignore:"true"`
	GSTMetadata       map[string]interface{} `json:"gst_metadata,omitempty"`
	IsService         *bool                  `json:"is_service"`
	Currency          string                 `json:"currency"`
	Unit              string                 `json:"unit"`
	MinStock          int64                  `json:"min_stock"`
	LowStockThreshold *int64                 `json:"low_stock_threshold,omitempty"`
	Images            []ProductImageInput    `json:"images,omitempty"`
	CustomColumns     map[string]interface{} `json:"custom_columns,omitempty"`
	Variants          []ProductVariantInput  `json:"variants,omitempty"`
	Categories        []string               `json:"categories"` // e.g. ["electronics", "laptops", "computers"]
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
	if input.CategoryID != "" {
		product.CategoryID = stringPointer(input.CategoryID)
	}
	if input.SKU != "" {
		product.SKU = input.SKU
	}
	if input.Barcode != "" {
		product.Barcode = input.Barcode
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
	if input.GSTMetadata != nil {
		product.GSTMetadata = mustMarshalMap(input.GSTMetadata)
	}
	if input.IsService != nil {
		product.IsService = *input.IsService
	}
	if input.Currency != "" {
		product.Currency = input.Currency
	}
	if input.Unit != "" || input.UQCCode != "" {
		product.Unit = gst.CanonicalProductUQC(input.Unit, input.UQCCode)
		product.UQCCode = product.Unit
	}
	if input.MinStock >= 0 {
		product.MinStock = input.MinStock
	}
	if input.LowStockThreshold != nil {
		product.LowStockThreshold = *input.LowStockThreshold
	}
	if input.Categories != nil {
		product.Categories = input.Categories
	}

	if s.db != nil {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Save(product).Error; err != nil {
				return err
			}
			return s.syncProductRelationsTx(tx, product, input.Images, input.CustomColumns, input.Variants)
		}); err != nil {
			log.Error("failed to update product", "error", err)
			return nil, err
		}
		product, _ = s.GetByBusiness(ctx, businessID, id)
		log.Info("product updated", "product_id", product.ID)
		return product, nil
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
	VariantID        string                 `json:"variant_id,omitempty"`
	WarehouseID      string                 `json:"warehouse_id,omitempty"`
	Quantity         int64                  `json:"quantity" binding:"required"`
	Reason           string                 `json:"reason"`
	UnitCost         float64                `json:"unit_cost"`
	BatchAllocations []BatchAllocationInput `json:"batch_allocations,omitempty"`
	SerialIDs        []string               `json:"serial_ids,omitempty"`
}

func (s *ProductService) AdjustStockByBusiness(ctx context.Context, businessID, productID string, input StockAdjustmentInput) (*models.Product, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "adjust_stock", "product_id", productID, "quantity", input.Quantity, "business_id", businessID)
	product, err := s.GetByBusiness(ctx, businessID, productID)
	if err != nil {
		log.Error("failed to load product for stock adjustment", "error", err)
		return nil, err
	}
	if s.inventory != nil {
		_, err = s.inventory.RecordAdjustment(ctx, InventoryAdjustmentInput{
			BusinessID:       businessID,
			ProductID:        productID,
			VariantID:        input.VariantID,
			WarehouseID:      input.WarehouseID,
			Quantity:         float64(input.Quantity),
			Reason:           input.Reason,
			UnitCost:         input.UnitCost,
			BatchAllocations: input.BatchAllocations,
			SerialIDs:        input.SerialIDs,
		})
		if err != nil {
			log.Error("failed to adjust stock", "error", err)
			return nil, err
		}
		return s.GetByBusiness(ctx, businessID, productID)
	}
	if err := s.repo.AdjustStock(ctx, product.ID, input.Quantity); err != nil {
		log.Error("failed to adjust stock", "error", err)
		return nil, err
	}
	product.StockLevel += input.Quantity
	log.Info("product stock adjusted", "product_id", productID, "stock_level", product.StockLevel)
	return product, nil
}

func (s *ProductService) GetImageUploadURL(ctx context.Context, productID, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	log := logger.FromContext(ctx).With("service", "product", "operation", "get_image_upload_url", "product_id", productID)
	if s.s3 == nil || s.s3.cfg == nil || strings.TrimSpace(s.s3.cfg.S3.BucketProducts) == "" {
		return nil, fmt.Errorf("product image storage is not configured")
	}
	contentType, err := NormalizeImageUploadContentType(contentType)
	if err != nil {
		return nil, err
	}
	if err := validateUploadSize("product image", sizeBytes, MaxProductImageUploadBytes); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("products/%s/image", productID)
	upload, err := s.s3.GeneratePresignedUpload(ctx, s.s3.cfg.S3.BucketProducts, key, contentType, sizeBytes, 3600)
	if err != nil {
		log.Error("failed to generate image upload URL", "error", err)
		return nil, err
	}
	log.Debug("generated image upload URL", "product_id", productID)
	return upload, nil
}

func (s *ProductService) GetImageUploadURLByBusiness(ctx context.Context, businessID, productID, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	if _, err := s.GetByBusiness(ctx, businessID, productID); err != nil {
		return nil, err
	}
	return s.GetImageUploadURL(ctx, productID, contentType, sizeBytes)
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

type ProductListFilter struct {
	CategoryID   string
	WarehouseID  string
	Query        string
	LowStockOnly bool
}

func (s *ProductService) CloneByBusiness(ctx context.Context, businessID, id string) (*models.Product, error) {
	product, err := s.getProductDetailed(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	cloneSKU := fmt.Sprintf("%s-CLONE", product.SKU)
	if _, err := s.repo.GetBySKU(ctx, businessID, cloneSKU); err == nil {
		cloneSKU = fmt.Sprintf("%s-%d", cloneSKU, timeNowUnix())
	}
	input := CreateProductInput{
		BusinessID:        businessID,
		CategoryID:        derefString(product.CategoryID),
		Name:              product.Name + " Copy",
		SKU:               cloneSKU,
		Barcode:           "",
		Description:       product.Description,
		Price:             product.Price,
		CostPrice:         product.CostPrice,
		ValuationMethod:   product.ValuationMethod,
		HSNSACCode:        product.HSNSACCode,
		UQCCode:           product.Unit,
		GSTMetadata:       unmarshalJSONMap(product.GSTMetadata),
		IsService:         product.IsService,
		Currency:          product.Currency,
		Unit:              product.Unit,
		MinStock:          product.MinStock,
		LowStockThreshold: product.LowStockThreshold,
		CustomColumns:     product.CustomColumns,
	}
	for _, image := range product.Images {
		input.Images = append(input.Images, ProductImageInput{
			URL:       image.URL,
			Key:       image.Key,
			AltText:   image.AltText,
			Position:  image.Position,
			IsPrimary: image.IsPrimary,
		})
	}
	for _, variant := range product.Variants {
		input.Variants = append(input.Variants, ProductVariantInput{
			Name:              variant.Name,
			SKU:               cloneVariantSKU(variant.SKU),
			Barcode:           "",
			Attributes:        unmarshalJSONMap(variant.Attributes),
			IsDefault:         variant.IsDefault,
			TrackBatches:      variant.TrackBatches,
			TrackSerials:      variant.TrackSerials,
			Price:             variant.Price,
			CostPrice:         variant.CostPrice,
			LowStockThreshold: variant.LowStockThreshold,
		})
	}
	return s.Create(ctx, input)
}

func cloneVariantSKU(value string) string {
	if strings.TrimSpace(value) == "" {
		return fmt.Sprintf("VAR-%d", timeNowUnix())
	}
	return fmt.Sprintf("%s-CLONE", value)
}

func timeNowUnix() int64 {
	return time.Now().UnixNano() % 1000000
}

func (s *ProductService) listProductsDetailed(ctx context.Context, businessID string, filter ProductListFilter, page, limit int) ([]*models.Product, int64, error) {
	var total int64
	var products []models.Product
	offset := (page - 1) * limit
	query := s.db.WithContext(ctx).
		Model(&models.Product{}).
		Where("business_id = ? AND deleted_at IS NULL", businessID)
	if filter.CategoryID != "" {
		query = query.Where("category_id = ?", filter.CategoryID)
	}
	if filter.Query != "" {
		q := "%" + strings.ToLower(strings.TrimSpace(filter.Query)) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(sku) LIKE ? OR LOWER(COALESCE(barcode, '')) LIKE ?", q, q, q)
	}
	if filter.LowStockOnly {
		query = query.Where("COALESCE(low_stock_threshold, min_stock, 0) >= stock_level")
	}
	if filter.WarehouseID != "" {
		query = query.Joins("LEFT JOIN product_warehouse_catalogs pwc ON pwc.product_id = products.id AND pwc.warehouse_id = ? AND pwc.deleted_at IS NULL", filter.WarehouseID).
			Where("COALESCE(pwc.is_visible, TRUE) = TRUE")
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Preload("Category").Preload("Variants").Preload("Images").Preload("CustomValues.Column").Order("created_at DESC").Offset(offset).Limit(limit).Find(&products).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.Product, 0, len(products))
	for i := range products {
		s.hydrateCustomColumns(&products[i])
		sort.Slice(products[i].Variants, func(a, b int) bool {
			if products[i].Variants[a].IsDefault == products[i].Variants[b].IsDefault {
				return products[i].Variants[a].Name < products[i].Variants[b].Name
			}
			return products[i].Variants[a].IsDefault
		})
		result = append(result, &products[i])
	}
	return result, total, nil
}

func (s *ProductService) getProductDetailed(ctx context.Context, businessID, id string) (*models.Product, error) {
	var product models.Product
	err := s.db.WithContext(ctx).
		Preload("Category").
		Preload("Variants").
		Preload("Images").
		Preload("CustomValues.Column").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&product).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("product not found")
		}
		return nil, err
	}
	s.hydrateCustomColumns(&product)
	sort.Slice(product.Variants, func(i, j int) bool {
		if product.Variants[i].IsDefault == product.Variants[j].IsDefault {
			return product.Variants[i].Name < product.Variants[j].Name
		}
		return product.Variants[i].IsDefault
	})
	return &product, nil
}

func (s *ProductService) listProductVariantsTx(tx *gorm.DB, productID string) ([]*models.ProductVariant, error) {
	var variants []models.ProductVariant
	if err := tx.Where("product_id = ? AND deleted_at IS NULL", productID).Order("is_default DESC, created_at ASC").Find(&variants).Error; err != nil {
		return nil, err
	}
	result := make([]*models.ProductVariant, 0, len(variants))
	for i := range variants {
		result = append(result, &variants[i])
	}
	return result, nil
}

func (s *ProductService) hydrateCustomColumns(product *models.Product) {
	if product == nil {
		return
	}
	product.CustomColumns = map[string]interface{}{}
	for _, value := range product.CustomValues {
		if value == nil || value.Column == nil {
			continue
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(value.Value), &parsed); err != nil {
			parsed = value.Value
		}
		product.CustomColumns[value.Column.Slug] = parsed
	}
}

func (s *ProductService) syncProductRelationsTx(tx *gorm.DB, product *models.Product, images []ProductImageInput, customColumns map[string]interface{}, variants []ProductVariantInput) error {
	if len(images) > 0 {
		if err := tx.Where("product_id = ?", product.ID).Delete(&models.ProductImage{}).Error; err != nil {
			return err
		}
		for idx, image := range images {
			record := &models.ProductImage{
				BusinessID: product.BusinessID,
				ProductID:  product.ID,
				URL:        image.URL,
				Key:        image.Key,
				AltText:    image.AltText,
				Position:   image.Position,
				IsPrimary:  image.IsPrimary,
			}
			if record.Position == 0 {
				record.Position = idx + 1
			}
			if err := tx.Create(record).Error; err != nil {
				return err
			}
		}
		if len(images) > 0 {
			product.ImageURL = images[0].URL
			product.ImageKey = images[0].Key
			if err := tx.Model(product).Updates(map[string]interface{}{
				"image_url": product.ImageURL,
				"image_key": product.ImageKey,
			}).Error; err != nil {
				return err
			}
		}
	}

	if customColumns != nil {
		if err := tx.Where("product_id = ?", product.ID).Delete(&models.ProductCustomValue{}).Error; err != nil {
			return err
		}
		for key, value := range customColumns {
			slug := slugifyValue(key)
			column := &models.ProductCustomColumn{}
			err := tx.Where("business_id = ? AND slug = ? AND deleted_at IS NULL", product.BusinessID, slug).First(column).Error
			if err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				column = &models.ProductCustomColumn{
					BusinessID: product.BusinessID,
					Name:       key,
					Slug:       slug,
					DataType:   inferCustomColumnType(value),
					IsActive:   true,
				}
				if err := tx.Create(column).Error; err != nil {
					return err
				}
			}
			buf, err := json.Marshal(value)
			if err != nil {
				return err
			}
			record := &models.ProductCustomValue{
				BusinessID: product.BusinessID,
				ProductID:  product.ID,
				ColumnID:   column.ID,
				Value:      string(buf),
			}
			if err := tx.Create(record).Error; err != nil {
				return err
			}
		}
	}

	if len(variants) == 0 {
		if s.inventory != nil {
			_, err := s.inventory.ensureDefaultVariantTx(tx, product.BusinessID, product)
			return err
		}
		return nil
	}
	defaultMarked := false
	for _, variantInput := range variants {
		if variantInput.IsDefault {
			defaultMarked = true
			break
		}
	}
	if !defaultMarked && len(variants) > 0 {
		variants[0].IsDefault = true
	}
	for idx, variantInput := range variants {
		variant := &models.ProductVariant{}
		query := tx.Where("product_id = ? AND deleted_at IS NULL", product.ID)
		if variantInput.ID != "" {
			query = query.Where("id = ?", variantInput.ID)
		} else if variantInput.SKU != "" {
			query = query.Where("sku = ?", variantInput.SKU)
		} else {
			query = query.Where("is_default = TRUE")
		}
		err := query.First(variant).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			variant = &models.ProductVariant{
				BusinessID: product.BusinessID,
				ProductID:  product.ID,
			}
		}
		variant.Name = firstNonEmpty(variantInput.Name, fmt.Sprintf("Variant %d", idx+1))
		variant.SKU = firstNonEmpty(variantInput.SKU, fmt.Sprintf("%s-%d", product.SKU, idx+1))
		variant.Barcode = firstNonEmpty(variantInput.Barcode, variant.Barcode)
		variant.Attributes = mustMarshalMap(variantInput.Attributes)
		variant.IsDefault = variantInput.IsDefault
		variant.TrackBatches = variantInput.TrackBatches
		variant.TrackSerials = variantInput.TrackSerials
		if variantInput.Price > 0 {
			variant.Price = variantInput.Price
		} else if variant.Price == 0 {
			variant.Price = product.Price
		}
		if variantInput.CostPrice > 0 {
			variant.CostPrice = variantInput.CostPrice
		} else if variant.CostPrice == 0 {
			variant.CostPrice = product.CostPrice
		}
		if variantInput.LowStockThreshold > 0 {
			variant.LowStockThreshold = variantInput.LowStockThreshold
		} else if variant.LowStockThreshold == 0 {
			variant.LowStockThreshold = float64(maxInt64(product.LowStockThreshold, product.MinStock))
		}
		variant.IsActive = true
		if variant.ID == "" {
			if err := tx.Create(variant).Error; err != nil {
				return err
			}
		} else if err := tx.Save(variant).Error; err != nil {
			return err
		}
		if len(variantInput.Images) > 0 {
			if err := tx.Where("variant_id = ?", variant.ID).Delete(&models.ProductImage{}).Error; err != nil {
				return err
			}
			for imageIdx, image := range variantInput.Images {
				record := &models.ProductImage{
					BusinessID: product.BusinessID,
					ProductID:  product.ID,
					VariantID:  &variant.ID,
					URL:        image.URL,
					Key:        image.Key,
					AltText:    image.AltText,
					Position:   image.Position,
					IsPrimary:  image.IsPrimary,
				}
				if record.Position == 0 {
					record.Position = imageIdx + 1
				}
				if err := tx.Create(record).Error; err != nil {
					return err
				}
			}
		}
		if variantInput.StockLevel > 0 && s.inventory != nil && !product.IsService {
			if _, err := s.inventory.applyInventoryMutationTx(tx, inventoryMutationInput{
				BusinessID:      product.BusinessID,
				Product:         product,
				Variant:         variant,
				WarehouseID:     "",
				Quantity:        variantInput.StockLevel,
				Reason:          "variant opening quantity",
				UnitCost:        firstNonZero(variant.CostPrice, product.CostPrice),
				TransactionType: models.InventoryTransactionTypeOpeningBalance,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func inferCustomColumnType(value interface{}) string {
	switch value.(type) {
	case bool:
		return "boolean"
	case float64, float32, int, int32, int64, uint, uint32, uint64:
		return "number"
	case []interface{}:
		return "multi_select"
	case map[string]interface{}:
		return "object"
	default:
		return "text"
	}
}

func slugifyValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	if value == "" {
		return fmt.Sprintf("field_%d", timeNowUnix())
	}
	return value
}

// ProductServiceTestable is a test-friendly version of ProductService
type ProductServiceTestable struct {
	repo ProductRepositoryTestable
	s3   S3ServiceTestable
	log  *logger.Logger
}

// S3ServiceTestable is the testable interface for S3 operations
type S3ServiceTestable interface {
	GeneratePresignedUpload(ctx context.Context, bucket, key, contentType string, sizeBytes, expiresIn int64) (*PresignedUpload, error)
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
	product.Unit = gst.CanonicalProductUQC(input.Unit, input.UQCCode)
	product.UQCCode = product.Unit

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
	if input.Unit != "" || input.UQCCode != "" {
		product.Unit = gst.CanonicalProductUQC(input.Unit, input.UQCCode)
		product.UQCCode = product.Unit
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
func (s *ProductServiceTestable) GetImageUploadURLByBusiness(ctx context.Context, businessID, productID, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	if _, err := s.GetByBusiness(ctx, businessID, productID); err != nil {
		return nil, err
	}
	contentType, err := NormalizeImageUploadContentType(contentType)
	if err != nil {
		return nil, err
	}
	if err := validateUploadSize("product image", sizeBytes, MaxProductImageUploadBytes); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("products/%s/image", productID)
	return s.s3.GeneratePresignedUpload(ctx, "product-images", key, contentType, sizeBytes, 3600)
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
