package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"invoice-backend/internal/models"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStorefrontAdministrationRejectsForeignTenantAtServiceBoundary(t *testing.T) {
	database := newStorefrontTenantScopeTestDB(t)
	service := &CommerceService{db: database}
	ctx := context.Background()
	const (
		ownerBusiness   = "business-owner"
		foreignBusiness = "business-foreign"
		foreignStore    = "storefront-foreign"
		foreignCoupon   = "coupon-foreign"
	)
	require.NoError(t, database.Create(&models.Storefront{
		ID: foreignStore, BusinessID: foreignBusiness, Name: "Foreign", Slug: "foreign",
		Status: models.StorefrontStatusPublished, Currency: "INR",
	}).Error)
	require.NoError(t, database.Create(&models.StorefrontProduct{
		ID: "listing-foreign", StorefrontID: foreignStore, ProductID: "product-foreign", IsPublished: true,
	}).Error)
	require.NoError(t, database.Create(&models.StorefrontCoupon{
		ID: foreignCoupon, StorefrontID: foreignStore, Code: "FOREIGN", DiscountType: "fixed",
		DiscountValue: 10, IsActive: true,
	}).Error)
	require.NoError(t, database.Create(&models.FeatureEntitlement{
		ID: "entitlement-owner", BusinessID: ownerBusiness, FeatureKey: FeatureOnlineStore, Enabled: true,
	}).Error)

	_, err := service.ListStorefrontProducts(ctx, ownerBusiness, foreignStore)
	require.Error(t, err)
	_, err = service.ReplaceStorefrontProducts(ctx, ownerBusiness, foreignStore, nil)
	require.Error(t, err)
	_, err = service.ListStorefrontCoupons(ctx, ownerBusiness, foreignStore)
	require.Error(t, err)
	_, err = service.CreateStorefrontCoupon(ctx, ownerBusiness, foreignStore, UpsertStorefrontCouponInput{
		Code: "ATTACKER", DiscountType: "fixed", DiscountValue: 100,
	})
	require.Error(t, err)
	_, err = service.UpdateStorefrontCoupon(ctx, ownerBusiness, foreignStore, foreignCoupon, UpsertStorefrontCouponInput{
		Code: "MUTATED", DiscountType: "fixed", DiscountValue: 100,
	})
	require.Error(t, err)

	var coupons []models.StorefrontCoupon
	require.NoError(t, database.Where("storefront_id = ?", foreignStore).Find(&coupons).Error)
	require.Len(t, coupons, 1)
	require.Equal(t, "FOREIGN", coupons[0].Code)
}

func newStorefrontTenantScopeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE storefronts (
			id TEXT PRIMARY KEY, business_id TEXT NOT NULL, name TEXT NOT NULL, slug TEXT NOT NULL,
			status TEXT NOT NULL, currency TEXT NOT NULL, allow_cod NUMERIC DEFAULT 1,
			allow_online_payment NUMERIC DEFAULT 0, auto_invoice_on_paid NUMERIC DEFAULT 1,
			minimum_order_value NUMERIC DEFAULT 0, settings TEXT DEFAULT '{}', blocked_users TEXT DEFAULT '[]',
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE storefront_products (
			id TEXT PRIMARY KEY, storefront_id TEXT NOT NULL, product_id TEXT NOT NULL, category_id TEXT,
			is_published NUMERIC DEFAULT 0, display_price NUMERIC DEFAULT 0, compare_at_price NUMERIC DEFAULT 0,
			sort_order INTEGER DEFAULT 0, badge TEXT, seo TEXT DEFAULT '{}', metadata TEXT DEFAULT '{}',
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE storefront_coupons (
			id TEXT PRIMARY KEY, storefront_id TEXT NOT NULL, code TEXT NOT NULL, discount_type TEXT NOT NULL,
			discount_value NUMERIC DEFAULT 0, minimum_order_value NUMERIC DEFAULT 0,
			max_discount_amount NUMERIC DEFAULT 0, usage_limit INTEGER DEFAULT 0,
			usage_limit_per_customer INTEGER DEFAULT 0, starts_at DATETIME, ends_at DATETIME,
			is_active NUMERIC DEFAULT 1, metadata TEXT DEFAULT '{}', created_at DATETIME,
			updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE feature_entitlements (
			id TEXT PRIMARY KEY, business_id TEXT NOT NULL, feature_key TEXT NOT NULL, enabled NUMERIC DEFAULT 0,
			limit_value INTEGER, metadata TEXT DEFAULT '{}', created_at DATETIME, updated_at DATETIME,
			deleted_at DATETIME
		)`,
	} {
		require.NoError(t, database.Exec(statement).Error)
	}
	return database
}
