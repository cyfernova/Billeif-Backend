package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDefaultEntitlementSeedsForSubscriptionUsesCatalogLimits(t *testing.T) {
	seeds := defaultEntitlementSeedsForSubscription(&models.Subscription{
		Plan:     "starter",
		PlanCode: "pro",
		Status:   "active",
	})

	for _, featureKey := range []string{
		FeatureOnlineStore,
		FeatureMultiCurrency,
		FeatureExportDocuments,
		FeatureSEZDocuments,
		FeatureDeemedExportDocuments,
		FeatureMultiUser,
		FeatureCustomRoles,
		FeatureMultiBusiness,
		FeatureBranches,
		FeaturePrioritySupport,
		FeatureDriveStorageMB,
		FeatureWhatsAppNotifications,
	} {
		if !seedEnabled(seeds, featureKey) {
			t.Fatalf("expected %s to be enabled", featureKey)
		}
	}

	require.Equal(t, int64(3), *seedLimit(seeds, FeatureMultiUser))
	require.Nil(t, seedLimit(seeds, FeatureBranches))
	require.Equal(t, int64(512), *seedLimit(seeds, FeatureDriveStorageMB))
}

func TestFeatureEntitlementsNeedSyncDetectsDisabledRows(t *testing.T) {
	seeds := defaultEntitlementSeedsForSubscription(&models.Subscription{Plan: "starter", PlanCode: "pro", Status: "active"})
	entitlements := make([]*models.FeatureEntitlement, 0, len(seeds))
	for _, seed := range seeds {
		entitlements = append(entitlements, &models.FeatureEntitlement{
			FeatureKey: seed.FeatureKey,
			Enabled:    seed.Enabled,
			LimitValue: seed.LimitValue,
		})
	}

	if featureEntitlementsNeedSync(entitlements, seeds) {
		t.Fatal("expected complete enabled entitlement set to be current")
	}

	entitlements[0].Enabled = false
	if !featureEntitlementsNeedSync(entitlements, seeds) {
		t.Fatal("expected disabled entitlement to require sync")
	}

	entitlements[0].Enabled = true
	for _, entitlement := range entitlements {
		if entitlement.FeatureKey == FeatureMultiUser {
			entitlement.LimitValue = int64Pointer(1)
			break
		}
	}
	if !featureEntitlementsNeedSync(entitlements, seeds) {
		t.Fatal("expected stale entitlement limit to require sync")
	}

	freeSeeds := defaultEntitlementSeedsForSubscription(&models.Subscription{Plan: "free", PlanCode: "free", Status: "active"})
	freeEntitlements := make([]*models.FeatureEntitlement, 0, len(freeSeeds))
	for _, seed := range freeSeeds {
		freeEntitlements = append(freeEntitlements, &models.FeatureEntitlement{
			FeatureKey: seed.FeatureKey,
			Enabled:    seed.Enabled,
			LimitValue: seed.LimitValue,
		})
	}
	freeEntitlements[0].LimitValue = int64Pointer(1)
	if !featureEntitlementsNeedSync(freeEntitlements, freeSeeds) {
		t.Fatal("expected stale limit on disabled entitlement to require sync")
	}
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
		SizeBytes:   MaxDriveAssetUploadBytes + 1,
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

func TestCreateDriveUploadSignsDeclaredSizeAndAccountsForQuotaUsage(t *testing.T) {
	db := newDriveUploadTestDB(t)
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example.com")
		options.UsePathStyle = true
	})
	service := &CommerceService{
		cfg: &config.Config{S3: config.S3Config{BucketDrive: "private-drive"}},
		db:  db,
		s3:  &S3Service{client: client},
	}

	session, err := service.CreateDriveUpload(context.Background(), "business-123", "user-456", CreateDriveAssetInput{
		Name:        "invoice.pdf",
		ContentType: "application/pdf",
		SizeBytes:   8192,
	})
	require.NoError(t, err)
	require.Equal(t, "8192", session.RequiredHeaders["Content-Length"])
	require.Equal(t, "application/pdf", session.RequiredHeaders["Content-Type"])
	parsed, err := url.Parse(session.UploadURL)
	require.NoError(t, err)
	require.Contains(t, parsed.EscapedPath(), "/private-drive/business-123/")
	require.Equal(t, "business-123", session.Asset.BusinessID)
	require.NotNil(t, session.Asset.UploadedBy)
	require.Equal(t, "user-456", *session.Asset.UploadedBy)

	var usageBytes int64
	require.NoError(t, db.Model(&models.DriveAsset{}).
		Select("COALESCE(SUM(size_bytes), 0)").
		Where("business_id = ? AND deleted_at IS NULL", "business-123").
		Scan(&usageBytes).Error)
	require.Equal(t, int64(8192), usageBytes)
}

func newDriveUploadTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE feature_entitlements (
		id TEXT PRIMARY KEY,
		business_id TEXT NOT NULL,
		feature_key TEXT NOT NULL,
		enabled NUMERIC NOT NULL,
		limit_value INTEGER,
		metadata TEXT DEFAULT '{}',
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE drive_assets (
		id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
		business_id TEXT NOT NULL,
		uploaded_by TEXT,
		name TEXT NOT NULL,
		folder_path TEXT,
		bucket TEXT NOT NULL,
		object_key TEXT NOT NULL,
		content_type TEXT,
		size_bytes INTEGER NOT NULL,
		category TEXT,
		metadata TEXT DEFAULT '{}',
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error)

	for index, seed := range defaultEntitlementSeedsForSubscription(&models.Subscription{Plan: "starter", PlanCode: "pro", Status: "active"}) {
		require.NoError(t, db.Exec(
			"INSERT INTO feature_entitlements (id, business_id, feature_key, enabled, limit_value, metadata, created_at, updated_at) VALUES (?, ?, ?, ?, ?, '{}', ?, ?)",
			fmt.Sprintf("entitlement-%d", index), "business-123", seed.FeatureKey, seed.Enabled, seed.LimitValue, time.Now().UTC(), time.Now().UTC(),
		).Error)
	}
	return db
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
