ALTER TABLE drive_assets
    ADD COLUMN IF NOT EXISTS folder_path VARCHAR(500) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS email_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    email VARCHAR(255) NOT NULL,
    sender_name VARCHAR(160),
    reply_to_email VARCHAR(255),
    provider VARCHAR(40) NOT NULL DEFAULT 'ses',
    account_type VARCHAR(40) NOT NULL DEFAULT 'transactional',
    status VARCHAR(40) NOT NULL DEFAULT 'pending_verification',
    identity_arn VARCHAR(255),
    configuration_set VARCHAR(255),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    track_deliveries BOOLEAN NOT NULL DEFAULT TRUE,
    last_tested_at TIMESTAMP,
    verified_at TIMESTAMP,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_email_accounts_business_email
    ON email_accounts (business_id, LOWER(email))
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS email_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    email_account_id UUID REFERENCES email_accounts(id) ON DELETE SET NULL,
    recipient VARCHAR(255),
    subject VARCHAR(255),
    source_email VARCHAR(255),
    status VARCHAR(40) NOT NULL DEFAULT 'queued',
    provider_message_id VARCHAR(255),
    error_message TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    sent_at TIMESTAMP,
    delivered_at TIMESTAMP,
    failed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_email_deliveries_business_created
    ON email_deliveries (business_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_email_deliveries_account_created
    ON email_deliveries (email_account_id, created_at DESC)
    WHERE deleted_at IS NULL;
