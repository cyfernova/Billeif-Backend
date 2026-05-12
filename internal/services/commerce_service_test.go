package services

import (
	"encoding/json"
	"strings"
	"testing"

	"invoice-backend/internal/models"
)

func TestDefaultEntitlementSeedsForSubscription(t *testing.T) {
	t.Run("pro plan enables storefront but not custom roles", func(t *testing.T) {
		seeds := defaultEntitlementSeedsForSubscription(&models.Subscription{
			Plan:         "starter",
			PlanCode:     "pro",
			MaxUsers:     2,
			MaxStorageMB: 256,
		})

		if !seedEnabled(seeds, FeatureOnlineStore) {
			t.Fatalf("expected online store to be enabled for pro")
		}
		if seedEnabled(seeds, FeatureCustomRoles) {
			t.Fatalf("expected custom roles to be disabled for pro")
		}
		if limit := seedLimit(seeds, FeatureDriveStorageMB); limit == nil || *limit < 512 {
			t.Fatalf("expected drive storage limit to be at least 512MB for pro")
		}
	})

	t.Run("biz plan grants unlimited multi user access", func(t *testing.T) {
		seeds := defaultEntitlementSeedsForSubscription(&models.Subscription{
			Plan:         "enterprise",
			PlanCode:     "biz",
			MaxUsers:     20,
			MaxStorageMB: 2048,
		})

		if !seedEnabled(seeds, FeatureMultiBusiness) {
			t.Fatalf("expected multi business to be enabled for biz")
		}
		limit := seedLimit(seeds, FeatureMultiUser)
		if limit == nil || *limit != -1 {
			t.Fatalf("expected biz multi user limit to be unlimited, got %v", limit)
		}
	})
}

func TestPermissionOverrideValue(t *testing.T) {
	override, ok := permissionOverrideValue(`{"storefront.manage":true}`, PermissionStorefrontManage)
	if !ok || !override {
		t.Fatalf("expected direct permission override to be applied")
	}

	override, ok = permissionOverrideValue(`{"*":false}`, PermissionOrdersManage)
	if !ok || override {
		t.Fatalf("expected wildcard permission override to be applied")
	}

	if _, ok := permissionOverrideValue(`{"storefront.manage":"yes"}`, PermissionStorefrontManage); ok {
		t.Fatalf("expected non-bool override to be ignored")
	}
}

func TestPublicStorefrontCatalogResponseOmitsInternalFields(t *testing.T) {
	storefront := &models.Storefront{
		ID:           "store-1",
		BusinessID:   "biz-secret",
		Name:         "Public Store",
		Slug:         "public-store",
		Status:       models.StorefrontStatusPublished,
		Currency:     "INR",
		Settings:     `{"theme":"hidden"}`,
		BlockedUsers: `["blocked@example.com"]`,
	}
	category := &models.StorefrontCategory{
		ID:           "cat-1",
		StorefrontID: "store-1",
		Name:         "Tea",
		Slug:         "tea",
		SEO:          `{"secret":"hidden"}`,
	}
	item := &StorefrontCatalogItem{
		StorefrontProduct: &models.StorefrontProduct{
			ID:             "sf-prod-1",
			StorefrontID:   "store-1",
			ProductID:      "prod-1",
			DisplayPrice:   120,
			CompareAtPrice: 150,
			SEO:            `{"title":"Tea"}`,
			Metadata:       `{"label":"fresh"}`,
		},
		Product: &models.Product{
			ID:                "prod-1",
			BusinessID:        "biz-secret",
			Name:              "Assam Tea",
			SKU:               "TEA-1",
			Price:             130,
			CostPrice:         80,
			StockLevel:        999,
			LowStockThreshold: 25,
			ImageKey:          "private/key",
			ImageURL:          "https://cdn.example.com/tea.png",
			Currency:          "INR",
			Unit:              "PCS",
		},
	}

	resp := publicStorefrontCatalogResponse(storefront, []*models.StorefrontCategory{category}, []*StorefrontCatalogItem{item})

	if resp.Storefront.Name != "Public Store" || resp.Storefront.Slug != "public-store" {
		t.Fatalf("expected public storefront fields to be preserved")
	}
	if len(resp.Categories) != 1 || len(resp.Products) != 1 {
		t.Fatalf("expected one category and product in public response")
	}
	product := resp.Products[0]
	if product.ProductID != "prod-1" || product.Name != "Assam Tea" || product.ImageURL == "" {
		t.Fatalf("expected public product fields to be preserved: %+v", product)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal public catalog response: %v", err)
	}
	for _, forbidden := range []string{"business_id", "cost_price", "stock_level", "low_stock_threshold", "image_key", "blocked_users", "settings", "storefront_id"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("public catalog leaked %q in %s", forbidden, string(body))
		}
	}
}

