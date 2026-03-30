ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS generate_einvoice BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS generate_ewaybill BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS reverse_charge BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS reverse_charge_reason TEXT,
    ADD COLUMN IF NOT EXISTS dispatch_from JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS dispatch_to JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS distance_km DECIMAL(10,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS transporter JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS vehicle JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS multi_vehicle_plan JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS current_einvoice_id UUID,
    ADD COLUMN IF NOT EXISTS current_ewaybill_id UUID;

CREATE TABLE IF NOT EXISTS gst_integration_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    provider VARCHAR(80) NOT NULL,
    service_type VARCHAR(40) NOT NULL,
    gsp_name VARCHAR(120),
    portal_username VARCHAR(160),
    encrypted_credentials TEXT NOT NULL,
    credential_hint VARCHAR(255),
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    last_validated_at TIMESTAMP,
    last_error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gst_integration_accounts_business_service
    ON gst_integration_accounts (business_id, service_type, deleted_at);

CREATE TABLE IF NOT EXISTS einvoice_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    integration_account_id UUID REFERENCES gst_integration_accounts(id) ON DELETE SET NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    irn VARCHAR(120),
    ack_number VARCHAR(120),
    ack_date TIMESTAMP,
    signed_qr_code_payload TEXT,
    qr_code_url VARCHAR(500),
    provider_reference_id VARCHAR(120),
    provider_name VARCHAR(80),
    request_payload JSONB NOT NULL DEFAULT '{}',
    response_payload JSONB NOT NULL DEFAULT '{}',
    error_class VARCHAR(30),
    last_error TEXT,
    generated_at TIMESTAMP,
    cancelled_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_einvoice_records_document
    ON einvoice_records (document_id, status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_einvoice_records_business_irn
    ON einvoice_records (business_id, irn, deleted_at);

CREATE TABLE IF NOT EXISTS ewaybill_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    integration_account_id UUID REFERENCES gst_integration_accounts(id) ON DELETE SET NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    eway_bill_number VARCHAR(120),
    eway_bill_date TIMESTAMP,
    valid_until TIMESTAMP,
    supply_type VARCHAR(30),
    part_a_status VARCHAR(30),
    part_b_status VARCHAR(30),
    distance_km DECIMAL(10,2) NOT NULL DEFAULT 0,
    distance_source VARCHAR(20) NOT NULL DEFAULT 'auto',
    transporter JSONB NOT NULL DEFAULT '{}',
    vehicle JSONB NOT NULL DEFAULT '{}',
    dispatch_from JSONB NOT NULL DEFAULT '{}',
    dispatch_to JSONB NOT NULL DEFAULT '{}',
    pdf_url VARCHAR(500),
    provider_reference_id VARCHAR(120),
    provider_name VARCHAR(80),
    request_payload JSONB NOT NULL DEFAULT '{}',
    response_payload JSONB NOT NULL DEFAULT '{}',
    error_class VARCHAR(30),
    last_error TEXT,
    generated_at TIMESTAMP,
    updated_part_b_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ewaybill_records_document
    ON ewaybill_records (document_id, status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_ewaybill_records_business_number
    ON ewaybill_records (business_id, eway_bill_number, deleted_at);

CREATE TABLE IF NOT EXISTS ewaybill_vehicle_movements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    eway_bill_id UUID NOT NULL REFERENCES ewaybill_records(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    movement_type VARCHAR(30) NOT NULL,
    vehicle_no VARCHAR(40),
    transport_doc_no VARCHAR(80),
    from_place VARCHAR(120),
    from_state VARCHAR(10),
    reason_code VARCHAR(40),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ewaybill_vehicle_movements_eway
    ON ewaybill_vehicle_movements (eway_bill_id, is_active, deleted_at);

CREATE TABLE IF NOT EXISTS gst_submission_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    operation VARCHAR(40) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'queued',
    idempotency_key VARCHAR(180) NOT NULL,
    queue_message_id VARCHAR(120),
    attempt_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP,
    last_attempt_at TIMESTAMP,
    succeeded_at TIMESTAMP,
    last_error TEXT,
    error_class VARCHAR(30),
    request_payload JSONB NOT NULL DEFAULT '{}',
    result_payload JSONB NOT NULL DEFAULT '{}',
    source VARCHAR(40),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_gst_submission_jobs_idempotency
    ON gst_submission_jobs (idempotency_key)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_gst_submission_jobs_status
    ON gst_submission_jobs (business_id, status, next_attempt_at, deleted_at);

CREATE TABLE IF NOT EXISTS gst_submission_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gst_job_id UUID NOT NULL REFERENCES gst_submission_jobs(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    attempt_number INT NOT NULL,
    status VARCHAR(30) NOT NULL,
    error_class VARCHAR(30),
    error_message TEXT,
    request_payload JSONB NOT NULL DEFAULT '{}',
    result_payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gst_submission_attempts_job
    ON gst_submission_attempts (gst_job_id, attempt_number, deleted_at);

CREATE TABLE IF NOT EXISTS pos_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    default_warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    receipt_width VARCHAR(20) NOT NULL DEFAULT '58mm',
    show_hsn BOOLEAN NOT NULL DEFAULT FALSE,
    footer_text TEXT,
    template VARCHAR(60) NOT NULL DEFAULT 'standard',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pos_profiles_business_default
    ON pos_profiles (business_id, is_default, deleted_at);

CREATE TABLE IF NOT EXISTS pos_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL,
    pos_profile_id UUID REFERENCES pos_profiles(id) ON DELETE SET NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'open',
    session_name VARCHAR(120),
    warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    cart_payload JSONB NOT NULL DEFAULT '{}',
    last_scanned_code VARCHAR(120),
    last_checked_out_document_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    opened_at TIMESTAMP NOT NULL DEFAULT NOW(),
    closed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pos_sessions_business_status
    ON pos_sessions (business_id, status, deleted_at);
