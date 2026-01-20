-- Drop views
DROP VIEW IF EXISTS public_agents_registry CASCADE;
DROP VIEW IF EXISTS active_agents_registry CASCADE;

-- Drop triggers and functions
DROP TRIGGER IF EXISTS trigger_agent_registry_updated_at ON agent_registry CASCADE;
DROP FUNCTION IF EXISTS update_agent_registry_updated_at() CASCADE;

-- Drop tables
DROP TABLE IF EXISTS agent_discovery_stats CASCADE;
DROP TABLE IF EXISTS agent_discovery_audit CASCADE;
DROP TABLE IF EXISTS agent_registry CASCADE;
