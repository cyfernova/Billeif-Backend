DROP INDEX IF EXISTS idx_bargaining_rounds_negotiation_round_unique;

DROP INDEX IF EXISTS idx_email_deliveries_invoice_created;
ALTER TABLE email_deliveries
    DROP CONSTRAINT IF EXISTS email_deliveries_lease_check,
    DROP CONSTRAINT IF EXISTS email_deliveries_attempts_check,
    DROP CONSTRAINT IF EXISTS email_deliveries_status_check,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS lease_owner,
    DROP COLUMN IF EXISTS attempts,
    DROP COLUMN IF EXISTS render_job_id,
    DROP COLUMN IF EXISTS invoice_id;

DROP INDEX IF EXISTS idx_document_render_jobs_invoice_created;
DROP INDEX IF EXISTS idx_document_render_jobs_final_invoice_version;
ALTER TABLE document_render_jobs
    DROP CONSTRAINT IF EXISTS document_render_jobs_lease_check,
    DROP CONSTRAINT IF EXISTS document_render_jobs_final_source_check,
    DROP CONSTRAINT IF EXISTS document_render_jobs_invoice_version_check,
    DROP CONSTRAINT IF EXISTS document_render_jobs_source_check,
    DROP CONSTRAINT IF EXISTS document_render_jobs_attempts_check,
    DROP CONSTRAINT IF EXISTS document_render_jobs_kind_check,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS lease_owner,
    DROP COLUMN IF EXISTS attempts,
    DROP COLUMN IF EXISTS object_key,
    DROP COLUMN IF EXISTS source_invoice_version,
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS invoice_id,
    ALTER COLUMN document_id SET NOT NULL;

DROP INDEX IF EXISTS idx_outbox_events_ready;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS api_idempotency_keys;
DROP TABLE IF EXISTS document_sequences;

DROP INDEX IF EXISTS idx_invoices_business_created_cursor;
DROP TRIGGER IF EXISTS trigger_prevent_issued_invoice_identity_mutation ON invoices;
DROP FUNCTION IF EXISTS prevent_issued_invoice_identity_mutation();

UPDATE invoices
SET
    status = CASE WHEN status = 'issued' THEN 'draft' ELSE status END,
    invoice_no = COALESCE(invoice_no, 'ROLLBACK-' || REPLACE(id::text, '-', ''));

ALTER TABLE invoices
    DROP CONSTRAINT IF EXISTS invoices_lifecycle_check,
    DROP CONSTRAINT IF EXISTS invoices_origin_customer_check,
    DROP CONSTRAINT IF EXISTS invoices_version_check,
    DROP CONSTRAINT IF EXISTS invoices_origin_check,
    DROP CONSTRAINT IF EXISTS invoices_status_check,
    ALTER COLUMN customer_id SET NOT NULL,
    ALTER COLUMN invoice_no SET NOT NULL,
    DROP COLUMN IF EXISTS buyer_snapshot,
    DROP COLUMN IF EXISTS seller_snapshot,
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS issued_at,
    ADD CONSTRAINT invoices_status_check
        CHECK (status IN ('draft', 'sent', 'paid', 'overdue', 'void', 'canceled'));

ALTER TABLE business_profiles
    DROP COLUMN IF EXISTS timezone;
