package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/phpdave11/gofpdf"
	"gorm.io/gorm"
)

type BarcodeService struct {
	db  *gorm.DB
	log *logger.Logger
}

type BarcodeRenderInput struct {
	Value  string `json:"value" binding:"required"`
	Label  string `json:"label,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type BarcodeRecord struct {
	ID          string `json:"id"`
	ProductID   string `json:"product_id,omitempty"`
	ProductName string `json:"product_name"`
	Barcode     string `json:"barcode"`
	Format      string `json:"format"`
	CreatedAt   string `json:"created_at"`
}

func NewBarcodeService(db *gorm.DB, log *logger.Logger) *BarcodeService {
	return &BarcodeService{db: db, log: log}
}

func (s *BarcodeService) GenerateValue(prefix, seed string) string {
	value := strings.ToUpper(strings.TrimSpace(firstNonEmpty(prefix, "SKU")))
	seed = strings.ToUpper(strings.TrimSpace(seed))
	seed = strings.ReplaceAll(seed, " ", "")
	seed = strings.ReplaceAll(seed, "-", "")
	if len(seed) > 24 {
		seed = seed[:24]
	}
	return fmt.Sprintf("%s-%s", value, seed)
}

func (s *BarcodeService) EnsureBarcode(ctx context.Context, businessID, productID, variantID string) (string, error) {
	if variantID != "" {
		var variant models.ProductVariant
		if err := s.db.WithContext(ctx).Where("id = ? AND business_id = ? AND product_id = ? AND deleted_at IS NULL", variantID, businessID, productID).First(&variant).Error; err != nil {
			return "", err
		}
		if strings.TrimSpace(variant.Barcode) == "" {
			variant.Barcode = s.GenerateValue("VAR", firstNonEmpty(variant.SKU, variant.ID))
			if err := s.db.WithContext(ctx).Save(&variant).Error; err != nil {
				return "", err
			}
		}
		return variant.Barcode, nil
	}
	var product models.Product
	if err := s.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", productID, businessID).First(&product).Error; err != nil {
		return "", err
	}
	if strings.TrimSpace(product.Barcode) == "" {
		product.Barcode = s.GenerateValue("PRD", firstNonEmpty(product.SKU, product.ID))
		if err := s.db.WithContext(ctx).Save(&product).Error; err != nil {
			return "", err
		}
	}
	return product.Barcode, nil
}

func (s *BarcodeService) List(ctx context.Context, businessID string, limit int) ([]BarcodeRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var products []models.Product
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL AND COALESCE(barcode, '') <> ''", businessID).
		Order("updated_at DESC").
		Limit(limit).
		Find(&products).Error; err != nil {
		return nil, err
	}
	records := make([]BarcodeRecord, 0, len(products))
	for _, product := range products {
		records = append(records, BarcodeRecord{
			ID:          product.ID,
			ProductID:   product.ID,
			ProductName: product.Name,
			Barcode:     product.Barcode,
			Format:      "CODE128",
			CreatedAt:   product.UpdatedAt.Format(time.RFC3339),
		})
	}
	return records, nil
}

func (s *BarcodeService) Lookup(ctx context.Context, businessID, code string) (map[string]interface{}, error) {
	code = strings.TrimSpace(code)
	var variant models.ProductVariant
	if err := s.db.WithContext(ctx).Where("business_id = ? AND (barcode = ? OR sku = ?) AND deleted_at IS NULL", businessID, code, code).First(&variant).Error; err == nil {
		var product models.Product
		if err := s.db.WithContext(ctx).Where("id = ?", variant.ProductID).First(&product).Error; err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"entity_type": "variant",
			"product":     product,
			"variant":     variant,
		}, nil
	}
	var product models.Product
	if err := s.db.WithContext(ctx).Where("business_id = ? AND (barcode = ? OR sku = ?) AND deleted_at IS NULL", businessID, code, code).First(&product).Error; err == nil {
		return map[string]interface{}{
			"entity_type": "product",
			"product":     product,
		}, nil
	}
	return nil, fmt.Errorf("barcode not found")
}

func (s *BarcodeService) RenderPNG(input BarcodeRenderInput) ([]byte, error) {
	width, height := barcodeDimensions(input.Width, input.Height)
	code, err := code128.Encode(input.Value)
	if err != nil {
		return nil, err
	}
	scaled, err := barcode.Scale(code, width, height)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *BarcodeService) RenderSVG(input BarcodeRenderInput) ([]byte, error) {
	pngBytes, err := s.RenderPNG(input)
	if err != nil {
		return nil, err
	}
	width, height := barcodeDimensions(input.Width, input.Height)
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d"><image href="data:image/png;base64,%s" width="%d" height="%d"/></svg>`, width, height, encoded, width, height)
	return []byte(svg), nil
}

func (s *BarcodeService) RenderPDF(input BarcodeRenderInput) ([]byte, error) {
	pngBytes, err := s.RenderPNG(input)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	var normalized bytes.Buffer
	if err := png.Encode(&normalized, rgba); err != nil {
		return nil, err
	}
	width, height := barcodeDimensions(input.Width, input.Height)
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "", 12)
	if strings.TrimSpace(input.Label) != "" {
		pdf.CellFormat(190, 10, input.Label, "", 1, "L", false, 0, "")
	}
	name := "barcode.png"
	opts := gofpdf.ImageOptions{ImageType: "PNG", ReadDpi: true}
	pdf.RegisterImageOptionsReader(name, opts, bytes.NewReader(normalized.Bytes()))
	pdf.ImageOptions(name, 10, 20, float64(width)/6.0, float64(height)/6.0, false, opts, 0, "")
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func barcodeDimensions(width, height int) (int, int) {
	if width <= 0 {
		width = 320
	}
	if height <= 0 {
		height = 100
	}
	if width > 2000 {
		width = 2000
	}
	if height > 1000 {
		height = 1000
	}
	return width, height
}
