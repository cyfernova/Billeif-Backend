package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCouponControlsPreserveRedemptionLimitsAndHistory(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:coupon-controls?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE storefronts (id TEXT PRIMARY KEY, business_id TEXT NOT NULL, name TEXT NOT NULL, slug TEXT NOT NULL, status TEXT DEFAULT 'draft', currency TEXT DEFAULT 'INR', allow_cod NUMERIC DEFAULT 1, allow_online_payment NUMERIC DEFAULT 0, auto_invoice_on_paid NUMERIC DEFAULT 1, minimum_order_value NUMERIC DEFAULT 0, settings TEXT DEFAULT '{}', blocked_users TEXT DEFAULT '[]', created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE storefront_coupons (id TEXT PRIMARY KEY, storefront_id TEXT NOT NULL, code TEXT NOT NULL, discount_type TEXT NOT NULL, discount_value NUMERIC DEFAULT 0, minimum_order_value NUMERIC DEFAULT 0, max_discount_amount NUMERIC DEFAULT 0, usage_limit INTEGER DEFAULT 0, usage_limit_per_customer INTEGER DEFAULT 0, redemption_count INTEGER DEFAULT 0, version INTEGER DEFAULT 1, starts_at DATETIME, ends_at DATETIME, is_active NUMERIC DEFAULT 1, metadata TEXT DEFAULT '{}', created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE storefront_coupon_redemptions (id TEXT PRIMARY KEY, storefront_coupon_id TEXT NOT NULL, store_order_id TEXT, customer_id TEXT, customer_email TEXT, discount_amount NUMERIC DEFAULT 0, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	storefront := &models.Storefront{ID: "store-1", BusinessID: "business-1", Name: "Store", Slug: "store", Currency: "INR"}
	if err := database.Create(storefront).Error; err != nil {
		t.Fatal(err)
	}
	coupon := &models.StorefrontCoupon{
		ID: "coupon-1", StorefrontID: storefront.ID, Code: "SAFE10",
		DiscountType: models.StoreCouponDiscountTypePercent, DiscountValue: 10,
		UsageLimit: 5, UsageLimitPerCustomer: 2, RedemptionCount: 2, Version: 3, IsActive: true,
	}
	if err := database.Create(coupon).Error; err != nil {
		t.Fatal(err)
	}
	orderID := "order-1"
	customerID := "customer-1"
	if err := database.Create(&models.StorefrontCouponRedemption{
		ID: "redemption-1", StorefrontCouponID: coupon.ID, StoreOrderID: &orderID,
		CustomerID: &customerID, CustomerEmail: "buyer@example.com", DiscountAmount: 10,
	}).Error; err != nil {
		t.Fatal(err)
	}
	service := &CommerceService{db: database}
	active := true
	version := int64(3)
	_, err = service.UpdateStorefrontCoupon(context.Background(), storefront.BusinessID, storefront.ID, coupon.ID, UpsertStorefrontCouponInput{
		Code: coupon.Code, DiscountType: coupon.DiscountType, DiscountValue: coupon.DiscountValue,
		UsageLimit: 1, UsageLimitPerCustomer: 2, IsActive: &active, Version: &version,
	})
	if err == nil {
		t.Fatal("expected update below redeemed total to fail")
	}
	stale := int64(2)
	_, err = service.UpdateStorefrontCoupon(context.Background(), storefront.BusinessID, storefront.ID, coupon.ID, UpsertStorefrontCouponInput{
		Code: coupon.Code, DiscountType: coupon.DiscountType, DiscountValue: coupon.DiscountValue,
		UsageLimit: 5, UsageLimitPerCustomer: 2, IsActive: &active, Version: &stale,
	})
	if !errors.Is(err, ErrCouponVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	if err := service.DeleteStorefrontCoupon(context.Background(), storefront.BusinessID, storefront.ID, coupon.ID); !errors.Is(err, ErrCouponRedeemed) {
		t.Fatalf("redeemed deletion error = %v", err)
	}
	if err := service.DeleteStorefrontCoupon(context.Background(), "foreign-business", storefront.ID, coupon.ID); err == nil {
		t.Fatal("foreign tenant deletion unexpectedly succeeded")
	}
}

func TestCouponControlsRejectInvalidActivationWindow(t *testing.T) {
	now := time.Now().UTC()
	active := true
	err := validateCouponInput(UpsertStorefrontCouponInput{
		Code: "DATE", DiscountType: models.StoreCouponDiscountTypeFixed, DiscountValue: 10,
		StartsAt: &now, EndsAt: &now, IsActive: &active,
	})
	if err == nil {
		t.Fatal("expected invalid activation window")
	}
}
