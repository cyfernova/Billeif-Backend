-- Add product_ids column to agent_registry
ALTER TABLE agent_registry ADD COLUMN IF NOT EXISTS product_ids TEXT[] DEFAULT '{}';

-- Create index for product_ids searches
CREATE INDEX IF NOT EXISTS idx_agent_registry_product_ids ON agent_registry USING GIN(product_ids)
WHERE product_ids IS NOT NULL AND cardinality(product_ids) > 0;
