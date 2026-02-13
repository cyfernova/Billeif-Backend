-- Down migration for bargaining agent types

-- Drop triggers
DROP TRIGGER IF EXISTS trigger_bargaining_negotiations_updated_at ON bargaining_negotiations;

-- Drop function
DROP FUNCTION IF EXISTS update_bargaining_updated_at();

-- Drop bargaining rounds table
DROP TABLE IF EXISTS bargaining_rounds CASCADE;

-- Drop bargaining negotiations table
DROP TABLE IF EXISTS bargaining_negotiations CASCADE;

-- Remove check constraint from agents table
ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_type_check;

-- Drop indexes
DROP INDEX IF EXISTS idx_agents_config;
DROP INDEX IF EXISTS idx_agents_type_active;

-- Remove comments
COMMENT ON COLUMN agents.type IS NULL;
COMMENT ON COLUMN agents.config IS NULL;
