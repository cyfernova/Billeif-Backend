CREATE TABLE ai_bargaining_proposals (
    negotiation_id UUID NOT NULL REFERENCES bargaining_negotiations(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE RESTRICT,
    agent_id UUID NOT NULL,
    round_number INTEGER NOT NULL CHECK (round_number BETWEEN 1 AND 20),
    proposal JSONB NOT NULL CHECK (jsonb_typeof(proposal) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (negotiation_id, round_number),
    FOREIGN KEY (agent_id, business_id) REFERENCES agents(id, business_id)
);
INSERT INTO ai_provider_circuit_breakers (provider_key, state, updated_at)
VALUES ('deepseek', 'closed', NOW()) ON CONFLICT (provider_key) DO NOTHING;
-- Activate only the untouched bootstrap gate. Administrative pauses survive rollout.
-- The independently configured runtime gate remains disabled outside development.
UPDATE ai_governance_controls SET execution_enabled = TRUE,
    reason_code = 'governed_bargaining_rollout', version = version + 1, updated_at = NOW()
WHERE scope_kind = 'global' AND reason_code = 'disabled_by_default' AND version = 1;
