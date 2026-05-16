ALTER TABLE invoices
    ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS customer_snapshot JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS document_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS template_override JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS payment_display JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS terms_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS eway_details_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS einvoice_settings_json JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_invoices_business_version
    ON invoices (business_id, id, version)
    WHERE deleted_at IS NULL;
