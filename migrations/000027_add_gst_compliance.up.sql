ALTER TABLE business_profiles
    ADD COLUMN IF NOT EXISTS gst_filing_frequency VARCHAR(20) NOT NULL DEFAULT 'monthly',
    ADD COLUMN IF NOT EXISTS gst_registered BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS gst_tds_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS tax_preferences_json JSONB NOT NULL DEFAULT '{}';

ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS gstin VARCHAR(20),
    ADD COLUMN IF NOT EXISTS pan VARCHAR(10),
    ADD COLUMN IF NOT EXISTS company_name VARCHAR(255),
    ADD COLUMN IF NOT EXISTS state_code VARCHAR(10),
    ADD COLUMN IF NOT EXISTS billing_address_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS shipping_address_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS withholding_defaults_json JSONB NOT NULL DEFAULT '{}';

ALTER TABLE vendors
    ADD COLUMN IF NOT EXISTS gstin VARCHAR(20),
    ADD COLUMN IF NOT EXISTS pan VARCHAR(10),
    ADD COLUMN IF NOT EXISTS company_name VARCHAR(255),
    ADD COLUMN IF NOT EXISTS state_code VARCHAR(10),
    ADD COLUMN IF NOT EXISTS billing_address_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS shipping_address_json JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS withholding_defaults_json JSONB NOT NULL DEFAULT '{}';

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS uqc_code VARCHAR(20) NOT NULL DEFAULT 'OTH',
    ADD COLUMN IF NOT EXISTS gst_metadata JSONB NOT NULL DEFAULT '{}';

ALTER TABLE invoices
    ADD COLUMN IF NOT EXISTS tax_profile JSONB NOT NULL DEFAULT '{}';

ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS payment_type VARCHAR(20) NOT NULL DEFAULT 'normal',
    ADD COLUMN IF NOT EXISTS withholding_data JSONB NOT NULL DEFAULT '{}';

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS party_gstin VARCHAR(20),
    ADD COLUMN IF NOT EXISTS party_pan VARCHAR(10),
    ADD COLUMN IF NOT EXISTS party_state_code VARCHAR(10),
    ADD COLUMN IF NOT EXISTS supply_type VARCHAR(40),
    ADD COLUMN IF NOT EXISTS export_type VARCHAR(40),
    ADD COLUMN IF NOT EXISTS bill_of_supply BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS withholding_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tds_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tcs_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS report_tags JSONB NOT NULL DEFAULT '{}';

ALTER TABLE document_lines
    ADD COLUMN IF NOT EXISTS uqc_code VARCHAR(20) NOT NULL DEFAULT 'OTH',
    ADD COLUMN IF NOT EXISTS report_tags JSONB NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS tax_section_master (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(30) NOT NULL UNIQUE,
    kind VARCHAR(20) NOT NULL,
    description TEXT,
    rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    is_gst_tds BOOLEAN NOT NULL DEFAULT FALSE,
    is_tds BOOLEAN NOT NULL DEFAULT FALSE,
    is_tcs BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS document_withholdings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    section_code VARCHAR(30) NOT NULL,
    withholding_type VARCHAR(20) NOT NULL,
    rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    taxable_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_document_withholdings_business_document
    ON document_withholdings (business_id, document_id, deleted_at);

CREATE TABLE IF NOT EXISTS payment_withholdings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    payment_id UUID NOT NULL REFERENCES payments(id) ON DELETE CASCADE,
    invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    section_code VARCHAR(30) NOT NULL,
    withholding_type VARCHAR(20) NOT NULL,
    rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    taxable_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_payment_withholdings_business_payment
    ON payment_withholdings (business_id, payment_id, deleted_at);

CREATE TABLE IF NOT EXISTS gst_report_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    report_type VARCHAR(40) NOT NULL,
    period_start TIMESTAMP NOT NULL,
    period_end TIMESTAMP NOT NULL,
    filing_frequency VARCHAR(20) NOT NULL DEFAULT 'monthly',
    export_format VARCHAR(20) NOT NULL DEFAULT 'json',
    status VARCHAR(20) NOT NULL DEFAULT 'completed',
    warnings JSONB NOT NULL DEFAULT '[]',
    payload JSONB NOT NULL DEFAULT '{}',
    created_by VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gst_report_runs_business_type
    ON gst_report_runs (business_id, report_type, period_start, period_end, deleted_at);

CREATE TABLE IF NOT EXISTS gstr2b_imports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    period_start TIMESTAMP NOT NULL,
    period_end TIMESTAMP NOT NULL,
    source VARCHAR(20) NOT NULL DEFAULT 'api',
    status VARCHAR(20) NOT NULL DEFAULT 'processed',
    notes TEXT,
    raw_payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gstr2b_imports_business_period
    ON gstr2b_imports (business_id, period_start, period_end, deleted_at);

CREATE TABLE IF NOT EXISTS gstr2b_import_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    import_id UUID NOT NULL REFERENCES gstr2b_imports(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    supplier_gstin VARCHAR(20),
    supplier_name VARCHAR(255),
    document_number VARCHAR(80),
    document_date TIMESTAMP,
    document_type VARCHAR(30),
    taxable_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    cgst_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    sgst_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    igst_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    cess_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    place_of_supply VARCHAR(10),
    raw_payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gstr2b_import_lines_business_doc
    ON gstr2b_import_lines (business_id, supplier_gstin, document_number, deleted_at);

CREATE TABLE IF NOT EXISTS gstr2b_match_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    import_id UUID NOT NULL REFERENCES gstr2b_imports(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    import_line_id UUID REFERENCES gstr2b_import_lines(id) ON DELETE SET NULL,
    document_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    status VARCHAR(30) NOT NULL,
    mismatch_reason TEXT,
    books_taxable_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    import_taxable_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    books_tax_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    import_tax_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gstr2b_match_results_business_status
    ON gstr2b_match_results (business_id, status, deleted_at);
