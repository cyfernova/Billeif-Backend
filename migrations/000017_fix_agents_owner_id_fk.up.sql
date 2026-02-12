-- Migration: Fix agents owner_id foreign key constraint
-- The owner_id can reference either a user_id or a business_id depending on agent type,
-- so the FK constraint to users(id) is incorrect.
ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_owner_id_fkey;
