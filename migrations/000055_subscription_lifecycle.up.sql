ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS billing_mode VARCHAR(32) NOT NULL DEFAULT 'free',
    ADD COLUMN IF NOT EXISTS provider_mode VARCHAR(16),
    ADD COLUMN IF NOT EXISTS provider_customer_id VARCHAR(160),
    ADD COLUMN IF NOT EXISTS provider_subscription_id VARCHAR(160),
    ADD COLUMN IF NOT EXISTS provider_plan_id VARCHAR(160),
    ADD COLUMN IF NOT EXISTS period_start TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS period_end TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS next_renewal_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS grace_deadline TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS cancellation_effective_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pending_plan_id VARCHAR(80),
    ADD COLUMN IF NOT EXISTS pending_provider_plan_id VARCHAR(160),
    ADD COLUMN IF NOT EXISTS pending_plan_effective_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_provider_event_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_provider_paid_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reconciliation_code VARCHAR(80),
    ADD COLUMN IF NOT EXISTS lifecycle_version BIGINT NOT NULL DEFAULT 1;

UPDATE subscriptions
SET billing_mode = CASE
        WHEN plan = 'free' THEN 'free'
        ELSE 'legacy_one_time'
    END,
    period_start = COALESCE(period_start, start_date),
    period_end = COALESCE(period_end, end_date),
    next_renewal_at = NULL;

ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_status_check;
UPDATE subscriptions SET status = 'cancelled' WHERE status = 'canceled';
ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_status_check CHECK (status IN (
        'pending_payment', 'active', 'renewal_pending', 'past_due', 'grace_period',
        'cancellation_scheduled', 'cancelled', 'expired', 'suspended', 'reconciliation_required'
    )),
    ADD CONSTRAINT subscriptions_billing_mode_check CHECK (billing_mode IN ('free', 'legacy_one_time', 'renewable')),
    ADD CONSTRAINT subscriptions_provider_mode_check CHECK (provider_mode IS NULL OR provider_mode IN ('test', 'live')),
    ADD CONSTRAINT subscriptions_provider_identity_complete_check CHECK (
        (billing_mode <> 'renewable') OR
        (provider_mode IS NOT NULL AND provider_subscription_id IS NOT NULL AND provider_plan_id IS NOT NULL) OR
        status IN ('pending_payment', 'reconciliation_required')
    ),
    ADD CONSTRAINT subscriptions_period_check CHECK (period_end IS NULL OR period_start IS NULL OR period_end > period_start),
    ADD CONSTRAINT subscriptions_grace_check CHECK (grace_deadline IS NULL OR period_end IS NULL OR grace_deadline >= period_end),
    ADD CONSTRAINT subscriptions_paid_count_check CHECK (last_provider_paid_count >= 0),
    ADD CONSTRAINT subscriptions_lifecycle_version_check CHECK (lifecycle_version > 0);

CREATE UNIQUE INDEX IF NOT EXISTS ux_subscriptions_provider_identity
    ON subscriptions (provider_mode, provider_subscription_id)
    WHERE provider_subscription_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_subscriptions_reconciliation_due
    ON subscriptions (status, updated_at)
    WHERE status = 'reconciliation_required' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_subscriptions_grace_due
    ON subscriptions (grace_deadline)
    WHERE status IN ('past_due', 'grace_period') AND deleted_at IS NULL;

ALTER TABLE payment_attempts
    ADD COLUMN IF NOT EXISTS purchase_type VARCHAR(32) NOT NULL DEFAULT 'checkout';
UPDATE payment_attempts
SET purchase_type = 'legacy_one_time'
WHERE target_type = 'plan';
UPDATE payment_attempts
SET failure_reason = 'legacy_payment_failed'
WHERE failure_reason IS NOT NULL AND BTRIM(failure_reason) <> '';
ALTER TABLE payment_attempts
    ADD CONSTRAINT payment_attempts_purchase_type_check
    CHECK (purchase_type IN ('checkout', 'legacy_one_time'));

ALTER TABLE subscription_quota_usage
    ALTER COLUMN period_start TYPE TIMESTAMPTZ
    USING period_start::TIMESTAMPTZ;

