package migrationbundle

import "testing"

func TestSubscriptionQuotaMigrationCreatesAtomicUsageIdentityAndBackfill(t *testing.T) {
	body := migrationSQL(t, "000052_subscription_quota_usage.up.sql")
	requireSQLFragments(t, body,
		"CREATE TABLE IF NOT EXISTS subscription_quota_usage",
		"business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE",
		"PRIMARY KEY (business_id, feature_key, period_start)",
		"CHECK (used_value >= 0)",
		"FROM gst_submission_jobs",
		"WHEN 'generate_einvoice' THEN 'einvoice'",
		"WHEN 'generate_ewaybill' THEN 'ewaybill'",
		"ON CONFLICT (business_id, feature_key, period_start)",
	)
}

func TestSubscriptionQuotaDownMigrationOnlyDropsUsageState(t *testing.T) {
	body := migrationSQL(t, "000052_subscription_quota_usage.down.sql")
	requireSQLFragments(t, body, "DROP TABLE IF EXISTS subscription_quota_usage")
}
