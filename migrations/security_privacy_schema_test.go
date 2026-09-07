package migrationbundle

import "testing"

func TestSecurityPrivacyMigrationContainsScopedOneTimeAndRetentionContracts(t *testing.T) {
	body := migrationSQL(t, "000057_security_privacy_foundations.up.sql")
	requireSQLFragments(t, body,
		"CREATE TABLE security_step_up_grants",
		"token_hash CHAR(64) NOT NULL UNIQUE",
		"command_hash CHAR(64) NOT NULL",
		"consumed_at TIMESTAMPTZ",
		"CREATE TABLE security_pending_uploads",
		"checksum_sha256",
		"status IN ('pending', 'quarantined', 'clean', 'rejected', 'deleted')",
		"CREATE TABLE privacy_requests",
		"UNIQUE (business_id, subject, kind, idempotency_key)",
		"status = 'retention_hold'",
		"CREATE TABLE security_audit_events",
	)
	down := migrationSQL(t, "000057_security_privacy_foundations.down.sql")
	requireSQLFragments(t, down,
		"DROP TABLE IF EXISTS privacy_requests",
		"DROP TABLE IF EXISTS security_pending_uploads",
		"DROP TABLE IF EXISTS security_audit_events",
		"DROP TABLE IF EXISTS security_step_up_grants",
	)
}
