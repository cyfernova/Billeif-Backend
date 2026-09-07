package migrationbundle

import "testing"

func TestCartCouponControlsMigrationDefinesScopeVersionAndSerializedUsage(t *testing.T) {
	body := migrationSQL(t, "000060_cart_coupon_controls.up.sql")
	requireSQLFragments(t, body,
		"ADD COLUMN business_id UUID",
		"REFERENCES business_profiles(id)",
		"ADD COLUMN version BIGINT NOT NULL DEFAULT 1",
		"CREATE INDEX idx_cart_mandates_business_user",
		"ADD COLUMN cleanup_object_key TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN redemption_count BIGINT NOT NULL DEFAULT 0",
		"ck_storefront_coupons_values",
		"ON DELETE RESTRICT",
	)
	down := migrationSQL(t, "000060_cart_coupon_controls.down.sql")
	requireSQLFragments(t, down,
		"DROP COLUMN IF EXISTS business_id",
		"DROP COLUMN IF EXISTS cleanup_object_key",
		"DROP COLUMN IF EXISTS redemption_count",
		"CHECK (total_amount >= 0)",
		"ON DELETE CASCADE",
	)
}
