ALTER TABLE business_profiles
    ADD COLUMN timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Kolkata';

ALTER TABLE invoices
    DROP CONSTRAINT IF EXISTS invoices_status_check,
    ALTER COLUMN invoice_no DROP NOT NULL,
    ALTER COLUMN customer_id DROP NOT NULL,
    ADD COLUMN issued_at TIMESTAMPTZ,
    ADD COLUMN origin VARCHAR(24) NOT NULL DEFAULT 'conversion',
    ADD COLUMN seller_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN buyer_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE invoices AS invoice
SET seller_snapshot = jsonb_build_object(
        'name', business.name,
        'email', business.email,
        'phone', COALESCE(business.phone, ''),
        'address', COALESCE(business.address, ''),
        'city', COALESCE(business.city, ''),
        'state', COALESCE(business.state, ''),
        'country', COALESCE(business.country, ''),
        'postal_code', COALESCE(business.postal_code, ''),
        'tax_id', COALESCE(business.tax_id, ''),
        'gstin', COALESCE(business.gstin, '')
    )
FROM business_profiles AS business
WHERE business.id = invoice.business_id;

UPDATE invoices AS invoice
SET buyer_snapshot = jsonb_build_object(
        'name', customer.name,
        'email', COALESCE(customer.email, ''),
        'phone', COALESCE(customer.phone, ''),
        'address', COALESCE(customer.address, ''),
        'city', COALESCE(customer.city, ''),
        'state', COALESCE(customer.state, ''),
        'country', COALESCE(customer.country, ''),
        'postal_code', COALESCE(customer.postal_code, ''),
        'tax_id', COALESCE(customer.tax_id, ''),
        'gstin', COALESCE(customer.gstin, '')
    )
FROM customers AS customer
WHERE customer.id = invoice.customer_id;

UPDATE invoices
SET
    status = CASE WHEN status = 'draft' THEN 'issued' ELSE status END,
    issued_at = COALESCE(sent_at, invoice_date, created_at);

UPDATE invoices
SET status = 'partially_paid'
WHERE status = 'partial';

ALTER TABLE invoices
    ADD CONSTRAINT invoices_status_check
        CHECK (status IN ('draft', 'issued', 'sent', 'partially_paid', 'paid', 'overdue', 'void', 'canceled')),
    ADD CONSTRAINT invoices_origin_check
        CHECK (origin IN ('manual', 'pos', 'storefront', 'subscription', 'conversion')),
    ADD CONSTRAINT invoices_version_check
        CHECK (version >= 1),
    ADD CONSTRAINT invoices_origin_customer_check
        CHECK (
            customer_id IS NOT NULL
            OR (
                origin IN ('manual', 'pos')
                AND NULLIF(BTRIM(buyer_snapshot->>'name'), '') IS NOT NULL
            )
        ),
    ADD CONSTRAINT invoices_lifecycle_check
        CHECK (
            (
                status = 'draft'
                AND invoice_no IS NULL
                AND issued_at IS NULL
            )
            OR (
                status <> 'draft'
                AND invoice_no IS NOT NULL
                AND BTRIM(invoice_no) <> ''
                AND issued_at IS NOT NULL
                AND NULLIF(BTRIM(seller_snapshot->>'name'), '') IS NOT NULL
                AND NULLIF(BTRIM(buyer_snapshot->>'name'), '') IS NOT NULL
            )
        );

CREATE OR REPLACE FUNCTION prevent_issued_invoice_identity_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status <> 'draft' AND (
        NEW.invoice_no IS DISTINCT FROM OLD.invoice_no
        OR NEW.issued_at IS DISTINCT FROM OLD.issued_at
        OR NEW.origin IS DISTINCT FROM OLD.origin
        OR NEW.seller_snapshot IS DISTINCT FROM OLD.seller_snapshot
        OR NEW.buyer_snapshot IS DISTINCT FROM OLD.buyer_snapshot
    ) THEN
        RAISE EXCEPTION 'issued invoice legal identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_prevent_issued_invoice_identity_mutation
BEFORE UPDATE ON invoices
FOR EACH ROW
EXECUTE FUNCTION prevent_issued_invoice_identity_mutation();

