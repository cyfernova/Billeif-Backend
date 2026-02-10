-- Add buyer and seller agent types to the agents table
-- This migration enables the bargaining system with buyer and seller agents

-- Update the agents table type constraint to include buyer and seller
-- First, drop existing check constraint if it exists (the constraint may not exist if handled by application layer)
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'agents_type_check'
    ) THEN
        ALTER TABLE agents DROP CONSTRAINT agents_type_check;
    END IF;
END $$;

-- Add a check constraint for agent types including buyer and seller
ALTER TABLE agents
ADD CONSTRAINT agents_type_check
CHECK (type IN ('buyer', 'seller', 'shopping', 'merchant', 'credential_provider', 'payment_processor'));

-- Ensure Config field is JSONB and properly indexed
CREATE INDEX IF NOT EXISTS idx_agents_config ON agents USING GIN(config)
WHERE deleted_at IS NULL;

-- Add index on Type and Active status for faster filtering
CREATE INDEX IF NOT EXISTS idx_agents_type_active ON agents(type, is_active, deleted_at);

-- Add comment to clarify the agent types
COMMENT ON COLUMN agents.type IS 'Agent type: buyer/seller for bargaining, shopping/merchant for marketplace, credential_provider/payment_processor for payments';

-- Add comment on Config field
COMMENT ON COLUMN agents.config IS 'Agent configuration stored as JSONB. Contains buyer/seller bargaining parameters or other agent-specific settings';

-- Ensure the bargaining negotiations table exists with proper indexes
CREATE TABLE IF NOT EXISTS bargaining_negotiations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    buyer_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    seller_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    marketplace_order_id UUID,

    initial_amount DECIMAL(15,2) NOT NULL,
    current_amount DECIMAL(15,2) NOT NULL,
    buyer_volatility DECIMAL(3,2) NOT NULL DEFAULT 0.5,
    seller_volatility DECIMAL(3,2) NOT NULL DEFAULT 0.5,

    status VARCHAR(50) NOT NULL DEFAULT 'initiated',
    rounds INT NOT NULL DEFAULT 0,
    max_rounds INT NOT NULL DEFAULT 5,

    expires_at TIMESTAMP NOT NULL,
    metadata JSONB DEFAULT '{}',

    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    completed_at TIMESTAMP,
    deleted_at TIMESTAMP
);

-- Indexes for bargaining negotiations
CREATE INDEX IF NOT EXISTS idx_bargaining_negotiations_buyer ON bargaining_negotiations(buyer_agent_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_bargaining_negotiations_seller ON bargaining_negotiations(seller_agent_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_bargaining_negotiations_user ON bargaining_negotiations(user_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_bargaining_negotiations_status ON bargaining_negotiations(status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_bargaining_negotiations_order ON bargaining_negotiations(marketplace_order_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_bargaining_negotiations_expires ON bargaining_negotiations(expires_at);

-- Create bargaining rounds table
CREATE TABLE IF NOT EXISTS bargaining_rounds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    negotiation_id UUID NOT NULL REFERENCES bargaining_negotiations(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,

    round_number INT NOT NULL,
    proposed_amount DECIMAL(15,2) NOT NULL,
    previous_amount DECIMAL(15,2) NOT NULL,

    agent_type VARCHAR(10) NOT NULL CHECK (agent_type IN ('buyer', 'seller')),
    action VARCHAR(20) NOT NULL CHECK (action IN ('counteroffer', 'accept', 'reject')),

    reason TEXT,
    volatility_factor DECIMAL(5,4),
    metadata JSONB DEFAULT '{}',

    created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for bargaining rounds
CREATE INDEX IF NOT EXISTS idx_bargaining_rounds_negotiation ON bargaining_rounds(negotiation_id);
CREATE INDEX IF NOT EXISTS idx_bargaining_rounds_agent ON bargaining_rounds(agent_id);
CREATE INDEX IF NOT EXISTS idx_bargaining_rounds_round_number ON bargaining_rounds(negotiation_id, round_number);

-- Add triggers for updated_at on bargaining tables
CREATE OR REPLACE FUNCTION update_bargaining_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_bargaining_negotiations_updated_at
BEFORE UPDATE ON bargaining_negotiations
FOR EACH ROW
EXECUTE FUNCTION update_bargaining_updated_at();

-- Add comments for bargaining tables
COMMENT ON TABLE bargaining_negotiations IS 'Stores bargaining negotiations between buyer and seller agents with mentee-assisted decision making';
COMMENT ON TABLE bargaining_rounds IS 'Stores individual rounds of bargaining negotiations with proposals and outcomes';

COMMENT ON COLUMN bargaining_negotiations.buyer_volatility IS 'Determines buyer agent aggressiveness (0.0 = conservative, 1.0 = aggressive)';
COMMENT ON COLUMN bargaining_negotiations.seller_volatility IS 'Determines seller agent aggressiveness (0.0 = conservative, 1.0 = aggressive)';

COMMENT ON COLUMN bargaining_rounds.volatility_factor IS 'Calculated volatility impact on the bargaining decision';
