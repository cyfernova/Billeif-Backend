CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL,
    source_event_key VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL CHECK (type IN ('invoice', 'payment', 'order', 'workflow', 'agent', 'system')),
    title VARCHAR(255) NOT NULL CHECK (length(trim(title)) > 0),
    body TEXT NOT NULL CHECK (length(trim(body)) > 0),
    resource_type VARCHAR(64),
    resource_id VARCHAR(255),
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ux_notifications_business_user_event UNIQUE (business_id, user_id, source_event_key)
);

CREATE INDEX idx_notifications_business_user_created
    ON notifications (business_id, user_id, created_at DESC, id DESC);

CREATE INDEX idx_notifications_business_user_unread
    ON notifications (business_id, user_id, created_at DESC)
    WHERE read_at IS NULL;
