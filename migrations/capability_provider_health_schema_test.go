package migrationbundle

import (
	"strings"
	"testing"
)

func TestCapabilityProviderHealthMigrationCreatesOneSanitizedGSTSnapshotPerBusiness(t *testing.T) {
	body := migrationSQL(t, "000054_capability_provider_health_snapshots.up.sql")
	requireSQLFragments(t, body,
		"CREATE TABLE capability_provider_health_snapshots",
		"business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE",
		"provider_key VARCHAR(64) NOT NULL",
		"status VARCHAR(32) NOT NULL",
		"observed_at TIMESTAMPTZ NOT NULL",
		"fresh_until TIMESTAMPTZ NOT NULL",
		"retry_at TIMESTAMPTZ",
		"customer_code VARCHAR(64) NOT NULL DEFAULT ''",
		"PRIMARY KEY (business_id, provider_key)",
		"provider_key = 'gst_provider'",
		"status IN ('healthy', 'degraded', 'unavailable')",
		"status = 'healthy' AND customer_code = ''",
		"status = 'degraded' AND customer_code IN ('provider_degraded', 'provider_rate_limited')",
		"status = 'unavailable' AND customer_code = 'provider_unavailable'",
		"fresh_until >= observed_at",
	)
	for _, forbidden := range []string{"credential", "account_id", "operator_detail", "raw_error", "response_body"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("snapshot migration contains sensitive field %q", forbidden)
		}
	}
}

func TestCapabilityProviderHealthDownMigrationOnlyDropsDerivedSnapshotState(t *testing.T) {
	body := migrationSQL(t, "000054_capability_provider_health_snapshots.down.sql")
	requireSQLFragments(t, body, "DROP TABLE IF EXISTS capability_provider_health_snapshots")
	if strings.Contains(strings.ToUpper(body), "ALTER TABLE") {
		t.Fatal("down migration must not modify predecessor tables")
	}
}
