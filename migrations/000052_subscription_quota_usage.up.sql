CREATE TABLE IF NOT EXISTS subscription_quota_usage (
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    feature_key VARCHAR(120) NOT NULL,
    period_start DATE NOT NULL,
    used_value BIGINT NOT NULL DEFAULT 0 CHECK (used_value >= 0),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (business_id, feature_key, period_start)
);

INSERT INTO subscription_quota_usage (business_id, feature_key, period_start, used_value)
SELECT
    business_id,
    CASE operation
        WHEN 'generate_einvoice' THEN 'einvoice'
        WHEN 'generate_ewaybill' THEN 'ewaybill'
    END AS feature_key,
    DATE_TRUNC('month', created_at)::DATE AS period_start,
    COUNT(*)::BIGINT AS used_value
FROM gst_submission_jobs
WHERE operation IN ('generate_einvoice', 'generate_ewaybill')
  AND deleted_at IS NULL
GROUP BY business_id, feature_key, DATE_TRUNC('month', created_at)::DATE
ON CONFLICT (business_id, feature_key, period_start)
DO UPDATE SET
    used_value = GREATEST(subscription_quota_usage.used_value, EXCLUDED.used_value),
    updated_at = NOW();

CREATE INDEX IF NOT EXISTS idx_subscription_quota_usage_period
    ON subscription_quota_usage (period_start, feature_key);
