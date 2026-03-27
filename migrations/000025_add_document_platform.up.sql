ALTER TABLE business_profiles
    ADD COLUMN IF NOT EXISTS gstin VARCHAR(20),
    ADD COLUMN IF NOT EXISTS business_state_code VARCHAR(10),
    ADD COLUMN IF NOT EXISTS composition_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS default_gst_treatment VARCHAR(50) NOT NULL DEFAULT 'regular',
    ADD COLUMN IF NOT EXISTS export_lut_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS sez_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS numbering_rules JSONB NOT NULL DEFAULT '{}';

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS cost_price DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS valuation_method VARCHAR(30) NOT NULL DEFAULT 'last_purchase',
    ADD COLUMN IF NOT EXISTS hsn_sac_code VARCHAR(40),
    ADD COLUMN IF NOT EXISTS is_service BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_type VARCHAR(50) NOT NULL,
    party_type VARCHAR(20) NOT NULL,
    party_id UUID,
    status VARCHAR(50) NOT NULL DEFAULT 'draft',
    draft_state VARCHAR(50) NOT NULL DEFAULT 'draft',
    tax_mode VARCHAR(20) NOT NULL DEFAULT 'gst',
    gst_treatment VARCHAR(50) NOT NULL DEFAULT 'regular',
    place_of_supply VARCHAR(50),
    serial_number VARCHAR(80) NOT NULL,
    issue_date TIMESTAMP NOT NULL,
    due_date TIMESTAMP,
    dispatch_date TIMESTAMP,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    exchange_rate DECIMAL(18,6) NOT NULL DEFAULT 1,
    locale VARCHAR(20) NOT NULL DEFAULT 'en-IN',
    source_linkage JSONB NOT NULL DEFAULT '{}',
    render_profile_id UUID,
    shipment_id UUID,
    profit_snapshot_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    cancellation_reason TEXT,
    cancelled_at TIMESTAMP,
    pdf_url VARCHAR(500),
    pdf_filename VARCHAR(255),
    notes TEXT,
    terms TEXT,
    declaration TEXT,
    direction VARCHAR(20),
    subtotal DECIMAL(15,2) NOT NULL DEFAULT 0,
    discount_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    cess_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    total DECIMAL(15,2) NOT NULL DEFAULT 0,
    paid_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    balance_due DECIMAL(15,2) NOT NULL DEFAULT 0,
    extra_fields JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_business_type_serial
    ON documents (business_id, document_type, serial_number)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_documents_business_type ON documents (business_id, document_type, deleted_at);
