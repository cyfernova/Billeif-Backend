DROP INDEX IF EXISTS ux_journals_business_source;
DROP INDEX IF EXISTS idx_payments_business_status;

ALTER TABLE payments
    DROP CONSTRAINT IF EXISTS payments_status_check;

ALTER TABLE payments
    DROP COLUMN IF EXISTS reversed_at,
    DROP COLUMN IF EXISTS reversal_reason,
    DROP COLUMN IF EXISTS status;
