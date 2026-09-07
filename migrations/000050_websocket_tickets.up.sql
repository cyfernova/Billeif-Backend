CREATE TABLE websocket_tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_digest CHAR(64) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_websocket_ticket_digest_hex
        CHECK (ticket_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_websocket_ticket_expiry
        CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX ux_websocket_tickets_digest
    ON websocket_tickets (ticket_digest);

CREATE INDEX idx_websocket_tickets_subject_business
    ON websocket_tickets (subject, business_id);

CREATE INDEX idx_websocket_tickets_expires_at
    ON websocket_tickets (expires_at);

CREATE INDEX idx_websocket_tickets_unconsumed
    ON websocket_tickets (ticket_digest, expires_at)
    WHERE consumed_at IS NULL;
