-- Restore the original FK constraint on agents.owner_id (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'agents_owner_id_fkey' AND table_name = 'agents'
    ) THEN
        ALTER TABLE agents ADD CONSTRAINT agents_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
END $$;
