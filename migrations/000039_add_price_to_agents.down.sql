-- Remove price column from agents table
ALTER TABLE agents DROP COLUMN IF EXISTS price;

-- Drop the index
DROP INDEX IF EXISTS idx_agents_price;
