DROP INDEX IF EXISTS ux_payment_mandates_cart_mandate_id;

ALTER TABLE cart_mandates
    DROP COLUMN IF EXISTS merchant_signature_public_key,
    DROP COLUMN IF EXISTS signature_public_key;
