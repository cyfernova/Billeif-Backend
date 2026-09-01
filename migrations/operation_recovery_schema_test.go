package migrationbundle

import "testing"

func TestOperationRecoveryMigrationAddsTenantBoundSanitizedCommandAudit(t *testing.T) {
	body := migrationSQL(t, "000056_operation_recovery.up.sql")
	requireSQLFragments(t, body,
		"CREATE TABLE operation_recovery_commands",
		"business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE",
		"operation_type VARCHAR(64) NOT NULL",
		"operation_id UUID NOT NULL",
		"actor_subject VARCHAR(255) NOT NULL",
		"principal_kind VARCHAR(16) NOT NULL",
		"reason VARCHAR(500) NOT NULL",
		"idempotency_key VARCHAR(180) NOT NULL",
		"request_hash CHAR(64) NOT NULL",
		"operation_version VARCHAR(64) NOT NULL",
		"correlation_id UUID NOT NULL",
		"result_code VARCHAR(80) NOT NULL",
		"UNIQUE (business_id, actor_subject, action, idempotency_key)",
		"CREATE UNIQUE INDEX ux_operation_recovery_effect",
		"WHERE result_code = 'accepted'",
	)
	for _, forbidden := range []string{
		"raw_payload", "raw_error", "provider_reference", "queue_message_id", "account_id", "secret_id",
	} {
		if containsSQLFragment(body, forbidden) {
			t.Fatalf("operation recovery audit must not persist %s", forbidden)
		}
	}
}

func TestOperationRecoveryDownMigrationIsPaired(t *testing.T) {
	body := migrationSQL(t, "000056_operation_recovery.down.sql")
	requireSQLFragments(t, body,
		"DROP INDEX IF EXISTS ux_operation_recovery_effect",
		"DROP TABLE IF EXISTS operation_recovery_commands",
	)
}
