CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    plan VARCHAR(50) NOT NULL CHECK (plan IN ('free', 'starter', 'professional', 'enterprise')),
    status VARCHAR(50) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'canceled', 'expired')),
    max_invoices BIGINT DEFAULT 10,
    max_customers BIGINT DEFAULT 10,
    max_users BIGINT DEFAULT 3,
    max_storage_mb BIGINT DEFAULT 100,
    start_date TIMESTAMP NOT NULL,
    end_date TIMESTAMP,
    next_billing_date TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE
);

CREATE INDEX idx_subscriptions_business_id ON subscriptions(business_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_subscriptions_status ON subscriptions(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_subscriptions_deleted_at ON subscriptions(deleted_at);