CREATE INDEX IF NOT EXISTS idx_documents_party ON documents (party_type, party_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_documents_status ON documents (status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_documents_issue_date ON documents (issue_date, deleted_at);

CREATE TABLE IF NOT EXISTS document_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    product_id UUID REFERENCES products(id) ON DELETE SET NULL,
    description VARCHAR(500) NOT NULL,
    hsn_sac_code VARCHAR(40),
    unit VARCHAR(40),
    warehouse_id UUID,
    quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    free_quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    remaining_quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    unit_price DECIMAL(15,2) NOT NULL DEFAULT 0,
    discount_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    cgst_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    sgst_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    igst_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    cess_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    cgst_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    sgst_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    igst_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    cess_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    line_subtotal DECIMAL(15,2) NOT NULL DEFAULT 0,
    line_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    cost_snapshot DECIMAL(15,2) NOT NULL DEFAULT 0,
    margin_snapshot DECIMAL(15,2) NOT NULL DEFAULT 0,
    packing_metadata JSONB NOT NULL DEFAULT '{}',
    stock_effect VARCHAR(20) NOT NULL DEFAULT 'none',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_document_lines_document ON document_lines (document_id);
CREATE INDEX IF NOT EXISTS idx_document_lines_product ON document_lines (product_id);
CREATE INDEX IF NOT EXISTS idx_document_lines_warehouse ON document_lines (warehouse_id);

CREATE TABLE IF NOT EXISTS document_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    source_document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    source_line_id UUID REFERENCES document_lines(id) ON DELETE SET NULL,
    target_document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    target_line_id UUID REFERENCES document_lines(id) ON DELETE SET NULL,
    link_type VARCHAR(50) NOT NULL,
    quantity_used DECIMAL(15,3) NOT NULL DEFAULT 0,
    quantity_remaining DECIMAL(15,3) NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_document_links_source ON document_links (source_document_id, link_type);
CREATE INDEX IF NOT EXISTS idx_document_links_target ON document_links (target_document_id, link_type);

CREATE TABLE IF NOT EXISTS render_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    header_html TEXT,
    footer_html TEXT,
    watermark_text VARCHAR(255),
    banner_text VARCHAR(255),
    font_family VARCHAR(80) NOT NULL DEFAULT 'Noto Sans',
    page_size VARCHAR(20) NOT NULL DEFAULT 'A4',
    layout_config JSONB NOT NULL DEFAULT '{}',
    password_protected BOOLEAN NOT NULL DEFAULT FALSE,
    password VARCHAR(255),
    copy_allowed BOOLEAN NOT NULL DEFAULT TRUE,
    print_allowed BOOLEAN NOT NULL DEFAULT TRUE,
    custom_labels JSONB NOT NULL DEFAULT '{}',
    visibility_config JSONB NOT NULL DEFAULT '{}',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_render_profiles_business ON render_profiles (business_id, deleted_at);

CREATE TABLE IF NOT EXISTS document_render_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    render_profile_id UUID REFERENCES render_profiles(id) ON DELETE SET NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'queued',
    locale VARCHAR(20) NOT NULL DEFAULT 'en-IN',
    template_version VARCHAR(50) NOT NULL DEFAULT 'v1',
    output_url VARCHAR(500),
    output_filename VARCHAR(255),
    error_message TEXT,
    requested_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_document_render_jobs_document ON document_render_jobs (document_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_document_render_jobs_status ON document_render_jobs (status, deleted_at);

CREATE TABLE IF NOT EXISTS warehouses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    code VARCHAR(60) NOT NULL,
    address VARCHAR(500),
    city VARCHAR(100),
    state VARCHAR(100),
    country VARCHAR(100),
    postal_code VARCHAR(20),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_warehouses_business_code ON warehouses (business_id, code) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_warehouses_business ON warehouses (business_id, deleted_at);

CREATE TABLE IF NOT EXISTS stock_moves (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    document_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    document_line_id UUID REFERENCES document_lines(id) ON DELETE SET NULL,
    direction VARCHAR(20) NOT NULL,
    quantity DECIMAL(15,3) NOT NULL,
    reason VARCHAR(255),
    metadata JSONB NOT NULL DEFAULT '{}',
    recorded_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_stock_moves_product ON stock_moves (product_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_stock_moves_document ON stock_moves (document_id, recorded_at DESC);

CREATE TABLE IF NOT EXISTS inventory_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    document_line_id UUID REFERENCES document_lines(id) ON DELETE SET NULL,
    quantity DECIMAL(15,3) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'active',
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_inventory_reservations_product ON inventory_reservations (product_id, status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_inventory_reservations_document ON inventory_reservations (document_id, deleted_at);

CREATE TABLE IF NOT EXISTS journals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(150) NOT NULL,
    reference VARCHAR(100),
    status VARCHAR(30) NOT NULL DEFAULT 'draft',
    posting_date TIMESTAMP NOT NULL,
    notes TEXT,
    source_type VARCHAR(50),
    source_id UUID,
    reversal_of_id UUID REFERENCES journals(id) ON DELETE SET NULL,
    posted_at TIMESTAMP,
    reversed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_journals_business_status ON journals (business_id, status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_journals_source ON journals (source_type, source_id);

CREATE TABLE IF NOT EXISTS journal_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    journal_id UUID NOT NULL REFERENCES journals(id) ON DELETE CASCADE,
    account_code VARCHAR(60) NOT NULL,
    account_name VARCHAR(120) NOT NULL,
    entry_type VARCHAR(10) NOT NULL,
    amount DECIMAL(15,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    description VARCHAR(500),
    document_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    document_line_id UUID REFERENCES document_lines(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_journal_lines_journal ON journal_lines (journal_id);
CREATE INDEX IF NOT EXISTS idx_journal_lines_document ON journal_lines (document_id);

CREATE TABLE IF NOT EXISTS shipments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    provider VARCHAR(60) NOT NULL,
    courier VARCHAR(100),
    status VARCHAR(40) NOT NULL DEFAULT 'draft',
    tracking_number VARCHAR(120),
    tracking_url VARCHAR(500),
    package_count INT NOT NULL DEFAULT 1,
    package_dimensions JSONB NOT NULL DEFAULT '{}',
    weight_kg DECIMAL(10,3) NOT NULL DEFAULT 0,
    address_snapshot JSONB NOT NULL DEFAULT '{}',
    provider_payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_shipments_document ON shipments (document_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_shipments_tracking ON shipments (tracking_number);

CREATE TABLE IF NOT EXISTS shipping_labels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    shipment_id UUID NOT NULL REFERENCES shipments(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    label_format VARCHAR(20) NOT NULL DEFAULT 'pdf',
    label_url VARCHAR(500),
    label_zpl TEXT,
    provider_label_id VARCHAR(120),
    provider_payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_shipping_labels_shipment ON shipping_labels (shipment_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_shipping_labels_document ON shipping_labels (document_id, deleted_at);

CREATE OR REPLACE FUNCTION update_document_platform_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_documents_updated_at ON documents;
CREATE TRIGGER trigger_documents_updated_at
BEFORE UPDATE ON documents
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_document_lines_updated_at ON document_lines;
CREATE TRIGGER trigger_document_lines_updated_at
BEFORE UPDATE ON document_lines
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_render_profiles_updated_at ON render_profiles;
CREATE TRIGGER trigger_render_profiles_updated_at
BEFORE UPDATE ON render_profiles
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_document_render_jobs_updated_at ON document_render_jobs;
CREATE TRIGGER trigger_document_render_jobs_updated_at
BEFORE UPDATE ON document_render_jobs
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_warehouses_updated_at ON warehouses;
CREATE TRIGGER trigger_warehouses_updated_at
BEFORE UPDATE ON warehouses
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_stock_moves_updated_at ON stock_moves;
CREATE TRIGGER trigger_stock_moves_updated_at
BEFORE UPDATE ON stock_moves
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_inventory_reservations_updated_at ON inventory_reservations;
CREATE TRIGGER trigger_inventory_reservations_updated_at
BEFORE UPDATE ON inventory_reservations
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_journals_updated_at ON journals;
CREATE TRIGGER trigger_journals_updated_at
BEFORE UPDATE ON journals
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_journal_lines_updated_at ON journal_lines;
CREATE TRIGGER trigger_journal_lines_updated_at
BEFORE UPDATE ON journal_lines
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_shipments_updated_at ON shipments;
CREATE TRIGGER trigger_shipments_updated_at
BEFORE UPDATE ON shipments
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

DROP TRIGGER IF EXISTS trigger_shipping_labels_updated_at ON shipping_labels;
CREATE TRIGGER trigger_shipping_labels_updated_at
BEFORE UPDATE ON shipping_labels
FOR EACH ROW
EXECUTE FUNCTION update_document_platform_updated_at();

INSERT INTO documents (
    id,
    business_id,
    document_type,
    party_type,
    party_id,
    status,
    draft_state,
    tax_mode,
    gst_treatment,
    serial_number,
    issue_date,
    due_date,
    currency,
    locale,
    pdf_url,
    pdf_filename,
    notes,
    subtotal,
    tax_total,
    total,
    paid_amount,
    balance_due,
    created_at,
    updated_at
)
SELECT
    i.id,
    i.business_id,
    'sales_invoice',
    'customer',
    i.customer_id,
    CASE
        WHEN i.status = 'draft' THEN 'draft'
        WHEN i.status = 'sent' THEN 'sent'
        WHEN i.status = 'paid' THEN 'completed'
        WHEN i.status = 'overdue' THEN 'issued'
        WHEN i.status IN ('void', 'canceled') THEN 'cancelled'
        ELSE 'issued'
    END,
    CASE
        WHEN i.status = 'draft' THEN 'draft'
        ELSE 'final'
    END,
    CASE
        WHEN COALESCE(i.tax, 0) > 0 THEN 'gst'
        ELSE 'non_gst'
    END,
    'regular',
    i.invoice_no,
    i.invoice_date,
    i.due_date,
    i.currency,
    'en-IN',
    i.pdf_url,
    i.pdf_filename,
    i.notes,
    i.subtotal,
    i.tax,
    i.total,
    i.paid_amount,
    i.balance_due,
    i.created_at,
    i.updated_at
FROM invoices i
WHERE i.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM documents d WHERE d.id = i.id
  );

INSERT INTO document_lines (
    id,
    document_id,
    product_id,
    description,
    quantity,
    remaining_quantity,
    unit_price,
    discount_amount,
    tax_rate,
    tax_amount,
    line_subtotal,
    line_total,
    cost_snapshot,
    margin_snapshot,
    stock_effect,
    created_at,
    updated_at
)
SELECT
    ii.id,
    ii.invoice_id,
    ii.product_id,
    ii.description,
    ii.quantity,
    ii.quantity,
    ii.unit_price,
    ii.discount,
    ii.tax_rate,
    ((ii.quantity * ii.unit_price) - ii.discount) * (ii.tax_rate / 100.0),
    ((ii.quantity * ii.unit_price) - ii.discount),
    ii.total,
    COALESCE(p.cost_price, 0),
    GREATEST(ii.total - (COALESCE(p.cost_price, 0) * ii.quantity), 0),
    'out',
    ii.created_at,
    ii.updated_at
FROM invoice_items ii
LEFT JOIN products p ON p.id = ii.product_id
WHERE NOT EXISTS (
    SELECT 1 FROM document_lines dl WHERE dl.id = ii.id
);
