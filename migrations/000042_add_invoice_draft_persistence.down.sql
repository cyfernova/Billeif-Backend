DROP INDEX IF EXISTS idx_invoices_business_version;

ALTER TABLE invoices
    DROP COLUMN IF EXISTS einvoice_settings_json,
    DROP COLUMN IF EXISTS eway_details_json,
    DROP COLUMN IF EXISTS terms_json,
    DROP COLUMN IF EXISTS payment_display,
    DROP COLUMN IF EXISTS template_override,
    DROP COLUMN IF EXISTS document_json,
    DROP COLUMN IF EXISTS customer_snapshot,
    DROP COLUMN IF EXISTS version;
