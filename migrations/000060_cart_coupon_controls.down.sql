ALTER TABLE storefront_coupon_redemptions
    DROP CONSTRAINT IF EXISTS storefront_coupon_redemptions_storefront_coupon_id_fkey,
    ADD CONSTRAINT storefront_coupon_redemptions_storefront_coupon_id_fkey
        FOREIGN KEY (storefront_coupon_id) REFERENCES storefront_coupons(id) ON DELETE CASCADE;

ALTER TABLE storefront_coupons
    DROP CONSTRAINT IF EXISTS ck_storefront_coupons_values,
    DROP CONSTRAINT IF EXISTS ck_storefront_coupons_version,
    DROP CONSTRAINT IF EXISTS ck_storefront_coupons_redemption_count,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS redemption_count;

ALTER TABLE security_pending_uploads
    DROP COLUMN IF EXISTS cleanup_object_key;

DROP INDEX IF EXISTS idx_cart_mandates_business_user;
ALTER TABLE cart_mandates
    DROP CONSTRAINT IF EXISTS ck_cart_mandates_amounts,
    DROP CONSTRAINT IF EXISTS ck_cart_mandates_version,
    DROP CONSTRAINT IF EXISTS fk_cart_mandates_business,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS tax_amount,
    DROP COLUMN IF EXISTS subtotal_amount,
    DROP COLUMN IF EXISTS business_id;

ALTER TABLE cart_mandates
    ADD CONSTRAINT cart_mandates_total_amount_check CHECK (total_amount >= 0);
