-- Remove product_ids column from agent_registry
ALTER TABLE agent_registry DROP COLUMN IF EXISTS product_ids;
