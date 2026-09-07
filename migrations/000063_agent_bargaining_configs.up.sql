CREATE TABLE agent_bargaining_configs (
    agent_id UUID PRIMARY KEY,
    business_id UUID NOT NULL,
    configuration JSONB NOT NULL CHECK (jsonb_typeof(configuration) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (agent_id, business_id) REFERENCES agents(id, business_id) ON DELETE CASCADE
);
