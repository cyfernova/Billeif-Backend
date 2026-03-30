ALTER TABLE products
    ADD COLUMN IF NOT EXISTS mrp DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS default_cess_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS default_price_list_id UUID;

ALTER TABLE product_variants
    ADD COLUMN IF NOT EXISTS mrp DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS default_cess_rate DECIMAL(7,3) NOT NULL DEFAULT 0;

ALTER TABLE product_warehouse_catalogs
    ADD COLUMN IF NOT EXISTS price_list_id UUID;

ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS default_price_list_id UUID,
    ADD COLUMN IF NOT EXISTS preferences_json JSONB NOT NULL DEFAULT '{}';

ALTER TABLE vendors
    ADD COLUMN IF NOT EXISTS default_price_list_id UUID,
    ADD COLUMN IF NOT EXISTS preferences_json JSONB NOT NULL DEFAULT '{}';

ALTER TABLE team_members
    ADD COLUMN IF NOT EXISTS permission_overrides JSONB NOT NULL DEFAULT '{}';

ALTER TABLE invoices
    ADD COLUMN IF NOT EXISTS price_list_id UUID,
    ADD COLUMN IF NOT EXISTS custom_fields JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS additional_charges JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS origin_subscription_id UUID,
    ADD COLUMN IF NOT EXISTS origin_run_id UUID,
    ADD COLUMN IF NOT EXISTS signed_at TIMESTAMP,
    ADD COLUMN IF NOT EXISTS signed_by_profile_id UUID,
    ADD COLUMN IF NOT EXISTS sign_metadata JSONB NOT NULL DEFAULT '{}';

ALTER TABLE invoice_items
    ADD COLUMN IF NOT EXISTS variant_id UUID,
    ADD COLUMN IF NOT EXISTS warehouse_id UUID,
    ADD COLUMN IF NOT EXISTS free_quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS mrp DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cess_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cess_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS custom_fields JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS charge_snapshot JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS batch_allocations JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS serial_ids JSONB NOT NULL DEFAULT '[]';

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS party_pan VARCHAR(10),
    ADD COLUMN IF NOT EXISTS party_state_code VARCHAR(10),
    ADD COLUMN IF NOT EXISTS supply_type VARCHAR(40),
    ADD COLUMN IF NOT EXISTS export_type VARCHAR(40),
    ADD COLUMN IF NOT EXISTS bill_of_supply BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS project_id UUID,
    ADD COLUMN IF NOT EXISTS price_list_id UUID,
    ADD COLUMN IF NOT EXISTS origin_subscription_id UUID,
    ADD COLUMN IF NOT EXISTS origin_run_id UUID,
    ADD COLUMN IF NOT EXISTS withholding_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tds_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tcs_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS report_tags JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS signed_at TIMESTAMP,
    ADD COLUMN IF NOT EXISTS signed_by_profile_id UUID,
    ADD COLUMN IF NOT EXISTS sign_metadata JSONB NOT NULL DEFAULT '{}';

ALTER TABLE document_lines
    ADD COLUMN IF NOT EXISTS variant_id UUID,
    ADD COLUMN IF NOT EXISTS uqc_code VARCHAR(20) NOT NULL DEFAULT 'OTH',
    ADD COLUMN IF NOT EXISTS mrp DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS custom_fields JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS charge_linkage JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS batch_allocations JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS serial_ids JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS report_tags JSONB NOT NULL DEFAULT '{}';