func TestEnsurePublishedStorefrontRejectsDraft(t *testing.T) {
	err := ensurePublishedStorefront(&models.Storefront{Status: models.StorefrontStatusDraft})
	if err == nil {
		t.Fatal("expected draft storefront to be rejected")
	}
}

func TestCheckoutInputMatchesOrderRejectsDifferentReplayBody(t *testing.T) {
	input := StorefrontCheckoutInput{
		PaymentMethod: "cod",
		Customer:      CheckoutCustomerInput{Name: "Alice", Email: "alice@example.com"},
		Items:         []CheckoutItemInput{{ProductID: "11111111-1111-1111-1111-111111111111", Quantity: 1}},
	}
	order := &models.StoreOrder{
		Snapshot: mustMarshalMap(map[string]interface{}{
			"checkout_fingerprint": checkoutInputFingerprint(input),
		}),
	}

	if !checkoutInputMatchesOrder(order, input) {
		t.Fatal("expected identical checkout input to match existing idempotent order")
	}

	changed := input
	changed.Customer.Email = "mallory@example.com"
	if checkoutInputMatchesOrder(order, changed) {
		t.Fatal("expected changed checkout input to be rejected for reused idempotency key")
	}
}

func TestNormalizePublicPaymentMethodRejectsUnsupportedMethods(t *testing.T) {
	if got := normalizePublicPaymentMethod(""); got != "cod" {
		t.Fatalf("expected blank payment method to default to cod, got %q", got)
	}
	if got := normalizePublicPaymentMethod("online"); got != "online" {
		t.Fatalf("expected online payment method, got %q", got)
	}
	if got := normalizePublicPaymentMethod("invoice-me-later"); got != "" {
		t.Fatalf("expected unsupported payment method to be rejected, got %q", got)
	}
}

func TestValidateDriveAssetUploadRejectsActiveContentAndOversize(t *testing.T) {
	if _, err := validateDriveAssetUpload(CreateDriveAssetInput{
		ContentType: "text/html",
		SizeBytes:   1024,
	}); err == nil {
		t.Fatal("expected active content type to be rejected")
	}

	if _, err := validateDriveAssetUpload(CreateDriveAssetInput{
		ContentType: "image/png",
		SizeBytes:   maxDriveAssetUploadBytes + 1,
	}); err == nil {
		t.Fatal("expected oversized upload to be rejected")
	}

	contentType, err := validateDriveAssetUpload(CreateDriveAssetInput{
		ContentType: " IMAGE/PNG ",
		SizeBytes:   1024,
	})
	if err != nil {
		t.Fatalf("expected image/png upload to be accepted: %v", err)
	}
	if contentType != "image/png" {
		t.Fatalf("expected content type to normalize, got %q", contentType)
	}
}

func seedEnabled(seeds []entitlementSeed, featureKey string) bool {
	for _, seed := range seeds {
		if seed.FeatureKey == featureKey {
			return seed.Enabled
		}
	}
	return false
}

func seedLimit(seeds []entitlementSeed, featureKey string) *int64 {
	for _, seed := range seeds {
		if seed.FeatureKey == featureKey {
			return seed.LimitValue
		}
	}
	return nil
}
