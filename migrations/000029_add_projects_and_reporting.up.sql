CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(150) NOT NULL,
    code VARCHAR(80) NOT NULL,
    description TEXT,
    color VARCHAR(32),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_business_code
    ON projects (business_id, code)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_business_name
    ON projects (business_id, name, deleted_at);

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

ALTER TABLE invoices
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

ALTER TABLE journals
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

ALTER TABLE ledger_entries
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

ALTER TABLE stock_moves
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_documents_project_id ON documents (project_id, issue_date DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_invoices_project_id ON invoices (project_id, invoice_date DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_payments_project_id ON payments (project_id, payment_date DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_journals_project_id ON journals (project_id, posting_date DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_ledger_entries_project_id ON ledger_entries (project_id, entry_date DESC);
CREATE INDEX IF NOT EXISTS idx_stock_moves_project_id ON stock_moves (project_id, recorded_at DESC) WHERE deleted_at IS NULL;

WITH candidate_projects AS (
    SELECT DISTINCT d.business_id,
           (d.report_tags->>'project_id')::UUID AS project_id
    FROM documents d
    WHERE d.deleted_at IS NULL
      AND COALESCE(d.report_tags->>'project_id', '') ~* '^[0-9a-f-]{36}$'
    UNION
    SELECT DISTINCT i.business_id,
           (i.tax_profile->'report_tags'->>'project_id')::UUID AS project_id
    FROM invoices i
    WHERE i.deleted_at IS NULL
      AND COALESCE(i.tax_profile->'report_tags'->>'project_id', '') ~* '^[0-9a-f-]{36}$'
)
INSERT INTO projects (id, business_id, name, code, created_at, updated_at)
SELECT cp.project_id,
       cp.business_id,
       'Imported Project ' || SUBSTRING(cp.project_id::TEXT, 1, 8),
       'PRJ-' || UPPER(SUBSTRING(REPLACE(cp.project_id::TEXT, '-', ''), 1, 8)),
       NOW(),
       NOW()
FROM candidate_projects cp
LEFT JOIN projects p ON p.id = cp.project_id
WHERE cp.project_id IS NOT NULL
  AND p.id IS NULL;

UPDATE documents
SET project_id = (report_tags->>'project_id')::UUID
WHERE project_id IS NULL
  AND deleted_at IS NULL
  AND COALESCE(report_tags->>'project_id', '') ~* '^[0-9a-f-]{36}$';

UPDATE invoices
SET project_id = (tax_profile->'report_tags'->>'project_id')::UUID
WHERE project_id IS NULL
  AND deleted_at IS NULL
  AND COALESCE(tax_profile->'report_tags'->>'project_id', '') ~* '^[0-9a-f-]{36}$';

INSERT INTO documents (
    id, business_id, document_type, party_type, party_id, status, draft_state,
    tax_mode, gst_treatment, place_of_supply, party_gstin, party_pan, party_state_code,
    supply_type, export_type, bill_of_supply, serial_number, issue_date, due_date,
    currency, exchange_rate, locale, source_linkage, profit_snapshot_enabled, pdf_url,
    pdf_filename, notes, subtotal, tax_total, total, paid_amount, balance_due,
    extra_fields, report_tags, withholding_total, tds_total, tcs_total, project_id,
    created_at, updated_at
)
SELECT i.id,
       i.business_id,
       CASE
           WHEN COALESCE((i.tax_profile->>'bill_of_supply')::BOOLEAN, FALSE) THEN 'bill_of_supply'
           ELSE 'sales_invoice'
       END,
       'customer',
       i.customer_id,
       CASE i.status
           WHEN 'draft' THEN 'draft'
           WHEN 'canceled' THEN 'cancelled'
           ELSE 'sent'
       END,
       CASE WHEN i.status = 'draft' THEN 'draft' ELSE 'final' END,
       CASE WHEN i.tax > 0 THEN 'gst' ELSE 'non_gst' END,
       COALESCE(NULLIF(i.tax_profile->>'gst_treatment', ''), 'regular'),
       COALESCE(NULLIF(i.tax_profile->>'place_of_supply', ''), ''),
       COALESCE(NULLIF(i.tax_profile->>'counterparty_gstin', ''), ''),
       COALESCE(NULLIF(i.tax_profile->>'counterparty_pan', ''), ''),
       COALESCE(NULLIF(i.tax_profile->>'counterparty_state_code', ''), ''),
       COALESCE(NULLIF(i.tax_profile->>'supply_type', ''), ''),
       COALESCE(NULLIF(i.tax_profile->>'export_type', ''), ''),
       COALESCE((i.tax_profile->>'bill_of_supply')::BOOLEAN, FALSE),
       i.invoice_no,
       i.invoice_date,
       i.due_date,
       i.currency,
       1,
       'en-IN',
       COALESCE(i.tax_profile->'source_linkage', '{}'::JSONB),
       TRUE,
       i.pdf_url,
       i.pdf_filename,
       i.notes,
       i.subtotal,
       i.tax,
       i.total,
       i.paid_amount,
       i.balance_due,
       '{}'::JSONB,
       COALESCE(i.tax_profile->'report_tags', '{}'::JSONB),
       0,
       0,
       0,
       COALESCE(i.project_id,
           CASE
               WHEN COALESCE(i.tax_profile->'report_tags'->>'project_id', '') ~* '^[0-9a-f-]{36}$'
                   THEN (i.tax_profile->'report_tags'->>'project_id')::UUID
               ELSE NULL
           END
       ),
       i.created_at,
       i.updated_at
FROM invoices i
LEFT JOIN documents d ON d.id = i.id
WHERE i.deleted_at IS NULL
  AND d.id IS NULL;

INSERT INTO document_lines (
    id, document_id, product_id, variant_id, description, hsn_sac_code, uqc_code,
    unit, warehouse_id, quantity, remaining_quantity, unit_price, discount_amount,
    tax_rate, tax_amount, line_subtotal, line_total, cost_snapshot, margin_snapshot,
    batch_allocations, serial_ids, stock_effect, report_tags, created_at, updated_at
)
SELECT ii.id,
       ii.invoice_id,
       ii.product_id,
       ii.variant_id,
       ii.description,
       COALESCE(p.hsn_sac_code, ''),
       COALESCE(NULLIF(p.uqc_code, ''), 'OTH'),
       COALESCE(NULLIF(p.unit, ''), 'PCS'),
       ii.warehouse_id,
       ii.quantity,
       ii.quantity,
       ii.unit_price,
       ii.discount,
       ii.tax_rate,
       GREATEST(ii.total - ((ii.quantity * ii.unit_price) - ii.discount), 0),
       ((ii.quantity * ii.unit_price) - ii.discount),
       ii.total,
       COALESCE(p.cost_price, 0),
       ((ii.quantity * ii.unit_price) - ii.discount) - (COALESCE(p.cost_price, 0) * ii.quantity),
       COALESCE(ii.batch_allocations, '[]'::JSONB),
       COALESCE(ii.serial_ids, '[]'::JSONB),
       'out',
       '{}'::JSONB,
       ii.created_at,
       ii.updated_at
FROM invoice_items ii
LEFT JOIN document_lines dl ON dl.id = ii.id
LEFT JOIN products p ON p.id = ii.product_id
JOIN documents d ON d.id = ii.invoice_id
WHERE dl.id IS NULL;

UPDATE payments p
SET project_id = COALESCE(
    p.project_id,
    i.project_id,
    d.project_id
)
FROM invoices i
LEFT JOIN documents d ON d.id = i.id
WHERE p.invoice_id = i.id
  AND p.deleted_at IS NULL;

UPDATE journals j
SET project_id = d.project_id
FROM documents d
WHERE j.project_id IS NULL
  AND j.source_type = 'document'
  AND j.source_id = d.id
  AND j.deleted_at IS NULL;

UPDATE ledger_entries le
SET project_id = COALESCE(le.project_id, p.project_id, d.project_id)
FROM payments p
LEFT JOIN documents d ON d.id = p.invoice_id
WHERE le.payment_id = p.id
  AND le.project_id IS NULL;

UPDATE ledger_entries le
SET project_id = COALESCE(le.project_id, d.project_id)
FROM documents d
WHERE le.project_id IS NULL
  AND le.invoice_id = d.id;

UPDATE stock_moves sm
SET project_id = d.project_id
FROM documents d
WHERE sm.project_id IS NULL
  AND sm.document_id = d.id
  AND sm.deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS report_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    report_key VARCHAR(80) NOT NULL,
    run_kind VARCHAR(30) NOT NULL DEFAULT 'export',
    filters JSONB NOT NULL DEFAULT '{}',
    visible_columns JSONB NOT NULL DEFAULT '[]',
    export_format VARCHAR(20) NOT NULL DEFAULT 'json',
    status VARCHAR(20) NOT NULL DEFAULT 'completed',
    payload JSONB NOT NULL DEFAULT '{}',
    summary JSONB NOT NULL DEFAULT '{}',
    generated_by VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_report_runs_business_key
    ON report_runs (business_id, report_key, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS report_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    report_key VARCHAR(80) NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_report_preferences_unique
    ON report_preferences (business_id, user_id, report_key)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS report_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    report_key VARCHAR(80) NOT NULL,
    title VARCHAR(160),
    share_mode VARCHAR(20) NOT NULL DEFAULT 'snapshot',
    report_run_id UUID REFERENCES report_runs(id) ON DELETE SET NULL,
    filters JSONB NOT NULL DEFAULT '{}',
    visible_columns JSONB NOT NULL DEFAULT '[]',
    token_hash VARCHAR(64) NOT NULL,
    passcode_hash TEXT,
    requires_passcode BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMP,
    revoked_at TIMESTAMP,
    last_accessed_at TIMESTAMP,
    access_count BIGINT NOT NULL DEFAULT 0,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_report_shares_token_hash
    ON report_shares (token_hash)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_report_shares_business_created
    ON report_shares (business_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS report_share_access_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_share_id UUID NOT NULL REFERENCES report_shares(id) ON DELETE CASCADE,
    accessed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    successful BOOLEAN NOT NULL DEFAULT FALSE,
    failure_reason VARCHAR(120),
    ip_address VARCHAR(64),
    user_agent VARCHAR(500),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_report_share_access_logs_share_time
    ON report_share_access_logs (report_share_id, accessed_at DESC)
    WHERE deleted_at IS NULL;
