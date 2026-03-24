ALTER TABLE marketplace_products
ADD COLUMN IF NOT EXISTS reserved_inventory_count INT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS procurement_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    shopping_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    intent TEXT NOT NULL,
    quantity INT NOT NULL DEFAULT 1,
    max_budget DECIMAL(15,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    payment_terms JSONB NOT NULL DEFAULT '[]',
    auto_buy BOOLEAN NOT NULL DEFAULT TRUE,
    max_sellers INT NOT NULL DEFAULT 5,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    idempotency_key VARCHAR(255),
    intent_mandate_id UUID,
    winning_candidate_id UUID,
    winning_negotiation_id UUID,
    cart_mandate_id UUID,
    payment_mandate_id UUID,
    order_id UUID,
    failure_reason TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    cancelled_at TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_procurement_runs_idempotency
ON procurement_runs(user_id, shopping_agent_id, idempotency_key)
WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_procurement_runs_user ON procurement_runs(user_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_procurement_runs_shopping_agent ON procurement_runs(shopping_agent_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_procurement_runs_status ON procurement_runs(status, deleted_at);

CREATE TABLE IF NOT EXISTS procurement_candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    procurement_run_id UUID NOT NULL REFERENCES procurement_runs(id) ON DELETE CASCADE,
    merchant_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    marketplace_product_id UUID NOT NULL REFERENCES marketplace_products(id) ON DELETE CASCADE,
    match_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    eligibility_status VARCHAR(50) NOT NULL DEFAULT 'eligible',
    negotiation_id UUID,
    final_amount DECIMAL(15,2),
    terminal_reason TEXT,
    selection_reason TEXT,
    reserved_quantity INT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_procurement_candidates_run ON procurement_candidates(procurement_run_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_procurement_candidates_merchant ON procurement_candidates(merchant_agent_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_procurement_candidates_status ON procurement_candidates(eligibility_status, deleted_at);

CREATE OR REPLACE FUNCTION update_procurement_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_procurement_runs_updated_at ON procurement_runs;
CREATE TRIGGER trigger_procurement_runs_updated_at
BEFORE UPDATE ON procurement_runs
FOR EACH ROW
EXECUTE FUNCTION update_procurement_updated_at();

DROP TRIGGER IF EXISTS trigger_procurement_candidates_updated_at ON procurement_candidates;
CREATE TRIGGER trigger_procurement_candidates_updated_at
BEFORE UPDATE ON procurement_candidates
FOR EACH ROW
EXECUTE FUNCTION update_procurement_updated_at();
