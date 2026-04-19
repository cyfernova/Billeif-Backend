-- Add price column to agents table
ALTER TABLE agents ADD COLUMN IF NOT EXISTS price FLOAT DEFAULT 0;

-- Create index for faster price queries
CREATE INDEX IF NOT EXISTS idx_agents_price ON agents (price);
