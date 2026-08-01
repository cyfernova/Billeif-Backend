CREATE TABLE bargaining_round_claims (
    negotiation_id UUID NOT NULL REFERENCES bargaining_negotiations(id) ON DELETE CASCADE,
    round_number INTEGER NOT NULL,
    lease_owner VARCHAR(255) NOT NULL,
    lease_expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (negotiation_id, round_number),
    CHECK (round_number > 0),
    CHECK (lease_expires_at > created_at)
);

CREATE INDEX idx_bargaining_round_claims_reclaimable
    ON bargaining_round_claims (lease_expires_at)
    WHERE completed_at IS NULL;