ALTER TABLE stock_moves
    ADD COLUMN IF NOT EXISTS variant_id UUID,
    ADD COLUMN IF NOT EXISTS source_warehouse_id UUID,
    ADD COLUMN IF NOT EXISTS project_id UUID,
    ADD COLUMN IF NOT EXISTS batch_id UUID,
    ADD COLUMN IF NOT EXISTS serial_number_id UUID,
    ADD COLUMN IF NOT EXISTS transaction_type VARCHAR(40),
    ADD COLUMN IF NOT EXISTS unit_cost DECIMAL(15,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS actor_id UUID,
    ADD COLUMN IF NOT EXISTS actor_role VARCHAR(80);

CREATE TABLE IF NOT EXISTS invoice_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'active',
    cadence VARCHAR(40) NOT NULL,
    timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    start_date TIMESTAMP NOT NULL,
    end_date TIMESTAMP,
    next_run_at TIMESTAMP,
    last_run_at TIMESTAMP,
    auto_send BOOLEAN NOT NULL DEFAULT FALSE,
    price_policy VARCHAR(40) NOT NULL DEFAULT 'freeze_on_create',
    price_list_id UUID,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    notes TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    template_invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    template_document_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_invoice_subscriptions_business_status
    ON invoice_subscriptions (business_id, status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_invoice_subscriptions_next_run
    ON invoice_subscriptions (business_id, next_run_at, deleted_at);

CREATE TABLE IF NOT EXISTS invoice_subscription_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID NOT NULL REFERENCES invoice_subscriptions(id) ON DELETE CASCADE,
    product_id UUID REFERENCES products(id) ON DELETE SET NULL,
    variant_id UUID REFERENCES product_variants(id) ON DELETE SET NULL,
    warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    description VARCHAR(500) NOT NULL,
    quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    free_quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    unit_price DECIMAL(15,2) NOT NULL DEFAULT 0,
    mrp DECIMAL(15,2) NOT NULL DEFAULT 0,
    discount_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    cess_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    custom_fields JSONB NOT NULL DEFAULT '{}',
    additional_charge JSONB NOT NULL DEFAULT '{}',
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_invoice_subscription_lines_subscription
    ON invoice_subscription_lines (subscription_id, position);

CREATE TABLE IF NOT EXISTS invoice_subscription_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID NOT NULL REFERENCES invoice_subscriptions(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    scheduled_for TIMESTAMP NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    idempotency_key VARCHAR(180) NOT NULL,
    invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    document_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    last_error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_invoice_subscription_runs_idempotency
    ON invoice_subscription_runs (idempotency_key)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_invoice_subscription_runs_subscription
    ON invoice_subscription_runs (subscription_id, scheduled_for DESC, deleted_at);

CREATE TABLE IF NOT EXISTS price_lists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    code VARCHAR(80) NOT NULL,
    description TEXT,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_price_lists_business_code
    ON price_lists (business_id, code)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_price_lists_business_active
    ON price_lists (business_id, is_active, deleted_at);

CREATE TABLE IF NOT EXISTS price_list_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    price_list_id UUID NOT NULL REFERENCES price_lists(id) ON DELETE CASCADE,
    entity_type VARCHAR(20) NOT NULL,
    product_id UUID REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID REFERENCES product_variants(id) ON DELETE CASCADE,
    price DECIMAL(15,2) NOT NULL DEFAULT 0,
    mrp DECIMAL(15,2) NOT NULL DEFAULT 0,
    cess_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_price_list_items_product
    ON price_list_items (price_list_id, product_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_price_list_items_variant
    ON price_list_items (price_list_id, variant_id, deleted_at);

CREATE TABLE IF NOT EXISTS price_list_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    price_list_id UUID NOT NULL REFERENCES price_lists(id) ON DELETE CASCADE,
    scope_type VARCHAR(30) NOT NULL,
    scope_id VARCHAR(120) NOT NULL,
    priority INT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_price_list_assignments_scope
    ON price_list_assignments (business_id, scope_type, scope_id, priority DESC, deleted_at);

CREATE TABLE IF NOT EXISTS party_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_party_groups_business
    ON party_groups (business_id, deleted_at);

CREATE TABLE IF NOT EXISTS party_group_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    party_group_id UUID NOT NULL REFERENCES party_groups(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    party_type VARCHAR(20) NOT NULL,
    party_id VARCHAR(120) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_party_group_members_unique
    ON party_group_members (party_group_id, party_type, party_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_party_group_members_party
    ON party_group_members (business_id, party_type, party_id, deleted_at);

CREATE TABLE IF NOT EXISTS bulk_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_type VARCHAR(50) NOT NULL,
    action VARCHAR(50),
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    file_name VARCHAR(255),
    file_key VARCHAR(500),
    content_type VARCHAR(120),
    total_rows INT NOT NULL DEFAULT 0,
    processed_rows INT NOT NULL DEFAULT 0,
    succeeded_rows INT NOT NULL DEFAULT 0,
    failed_rows INT NOT NULL DEFAULT 0,
    request_payload JSONB NOT NULL DEFAULT '{}',
    result_payload JSONB NOT NULL DEFAULT '{}',
    last_error TEXT,
    queued_at TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bulk_jobs_business_status
    ON bulk_jobs (business_id, status, created_at DESC, deleted_at);

CREATE TABLE IF NOT EXISTS bulk_job_rows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bulk_job_id UUID NOT NULL REFERENCES bulk_jobs(id) ON DELETE CASCADE,
    row_number INT NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    entity_id UUID,
    entity_type VARCHAR(40),
    input JSONB NOT NULL DEFAULT '{}',
    result JSONB NOT NULL DEFAULT '{}',
    error TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bulk_job_rows_job
    ON bulk_job_rows (bulk_job_id, row_number, deleted_at);

CREATE TABLE IF NOT EXISTS bulk_job_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bulk_job_id UUID NOT NULL REFERENCES bulk_jobs(id) ON DELETE CASCADE,
    artifact_type VARCHAR(50) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    file_key VARCHAR(500) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bulk_job_artifacts_job
    ON bulk_job_artifacts (bulk_job_id, deleted_at);

CREATE TABLE IF NOT EXISTS activity_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    actor_role VARCHAR(80),
    request_id VARCHAR(100),
    ip_address VARCHAR(100),
    entity_type VARCHAR(60) NOT NULL,
    entity_id VARCHAR(120) NOT NULL,
    action VARCHAR(80) NOT NULL,
    reason TEXT,
    snapshot JSONB NOT NULL DEFAULT '{}',
    diff JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_activity_logs_business_created
    ON activity_logs (business_id, created_at DESC, deleted_at);
CREATE INDEX IF NOT EXISTS idx_activity_logs_entity
    ON activity_logs (business_id, entity_type, entity_id, created_at DESC, deleted_at);

CREATE TABLE IF NOT EXISTS signature_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    provider VARCHAR(80),
    signer_name VARCHAR(160),
    certificate_sn VARCHAR(160),
    file_name VARCHAR(255),
    file_key VARCHAR(500),
    encryption_iv VARCHAR(255),
    metadata JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_used_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_signature_profiles_business
    ON signature_profiles (business_id, is_active, deleted_at);

CREATE TABLE IF NOT EXISTS signed_document_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_id UUID REFERENCES documents(id) ON DELETE CASCADE,
    invoice_id UUID REFERENCES invoices(id) ON DELETE CASCADE,
    signature_profile_id UUID NOT NULL REFERENCES signature_profiles(id) ON DELETE RESTRICT,
    file_name VARCHAR(255) NOT NULL,
    file_key VARCHAR(500) NOT NULL,
    source_pdf_url VARCHAR(500),
    signed_pdf_url VARCHAR(500),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_signed_document_artifacts_document
    ON signed_document_artifacts (document_id, created_at DESC, deleted_at);
CREATE INDEX IF NOT EXISTS idx_signed_document_artifacts_invoice
    ON signed_document_artifacts (invoice_id, created_at DESC, deleted_at);

CREATE TABLE IF NOT EXISTS custom_field_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    entity_type VARCHAR(40) NOT NULL,
    name VARCHAR(160) NOT NULL,
    slug VARCHAR(160) NOT NULL,
    data_type VARCHAR(40) NOT NULL,
    visibility JSONB NOT NULL DEFAULT '[]',
    default_value JSONB NOT NULL DEFAULT 'null',
    options JSONB NOT NULL DEFAULT '[]',
    is_required BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    linked_definition_id UUID REFERENCES custom_field_definitions(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_field_definitions_slug
    ON custom_field_definitions (business_id, entity_type, slug)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS custom_field_values (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    definition_id UUID NOT NULL REFERENCES custom_field_definitions(id) ON DELETE CASCADE,
    entity_type VARCHAR(40) NOT NULL,
    entity_id VARCHAR(120) NOT NULL,
    value JSONB NOT NULL DEFAULT 'null',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_field_values_unique
    ON custom_field_values (definition_id, entity_type, entity_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS charge_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    slug VARCHAR(160) NOT NULL,
    charge_type VARCHAR(40) NOT NULL,
    value_type VARCHAR(40) NOT NULL,
    default_value DECIMAL(15,2) NOT NULL DEFAULT 0,
    taxable BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    visibility JSONB NOT NULL DEFAULT '[]',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_charge_definitions_slug
    ON charge_definitions (business_id, slug)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS party_addresses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    party_type VARCHAR(20) NOT NULL,
    party_id VARCHAR(120) NOT NULL,
    address_type VARCHAR(30) NOT NULL,
    label VARCHAR(120),
    contact_name VARCHAR(160),
    line1 VARCHAR(255),
    line2 VARCHAR(255),
    city VARCHAR(120),
    state VARCHAR(120),
    country VARCHAR(120),
    postal_code VARCHAR(30),
    gstin VARCHAR(20),
    state_code VARCHAR(10),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_party_addresses_scope
    ON party_addresses (business_id, party_type, party_id, address_type, deleted_at);
