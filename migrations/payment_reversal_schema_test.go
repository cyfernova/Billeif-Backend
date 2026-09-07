package migrationbundle

import "testing"

func TestPaymentReversalMigrationAddsImmutableLifecycleAndJournalIdentity(t *testing.T) {
	body := migrationSQL(t, "000053_payment_reversals.up.sql")
	requireSQLFragments(t, body,
		"ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'posted'",
		"ADD COLUMN IF NOT EXISTS reversal_reason TEXT",
		"ADD COLUMN IF NOT EXISTS reversed_at TIMESTAMP",
		"CHECK (status IN ('posted', 'reversed'))",
		"CREATE INDEX IF NOT EXISTS idx_payments_business_status",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_journals_business_source",
		"ON journals (business_id, source_type, source_id)",
	)
}

func TestPaymentReversalDownMigrationOnlyDropsNewLifecycleState(t *testing.T) {
	body := migrationSQL(t, "000053_payment_reversals.down.sql")
	requireSQLFragments(t, body,
		"DROP INDEX IF EXISTS ux_journals_business_source",
		"DROP INDEX IF EXISTS idx_payments_business_status",
		"DROP CONSTRAINT IF EXISTS payments_status_check",
		"DROP COLUMN IF EXISTS reversed_at",
		"DROP COLUMN IF EXISTS reversal_reason",
		"DROP COLUMN IF EXISTS status",
	)
}
