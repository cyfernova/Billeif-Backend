ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'posted',
    ADD COLUMN IF NOT EXISTS reversal_reason TEXT,
    ADD COLUMN IF NOT EXISTS reversed_at TIMESTAMP;

ALTER TABLE payments
    DROP CONSTRAINT IF EXISTS payments_status_check;

ALTER TABLE payments
    ADD CONSTRAINT payments_status_check
    CHECK (status IN ('posted', 'reversed'));

CREATE INDEX IF NOT EXISTS idx_payments_business_status
    ON payments (business_id, status, payment_date DESC)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_journals_business_source
    ON journals (business_id, source_type, source_id)
    WHERE source_type <> '' AND source_id IS NOT NULL AND deleted_at IS NULL;
