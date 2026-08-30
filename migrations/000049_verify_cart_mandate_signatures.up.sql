ALTER TABLE cart_mandates
    ADD COLUMN signature_public_key VARCHAR(500),
    ADD COLUMN merchant_signature_public_key VARCHAR(500);

CREATE UNIQUE INDEX ux_payment_mandates_cart_mandate_id
    ON payment_mandates (cart_mandate_id);

-- Existing mandates were created without verifiable canonical signatures.
-- Leave their key columns NULL so the application rejects them fail-closed.
