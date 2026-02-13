-- Revert: restore the original UNIQUE constraint
DROP INDEX IF EXISTS unique_agent_name;
ALTER TABLE agents ADD CONSTRAINT unique_agent_name UNIQUE(owner_id, name);
