package migrationbundle

import "testing"

func TestDurableBulkImportsMigrationDefinesTwoPhaseAndRecoveryContracts(t *testing.T) {
	body := migrationSQL(t, "000059_durable_bulk_imports.up.sql")
	requireSQLFragments(t, body,
		"ADD COLUMN upload_id UUID REFERENCES security_pending_uploads(id) ON DELETE RESTRICT",
		"ADD COLUMN validation_version INT NOT NULL DEFAULT 0",
		"ADD COLUMN commit_command_id UUID",
		"ADD COLUMN cancel_requested BOOLEAN NOT NULL DEFAULT FALSE",
		"ADD COLUMN lease_expires_at TIMESTAMPTZ",
		"ADD COLUMN retain_until TIMESTAMPTZ",
		"CREATE UNIQUE INDEX uq_bulk_jobs_import_commit_command",
		"CREATE UNIQUE INDEX uq_bulk_jobs_import_upload",
		"ADD COLUMN input_hash CHAR(64)",
		"ADD COLUMN idempotency_key VARCHAR(180)",
		"CREATE UNIQUE INDEX uq_bulk_job_rows_idempotency",
		"ADD COLUMN status VARCHAR(30) NOT NULL DEFAULT 'pending'",
		"CREATE UNIQUE INDEX uq_bulk_job_artifacts_job_type",
		"legacy_import_requires_revalidation",
		"AND status IN ('pending', 'queued', 'processing')",
	)

	down := migrationSQL(t, "000059_durable_bulk_imports.down.sql")
	requireSQLFragments(t, down,
		"DROP INDEX IF EXISTS uq_bulk_job_rows_idempotency",
		"DROP COLUMN IF EXISTS commit_command_id",
		"DROP COLUMN IF EXISTS upload_id",
		"DROP INDEX IF EXISTS uq_bulk_jobs_import_upload",
		"DROP INDEX IF EXISTS uq_bulk_job_artifacts_job_type",
	)
}
