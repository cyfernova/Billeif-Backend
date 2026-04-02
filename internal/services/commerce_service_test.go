package services

import (
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
