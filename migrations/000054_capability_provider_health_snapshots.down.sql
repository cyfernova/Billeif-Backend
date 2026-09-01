DROP TABLE IF EXISTS capability_provider_health_snapshots;

ALTER TABLE gst_integration_accounts
    DROP CONSTRAINT IF EXISTS gst_integration_accounts_id_business_revision_unique,
    DROP CONSTRAINT IF EXISTS gst_integration_accounts_credential_revision_check,
    DROP COLUMN IF EXISTS credential_revision;
