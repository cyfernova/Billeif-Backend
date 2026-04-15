ALTER TABLE invoice_items
    DROP COLUMN IF EXISTS hsn_sac_code,
    DROP COLUMN IF EXISTS unit;
