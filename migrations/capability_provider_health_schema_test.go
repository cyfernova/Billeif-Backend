package migrationbundle

import (
	"strings"
	"testing"
)

func TestCapabilityProviderHealthMigrationCreatesOneSanitizedGSTSnapshotPerBusiness(t *testing.T) {
	body := migrationSQL(t, "000054_capability_provider_health_snapshots.up.sql")
	requireSQLFragments(t, body,
		"ALTER TABLE gst_integration_accounts",
		"credential_revision BIGINT NOT NULL DEFAULT 1",
		"UNIQUE (id, business_id, credential_revision)",
		"CREATE TABLE capability_provider_health_snapshots",
		"business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE",
		"provider_key VARCHAR(64) NOT NULL",
		"integration_account_id UUID NOT NULL",
		"credential_revision BIGINT NOT NULL",
		"observation_revision BIGINT NOT NULL DEFAULT 1",
		"status VARCHAR(32) NOT NULL",
		"observed_at TIMESTAMPTZ NOT NULL",
		"fresh_until TIMESTAMPTZ NOT NULL",
		"retry_at TIMESTAMPTZ",
		"customer_code VARCHAR(64) NOT NULL DEFAULT ''",
		"PRIMARY KEY (business_id, provider_key)",
		"FOREIGN KEY (integration_account_id, business_id, credential_revision)",
		"REFERENCES gst_integration_accounts(id, business_id, credential_revision) ON DELETE CASCADE",
		"DEFERRABLE INITIALLY DEFERRED",
		"provider_key = 'gst_provider'",
		"status IN ('healthy', 'degraded', 'unavailable')",
		"status = 'healthy' AND customer_code = ''",
		"status = 'degraded' AND customer_code IN ('provider_degraded', 'provider_rate_limited')",
		"status = 'unavailable' AND customer_code = 'provider_unavailable'",
		"fresh_until >= observed_at",
	)
	for _, forbidden := range []string{"encrypted_credentials", "credential_value", "operator_detail", "raw_error", "response_body"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("snapshot migration contains sensitive field %q", forbidden)
		}
	}
}

func TestCapabilityProviderHealthDownMigrationRevertsSnapshotAndRevisionState(t *testing.T) {
	body := migrationSQL(t, "000054_capability_provider_health_snapshots.down.sql")
	requireSQLFragments(t, body,
		"DROP TABLE IF EXISTS capability_provider_health_snapshots",
		"DROP CONSTRAINT IF EXISTS gst_integration_accounts_id_business_revision_unique",
		"DROP COLUMN IF EXISTS credential_revision",
	)
}
