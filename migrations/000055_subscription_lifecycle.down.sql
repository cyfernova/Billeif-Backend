DROP INDEX IF EXISTS idx_subscription_commands_reconciliation;
DROP TABLE IF EXISTS subscription_commands;
DROP INDEX IF EXISTS idx_subscription_audit_business_time;
DROP TABLE IF EXISTS subscription_audit_records;
DROP INDEX IF EXISTS idx_subscription_billing_business_time;
DROP TABLE IF EXISTS subscription_billing_records;

ALTER TABLE razorpay_webhook_events
    DROP CONSTRAINT IF EXISTS razorpay_webhook_events_subscription_fk,
    DROP CONSTRAINT IF EXISTS razorpay_webhook_events_business_fk,
    DROP CONSTRAINT IF EXISTS razorpay_webhook_events_mode_identity_unique,
    DROP CONSTRAINT IF EXISTS razorpay_webhook_events_replay_count_check,
    DROP CONSTRAINT IF EXISTS razorpay_webhook_events_attempt_count_check,
    DROP CONSTRAINT IF EXISTS razorpay_webhook_events_processing_status_check,
    DROP CONSTRAINT IF EXISTS razorpay_webhook_events_provider_mode_check;
ALTER TABLE razorpay_webhook_events
    ADD CONSTRAINT razorpay_webhook_events_razorpay_event_id_key UNIQUE (razorpay_event_id),
    DROP COLUMN IF EXISTS last_replayed_at,
    DROP COLUMN IF EXISTS replay_count,
    DROP COLUMN IF EXISTS subscription_id,
    DROP COLUMN IF EXISTS business_id,
    DROP COLUMN IF EXISTS sanitized_error_code,
    DROP COLUMN IF EXISTS attempt_count,
    DROP COLUMN IF EXISTS processing_status,
    DROP COLUMN IF EXISTS provider_occurred_at,
    DROP COLUMN IF EXISTS received_at,
    DROP COLUMN IF EXISTS signature_verified,
    DROP COLUMN IF EXISTS payload_hash,
    DROP COLUMN IF EXISTS provider_mode;

ALTER TABLE payment_attempts
    DROP CONSTRAINT IF EXISTS payment_attempts_purchase_type_check,
    DROP COLUMN IF EXISTS purchase_type;

ALTER TABLE subscription_quota_usage
    ALTER COLUMN period_start TYPE DATE
    USING period_start::DATE;

DROP INDEX IF EXISTS idx_subscriptions_grace_due;
DROP INDEX IF EXISTS idx_subscriptions_reconciliation_due;
DROP INDEX IF EXISTS ux_subscriptions_provider_identity;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_lifecycle_version_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_paid_count_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_grace_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_period_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_provider_identity_complete_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_provider_mode_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_billing_mode_check;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS subscriptions_status_check;
UPDATE subscriptions SET status = 'canceled' WHERE status = 'cancelled';
UPDATE subscriptions
SET status = CASE
	WHEN status = 'canceled' THEN 'canceled'
    WHEN status IN ('active', 'renewal_pending', 'past_due', 'grace_period', 'cancellation_scheduled') THEN 'active'
    ELSE 'expired'
END;
ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_status_check CHECK (status IN ('active', 'canceled', 'expired'));
ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS lifecycle_version,
    DROP COLUMN IF EXISTS reconciliation_code,
    DROP COLUMN IF EXISTS last_provider_paid_count,
    DROP COLUMN IF EXISTS last_provider_event_at,
    DROP COLUMN IF EXISTS pending_plan_effective_at,
    DROP COLUMN IF EXISTS pending_provider_plan_id,
    DROP COLUMN IF EXISTS pending_plan_id,
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS cancellation_effective_at,
    DROP COLUMN IF EXISTS cancel_at_period_end,
    DROP COLUMN IF EXISTS grace_deadline,
    DROP COLUMN IF EXISTS next_renewal_at,
    DROP COLUMN IF EXISTS period_end,
    DROP COLUMN IF EXISTS period_start,
    DROP COLUMN IF EXISTS provider_plan_id,
    DROP COLUMN IF EXISTS provider_subscription_id,
    DROP COLUMN IF EXISTS provider_customer_id,
    DROP COLUMN IF EXISTS provider_mode,
    DROP COLUMN IF EXISTS billing_mode;
