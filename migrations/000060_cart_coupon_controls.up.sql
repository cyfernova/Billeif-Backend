ALTER TABLE cart_mandates
    ADD COLUMN business_id UUID,
    ADD COLUMN subtotal_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN tax_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE cart_mandates AS cart
SET business_id = agent.business_id,
    subtotal_amount = cart.total_amount
FROM agents AS agent
WHERE agent.id = cart.agent_id;

ALTER TABLE cart_mandates
    ALTER COLUMN business_id SET NOT NULL,
    ADD CONSTRAINT fk_cart_mandates_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE,
    ADD CONSTRAINT ck_cart_mandates_version CHECK (version >= 1),
    ADD CONSTRAINT ck_cart_mandates_amounts CHECK (subtotal_amount >= 0 AND tax_amount >= 0 AND total_amount >= 0);

ALTER TABLE cart_mandates DROP CONSTRAINT IF EXISTS cart_mandates_total_amount_check;

CREATE INDEX idx_cart_mandates_business_user
    ON cart_mandates (business_id, user_id, created_at DESC);

ALTER TABLE security_pending_uploads
    ADD COLUMN cleanup_object_key TEXT NOT NULL DEFAULT '';

ALTER TABLE storefront_coupons
    ADD COLUMN redemption_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT ck_storefront_coupons_redemption_count CHECK (redemption_count >= 0),
    ADD CONSTRAINT ck_storefront_coupons_version CHECK (version >= 1),
    ADD CONSTRAINT ck_storefront_coupons_values CHECK (
        discount_value > 0
        AND minimum_order_value >= 0
        AND max_discount_amount >= 0
        AND usage_limit >= 0
        AND usage_limit_per_customer >= 0
        AND (starts_at IS NULL OR ends_at IS NULL OR starts_at < ends_at)
    ) NOT VALID;

UPDATE storefront_coupons AS coupon
SET redemption_count = redemption.total
FROM (
    SELECT storefront_coupon_id, COUNT(*)::BIGINT AS total
    FROM storefront_coupon_redemptions
    WHERE deleted_at IS NULL
    GROUP BY storefront_coupon_id
) AS redemption
WHERE redemption.storefront_coupon_id = coupon.id;

ALTER TABLE storefront_coupon_redemptions
    DROP CONSTRAINT IF EXISTS storefront_coupon_redemptions_storefront_coupon_id_fkey,
    ADD CONSTRAINT storefront_coupon_redemptions_storefront_coupon_id_fkey
        FOREIGN KEY (storefront_coupon_id) REFERENCES storefront_coupons(id) ON DELETE RESTRICT;
