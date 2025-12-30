CREATE TABLE IF NOT EXISTS webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    name VARCHAR(255) NOT NULL,
    url VARCHAR(500) NOT NULL,
    events TEXT NOT NULL,
    secret VARCHAR(255) NOT NULL,
    is_active BOOLEAN DEFAULT true,
    last_triggered TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE
);

CREATE INDEX idx_webhooks_business_id ON webhooks(business_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_webhooks_deleted_at ON webhooks(deleted_at);
