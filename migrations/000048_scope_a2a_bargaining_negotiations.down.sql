DROP TRIGGER IF EXISTS trigger_bargaining_scope_immutable
    ON bargaining_negotiations;

DROP FUNCTION IF EXISTS prevent_bargaining_scope_mutation();

DROP TRIGGER IF EXISTS trigger_populate_bargaining_business_scope
    ON bargaining_negotiations;

DROP FUNCTION IF EXISTS populate_bargaining_business_scope();

UPDATE bargaining_negotiations
SET status = 'expired',
    completed_at = COALESCE(completed_at, NOW())
WHERE status = 'stopped';

DROP INDEX IF EXISTS idx_bargaining_negotiations_owner_id;
DROP INDEX IF EXISTS idx_bargaining_negotiations_owner_session;
DROP INDEX IF EXISTS idx_bargaining_negotiations_session_unique;

ALTER TABLE bargaining_negotiations
    DROP CONSTRAINT IF EXISTS bargaining_negotiations_business_id_fkey;

ALTER TABLE bargaining_negotiations
    DROP COLUMN IF EXISTS business_id;
