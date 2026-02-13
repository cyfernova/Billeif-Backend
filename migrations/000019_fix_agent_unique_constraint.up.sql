-- Migration: Fix agent unique constraint to be soft-delete aware
-- The current UNIQUE(owner_id, name) prevents recreating an agent with the
-- same name after soft-deleting the original. Replace with a partial index.
ALTER TABLE agents DROP CONSTRAINT IF EXISTS unique_agent_name;
CREATE UNIQUE INDEX unique_agent_name ON agents(owner_id, name) WHERE deleted_at IS NULL;
