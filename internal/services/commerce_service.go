package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const storefrontCheckoutClaimTTL = 5 * time.Minute

type CommerceService struct {
	cfg              *config.Config
	db               *gorm.DB
	businessRepo     interfaces.BusinessRepository
	customerRepo     interfaces.CustomerRepository
	productRepo      interfaces.ProductRepository
	subscriptionRepo interfaces.SubscriptionRepository
	inventory        *InventoryService
	documents        *DocumentService
	s3               *S3Service
	httpClient       *http.Client
	log              *logger.Logger
}

type UpsertFeatureEntitlementInput struct {
	FeatureKey string                 `json:"feature_key" binding:"required"`
	Enabled    bool                   `json:"enabled"`
	LimitValue *int64                 `json:"limit_value,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type UpsertRoleInput struct {
	BusinessID  string   `json:"business_id,omitempty"`
	Name        string   `json:"name" binding:"required"`
	Key         string   `json:"key,omitempty"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type UpsertBranchInput struct {
	BusinessID string                 `json:"business_id,omitempty"`
	Name       string                 `json:"name" binding:"required"`
	Code       string                 `json:"code" binding:"required"`
	Email      string                 `json:"email,omitempty"`
	Phone      string                 `json:"phone,omitempty"`
	Address    string                 `json:"address,omitempty"`
	City       string                 `json:"city,omitempty"`
	State      string                 `json:"state,omitempty"`
	Country    string                 `json:"country,omitempty"`
	PostalCode string                 `json:"postal_code,omitempty"`
	IsDefault  bool                   `json:"is_default"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type UpsertStorefrontInput struct {
	BusinessID         string                 `json:"business_id,omitempty"`
	Name               string                 `json:"name" binding:"required"`
	Slug               string                 `json:"slug,omitempty"`
	Status             string                 `json:"status,omitempty"`
	Currency           string                 `json:"currency,omitempty"`
	AllowCOD           *bool                  `json:"allow_cod,omitempty"`
	AllowOnlinePayment *bool                  `json:"allow_online_payment,omitempty"`
	AutoInvoiceOnPaid  *bool                  `json:"auto_invoice_on_paid,omitempty"`
	MinimumOrderValue  *float64               `json:"minimum_order_value,omitempty"`
	Settings           map[string]interface{} `json:"settings,omitempty"`
	BlockedUsers       []string               `json:"blocked_users,omitempty"`
	Domains            []string               `json:"domains,omitempty"`
}

type UpsertStorefrontProductInput struct {
	ProductID      string                 `json:"product_id" binding:"required,uuid"`
	CategoryID     string                 `json:"category_id,omitempty" binding:"omitempty,uuid"`
	IsPublished    bool                   `json:"is_published"`
	DisplayPrice   float64                `json:"display_price"`
	CompareAtPrice float64                `json:"compare_at_price"`
	SortOrder      int                    `json:"sort_order"`
	Badge          string                 `json:"badge,omitempty"`
	SEO            map[string]interface{} `json:"seo,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

type UpsertStorefrontCouponInput struct {
	Code                  string                 `json:"code" binding:"required"`
	DiscountType          string                 `json:"discount_type" binding:"required,oneof=percentage fixed"`
	DiscountValue         float64                `json:"discount_value" binding:"required,gt=0"`
	MinimumOrderValue     float64                `json:"minimum_order_value"`
	MaxDiscountAmount     float64                `json:"max_discount_amount"`
	UsageLimit            int64                  `json:"usage_limit"`
	UsageLimitPerCustomer int64                  `json:"usage_limit_per_customer"`
	StartsAt              *time.Time             `json:"starts_at,omitempty"`
	EndsAt                *time.Time             `json:"ends_at,omitempty"`
	IsActive              *bool                  `json:"is_active,omitempty"`
	Metadata              map[string]interface{} `json:"metadata,omitempty"`
}

type CheckoutCustomerInput struct {
	Name       string                 `json:"name" binding:"required"`
	Email      string                 `json:"email,omitempty"`
	Phone      string                 `json:"phone,omitempty"`
	GSTIN      string                 `json:"gstin,omitempty"`
	PAN        string                 `json:"pan,omitempty"`
	StateCode  string                 `json:"state_code,omitempty"`
	Address    string                 `json:"address,omitempty"`
	City       string                 `json:"city,omitempty"`
	State      string                 `json:"state,omitempty"`
	Country    string                 `json:"country,omitempty"`
	PostalCode string                 `json:"postal_code,omitempty"`
	Billing    map[string]interface{} `json:"billing,omitempty"`
	Shipping   map[string]interface{} `json:"shipping,omitempty"`
}

type CheckoutItemInput struct {
	ProductID   string  `json:"product_id" binding:"required,uuid"`
	VariantID   string  `json:"variant_id,omitempty" binding:"omitempty,uuid"`
	Quantity    float64 `json:"quantity" binding:"required,gt=0"`
	WarehouseID string  `json:"warehouse_id,omitempty" binding:"omitempty,uuid"`
}

type StorefrontCheckoutInput struct {
	BranchID        string                 `json:"branch_id,omitempty" binding:"omitempty,uuid"`
	Currency        string                 `json:"currency,omitempty"`
	PaymentMethod   string                 `json:"payment_method,omitempty"`
	CouponCode      string                 `json:"coupon_code,omitempty"`
	Notes           string                 `json:"notes,omitempty"`
	ShippingTotal   float64                `json:"shipping_total"`
	BillingAddress  map[string]interface{} `json:"billing_address,omitempty"`
	ShippingAddress map[string]interface{} `json:"shipping_address,omitempty"`
	Customer        CheckoutCustomerInput  `json:"customer" binding:"required"`
	Items           []CheckoutItemInput    `json:"items" binding:"required,min=1,dive"`
}

type ValidateCouponInput struct {
	Code          string  `json:"code" binding:"required"`
	CustomerEmail string  `json:"customer_email,omitempty"`
	Subtotal      float64 `json:"subtotal" binding:"gte=0"`
}

type CreateDriveAssetInput struct {
	BusinessID  string                 `json:"business_id,omitempty"`
	Name        string                 `json:"name" binding:"required"`
	FolderPath  string                 `json:"folder_path,omitempty"`
	ContentType string                 `json:"content_type" binding:"required"`
	SizeBytes   int64                  `json:"size_bytes" binding:"required,gt=0,lte=26214400"`
	Category    string                 `json:"category,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

var allowedDriveAssetContentTypes = map[string]struct{}{
	"image/jpeg":      {},
	"image/png":       {},
	"image/webp":      {},
	"application/pdf": {},
}

func validateDriveAssetUpload(input CreateDriveAssetInput) (string, error) {
	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if _, ok := allowedDriveAssetContentTypes[contentType]; !ok {
		return "", fmt.Errorf("unsupported drive asset content type")
	}
	if err := validateUploadSize("drive asset", input.SizeBytes, MaxDriveAssetUploadBytes); err != nil {
		return "", err
	}
	return contentType, nil
}

type UpdateDriveAssetInput struct {
	Name       string                 `json:"name,omitempty"`
	FolderPath string                 `json:"folder_path,omitempty"`
	Category   string                 `json:"category,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type UpsertWhatsAppConfigInput struct {
	BusinessID       string                 `json:"business_id,omitempty"`
	PhoneNumberID    string                 `json:"phone_number_id,omitempty"`
	AccessToken      string                 `json:"access_token,omitempty"`
	WebhookSecret    string                 `json:"webhook_secret,omitempty"`
	VerifyToken      string                 `json:"verify_token,omitempty"`
	DefaultRecipient string                 `json:"default_recipient,omitempty"`
	Enabled          *bool                  `json:"enabled,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

type CheckoutResult struct {
	Order          *models.StoreOrder `json:"order"`
	GatewayOrderID string             `json:"gateway_order_id,omitempty"`
}

type CouponValidationResult struct {
	Valid         bool    `json:"valid"`
	DiscountTotal float64 `json:"discount_total"`
	Message       string  `json:"message,omitempty"`
}

type DriveUploadSession struct {
	Asset           *models.DriveAsset `json:"asset"`
	UploadURL       string             `json:"upload_url"`
	RequiredHeaders map[string]string  `json:"required_headers"`
}

type WhatsAppConfigResponse struct {
	ID                      string     `json:"id"`
	BusinessID              string     `json:"business_id"`
	PhoneNumberID           string     `json:"phone_number_id,omitempty"`
	DefaultRecipient        string     `json:"default_recipient,omitempty"`
	Enabled                 bool       `json:"enabled"`
	Metadata                string     `json:"metadata,omitempty"`
	AccessTokenConfigured   bool       `json:"access_token_configured"`
	WebhookSecretConfigured bool       `json:"webhook_secret_configured"`
	VerifyTokenConfigured   bool       `json:"verify_token_configured"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	LastDeliveryAt          *time.Time `json:"last_delivery_at,omitempty"`
}

type StorefrontCatalogItem struct {
	StorefrontProduct *models.StorefrontProduct `json:"storefront_product"`
	Product           *models.Product           `json:"product"`
}

type StorefrontCatalogResponse struct {
	Storefront *models.Storefront           `json:"storefront"`
	Categories []*models.StorefrontCategory `json:"categories"`
	Products   []*StorefrontCatalogItem     `json:"products"`
}

type PublicStorefront struct {
	Name               string  `json:"name"`
	Slug               string  `json:"slug"`
	Currency           string  `json:"currency"`
	AllowCOD           bool    `json:"allow_cod"`
	AllowOnlinePayment bool    `json:"allow_online_payment"`
	MinimumOrderValue  float64 `json:"minimum_order_value"`
}

type PublicStorefrontProduct struct {
	ID             string                 `json:"id"`
	ProductID      string                 `json:"product_id"`
	CategoryID     *string                `json:"category_id,omitempty"`
	Name           string                 `json:"name"`
	SKU            string                 `json:"sku,omitempty"`
	Description    string                 `json:"description,omitempty"`
	Price          float64                `json:"price"`
	MRP            float64                `json:"mrp,omitempty"`
	DisplayPrice   float64                `json:"display_price"`
	CompareAtPrice float64                `json:"compare_at_price,omitempty"`
	Currency       string                 `json:"currency"`
	Unit           string                 `json:"unit"`
	ImageURL       string                 `json:"image_url,omitempty"`
	IsService      bool                   `json:"is_service"`
	Badge          string                 `json:"badge,omitempty"`
	SortOrder      int                    `json:"sort_order"`
	SEO            map[string]interface{} `json:"seo,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

type PublicStorefrontCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

type PublicStorefrontCatalogResponse struct {
	Storefront *PublicStorefront           `json:"storefront"`
	Categories []*PublicStorefrontCategory `json:"categories"`
	Products   []*PublicStorefrontProduct  `json:"products"`
}

// PublicStoreOrderLineResponse contains only customer-safe line-item details.
type PublicStoreOrderLineResponse struct {
	Title          string  `json:"title"`
	SKU            string  `json:"sku"`
	Quantity       float64 `json:"quantity"`
	UnitPrice      float64 `json:"unit_price"`
	DiscountAmount float64 `json:"discount_amount"`
	TaxRate        float64 `json:"tax_rate"`
	TaxAmount      float64 `json:"tax_amount"`
	LineTotal      float64 `json:"line_total"`
}

// PublicStoreOrderResponse contains only the order details intended for a token holder.
type PublicStoreOrderResponse struct {
	OrderNumber   string                          `json:"order_number"`
	Status        string                          `json:"status"`
	PaymentStatus string                          `json:"payment_status"`
	PaymentMethod string                          `json:"payment_method"`
	Currency      string                          `json:"currency"`
	Subtotal      float64                         `json:"subtotal"`
	DiscountTotal float64                         `json:"discount_total"`
	TaxTotal      float64                         `json:"tax_total"`
	ShippingTotal float64                         `json:"shipping_total"`
	Total         float64                         `json:"total"`
	OrderedAt     time.Time                       `json:"ordered_at"`
	PaidAt        *time.Time                      `json:"paid_at,omitempty"`
	CancelledAt   *time.Time                      `json:"cancelled_at,omitempty"`
	Lines         []*PublicStoreOrderLineResponse `json:"lines"`
}

// NewPublicStoreOrderResponse maps a persistence model to the explicit public-order contract.
func NewPublicStoreOrderResponse(order *models.StoreOrder) *PublicStoreOrderResponse {
	lines := make([]*PublicStoreOrderLineResponse, 0, len(order.Lines))
	for _, line := range order.Lines {
		lines = append(lines, &PublicStoreOrderLineResponse{
			Title:          line.Title,
			SKU:            line.SKU,
			Quantity:       line.Quantity,
			UnitPrice:      line.UnitPrice,
			DiscountAmount: line.DiscountAmount,
			TaxRate:        line.TaxRate,
			TaxAmount:      line.TaxAmount,
			LineTotal:      line.LineTotal,
		})
	}
	return &PublicStoreOrderResponse{
		OrderNumber:   order.OrderNumber,
		Status:        order.Status,
		PaymentStatus: order.PaymentStatus,
		PaymentMethod: order.PaymentMethod,
		Currency:      order.Currency,
		Subtotal:      order.Subtotal,
		DiscountTotal: order.DiscountTotal,
		TaxTotal:      order.TaxTotal,
		ShippingTotal: order.ShippingTotal,
		Total:         order.Total,
		OrderedAt:     order.OrderedAt,
		PaidAt:        order.PaidAt,
		CancelledAt:   order.CancelledAt,
		Lines:         lines,
	}
}

func NewCommerceService(
	cfg *config.Config,
	db *gorm.DB,
	businessRepo interfaces.BusinessRepository,
	customerRepo interfaces.CustomerRepository,
	productRepo interfaces.ProductRepository,
	subscriptionRepo interfaces.SubscriptionRepository,
	inventory *InventoryService,
	documents *DocumentService,
	s3 *S3Service,
	log *logger.Logger,
) *CommerceService {
	httpTimeout := 15 * time.Second
	if cfg != nil && cfg.WhatsApp.Timeout > 0 {
		httpTimeout = time.Duration(cfg.WhatsApp.Timeout) * time.Second
	}
	return &CommerceService{
		cfg:              cfg,
		db:               db,
		businessRepo:     businessRepo,
		customerRepo:     customerRepo,
		productRepo:      productRepo,
		subscriptionRepo: subscriptionRepo,
		inventory:        inventory,
		documents:        documents,
		s3:               s3,
		httpClient:       &http.Client{Timeout: httpTimeout},
		log:              log,
	}
}

func (s *CommerceService) ListFeatureEntitlements(ctx context.Context, businessID string) ([]*models.FeatureEntitlement, error) {
	var entitlements []*models.FeatureEntitlement
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("feature_key ASC").
		Find(&entitlements).Error; err != nil {
		return nil, err
	}
	if s.subscriptionRepo == nil {
		return entitlements, nil
	}
	subscription, err := s.subscriptionRepo.GetByBusinessID(ctx, businessID)
	if err != nil || subscription == nil {
		subscription = &models.Subscription{BusinessID: businessID, Plan: "free", PlanCode: "free", Status: "active"}
	}
	seeds := defaultEntitlementSeedsForSubscription(subscription)
	if featureEntitlementsNeedSync(entitlements, seeds) {
		return s.SyncFeatureEntitlements(ctx, businessID)
	}
	return entitlements, nil
}

func (s *CommerceService) SyncFeatureEntitlements(ctx context.Context, businessID string) ([]*models.FeatureEntitlement, error) {
	subscription, err := s.subscriptionRepo.GetByBusinessID(ctx, businessID)
	if err != nil || subscription == nil {
		subscription = &models.Subscription{BusinessID: businessID, Plan: "free", PlanCode: "free", CatalogVersion: CurrentSubscriptionCatalogVersion}
	}
	subscription.PlanCode = normalizePlanCode(subscription.Plan, subscription.PlanCode)
	subscription.CatalogVersion = normalizeCatalogVersion(subscription.CatalogVersion)
	seeds := defaultEntitlementSeedsForSubscription(subscription)

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, seed := range seeds {
			var entitlement models.FeatureEntitlement
			err := tx.Where("business_id = ? AND feature_key = ? AND deleted_at IS NULL", businessID, seed.FeatureKey).First(&entitlement).Error
			switch {
			case err == nil:
				entitlement.Enabled = seed.Enabled
				entitlement.LimitValue = seed.LimitValue
				entitlement.Metadata = mustMarshalMap(seed.Metadata)
				if err := tx.Save(&entitlement).Error; err != nil {
					return err
				}
			case err == gorm.ErrRecordNotFound:
				entitlement = models.FeatureEntitlement{
					BusinessID: businessID,
					FeatureKey: seed.FeatureKey,
					Enabled:    seed.Enabled,
					LimitValue: seed.LimitValue,
					Metadata:   mustMarshalMap(seed.Metadata),
				}
				if err := tx.Create(&entitlement).Error; err != nil {
					return err
				}
			default:
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var entitlements []*models.FeatureEntitlement
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("feature_key ASC").
		Find(&entitlements).Error; err != nil {
		return nil, err
	}
	return entitlements, nil
}

func (s *CommerceService) UpsertFeatureEntitlements(ctx context.Context, businessID string, inputs []UpsertFeatureEntitlementInput) ([]*models.FeatureEntitlement, error) {
	if len(inputs) == 0 {
		return s.ListFeatureEntitlements(ctx, businessID)
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, input := range inputs {
			if strings.TrimSpace(input.FeatureKey) == "" {
				continue
			}
			var entitlement models.FeatureEntitlement
			err := tx.Where("business_id = ? AND feature_key = ? AND deleted_at IS NULL", businessID, input.FeatureKey).First(&entitlement).Error
			switch {
			case err == nil:
				entitlement.Enabled = input.Enabled
				entitlement.LimitValue = input.LimitValue
				if input.Metadata != nil {
					entitlement.Metadata = mustMarshalMap(input.Metadata)
				}
				if err := tx.Save(&entitlement).Error; err != nil {
					return err
				}
			case err == gorm.ErrRecordNotFound:
				entitlement = models.FeatureEntitlement{
					BusinessID: businessID,
					FeatureKey: input.FeatureKey,
					Enabled:    input.Enabled,
					LimitValue: input.LimitValue,
					Metadata:   mustMarshalMap(input.Metadata),
				}
				if err := tx.Create(&entitlement).Error; err != nil {
					return err
				}
			default:
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s.ListFeatureEntitlements(ctx, businessID)
}

func (s *CommerceService) ListRoles(ctx context.Context, businessID string) ([]*models.Role, error) {
	var roles []*models.Role
	if err := s.db.WithContext(ctx).
		Preload("Permissions", "deleted_at IS NULL").
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("is_system DESC, name ASC").
		Find(&roles).Error; err != nil {
		return nil, err
	}
	type roleCountRow struct {
		RoleID *string
		Role   string
		Total  int64
	}
	var counts []roleCountRow
	if err := s.db.WithContext(ctx).
		Model(&models.TeamMember{}).
		Select("role_id, LOWER(role) AS role, COUNT(*) AS total").
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Group("role_id, LOWER(role)").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	countByRoleID := map[string]int64{}
	countByLegacyRole := map[string]int64{}
	for _, count := range counts {
		if count.RoleID != nil && strings.TrimSpace(*count.RoleID) != "" {
			countByRoleID[*count.RoleID] += count.Total
			continue
		}
		countByLegacyRole[count.Role] += count.Total
	}
	for _, role := range roles {
		role.UserCount = countByRoleID[role.ID]
		if role.IsSystem {
			role.UserCount += countByLegacyRole[strings.ToLower(role.Key)]
		}
	}
	return roles, nil
}

func (s *CommerceService) CreateRole(ctx context.Context, input UpsertRoleInput) (*models.Role, error) {
	if err := s.ensureFeatureEnabled(ctx, input.BusinessID, FeatureCustomRoles); err != nil {
		return nil, err
	}
	role := &models.Role{
		BusinessID:  input.BusinessID,
		Name:        input.Name,
		Key:         normalizeLookupKey(firstNonEmpty(input.Key, input.Name)),
		Description: input.Description,
		IsSystem:    false,
	}
	if role.Key == "" {
		return nil, fmt.Errorf("role key is required")
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(role).Error; err != nil {
			return err
		}
		return s.replaceRolePermissionsTx(tx, role.ID, input.Permissions)
	}); err != nil {
		return nil, err
	}
	return s.getRole(ctx, input.BusinessID, role.ID)
}

func (s *CommerceService) UpdateRole(ctx context.Context, businessID, roleID string, input UpsertRoleInput) (*models.Role, error) {
	role, err := s.getRole(ctx, businessID, roleID)
	if err != nil {
		return nil, err
	}
	if role.IsSystem {
		return nil, fmt.Errorf("system roles cannot be modified")
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role.Name = coalesceString(input.Name, role.Name)
		if input.Key != "" {
			role.Key = normalizeLookupKey(input.Key)
		}
		if input.Description != "" {
			role.Description = input.Description
		}
		if err := tx.Save(role).Error; err != nil {
			return err
		}
		if input.Permissions != nil {
			return s.replaceRolePermissionsTx(tx, role.ID, input.Permissions)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s.getRole(ctx, businessID, roleID)
}

func (s *CommerceService) DeleteRole(ctx context.Context, businessID, roleID string) error {
	role, err := s.getRole(ctx, businessID, roleID)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return fmt.Errorf("system roles cannot be deleted")
	}
	return s.db.WithContext(ctx).Delete(role).Error
}

func (s *CommerceService) ListBranches(ctx context.Context, businessID string, branchIDs ...string) ([]*models.Branch, error) {
	var branches []*models.Branch
	query := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("is_default DESC, name ASC")
	if len(branchIDs) > 0 {
		query = query.Where("id IN ?", branchIDs)
	}
	if err := query.Find(&branches).Error; err != nil {
		return nil, err
	}
	return branches, nil
}

func (s *CommerceService) CreateBranch(ctx context.Context, input UpsertBranchInput) (*models.Branch, error) {
	if err := s.ensureFeatureEnabled(ctx, input.BusinessID, FeatureBranches); err != nil {
		return nil, err
	}
	branch := &models.Branch{
		BusinessID: input.BusinessID,
		Name:       input.Name,
		Code:       strings.ToUpper(strings.TrimSpace(input.Code)),
		Email:      input.Email,
		Phone:      input.Phone,
		Address:    input.Address,
		City:       input.City,
		State:      input.State,
		Country:    input.Country,
		PostalCode: input.PostalCode,
		IsDefault:  input.IsDefault,
		Metadata:   mustMarshalMap(input.Metadata),
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if branch.IsDefault {
			if err := tx.Model(&models.Branch{}).
				Where("business_id = ? AND deleted_at IS NULL", input.BusinessID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(branch).Error
	}); err != nil {
		return nil, err
	}
	return branch, nil
}

func (s *CommerceService) UpdateBranch(ctx context.Context, businessID, branchID string, input UpsertBranchInput) (*models.Branch, error) {
	var branch models.Branch
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", branchID, businessID).
		First(&branch).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.Name != "" {
			branch.Name = input.Name
		}
		if input.Code != "" {
			branch.Code = strings.ToUpper(strings.TrimSpace(input.Code))
		}
		if input.Email != "" {
			branch.Email = input.Email
		}
		if input.Phone != "" {
			branch.Phone = input.Phone
		}
		if input.Address != "" {
			branch.Address = input.Address
		}
		if input.City != "" {
			branch.City = input.City
		}
		if input.State != "" {
			branch.State = input.State
		}
		if input.Country != "" {
			branch.Country = input.Country
		}
		if input.PostalCode != "" {
			branch.PostalCode = input.PostalCode
		}
		if input.Metadata != nil {
			branch.Metadata = mustMarshalMap(input.Metadata)
		}
		if input.IsDefault {
			if err := tx.Model(&models.Branch{}).
				Where("business_id = ? AND deleted_at IS NULL", businessID).
				Update("is_default", false).Error; err != nil {
				return err
			}
			branch.IsDefault = true
		}
		return tx.Save(&branch).Error
	}); err != nil {
		return nil, err
	}
	return &branch, nil
}

func (s *CommerceService) DeleteBranch(ctx context.Context, businessID, branchID string) error {
	return s.db.WithContext(ctx).
		Where("business_id = ? AND id = ?", businessID, branchID).
		Delete(&models.Branch{}).Error
}

func (s *CommerceService) ListStorefronts(ctx context.Context, businessID string) ([]*models.Storefront, error) {
	var storefronts []*models.Storefront
	if err := s.db.WithContext(ctx).
		Preload("Domains", "deleted_at IS NULL").
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("created_at ASC").
		Find(&storefronts).Error; err != nil {
		return nil, err
	}
	return storefronts, nil
}

func (s *CommerceService) GetStorefront(ctx context.Context, businessID, storefrontID string) (*models.Storefront, error) {
	var storefront models.Storefront
	if err := s.db.WithContext(ctx).
		Preload("Domains", "deleted_at IS NULL").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", storefrontID, businessID).
		First(&storefront).Error; err != nil {
		return nil, err
	}
	return &storefront, nil
}

func (s *CommerceService) CreateStorefront(ctx context.Context, input UpsertStorefrontInput) (*models.Storefront, error) {
	if err := s.ensureFeatureEnabled(ctx, input.BusinessID, FeatureOnlineStore); err != nil {
		return nil, err
	}
	var existingCount int64
	if err := s.db.WithContext(ctx).Model(&models.Storefront{}).
		Where("business_id = ? AND deleted_at IS NULL", input.BusinessID).
		Count(&existingCount).Error; err != nil {
		return nil, err
	}
	if existingCount > 0 {
		return nil, fmt.Errorf("only one storefront is supported per business in v1")
	}
	business, err := s.businessRepo.GetByID(ctx, input.BusinessID)
	if err != nil {
		return nil, err
	}
	storefront := &models.Storefront{
		BusinessID:         input.BusinessID,
		Name:               input.Name,
		Slug:               normalizeLookupKey(firstNonEmpty(input.Slug, input.Name)),
		Status:             coalesceString(input.Status, models.StorefrontStatusDraft),
		Currency:           defaultCurrency(coalesceString(input.Currency, business.Currency)),
		AllowCOD:           boolValueOrDefault(input.AllowCOD, true),
		AllowOnlinePayment: boolValueOrDefault(input.AllowOnlinePayment, false),
		AutoInvoiceOnPaid:  boolValueOrDefault(input.AutoInvoiceOnPaid, true),
		MinimumOrderValue:  floatPointerValue(input.MinimumOrderValue),
		Settings:           mustMarshalMap(input.Settings),
		BlockedUsers:       marshalStringSlice(uniqueStrings(input.BlockedUsers)),
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(storefront).Error; err != nil {
			return err
		}
		return s.replaceStorefrontDomainsTx(tx, storefront.ID, input.Domains)
	}); err != nil {
		return nil, err
	}
	return s.GetStorefront(ctx, input.BusinessID, storefront.ID)
}

func (s *CommerceService) UpdateStorefrontSettings(ctx context.Context, businessID, storefrontID string, input UpsertStorefrontInput) (*models.Storefront, error) {
	storefront, err := s.GetStorefront(ctx, businessID, storefrontID)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.Name != "" {
			storefront.Name = input.Name
		}
		if input.Slug != "" {
			storefront.Slug = normalizeLookupKey(input.Slug)
		}
		if input.Status != "" {
			storefront.Status = input.Status
		}
		if input.Currency != "" {
			storefront.Currency = defaultCurrency(input.Currency)
		}
		if input.AllowCOD != nil {
			storefront.AllowCOD = *input.AllowCOD
		}
		if input.AllowOnlinePayment != nil {
			storefront.AllowOnlinePayment = *input.AllowOnlinePayment
		}
		if input.AutoInvoiceOnPaid != nil {
			storefront.AutoInvoiceOnPaid = *input.AutoInvoiceOnPaid
		}
		if input.MinimumOrderValue != nil {
			storefront.MinimumOrderValue = *input.MinimumOrderValue
		}
		if input.Settings != nil {
			storefront.Settings = mustMarshalMap(input.Settings)
		}
		if input.BlockedUsers != nil {
			storefront.BlockedUsers = marshalStringSlice(uniqueStrings(input.BlockedUsers))
		}
		if err := tx.Save(storefront).Error; err != nil {
			return err
		}
		if input.Domains != nil {
			return s.replaceStorefrontDomainsTx(tx, storefront.ID, input.Domains)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s.GetStorefront(ctx, businessID, storefrontID)
}

func (s *CommerceService) ListStorefrontProducts(ctx context.Context, businessID, storefrontID string) ([]*StorefrontCatalogItem, error) {
	var storefrontProducts []*models.StorefrontProduct
	if err := s.db.WithContext(ctx).
		Where("storefront_id = ? AND deleted_at IS NULL", storefrontID).
		Order("sort_order ASC, created_at ASC").
		Find(&storefrontProducts).Error; err != nil {
		return nil, err
	}
	items := make([]*StorefrontCatalogItem, 0, len(storefrontProducts))
	for _, storefrontProduct := range storefrontProducts {
		product, err := s.productRepo.GetByID(ctx, storefrontProduct.ProductID, businessID)
		if err != nil {
			continue
		}
		items = append(items, &StorefrontCatalogItem{
			StorefrontProduct: storefrontProduct,
			Product:           product,
		})
	}
	return items, nil
}

func (s *CommerceService) ReplaceStorefrontProducts(ctx context.Context, businessID, storefrontID string, inputs []UpsertStorefrontProductInput) ([]*StorefrontCatalogItem, error) {
	if err := s.ensureFeatureEnabled(ctx, businessID, FeatureOnlineStore); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, input := range inputs {
			product, err := s.productRepo.GetByID(ctx, input.ProductID, businessID)
			if err != nil {
				return err
			}
			eligible, err := s.productEligibleForStorefront(ctx, input.ProductID)
			if err != nil {
				return err
			}
			if input.IsPublished && !eligible {
				return fmt.Errorf("product %s uses batch or serial tracking and cannot be published to the storefront", product.Name)
			}
			var storefrontProduct models.StorefrontProduct
			err = tx.Where("storefront_id = ? AND product_id = ? AND deleted_at IS NULL", storefrontID, input.ProductID).First(&storefrontProduct).Error
			switch {
			case err == nil:
				storefrontProduct.CategoryID = stringPointer(input.CategoryID)
				storefrontProduct.IsPublished = input.IsPublished
				storefrontProduct.DisplayPrice = coalesceFloat(input.DisplayPrice, product.Price)
				storefrontProduct.CompareAtPrice = input.CompareAtPrice
				storefrontProduct.SortOrder = input.SortOrder
				storefrontProduct.Badge = input.Badge
				storefrontProduct.SEO = mustMarshalMap(input.SEO)
				storefrontProduct.Metadata = mustMarshalMap(input.Metadata)
				if err := tx.Save(&storefrontProduct).Error; err != nil {
					return err
				}
			case err == gorm.ErrRecordNotFound:
				storefrontProduct = models.StorefrontProduct{
					StorefrontID:   storefrontID,
					ProductID:      input.ProductID,
					CategoryID:     stringPointer(input.CategoryID),
					IsPublished:    input.IsPublished,
					DisplayPrice:   coalesceFloat(input.DisplayPrice, product.Price),
					CompareAtPrice: input.CompareAtPrice,
					SortOrder:      input.SortOrder,
					Badge:          input.Badge,
					SEO:            mustMarshalMap(input.SEO),
					Metadata:       mustMarshalMap(input.Metadata),
				}
				if err := tx.Create(&storefrontProduct).Error; err != nil {
					return err
				}
			default:
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s.ListStorefrontProducts(ctx, businessID, storefrontID)
}

func (s *CommerceService) ListStorefrontCoupons(ctx context.Context, storefrontID string) ([]*models.StorefrontCoupon, error) {
	var coupons []*models.StorefrontCoupon
	if err := s.db.WithContext(ctx).
		Where("storefront_id = ? AND deleted_at IS NULL", storefrontID).
		Order("created_at DESC").
		Find(&coupons).Error; err != nil {
		return nil, err
	}
	return coupons, nil
}

func (s *CommerceService) CreateStorefrontCoupon(ctx context.Context, storefrontID string, input UpsertStorefrontCouponInput) (*models.StorefrontCoupon, error) {
	coupon := &models.StorefrontCoupon{
		StorefrontID:          storefrontID,
		Code:                  strings.ToUpper(strings.TrimSpace(input.Code)),
		DiscountType:          input.DiscountType,
		DiscountValue:         input.DiscountValue,
		MinimumOrderValue:     input.MinimumOrderValue,
		MaxDiscountAmount:     input.MaxDiscountAmount,
		UsageLimit:            input.UsageLimit,
		UsageLimitPerCustomer: input.UsageLimitPerCustomer,
		StartsAt:              input.StartsAt,
		EndsAt:                input.EndsAt,
		IsActive:              boolValueOrDefault(input.IsActive, true),
		Metadata:              mustMarshalMap(input.Metadata),
	}
	if err := s.db.WithContext(ctx).Create(coupon).Error; err != nil {
		return nil, err
	}
	return coupon, nil
}

func (s *CommerceService) UpdateStorefrontCoupon(ctx context.Context, storefrontID, couponID string, input UpsertStorefrontCouponInput) (*models.StorefrontCoupon, error) {
	var coupon models.StorefrontCoupon
	if err := s.db.WithContext(ctx).
		Where("id = ? AND storefront_id = ? AND deleted_at IS NULL", couponID, storefrontID).
		First(&coupon).Error; err != nil {
		return nil, err
	}
	coupon.Code = strings.ToUpper(strings.TrimSpace(firstNonEmpty(input.Code, coupon.Code)))
	if input.DiscountType != "" {
		coupon.DiscountType = input.DiscountType
	}
	if input.DiscountValue > 0 {
		coupon.DiscountValue = input.DiscountValue
	}
	coupon.MinimumOrderValue = input.MinimumOrderValue
	coupon.MaxDiscountAmount = input.MaxDiscountAmount
	coupon.UsageLimit = input.UsageLimit
	coupon.UsageLimitPerCustomer = input.UsageLimitPerCustomer
	coupon.StartsAt = input.StartsAt
	coupon.EndsAt = input.EndsAt
	if input.IsActive != nil {
		coupon.IsActive = *input.IsActive
	}
	if input.Metadata != nil {
		coupon.Metadata = mustMarshalMap(input.Metadata)
	}
	if err := s.db.WithContext(ctx).Save(&coupon).Error; err != nil {
		return nil, err
	}
	return &coupon, nil
}

func (s *CommerceService) ListStorefrontOrders(ctx context.Context, businessID, storefrontID, status string, page, limit int) ([]*models.StoreOrder, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := s.db.WithContext(ctx).
		Model(&models.StoreOrder{}).
		Where("business_id = ? AND storefront_id = ? AND deleted_at IS NULL", businessID, storefrontID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []*models.StoreOrder
	if err := query.
		Preload("Lines", "deleted_at IS NULL").
		Preload("Events", "deleted_at IS NULL").
		Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

func (s *CommerceService) ApproveStoreOrder(ctx context.Context, businessID, storefrontID, orderID string) (*models.StoreOrder, error) {
	storefront, err := s.GetStorefront(ctx, businessID, storefrontID)
	if err != nil {
		return nil, err
	}
	var order models.StoreOrder
	transitioned := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Preload("Lines", "deleted_at IS NULL").
			Where("id = ? AND business_id = ? AND storefront_id = ? AND deleted_at IS NULL", orderID, businessID, storefrontID).
			First(&order).Error; err != nil {
			return err
		}
		if order.Status == models.StoreOrderStatusCancelled {
			return fmt.Errorf("cancelled orders cannot be approved")
		}
		if order.SalesInvoiceID == nil || *order.SalesInvoiceID == "" {
			invoiceID, err := s.createSalesInvoiceForOrder(ctx, &order)
			if err != nil {
				return err
			}
			order.SalesInvoiceID = stringPointer(invoiceID)
		}
		nextStatus := models.StoreOrderStatusConfirmed
		if order.PaymentStatus == models.StoreOrderPaymentStatusPaid {
			nextStatus = models.StoreOrderStatusPaid
		}
		transitioned = order.Status != nextStatus
		order.Status = nextStatus
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if !transitioned {
			return nil
		}
		return tx.Create(&models.StoreOrderEvent{
			StoreOrderID: order.ID,
			EventType:    "store_order.approved",
			Status:       order.Status,
			Payload:      mustMarshalMap(map[string]interface{}{"storefront_id": storefront.ID}),
		}).Error
	}); err != nil {
		return nil, err
	}
	if transitioned {
		s.queueStoreOrderNotification(ctx, &order, "store_order.approved")
	}
	return &order, nil
}

func (s *CommerceService) CancelStoreOrder(ctx context.Context, businessID, storefrontID, orderID, reason string) (*models.StoreOrder, error) {
	if _, err := s.GetStorefront(ctx, businessID, storefrontID); err != nil {
		return nil, err
	}
	var order models.StoreOrder
	transitioned := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Preload("Lines", "deleted_at IS NULL").
			Where("id = ? AND business_id = ? AND storefront_id = ? AND deleted_at IS NULL", orderID, businessID, storefrontID).
			First(&order).Error; err != nil {
			return err
		}
		if order.SalesInvoiceID != nil && strings.TrimSpace(*order.SalesInvoiceID) != "" {
			return fmt.Errorf("issued storefront orders require a compensating credit note before cancellation")
		}
		if order.Status == models.StoreOrderStatusCancelled {
			return nil
		}
		now := time.Now().UTC()
		order.Status = models.StoreOrderStatusCancelled
		order.CancelledAt = &now
		order.CancellationReason = reason
		if order.SalesOrderID != nil && strings.TrimSpace(*order.SalesOrderID) != "" {
			if s.inventory == nil {
				return fmt.Errorf("inventory service is not configured")
			}
			if err := s.inventory.ReleaseReservationsTx(ctx, tx, businessID, *order.SalesOrderID); err != nil {
				return err
			}
		}
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.StoreOrderEvent{
			StoreOrderID: order.ID,
			EventType:    "store_order.cancelled",
			Status:       order.Status,
			Payload:      mustMarshalMap(map[string]interface{}{"reason": reason}),
		}).Error; err != nil {
			return err
		}
		transitioned = true
		return nil
	}); err != nil {
		return nil, err
	}
	if transitioned {
		s.queueStoreOrderNotification(ctx, &order, "store_order.cancelled")
	}
	return &order, nil
}

func (s *CommerceService) GetCatalog(ctx context.Context, slug string) (*PublicStorefrontCatalogResponse, error) {
	storefront, err := s.findStorefrontBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := ensurePublishedStorefront(storefront); err != nil {
		return nil, err
	}
	var categories []*models.StorefrontCategory
	if err := s.db.WithContext(ctx).
		Where("storefront_id = ? AND deleted_at IS NULL", storefront.ID).
		Order("sort_order ASC, name ASC").
		Find(&categories).Error; err != nil {
		return nil, err
	}
	var storefrontProducts []*models.StorefrontProduct
	if err := s.db.WithContext(ctx).
		Where("storefront_id = ? AND deleted_at IS NULL AND is_published = TRUE", storefront.ID).
		Order("sort_order ASC, created_at ASC").
		Find(&storefrontProducts).Error; err != nil {
		return nil, err
	}
	items := make([]*StorefrontCatalogItem, 0, len(storefrontProducts))
	for _, storefrontProduct := range storefrontProducts {
		product, err := s.productRepo.GetByID(ctx, storefrontProduct.ProductID, storefront.BusinessID)
		if err != nil || !product.IsActive {
			continue
		}
		items = append(items, &StorefrontCatalogItem{StorefrontProduct: storefrontProduct, Product: product})
	}
	return publicStorefrontCatalogResponse(storefront, categories, items), nil
}

func (s *CommerceService) ValidateCoupon(ctx context.Context, slug string, input ValidateCouponInput) (*CouponValidationResult, error) {
	storefront, err := s.findStorefrontBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := ensurePublishedStorefront(storefront); err != nil {
		return nil, err
	}
	_, discount, err := s.resolveCoupon(ctx, storefront, strings.ToUpper(strings.TrimSpace(input.Code)), "", input.CustomerEmail, input.Subtotal)
	if err != nil {
		return &CouponValidationResult{Valid: false, Message: err.Error()}, nil
	}
	return &CouponValidationResult{
		Valid:         true,
		DiscountTotal: discount,
	}, nil
}

func storefrontCheckoutCommand(storefrontID string) string {
	return "storefront.checkout:" + storefrontID
}

func (s *CommerceService) claimStorefrontCheckout(
	ctx context.Context,
	businessID, storefrontID, idempotencyKey, requestHash string,
) (bool, string, error) {
	claim := &models.APIIdempotencyKey{
		BusinessID:     businessID,
		Command:        storefrontCheckoutCommand(storefrontID),
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		Status:         models.IdempotencyStatusInProgress,
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "business_id"}, {Name: "command"}, {Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(claim)
	if result.Error != nil {
		return false, "", result.Error
	}
	if result.RowsAffected == 1 {
		return true, "", nil
	}

	var existing models.APIIdempotencyKey
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND command = ? AND idempotency_key = ?", businessID, claim.Command, idempotencyKey).
		First(&existing).Error; err != nil {
		return false, "", err
	}
	if existing.RequestHash != requestHash {
		return false, "", fmt.Errorf("idempotency key already used for a different checkout request")
	}
	if existing.Status == models.IdempotencyStatusCompleted {
		if existing.ResultType == nil || *existing.ResultType != "store_order" || existing.ResultID == nil || *existing.ResultID == "" {
			return false, "", fmt.Errorf("completed checkout claim has no order")
		}
		return false, *existing.ResultID, nil
	}
	if existing.Status != models.IdempotencyStatusInProgress {
		return false, "", fmt.Errorf("checkout claim has invalid status")
	}

	now := time.Now().UTC()
	reclaimed := s.db.WithContext(ctx).Model(&models.APIIdempotencyKey{}).
		Where("id = ? AND request_hash = ? AND status = ? AND updated_at < ?", existing.ID, requestHash, models.IdempotencyStatusInProgress, now.Add(-storefrontCheckoutClaimTTL)).
		Updates(map[string]interface{}{"updated_at": now})
	if reclaimed.Error != nil {
		return false, "", reclaimed.Error
	}
	if reclaimed.RowsAffected == 1 {
		return true, "", nil
	}
	return false, "", fmt.Errorf("checkout is still processing; retry with the same idempotency key")
}

func completeStorefrontCheckoutClaim(
	tx *gorm.DB,
	businessID, storefrontID, idempotencyKey, requestHash, orderID string,
) error {
	now := time.Now().UTC()
	resultType := "store_order"
	result := tx.Model(&models.APIIdempotencyKey{}).
		Where("business_id = ? AND command = ? AND idempotency_key = ? AND request_hash = ? AND status = ?",
			businessID, storefrontCheckoutCommand(storefrontID), idempotencyKey, requestHash, models.IdempotencyStatusInProgress).
		Updates(map[string]interface{}{
			"status":       models.IdempotencyStatusCompleted,
			"result_type":  &resultType,
			"result_id":    &orderID,
			"completed_at": &now,
			"updated_at":   now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("checkout claim could not be completed")
	}
	return nil
}

func (s *CommerceService) abandonStorefrontCheckoutClaim(
	ctx context.Context,
	businessID, storefrontID, idempotencyKey, requestHash string,
) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = s.db.WithContext(cleanupCtx).
		Where("business_id = ? AND command = ? AND idempotency_key = ? AND request_hash = ? AND status = ?",
			businessID, storefrontCheckoutCommand(storefrontID), idempotencyKey, requestHash, models.IdempotencyStatusInProgress).
		Delete(&models.APIIdempotencyKey{}).Error
}

func (s *CommerceService) Checkout(ctx context.Context, slug, idempotencyKey string, input StorefrontCheckoutInput) (*CheckoutResult, error) {
	storefront, err := s.findStorefrontBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := ensurePublishedStorefront(storefront); err != nil {
		return nil, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, fmt.Errorf("idempotency key is required")
	}
	var existing models.StoreOrder
	err = s.db.WithContext(ctx).
		Preload("Lines", "deleted_at IS NULL").
		Where("storefront_id = ? AND idempotency_key = ? AND deleted_at IS NULL", storefront.ID, idempotencyKey).
		First(&existing).Error
	if err == nil {
		if !checkoutInputMatchesOrder(&existing, input) {
			return nil, fmt.Errorf("idempotency key already used for a different checkout request")
		}
		return &CheckoutResult{Order: &existing, GatewayOrderID: existing.GatewayOrderID}, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	requestHash := checkoutInputFingerprint(input)
	claimed, replayOrderID, err := s.claimStorefrontCheckout(ctx, storefront.BusinessID, storefront.ID, idempotencyKey, requestHash)
	if err != nil {
		return nil, err
	}
	if replayOrderID != "" {
		if err := s.db.WithContext(ctx).
			Preload("Lines", "deleted_at IS NULL").
			Where("id = ? AND business_id = ? AND storefront_id = ? AND deleted_at IS NULL", replayOrderID, storefront.BusinessID, storefront.ID).
			First(&existing).Error; err != nil {
			return nil, err
		}
		if !checkoutInputMatchesOrder(&existing, input) {
			return nil, fmt.Errorf("idempotency key already used for a different checkout request")
		}
		return &CheckoutResult{Order: &existing, GatewayOrderID: existing.GatewayOrderID}, nil
	}
	claimCompleted := false
	defer func() {
		if claimed && !claimCompleted {
			s.abandonStorefrontCheckoutClaim(ctx, storefront.BusinessID, storefront.ID, idempotencyKey, requestHash)
		}
	}()
	paymentMethod := normalizePublicPaymentMethod(input.PaymentMethod)
	if paymentMethod == "" {
		return nil, fmt.Errorf("unsupported payment method")
	}
	if input.ShippingTotal < 0 {
		return nil, fmt.Errorf("shipping total must be non-negative")
	}
	if paymentMethod == "online" && !storefront.AllowOnlinePayment {
		return nil, fmt.Errorf("online payments are not enabled for this storefront")
	}
	if paymentMethod == "cod" && !storefront.AllowCOD {
		return nil, fmt.Errorf("cash on delivery is not enabled for this storefront")
	}
	business, err := s.businessRepo.GetByID(ctx, storefront.BusinessID)
	if err != nil {
		return nil, err
	}
	orderCurrency := defaultCurrency(firstNonEmpty(input.Currency, storefront.Currency, business.Currency))
	customer, err := s.resolveCheckoutCustomer(ctx, storefront.BusinessID, input.Customer, input.BillingAddress, input.ShippingAddress)
	if err != nil {
		return nil, err
	}

	order := &models.StoreOrder{
		ID:              uuid.NewString(),
		BusinessID:      storefront.BusinessID,
		StorefrontID:    storefront.ID,
		BranchID:        stringPointer(input.BranchID),
		CustomerID:      stringPointer(customer.ID),
		PublicToken:     randomToken(18),
		OrderNumber:     s.nextStoreOrderNumber(),
		Status:          models.StoreOrderStatusPending,
		PaymentStatus:   models.StoreOrderPaymentStatusPending,
		PaymentMethod:   paymentMethod,
		Currency:        orderCurrency,
		ExchangeRate:    1,
		BillingAddress:  mustMarshalMap(firstAvailableMap(input.BillingAddress, input.Customer.Billing)),
		ShippingAddress: mustMarshalMap(firstAvailableMap(input.ShippingAddress, input.Customer.Shipping)),
		Notes:           input.Notes,
		IdempotencyKey:  idempotencyKey,
		OrderedAt:       time.Now().UTC(),
	}
	if order.PaymentMethod == "cod" {
		order.PaymentStatus = models.StoreOrderPaymentStatusCOD
		order.Status = models.StoreOrderStatusAwaitingApproval
	}
	if err := s.validateBranchID(ctx, storefront.BusinessID, input.BranchID); err != nil {
		return nil, err
	}

	lines, subtotal, taxTotal, err := s.buildStoreOrderLines(ctx, storefront, input.Items)
	if err != nil {
		return nil, err
	}
	order.Subtotal = subtotal
	order.TaxTotal = taxTotal
	order.ShippingTotal = input.ShippingTotal

	if !strings.EqualFold(orderCurrency, defaultCurrency(business.Currency)) {
		if err := s.ensureFeatureEnabled(ctx, storefront.BusinessID, FeatureMultiCurrency); err != nil {
			return nil, err
		}
		fxRate, err := s.ResolveFXRate(ctx, defaultCurrency(business.Currency), orderCurrency)
		if err != nil {
			return nil, err
		}
		order.ExchangeRate = fxRate.Rate
		order.FXProvider = fxRate.Provider
		order.FXBaseCurrency = fxRate.BaseCurrency
		order.FXQuoteCurrency = fxRate.QuoteCurrency
		order.FXRateTimestamp = &fxRate.FetchedAt
		for _, line := range lines {
			line.UnitPrice = roundMoney(line.UnitPrice * fxRate.Rate)
			line.DiscountAmount = roundMoney(line.DiscountAmount * fxRate.Rate)
			line.TaxAmount = roundMoney(line.TaxAmount * fxRate.Rate)
			line.LineTotal = roundMoney(line.LineTotal * fxRate.Rate)
		}
		order.Subtotal = roundMoney(order.Subtotal * fxRate.Rate)
		order.TaxTotal = roundMoney(order.TaxTotal * fxRate.Rate)
		order.ShippingTotal = roundMoney(order.ShippingTotal * fxRate.Rate)
	}

	couponCode := strings.ToUpper(strings.TrimSpace(input.CouponCode))
	if couponCode != "" {
		coupon, discount, err := s.resolveCoupon(ctx, storefront, couponCode, customer.ID, customer.Email, order.Subtotal)
		if err != nil {
			return nil, err
		}
		order.CouponID = &coupon.ID
		order.DiscountTotal = roundMoney(discount)
	}

	order.Total = roundMoney(order.Subtotal + order.TaxTotal + order.ShippingTotal - order.DiscountTotal)
	if order.Total < 0 {
		order.Total = 0
	}
	if storefront.MinimumOrderValue > 0 && order.Total < storefront.MinimumOrderValue {
		return nil, fmt.Errorf("minimum order value is %.2f", storefront.MinimumOrderValue)
	}
	order.Snapshot = mustMarshalMap(map[string]interface{}{
		"checkout_fingerprint": requestHash,
		"customer":             input.Customer,
		"items":                input.Items,
	})
	salesOrder, err := s.buildStoreOrderDocument(ctx, order, lines, models.DocumentTypeSalesOrder)
	if err != nil {
		return nil, err
	}
	order.SalesOrderID = stringPointer(salesOrder.ID)

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(salesOrder).Error; err != nil {
			return err
		}
		if err := s.inventory.ApplyDocumentTx(ctx, tx, salesOrder); err != nil {
			return err
		}
		if err := createStoreOrderDocumentRevisionTx(tx, salesOrder, order.ID, order.PaymentStatus); err != nil {
			return err
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		for _, line := range lines {
			line.StoreOrderID = order.ID
			if err := tx.Create(line).Error; err != nil {
				return err
			}
		}
		if order.CouponID != nil && *order.CouponID != "" {
			redemption := &models.StorefrontCouponRedemption{
				StorefrontCouponID: *order.CouponID,
				StoreOrderID:       &order.ID,
				CustomerID:         &customer.ID,
				CustomerEmail:      customer.Email,
				DiscountAmount:     order.DiscountTotal,
			}
			if err := tx.Create(redemption).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&models.StoreOrderEvent{
			StoreOrderID: order.ID,
			EventType:    "store_order.created",
			Status:       order.Status,
			Payload:      mustMarshalMap(map[string]interface{}{"payment_method": order.PaymentMethod}),
		}).Error; err != nil {
			return err
		}
		return completeStorefrontCheckoutClaim(tx, storefront.BusinessID, storefront.ID, idempotencyKey, requestHash, order.ID)
	}); err != nil {
		return nil, err
	}
	claimCompleted = true

	s.queueStoreOrderNotification(ctx, order, "store_order.created")
	return &CheckoutResult{
		Order:          order,
		GatewayOrderID: order.GatewayOrderID,
	}, nil
}

func normalizePublicPaymentMethod(method string) string {
	normalized := strings.ToLower(strings.TrimSpace(method))
	if normalized == "" {
		return "cod"
	}
	switch normalized {
	case "cod", "online":
		return normalized
	default:
		return ""
	}
}

func (s *CommerceService) GetPublicOrder(ctx context.Context, slug, token string) (*models.StoreOrder, error) {
	storefront, err := s.findStorefrontBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	var order models.StoreOrder
	if err := s.db.WithContext(ctx).
		Select("id", "order_number", "status", "payment_status", "payment_method", "currency", "subtotal", "discount_total", "tax_total", "shipping_total", "total", "ordered_at", "paid_at", "cancelled_at").
		Preload("Lines", func(db *gorm.DB) *gorm.DB {
			return db.Select("store_order_id", "title", "sku", "quantity", "unit_price", "discount_amount", "tax_rate", "tax_amount", "line_total").Where("deleted_at IS NULL")
		}).
		Where("storefront_id = ? AND public_token = ? AND deleted_at IS NULL", storefront.ID, token).
		First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func (s *CommerceService) ListDriveAssets(ctx context.Context, businessID string) ([]*models.DriveAsset, int64, error) {
	var assets []*models.DriveAsset
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("created_at DESC").
		Find(&assets).Error; err != nil {
		return nil, 0, err
	}
	var usage int64
	if err := s.db.WithContext(ctx).
		Model(&models.DriveAsset{}).
		Select("COALESCE(SUM(size_bytes), 0)").
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Scan(&usage).Error; err != nil {
		return nil, 0, err
	}
	return assets, usage, nil
}

func (s *CommerceService) CreateDriveUpload(ctx context.Context, businessID, userID string, input CreateDriveAssetInput) (*DriveUploadSession, error) {
	if err := s.ensureFeatureEnabled(ctx, businessID, FeatureDriveStorageMB); err != nil {
		return nil, err
	}
	contentType, err := validateDriveAssetUpload(input)
	if err != nil {
		return nil, err
	}
	usage, limit, err := s.driveUsageAndLimit(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if limit > 0 && usage+input.SizeBytes > limit {
		return nil, fmt.Errorf("drive storage quota exceeded")
	}
	asset := &models.DriveAsset{
		BusinessID:  businessID,
		UploadedBy:  stringPointer(userID),
		Name:        input.Name,
		FolderPath:  normalizeDriveFolderPath(input.FolderPath),
		Bucket:      s.cfg.S3.BucketDrive,
		ObjectKey:   fmt.Sprintf("%s/%s/%s", businessID, time.Now().UTC().Format("20060102"), uuid.NewString()),
		ContentType: contentType,
		SizeBytes:   input.SizeBytes,
		Category:    input.Category,
		Metadata:    mustMarshalMap(input.Metadata),
	}
	if err := s.db.WithContext(ctx).Create(asset).Error; err != nil {
		return nil, err
	}
	upload, err := s.s3.GeneratePresignedUpload(ctx, asset.Bucket, asset.ObjectKey, contentType, input.SizeBytes, 900)
	if err != nil {
		return nil, err
	}
	return &DriveUploadSession{
		Asset:           asset,
		UploadURL:       upload.UploadURL,
		RequiredHeaders: upload.RequiredHeaders,
	}, nil
}

func (s *CommerceService) UpdateDriveAsset(ctx context.Context, businessID, assetID string, input UpdateDriveAssetInput) (*models.DriveAsset, error) {
	var asset models.DriveAsset
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", assetID, businessID).
		First(&asset).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("drive asset not found")
		}
		return nil, err
	}
	if input.Name != "" {
		asset.Name = input.Name
	}
	if input.Category != "" {
		asset.Category = input.Category
	}
	asset.FolderPath = normalizeDriveFolderPath(input.FolderPath)
	if input.Metadata != nil {
		asset.Metadata = mustMarshalMap(input.Metadata)
	}
	if err := s.db.WithContext(ctx).Save(&asset).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (s *CommerceService) DeleteDriveAsset(ctx context.Context, businessID, assetID string) error {
	var asset models.DriveAsset
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", assetID, businessID).
		First(&asset).Error; err != nil {
		return err
	}
	if err := s.s3.Delete(ctx, asset.Bucket, asset.ObjectKey); err != nil {
		s.log.Warn("failed to delete drive asset from s3", "asset_id", asset.ID, "error", err)
	}
	return s.db.WithContext(ctx).Delete(&asset).Error
}

func (s *CommerceService) GetWhatsAppConfig(ctx context.Context, businessID string) (*WhatsAppConfigResponse, error) {
	var cfg models.WhatsAppConfig
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("created_at DESC").
		First(&cfg).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("whatsapp config not found")
		}
		return nil, err
	}
	return s.toWhatsAppConfigResponse(ctx, &cfg)
}

func (s *CommerceService) UpsertWhatsAppConfig(ctx context.Context, businessID string, input UpsertWhatsAppConfigInput) (*WhatsAppConfigResponse, error) {
	var cfg models.WhatsAppConfig
	err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		First(&cfg).Error
	switch {
	case err == nil:
		cfg.PhoneNumberID = firstNonEmpty(input.PhoneNumberID, cfg.PhoneNumberID)
		if input.AccessToken != "" {
			cfg.AccessToken = input.AccessToken
		}
		if input.WebhookSecret != "" {
			cfg.WebhookSecret = input.WebhookSecret
		}
		if input.VerifyToken != "" {
			cfg.VerifyToken = input.VerifyToken
		}
		if input.DefaultRecipient != "" {
			cfg.DefaultRecipient = input.DefaultRecipient
		}
		if input.Enabled != nil {
			cfg.Enabled = *input.Enabled
		}
		if input.Metadata != nil {
			cfg.Metadata = mustMarshalMap(input.Metadata)
		}
		if err := s.db.WithContext(ctx).Save(&cfg).Error; err != nil {
			return nil, err
		}
	case err == gorm.ErrRecordNotFound:
		cfg = models.WhatsAppConfig{
			BusinessID:       businessID,
			PhoneNumberID:    input.PhoneNumberID,
			AccessToken:      input.AccessToken,
			WebhookSecret:    input.WebhookSecret,
			VerifyToken:      input.VerifyToken,
			DefaultRecipient: input.DefaultRecipient,
			Enabled:          boolValueOrDefault(input.Enabled, false),
			Metadata:         mustMarshalMap(input.Metadata),
		}
		if err := s.db.WithContext(ctx).Create(&cfg).Error; err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	return s.toWhatsAppConfigResponse(ctx, &cfg)
}

func (s *CommerceService) ListNotificationDeliveries(ctx context.Context, businessID string, limit int) ([]*models.NotificationDelivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var deliveries []*models.NotificationDelivery
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("created_at DESC").
		Limit(limit).
		Find(&deliveries).Error; err != nil {
		return nil, err
	}
	return deliveries, nil
}

func (s *CommerceService) toWhatsAppConfigResponse(ctx context.Context, cfg *models.WhatsAppConfig) (*WhatsAppConfigResponse, error) {
	response := &WhatsAppConfigResponse{
		ID:                      cfg.ID,
		BusinessID:              cfg.BusinessID,
		PhoneNumberID:           cfg.PhoneNumberID,
		DefaultRecipient:        cfg.DefaultRecipient,
		Enabled:                 cfg.Enabled,
		Metadata:                cfg.Metadata,
		AccessTokenConfigured:   strings.TrimSpace(cfg.AccessToken) != "",
		WebhookSecretConfigured: strings.TrimSpace(cfg.WebhookSecret) != "",
		VerifyTokenConfigured:   strings.TrimSpace(cfg.VerifyToken) != "",
		CreatedAt:               cfg.CreatedAt,
		UpdatedAt:               cfg.UpdatedAt,
	}
	var lastDelivery models.NotificationDelivery
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND channel = ? AND deleted_at IS NULL", cfg.BusinessID, models.NotificationChannelWhatsApp).
		Order("created_at DESC").
		First(&lastDelivery).Error; err == nil {
		response.LastDeliveryAt = &lastDelivery.CreatedAt
	}
	return response, nil
}

func normalizeDriveFolderPath(folderPath string) string {
	trimmed := strings.TrimSpace(folderPath)
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '/'
	})
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/")
}

func (s *CommerceService) ResolveFXRate(ctx context.Context, baseCurrency, quoteCurrency string) (*models.FXRate, error) {
	baseCurrency = strings.ToUpper(strings.TrimSpace(baseCurrency))
	quoteCurrency = strings.ToUpper(strings.TrimSpace(quoteCurrency))
	if baseCurrency == "" || quoteCurrency == "" {
		return nil, fmt.Errorf("both currencies are required")
	}
	if baseCurrency == quoteCurrency {
		now := time.Now().UTC()
		return &models.FXRate{
			Provider:      "identity",
			BaseCurrency:  baseCurrency,
			QuoteCurrency: quoteCurrency,
			Rate:          1,
			FetchedAt:     now,
		}, nil
	}

	var cached models.FXRate
	err := s.db.WithContext(ctx).
		Where("base_currency = ? AND quote_currency = ? AND deleted_at IS NULL", baseCurrency, quoteCurrency).
		Order("fetched_at DESC").
		First(&cached).Error
	if err == nil && time.Since(cached.FetchedAt) < time.Hour {
		return &cached, nil
	}

	baseURL := strings.TrimRight(s.cfg.FX.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+baseCurrency, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.cfg.FX.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.FX.APIKey)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if cached.ID != "" {
			return &cached, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if cached.ID != "" {
			return &cached, nil
		}
		return nil, fmt.Errorf("fx provider returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	rates, _ := payload["rates"].(map[string]interface{})
	rate := floatValue(rates[quoteCurrency])
	if rate <= 0 {
		if nested, ok := payload["conversion_rates"].(map[string]interface{}); ok {
			rate = floatValue(nested[quoteCurrency])
		}
	}
	if rate <= 0 {
		return nil, fmt.Errorf("fx rate for %s/%s not available", baseCurrency, quoteCurrency)
	}
	fxRate := &models.FXRate{
		Provider:      firstNonEmpty(s.cfg.FX.Provider, "fx-api"),
		BaseCurrency:  baseCurrency,
		QuoteCurrency: quoteCurrency,
		Rate:          rate,
		FetchedAt:     time.Now().UTC(),
		Metadata:      mustMarshalMap(payload),
	}
	if err := s.db.WithContext(ctx).Create(fxRate).Error; err != nil {
		return nil, err
	}
	return fxRate, nil
}

func (s *CommerceService) getRole(ctx context.Context, businessID, roleID string) (*models.Role, error) {
	var role models.Role
	if err := s.db.WithContext(ctx).
		Preload("Permissions", "deleted_at IS NULL").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", roleID, businessID).
		First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

func (s *CommerceService) replaceRolePermissionsTx(tx *gorm.DB, roleID string, permissions []string) error {
	if err := tx.Where("role_id = ?", roleID).Delete(&models.RolePermission{}).Error; err != nil {
		return err
	}
	unique := uniqueStrings(permissions)
	for _, permission := range unique {
		if permission == "" {
			continue
		}
		record := &models.RolePermission{RoleID: roleID, PermissionKey: permission}
		if err := tx.Create(record).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *CommerceService) replaceStorefrontDomainsTx(tx *gorm.DB, storefrontID string, domains []string) error {
	if domains == nil {
		return nil
	}
	if err := tx.Where("storefront_id = ?", storefrontID).Delete(&models.StorefrontDomain{}).Error; err != nil {
		return err
	}
	for index, domain := range uniqueStrings(domains) {
		record := &models.StorefrontDomain{
			StorefrontID: storefrontID,
			Domain:       strings.ToLower(strings.TrimSpace(domain)),
			IsPrimary:    index == 0,
		}
		if record.Domain == "" {
			continue
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *CommerceService) findStorefrontBySlug(ctx context.Context, slug string) (*models.Storefront, error) {
	var storefront models.Storefront
	if err := s.db.WithContext(ctx).
		Preload("Domains", "deleted_at IS NULL").
		Where("slug = ? AND deleted_at IS NULL", normalizeLookupKey(slug)).
		First(&storefront).Error; err != nil {
		return nil, err
	}
	return &storefront, nil
}

func ensurePublishedStorefront(storefront *models.Storefront) error {
	if storefront == nil || storefront.Status != models.StorefrontStatusPublished {
		return fmt.Errorf("storefront is not accepting orders")
	}
	return nil
}

func publicStorefrontCatalogResponse(storefront *models.Storefront, categories []*models.StorefrontCategory, items []*StorefrontCatalogItem) *PublicStorefrontCatalogResponse {
	response := &PublicStorefrontCatalogResponse{
		Storefront: &PublicStorefront{
			Name:               storefront.Name,
			Slug:               storefront.Slug,
			Currency:           storefront.Currency,
			AllowCOD:           storefront.AllowCOD,
			AllowOnlinePayment: storefront.AllowOnlinePayment,
			MinimumOrderValue:  storefront.MinimumOrderValue,
		},
		Categories: make([]*PublicStorefrontCategory, 0, len(categories)),
		Products:   make([]*PublicStorefrontProduct, 0, len(items)),
	}
	for _, category := range categories {
		if category == nil {
			continue
		}
		response.Categories = append(response.Categories, &PublicStorefrontCategory{
			ID:          category.ID,
			Name:        category.Name,
			Slug:        category.Slug,
			Description: category.Description,
			ImageURL:    category.ImageURL,
			SortOrder:   category.SortOrder,
		})
	}
	for _, item := range items {
		if item == nil || item.StorefrontProduct == nil || item.Product == nil {
			continue
		}
		storefrontProduct := item.StorefrontProduct
		product := item.Product
		response.Products = append(response.Products, &PublicStorefrontProduct{
			ID:             storefrontProduct.ID,
			ProductID:      product.ID,
			CategoryID:     storefrontProduct.CategoryID,
			Name:           product.Name,
			SKU:            product.SKU,
			Description:    product.Description,
			Price:          product.Price,
			MRP:            product.MRP,
			DisplayPrice:   storefrontProduct.DisplayPrice,
			CompareAtPrice: storefrontProduct.CompareAtPrice,
			Currency:       product.Currency,
			Unit:           product.Unit,
			ImageURL:       product.ImageURL,
			IsService:      product.IsService,
			Badge:          storefrontProduct.Badge,
			SortOrder:      storefrontProduct.SortOrder,
			SEO:            unmarshalJSONMap(storefrontProduct.SEO),
			Metadata:       unmarshalJSONMap(storefrontProduct.Metadata),
		})
	}
	return response
}

func checkoutInputMatchesOrder(order *models.StoreOrder, input StorefrontCheckoutInput) bool {
	if order == nil {
		return false
	}
	snapshot := unmarshalJSONMap(order.Snapshot)
	if existing, ok := snapshot["checkout_fingerprint"].(string); ok && existing != "" {
		return existing == checkoutInputFingerprint(input)
	}

	customerBody, _ := json.Marshal(snapshot["customer"])
	itemsBody, _ := json.Marshal(snapshot["items"])
	currentCustomer, _ := json.Marshal(input.Customer)
	currentItems, _ := json.Marshal(input.Items)
	return bytes.Equal(customerBody, currentCustomer) && bytes.Equal(itemsBody, currentItems)
}

func checkoutInputFingerprint(input StorefrontCheckoutInput) string {
	body, _ := json.Marshal(struct {
		BranchID        string                 `json:"branch_id,omitempty"`
		Currency        string                 `json:"currency,omitempty"`
		PaymentMethod   string                 `json:"payment_method,omitempty"`
		CouponCode      string                 `json:"coupon_code,omitempty"`
		ShippingTotal   float64                `json:"shipping_total"`
		BillingAddress  map[string]interface{} `json:"billing_address,omitempty"`
		ShippingAddress map[string]interface{} `json:"shipping_address,omitempty"`
		Customer        CheckoutCustomerInput  `json:"customer"`
		Items           []CheckoutItemInput    `json:"items"`
	}{
		BranchID:        strings.TrimSpace(input.BranchID),
		Currency:        strings.TrimSpace(input.Currency),
		PaymentMethod:   strings.ToLower(strings.TrimSpace(input.PaymentMethod)),
		CouponCode:      strings.ToUpper(strings.TrimSpace(input.CouponCode)),
		ShippingTotal:   input.ShippingTotal,
		BillingAddress:  input.BillingAddress,
		ShippingAddress: input.ShippingAddress,
		Customer:        input.Customer,
		Items:           input.Items,
	})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (s *CommerceService) resolveCheckoutCustomer(ctx context.Context, businessID string, input CheckoutCustomerInput, billing, shipping map[string]interface{}) (*models.Customer, error) {
	query := s.db.WithContext(ctx).Model(&models.Customer{}).Where("business_id = ? AND deleted_at IS NULL", businessID)
	if strings.TrimSpace(input.Email) != "" {
		var customer models.Customer
		if err := query.Where("LOWER(email) = ?", strings.ToLower(strings.TrimSpace(input.Email))).First(&customer).Error; err == nil {
			return &customer, nil
		}
	}
	if strings.TrimSpace(input.Phone) != "" {
		var customer models.Customer
		if err := s.db.WithContext(ctx).Model(&models.Customer{}).
			Where("business_id = ? AND phone = ? AND deleted_at IS NULL", businessID, input.Phone).
			First(&customer).Error; err == nil {
			return &customer, nil
		}
	}
	customer := &models.Customer{
		BusinessID:   businessID,
		Name:         input.Name,
		Email:        input.Email,
		Phone:        input.Phone,
		Address:      input.Address,
		City:         input.City,
		State:        input.State,
		Country:      input.Country,
		PostalCode:   input.PostalCode,
		GSTIN:        input.GSTIN,
		PAN:          input.PAN,
		StateCode:    input.StateCode,
		BillingJSON:  mustMarshalMap(firstAvailableMap(billing, input.Billing)),
		ShippingJSON: mustMarshalMap(firstAvailableMap(shipping, input.Shipping)),
	}
	if err := s.db.WithContext(ctx).Create(customer).Error; err != nil {
		return nil, err
	}
	return customer, nil
}

func (s *CommerceService) buildStoreOrderLines(ctx context.Context, storefront *models.Storefront, inputs []CheckoutItemInput) ([]*models.StoreOrderLine, float64, float64, error) {
	items := make([]*models.StoreOrderLine, 0, len(inputs))
	var subtotal float64
	var taxTotal float64
	for _, input := range inputs {
		var storefrontProduct models.StorefrontProduct
		if err := s.db.WithContext(ctx).
			Where("storefront_id = ? AND product_id = ? AND is_published = TRUE AND deleted_at IS NULL", storefront.ID, input.ProductID).
			First(&storefrontProduct).Error; err != nil {
			return nil, 0, 0, fmt.Errorf("product is not available in this storefront")
		}
		product, err := s.productRepo.GetByID(ctx, input.ProductID, storefront.BusinessID)
		if err != nil {
			return nil, 0, 0, err
		}
		if !product.IsActive {
			return nil, 0, 0, fmt.Errorf("product %s is inactive", product.Name)
		}
		unitPrice := coalesceFloat(storefrontProduct.DisplayPrice, product.Price)
		sku := product.SKU
		title := product.Name
		snapshot := map[string]interface{}{
			"product_id":  product.ID,
			"image_url":   product.ImageURL,
			"description": product.Description,
		}
		if input.VariantID != "" {
			var variant models.ProductVariant
			if err := s.db.WithContext(ctx).
				Where("id = ? AND product_id = ? AND business_id = ? AND deleted_at IS NULL", input.VariantID, input.ProductID, storefront.BusinessID).
				First(&variant).Error; err != nil {
				return nil, 0, 0, err
			}
			unitPrice = coalesceFloat(variant.Price, unitPrice)
			sku = firstNonEmpty(variant.SKU, sku)
			title = product.Name + " / " + variant.Name
			snapshot["variant_id"] = variant.ID
			snapshot["variant_name"] = variant.Name
		}
		lineSubtotal := roundMoney(unitPrice * input.Quantity)
		taxRate := floatValue(unmarshalJSONMap(product.GSTMetadata)["tax_rate"])
		taxAmount := roundMoney(lineSubtotal * (taxRate / 100))
		lineTotal := roundMoney(lineSubtotal + taxAmount)
		item := &models.StoreOrderLine{
			ProductID:      stringPointer(product.ID),
			VariantID:      stringPointer(input.VariantID),
			WarehouseID:    stringPointer(input.WarehouseID),
			Title:          title,
			SKU:            sku,
			Quantity:       input.Quantity,
			UnitPrice:      unitPrice,
			DiscountAmount: 0,
			TaxRate:        taxRate,
			TaxAmount:      taxAmount,
			LineTotal:      lineTotal,
			Snapshot:       mustMarshalMap(snapshot),
		}
		subtotal += lineSubtotal
		taxTotal += taxAmount
		items = append(items, item)
	}
	return items, roundMoney(subtotal), roundMoney(taxTotal), nil
}

func (s *CommerceService) resolveCoupon(ctx context.Context, storefront *models.Storefront, code, customerID, customerEmail string, subtotal float64) (*models.StorefrontCoupon, float64, error) {
	var coupon models.StorefrontCoupon
	if err := s.db.WithContext(ctx).
		Where("storefront_id = ? AND code = ? AND deleted_at IS NULL", storefront.ID, code).
		First(&coupon).Error; err != nil {
		return nil, 0, fmt.Errorf("coupon not found")
	}
	now := time.Now().UTC()
	if !coupon.IsActive {
		return nil, 0, fmt.Errorf("coupon is inactive")
	}
	if coupon.StartsAt != nil && now.Before(*coupon.StartsAt) {
		return nil, 0, fmt.Errorf("coupon is not active yet")
	}
	if coupon.EndsAt != nil && now.After(*coupon.EndsAt) {
		return nil, 0, fmt.Errorf("coupon has expired")
	}
	if subtotal < coupon.MinimumOrderValue {
		return nil, 0, fmt.Errorf("coupon minimum order value is %.2f", coupon.MinimumOrderValue)
	}
	if coupon.UsageLimit > 0 {
		var total int64
		if err := s.db.WithContext(ctx).
			Model(&models.StorefrontCouponRedemption{}).
			Where("storefront_coupon_id = ? AND deleted_at IS NULL", coupon.ID).
			Count(&total).Error; err != nil {
			return nil, 0, err
		}
		if total >= coupon.UsageLimit {
			return nil, 0, fmt.Errorf("coupon usage limit reached")
		}
	}
	if coupon.UsageLimitPerCustomer > 0 {
		query := s.db.WithContext(ctx).
			Model(&models.StorefrontCouponRedemption{}).
			Where("storefront_coupon_id = ? AND deleted_at IS NULL", coupon.ID)
		if customerID != "" {
			query = query.Where("customer_id = ?", customerID)
		} else if customerEmail != "" {
			query = query.Where("LOWER(customer_email) = ?", strings.ToLower(customerEmail))
		}
		var total int64
		if err := query.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		if total >= coupon.UsageLimitPerCustomer {
			return nil, 0, fmt.Errorf("coupon customer usage limit reached")
		}
	}

	discount := 0.0
	switch coupon.DiscountType {
	case models.StoreCouponDiscountTypePercent:
		discount = subtotal * (coupon.DiscountValue / 100)
	case models.StoreCouponDiscountTypeFixed:
		discount = coupon.DiscountValue
	default:
		return nil, 0, fmt.Errorf("unsupported coupon type")
	}
	if coupon.MaxDiscountAmount > 0 && discount > coupon.MaxDiscountAmount {
		discount = coupon.MaxDiscountAmount
	}
	if discount > subtotal {
		discount = subtotal
	}
	return &coupon, roundMoney(discount), nil
}

func (s *CommerceService) createSalesInvoiceForOrder(ctx context.Context, order *models.StoreOrder) (string, error) {
	business, err := s.businessRepo.GetByID(ctx, order.BusinessID)
	if err != nil {
		return "", err
	}
	idempotencyKey := storefrontInvoiceIdempotencyKey(order.ID)
	input, err := canonicalStorefrontInvoiceInput(order, business, idempotencyKey)
	if err != nil {
		return "", err
	}
	document, err := s.documents.createStorefrontSalesInvoice(ctx, order.BusinessID, input)
	if err != nil {
		return "", err
	}
	issueResult, err := s.documents.IssueSalesDocumentByBusiness(
		ctx,
		order.BusinessID,
		models.DocumentTypeSalesInvoice,
		document.ID,
		IssueInvoiceInput{
			IdempotencyKey:  idempotencyKey,
			ExpectedVersion: 1,
			DocumentType:    invoiceDocumentTypeForTaxProfile(input.TaxProfile),
			Series:          "WEB",
		},
	)
	if err != nil {
		return "", err
	}
	return issueResult.Invoice.ID, nil
}

func storefrontInvoiceIdempotencyKey(orderID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("storefront-invoice:"+strings.TrimSpace(orderID))).String()
}

func canonicalStorefrontInvoiceInput(
	order *models.StoreOrder,
	business *models.BusinessProfile,
	idempotencyKey string,
) (CreateInvoiceInput, error) {
	if order == nil || business == nil || strings.TrimSpace(order.ID) == "" ||
		order.CustomerID == nil || strings.TrimSpace(*order.CustomerID) == "" || len(order.Lines) == 0 {
		return CreateInvoiceInput{}, &idempotency.InvalidPayloadError{}
	}
	if _, err := uuid.Parse(strings.TrimSpace(idempotencyKey)); err != nil {
		return CreateInvoiceInput{}, &idempotency.InvalidKeyError{}
	}
	items := make([]CreateInvoiceItemInput, 0, len(order.Lines))
	for _, line := range order.Lines {
		if line == nil || line.ProductID == nil || strings.TrimSpace(*line.ProductID) == "" || line.Quantity <= 0 {
			return CreateInvoiceInput{}, &idempotency.InvalidPayloadError{}
		}
		items = append(items, CreateInvoiceItemInput{
			ProductID:   strings.TrimSpace(*line.ProductID),
			VariantID:   strings.TrimSpace(derefString(line.VariantID)),
			WarehouseID: strings.TrimSpace(derefString(line.WarehouseID)),
			Description: line.Title,
			Quantity:    line.Quantity,
			UnitPrice:   line.UnitPrice,
			Discount:    line.DiscountAmount,
			TaxRate:     line.TaxRate,
		})
	}
	gstTreatment := firstNonEmpty(business.DefaultGSTTreatment, models.DocumentGSTTreatmentRegular)
	return CreateInvoiceInput{
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
		Origin:         models.InvoiceOriginStorefront,
		CustomerID:     strings.TrimSpace(*order.CustomerID),
		Currency:       defaultCurrency(firstNonEmpty(order.Currency, business.Currency)),
		InvoiceDate:    order.OrderedAt,
		DueDate:        order.OrderedAt,
		Notes:          order.Notes,
		TaxProfile: TaxProfileInput{
			GSTTreatment: gstTreatment,
			BillOfSupply: gstTreatment == models.DocumentGSTTreatmentComposition || gstTreatment == models.DocumentGSTTreatmentExempt,
			SupplyType:   "sale",
			SourceLinkage: map[string]interface{}{
				"source":                  "storefront",
				"store_order_id":          order.ID,
				"sales_order_document_id": derefString(order.SalesOrderID),
			},
		},
		Items: items,
	}, nil
}

func (s *CommerceService) buildStoreOrderDocument(
	ctx context.Context,
	order *models.StoreOrder,
	lines []*models.StoreOrderLine,
	documentType string,
) (*models.Document, error) {
	if order.CustomerID == nil || *order.CustomerID == "" {
		return nil, fmt.Errorf("customer is required for document generation")
	}
	documentLines := make([]CreateDocumentLineInput, 0, len(lines))
	for _, line := range lines {
		documentLines = append(documentLines, CreateDocumentLineInput{
			ProductID:      derefString(line.ProductID),
			VariantID:      derefString(line.VariantID),
			Description:    line.Title,
			WarehouseID:    derefString(line.WarehouseID),
			Quantity:       line.Quantity,
			UnitPrice:      line.UnitPrice,
			DiscountAmount: line.DiscountAmount,
			TaxRate:        line.TaxRate,
		})
	}
	input := CreateDocumentInput{
		BranchID:     derefString(order.BranchID),
		PartyID:      *order.CustomerID,
		PartyType:    models.DocumentPartyTypeCustomer,
		Status:       models.DocumentStatusIssued,
		DraftState:   models.DocumentDraftStateFinal,
		IssueDate:    order.OrderedAt,
		Currency:     order.Currency,
		ExchangeRate: order.ExchangeRate,
		Locale:       "en-IN",
		Notes:        order.Notes,
		Direction:    models.DocumentDirectionOutward,
		Lines:        documentLines,
	}
	return s.documents.buildDocument(ctx, order.BusinessID, documentType, input)
}

func createStoreOrderDocumentRevisionTx(
	tx *gorm.DB,
	document *models.Document,
	storeOrderID, paymentStatus string,
) error {
	snapshot, err := json.Marshal(document)
	if err != nil {
		return err
	}
	return tx.Create(&models.DocumentRevision{
		DocumentID: document.ID,
		BusinessID: document.BusinessID,
		Action:     "created",
		Snapshot:   string(snapshot),
		Metadata: mustMarshalMap(map[string]interface{}{
			"origin":         "storefront",
			"store_order_id": storeOrderID,
			"payment_status": paymentStatus,
		}),
	}).Error
}

func (s *CommerceService) validateBranchID(ctx context.Context, businessID, branchID string) error {
	if branchID == "" {
		return nil
	}
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&models.Branch{}).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", branchID, businessID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("branch not found")
	}
	return nil
}

func (s *CommerceService) productEligibleForStorefront(ctx context.Context, productID string) (bool, error) {
	var trackedCount int64
	if err := s.db.WithContext(ctx).
		Model(&models.ProductVariant{}).
		Where("product_id = ? AND deleted_at IS NULL AND (track_batches = TRUE OR track_serials = TRUE)", productID).
		Count(&trackedCount).Error; err != nil {
		return false, err
	}
	return trackedCount == 0, nil
}

func (s *CommerceService) ensureFeatureEnabled(ctx context.Context, businessID, featureKey string) error {
	entitlements, err := s.ListFeatureEntitlements(ctx, businessID)
	if err != nil {
		return err
	}
	for _, entitlement := range entitlements {
		if entitlement.FeatureKey == featureKey {
			if !entitlement.Enabled {
				return fmt.Errorf("%s is not enabled on the current plan", featureKey)
			}
			return nil
		}
	}
	return fmt.Errorf("%s is not enabled on the current plan", featureKey)
}

func (s *CommerceService) driveUsageAndLimit(ctx context.Context, businessID string) (usageBytes int64, limitBytes int64, err error) {
	if err := s.db.WithContext(ctx).
		Model(&models.DriveAsset{}).
		Select("COALESCE(SUM(size_bytes), 0)").
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Scan(&usageBytes).Error; err != nil {
		return 0, 0, err
	}
	entitlements, err := s.ListFeatureEntitlements(ctx, businessID)
	if err != nil {
		return usageBytes, 0, err
	}
	for _, entitlement := range entitlements {
		if entitlement.FeatureKey == FeatureDriveStorageMB && entitlement.Enabled && entitlement.LimitValue != nil && *entitlement.LimitValue > 0 {
			limitBytes = *entitlement.LimitValue * 1024 * 1024
			return usageBytes, limitBytes, nil
		}
	}
	return usageBytes, 0, nil
}

func (s *CommerceService) queueStoreOrderNotification(ctx context.Context, order *models.StoreOrder, eventKey string) {
	if order == nil {
		return
	}
	var cfg models.WhatsAppConfig
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL AND enabled = TRUE", order.BusinessID).
		Order("created_at DESC").
		First(&cfg).Error; err != nil {
		return
	}

	var customer models.Customer
	if order.CustomerID != nil && *order.CustomerID != "" {
		_ = s.db.WithContext(ctx).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", *order.CustomerID, order.BusinessID).
			First(&customer).Error
	}
	recipient := firstNonEmpty(customer.Phone, cfg.DefaultRecipient)
	if recipient == "" {
		return
	}

	delivery := &models.NotificationDelivery{
		BusinessID:   order.BusinessID,
		StoreOrderID: &order.ID,
		Channel:      models.NotificationChannelWhatsApp,
		EventKey:     eventKey,
		Recipient:    recipient,
		Status:       "queued",
		RequestPayload: mustMarshalMap(map[string]interface{}{
			"order_number": order.OrderNumber,
			"event_key":    eventKey,
		}),
	}
	if err := s.db.WithContext(ctx).Create(delivery).Error; err != nil {
		return
	}

	go s.sendWhatsAppDelivery(context.Background(), &cfg, delivery, order, &customer)
}

func (s *CommerceService) sendWhatsAppDelivery(ctx context.Context, cfg *models.WhatsAppConfig, delivery *models.NotificationDelivery, order *models.StoreOrder, customer *models.Customer) {
	if cfg == nil || delivery == nil || order == nil {
		return
	}
	bodyText := fmt.Sprintf("Order %s is now %s. Total: %.2f %s", order.OrderNumber, order.Status, order.Total, order.Currency)
	reqBody := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                delivery.Recipient,
		"type":              "text",
		"text": map[string]interface{}{
			"body": bodyText,
		},
	}
	rawBody, _ := json.Marshal(reqBody)
	url := strings.TrimRight(s.cfg.WhatsApp.BaseURL, "/") + "/" + cfg.PhoneNumberID + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rawBody))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	now := time.Now().UTC()
	if err != nil {
		_ = s.db.WithContext(context.Background()).
			Model(delivery).
			Updates(map[string]interface{}{
				"status":          "failed",
				"attempt_count":   delivery.AttemptCount + 1,
				"last_attempt_at": &now,
				"response_payload": mustMarshalMap(map[string]interface{}{
					"error": err.Error(),
				}),
			}).Error
		return
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	updates := map[string]interface{}{
		"attempt_count":   delivery.AttemptCount + 1,
		"last_attempt_at": &now,
		"response_payload": mustMarshalMap(map[string]interface{}{
			"raw": string(responseBody),
		}),
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		updates["status"] = "sent"
		updates["delivered_at"] = &now
	} else {
		updates["status"] = "failed"
	}
	_ = s.db.WithContext(context.Background()).Model(delivery).Updates(updates).Error
}

type entitlementSeed struct {
	FeatureKey string
	Enabled    bool
	LimitValue *int64
	Metadata   map[string]interface{}
}

func defaultEntitlementSeedsForSubscription(subscription *models.Subscription) []entitlementSeed {
	return defaultEntitlementSeedsForPlan(subscriptionPlanForSubscription(subscription, time.Now().UTC()))
}

func defaultEntitlementSeedsForPlan(plan SubscriptionPlan) []entitlementSeed {
	users := plan.Quotas[QuotaUsers]
	storageMB := plan.Quotas[QuotaStorageMB]
	return []entitlementSeed{
		{FeatureKey: FeatureOnlineStore, Enabled: plan.Features[FeatureOnlineStore]},
		{FeatureKey: FeatureMultiCurrency, Enabled: plan.Features[FeatureMultiCurrency]},
		{FeatureKey: FeatureExportDocuments, Enabled: plan.Features[FeatureExportDocuments]},
		{FeatureKey: FeatureSEZDocuments, Enabled: plan.Features[FeatureSEZDocuments]},
		{FeatureKey: FeatureDeemedExportDocuments, Enabled: plan.Features[FeatureDeemedExportDocuments]},
		{FeatureKey: FeatureMultiUser, Enabled: plan.Features[FeatureMultiUser], LimitValue: enabledLimit(plan.Features[FeatureMultiUser], users)},
		{FeatureKey: FeatureCustomRoles, Enabled: plan.Features[FeatureCustomRoles]},
		{FeatureKey: FeatureMultiBusiness, Enabled: plan.Features[FeatureMultiBusiness]},
		{FeatureKey: FeatureBranches, Enabled: plan.Features[FeatureBranches]},
		{FeatureKey: FeaturePrioritySupport, Enabled: plan.Features[FeaturePrioritySupport]},
		{FeatureKey: FeatureDriveStorageMB, Enabled: plan.Features[FeatureDriveStorageMB], LimitValue: enabledLimit(plan.Features[FeatureDriveStorageMB], storageMB)},
		{FeatureKey: FeatureWhatsAppNotifications, Enabled: plan.Features[FeatureWhatsAppNotifications]},
	}
}

func enabledLimit(enabled bool, limit int64) *int64 {
	if !enabled {
		return nil
	}
	return &limit
}

func featureEntitlementsNeedSync(entitlements []*models.FeatureEntitlement, required []entitlementSeed) bool {
	if len(entitlements) < len(required) {
		return true
	}

	byFeatureKey := make(map[string]*models.FeatureEntitlement, len(entitlements))
	for _, entitlement := range entitlements {
		if entitlement != nil {
			byFeatureKey[entitlement.FeatureKey] = entitlement
		}
	}

	for _, seed := range required {
		entitlement, ok := byFeatureKey[seed.FeatureKey]
		if !ok || entitlement.Enabled != seed.Enabled {
			return true
		}
		if (seed.LimitValue == nil) != (entitlement.LimitValue == nil) {
			return true
		}
		if seed.LimitValue != nil && *entitlement.LimitValue != *seed.LimitValue {
			return true
		}
	}

	return false
}

func normalizeLookupKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	var builder strings.Builder
	lastDash := false
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			builder.WriteRune(ch)
			lastDash = false
			continue
		}
		if ch == '-' && !lastDash {
			builder.WriteRune(ch)
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func randomToken(size int) string {
	if size <= 0 {
		size = 12
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return uuid.NewString()
	}
	return hex.EncodeToString(buf)
}

func firstAvailableMap(values ...map[string]interface{}) map[string]interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return map[string]interface{}{}
}

func floatPointerValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func int64Pointer(value int64) *int64 {
	v := value
	return &v
}

func currencyToMinorUnits(value float64) int64 {
	return int64(roundMoney(value * 100))
}

func roundMoney(value float64) float64 {
	return float64(int64((value+0.005)*100)) / 100
}

func (s *CommerceService) nextStoreOrderNumber() string {
	return fmt.Sprintf("WEB-%s-%s", time.Now().UTC().Format("20060102"), strings.ToUpper(randomToken(3)))
}
