ALTER TABLE gst_integration_accounts
    ADD COLUMN credential_revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT gst_integration_accounts_credential_revision_check
        CHECK (credential_revision > 0),
    ADD CONSTRAINT gst_integration_accounts_id_business_revision_unique
        UNIQUE (id, business_id, credential_revision);

CREATE TABLE capability_provider_health_snapshots (
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    provider_key VARCHAR(64) NOT NULL,
    integration_account_id UUID NOT NULL,
    credential_revision BIGINT NOT NULL,
    observation_revision BIGINT NOT NULL DEFAULT 1,
    status VARCHAR(32) NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    fresh_until TIMESTAMPTZ NOT NULL,
    retry_at TIMESTAMPTZ,
    customer_code VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (business_id, provider_key),
    FOREIGN KEY (integration_account_id, business_id, credential_revision)
        REFERENCES gst_integration_accounts(id, business_id, credential_revision) ON DELETE CASCADE
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT capability_provider_health_provider_check
        CHECK (provider_key = 'gst_provider'),
    CONSTRAINT capability_provider_health_status_check
        CHECK (status IN ('healthy', 'degraded', 'unavailable')),
    CONSTRAINT capability_provider_health_customer_code_check
        CHECK (customer_code IN ('', 'provider_degraded', 'provider_unavailable', 'provider_rate_limited')),
    CONSTRAINT capability_provider_health_classification_check
        CHECK (
            (status = 'healthy' AND customer_code = '') OR
            (status = 'degraded' AND customer_code IN ('provider_degraded', 'provider_rate_limited')) OR
            (status = 'unavailable' AND customer_code = 'provider_unavailable')
        ),
    CONSTRAINT capability_provider_health_freshness_check
        CHECK (fresh_until >= observed_at),
    CONSTRAINT capability_provider_health_credential_revision_check
        CHECK (credential_revision > 0),
    CONSTRAINT capability_provider_health_observation_revision_check
        CHECK (observation_revision > 0)
);