CREATE INDEX idx_invoices_business_created_cursor
    ON invoices (business_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE document_sequences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_type VARCHAR(50) NOT NULL,
    financial_year VARCHAR(9) NOT NULL,
    series VARCHAR(32) NOT NULL,
    last_number INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT document_sequences_last_number_check
        CHECK (last_number BETWEEN 0 AND 999999),
    CONSTRAINT document_sequences_business_type_year_series_unique
        UNIQUE (business_id, document_type, financial_year, series)
);

CREATE TABLE api_idempotency_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    command VARCHAR(100) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'in_progress',
    result_type VARCHAR(100),
    result_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT api_idempotency_keys_status_check
        CHECK (status IN ('in_progress', 'completed')),
    CONSTRAINT api_idempotency_keys_request_hash_check
        CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT api_idempotency_keys_result_check
        CHECK (
            (status = 'in_progress' AND result_type IS NULL AND result_id IS NULL AND completed_at IS NULL)
            OR
            (status = 'completed' AND result_type IS NOT NULL AND result_id IS NOT NULL AND completed_at IS NOT NULL)
        ),
    CONSTRAINT api_idempotency_keys_business_command_key_unique
        UNIQUE (business_id, command, idempotency_key)
);

CREATE TABLE outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    aggregate_type VARCHAR(100) NOT NULL,
    aggregate_id UUID NOT NULL,
    event_type VARCHAR(160) NOT NULL,
    payload JSONB NOT NULL,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner VARCHAR(255),
    lease_expires_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT outbox_events_publish_attempts_check CHECK (publish_attempts >= 0),
    CONSTRAINT outbox_events_lease_check
        CHECK (
            (lease_owner IS NULL AND lease_expires_at IS NULL)
            OR
            (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        )
);

CREATE INDEX idx_outbox_events_ready
    ON outbox_events (available_at, created_at, id)
    WHERE published_at IS NULL;

ALTER TABLE document_render_jobs
    ALTER COLUMN document_id DROP NOT NULL,
    ADD COLUMN invoice_id UUID REFERENCES invoices(id) ON DELETE CASCADE,
    ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'preview',
    ADD COLUMN source_invoice_version INTEGER,
    ADD COLUMN object_key VARCHAR(1024),
    ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN lease_owner VARCHAR(255),
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD CONSTRAINT document_render_jobs_kind_check
        CHECK (kind IN ('preview', 'final')),
    ADD CONSTRAINT document_render_jobs_attempts_check
        CHECK (attempts >= 0),
    ADD CONSTRAINT document_render_jobs_source_check
        CHECK (document_id IS NOT NULL OR invoice_id IS NOT NULL),
    ADD CONSTRAINT document_render_jobs_invoice_version_check
        CHECK (
            (invoice_id IS NULL AND source_invoice_version IS NULL)
            OR
            (invoice_id IS NOT NULL AND source_invoice_version >= 1)
        ),
    ADD CONSTRAINT document_render_jobs_final_source_check
        CHECK (
            kind = 'preview'
            OR (invoice_id IS NOT NULL AND source_invoice_version IS NOT NULL)
        ),
    ADD CONSTRAINT document_render_jobs_lease_check
        CHECK (
            (lease_owner IS NULL AND lease_expires_at IS NULL)
            OR
            (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        );

CREATE UNIQUE INDEX idx_document_render_jobs_final_invoice_version
    ON document_render_jobs (invoice_id, source_invoice_version)
    WHERE kind = 'final';

CREATE INDEX idx_document_render_jobs_invoice_created
    ON document_render_jobs (invoice_id, created_at DESC)
    WHERE deleted_at IS NULL;

ALTER TABLE email_deliveries
    ADD COLUMN invoice_id UUID REFERENCES invoices(id) ON DELETE CASCADE,
    ADD COLUMN render_job_id UUID REFERENCES document_render_jobs(id) ON DELETE SET NULL,
    ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN lease_owner VARCHAR(255),
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD CONSTRAINT email_deliveries_status_check
        CHECK (status IN ('queued', 'processing', 'sent', 'delivered', 'failed')),
    ADD CONSTRAINT email_deliveries_attempts_check
        CHECK (attempts >= 0),
    ADD CONSTRAINT email_deliveries_lease_check
        CHECK (
            (lease_owner IS NULL AND lease_expires_at IS NULL)
            OR
            (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        );

CREATE INDEX idx_email_deliveries_invoice_created
    ON email_deliveries (invoice_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX idx_bargaining_rounds_negotiation_round_unique
    ON bargaining_rounds (negotiation_id, round_number);
