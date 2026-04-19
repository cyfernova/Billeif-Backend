-- Add categories column to agents table (array of category names)
ALTER TABLE agents ADD COLUMN categories JSONB NOT NULL DEFAULT '[]';

-- Create index for faster category queries
CREATE INDEX idx_agents_categories ON agents USING GIN ((categories::jsonb));
