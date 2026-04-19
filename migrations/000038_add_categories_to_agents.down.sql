-- Remove categories column from agents table
ALTER TABLE agents DROP COLUMN IF EXISTS categories;

-- Drop the index
DROP INDEX IF EXISTS idx_agents_categories;