ALTER TABLE razorpay_webhook_events
    ADD COLUMN IF NOT EXISTS provider_mode VARCHAR(16),
    ADD COLUMN IF NOT EXISTS payload_hash CHAR(64),
    ADD COLUMN IF NOT EXISTS signature_verified BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS provider_occurred_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS processing_status VARCHAR(32),
    ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sanitized_error_code VARCHAR(80),
    ADD COLUMN IF NOT EXISTS business_id UUID,
    ADD COLUMN IF NOT EXISTS subscription_id UUID,
    ADD COLUMN IF NOT EXISTS replay_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_replayed_at TIMESTAMPTZ;

UPDATE razorpay_webhook_events
SET provider_mode = 'legacy_unknown',
    payload_hash = ENCODE(DIGEST(razorpay_event_id || ':' || event_type, 'sha256'), 'hex'),
    signature_verified = TRUE,
    received_at = COALESCE(received_at, created_at),
    processing_status = CASE WHEN processed_at IS NULL THEN 'reconciliation_required' ELSE 'processed' END,
    attempt_count = CASE WHEN processed_at IS NULL THEN 0 ELSE 1 END;

ALTER TABLE razorpay_webhook_events
    DROP CONSTRAINT IF EXISTS razorpay_webhook_events_razorpay_event_id_key;
ALTER TABLE razorpay_webhook_events
    ALTER COLUMN provider_mode SET NOT NULL,
    ALTER COLUMN payload_hash SET NOT NULL,
    ALTER COLUMN received_at SET NOT NULL,
    ALTER COLUMN processing_status SET NOT NULL,
    ADD CONSTRAINT razorpay_webhook_events_provider_mode_check CHECK (provider_mode IN ('test', 'live', 'legacy_unknown', 'unverified')),
    ADD CONSTRAINT razorpay_webhook_events_processing_status_check CHECK (processing_status IN ('received', 'processing', 'processed', 'rejected', 'reconciliation_required')),
    ADD CONSTRAINT razorpay_webhook_events_attempt_count_check CHECK (attempt_count >= 0),
    ADD CONSTRAINT razorpay_webhook_events_replay_count_check CHECK (replay_count >= 0),
    ADD CONSTRAINT razorpay_webhook_events_mode_identity_unique UNIQUE (provider_mode, razorpay_event_id),
    ADD CONSTRAINT razorpay_webhook_events_business_fk FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE SET NULL,
    ADD CONSTRAINT razorpay_webhook_events_subscription_fk FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_razorpay_webhook_events_processing
    ON razorpay_webhook_events (processing_status, received_at);
CREATE INDEX IF NOT EXISTS idx_razorpay_webhook_events_subscription
    ON razorpay_webhook_events (subscription_id, provider_occurred_at);

CREATE TABLE subscription_billing_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    subscription_id UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    provider_mode VARCHAR(16) NOT NULL CHECK (provider_mode IN ('test', 'live')),
    provider_event_id VARCHAR(160),
    provider_invoice_id VARCHAR(160),
    provider_payment_id VARCHAR(160),
    amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
    currency VARCHAR(3) NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('paid', 'failed', 'provider_verified')),
    receipt_reference VARCHAR(80) NOT NULL,
    period_start TIMESTAMPTZ,
    period_end TIMESTAMPTZ,
    quota_period_start TIMESTAMPTZ,
    quota_period_end TIMESTAMPTZ,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider_mode, provider_event_id)
);
CREATE INDEX idx_subscription_billing_business_time
    ON subscription_billing_records (business_id, occurred_at DESC);

CREATE TABLE subscription_audit_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    subscription_id UUID REFERENCES subscriptions(id) ON DELETE SET NULL,
    actor_user_id VARCHAR(120),
    action VARCHAR(80) NOT NULL,
    from_status VARCHAR(32),
    to_status VARCHAR(32),
    from_plan_id VARCHAR(80),
    to_plan_id VARCHAR(80),
    provider_mode VARCHAR(16),
    provider_event_id VARCHAR(160),
    sanitized_code VARCHAR(80) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_subscription_audit_business_time
    ON subscription_audit_records (business_id, occurred_at DESC);

CREATE TABLE subscription_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    subscription_id UUID REFERENCES subscriptions(id) ON DELETE SET NULL,
    actor_user_id VARCHAR(120) NOT NULL,
    action VARCHAR(80) NOT NULL,
    idempotency_key VARCHAR(180) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('initializing', 'completed', 'reconciliation_required', 'rejected')),
    sanitized_error_code VARCHAR(80),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    UNIQUE (business_id, actor_user_id, action, idempotency_key)
);
CREATE INDEX idx_subscription_commands_reconciliation
    ON subscription_commands (status, updated_at)
    WHERE status = 'reconciliation_required';
