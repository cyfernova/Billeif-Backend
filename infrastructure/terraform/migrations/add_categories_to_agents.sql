ALTER TABLE agents ADD COLUMN IF NOT EXISTS categories TEXT[] DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_agents_categories ON agents USING GIN (categories);