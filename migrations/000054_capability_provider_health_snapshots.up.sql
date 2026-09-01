CREATE TABLE capability_provider_health_snapshots (
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    provider_key VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    fresh_until TIMESTAMPTZ NOT NULL,
    retry_at TIMESTAMPTZ,
    customer_code VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (business_id, provider_key),
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
        CHECK (fresh_until >= observed_at)
);
