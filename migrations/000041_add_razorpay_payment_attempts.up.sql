CREATE TABLE IF NOT EXISTS payment_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(120) NOT NULL,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    target_type VARCHAR(40) NOT NULL,
    target_id VARCHAR(120) NOT NULL,
    amount_paise BIGINT NOT NULL CHECK (amount_paise > 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    razorpay_order_id VARCHAR(100) UNIQUE,
    razorpay_payment_id VARCHAR(100),
    status VARCHAR(40) NOT NULL DEFAULT 'created',
    idempotency_key VARCHAR(255) NOT NULL,
    failure_reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    paid_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_attempts_user_business_idempotency
    ON payment_attempts (user_id, business_id, idempotency_key);

CREATE INDEX IF NOT EXISTS idx_payment_attempts_business_target
    ON payment_attempts (business_id, target_type, target_id);

CREATE INDEX IF NOT EXISTS idx_payment_attempts_status
    ON payment_attempts (status);

CREATE INDEX IF NOT EXISTS idx_payment_attempts_payment_id
    ON payment_attempts (razorpay_payment_id);

CREATE TABLE IF NOT EXISTS razorpay_webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    razorpay_event_id VARCHAR(160) NOT NULL UNIQUE,
    event_type VARCHAR(120) NOT NULL,
    processed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_razorpay_webhook_events_event_type
    ON razorpay_webhook_events (event_type);
