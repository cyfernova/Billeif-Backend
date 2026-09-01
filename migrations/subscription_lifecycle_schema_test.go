package migrationbundle

import "testing"

func TestSubscriptionLifecycleMigrationExpandsAndClassifiesLegacyRows(t *testing.T) {
	body := migrationSQL(t, "000055_subscription_lifecycle.up.sql")
	requireSQLFragments(t, body,
		"ADD COLUMN IF NOT EXISTS billing_mode VARCHAR(32) NOT NULL DEFAULT 'free'",
		"ADD COLUMN IF NOT EXISTS provider_mode VARCHAR(16)",
		"ADD COLUMN IF NOT EXISTS provider_customer_id VARCHAR(160)",
		"ADD COLUMN IF NOT EXISTS provider_subscription_id VARCHAR(160)",
		"ADD COLUMN IF NOT EXISTS provider_plan_id VARCHAR(160)",
		"ADD COLUMN IF NOT EXISTS period_start TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS period_end TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS next_renewal_at TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS grace_deadline TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE",
		"ADD COLUMN IF NOT EXISTS cancellation_effective_at TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS pending_plan_id VARCHAR(80)",
		"ADD COLUMN IF NOT EXISTS pending_provider_plan_id VARCHAR(160)",
		"ADD COLUMN IF NOT EXISTS pending_plan_effective_at TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS last_provider_event_at TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS last_provider_paid_count BIGINT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS reconciliation_code VARCHAR(80)",
		"WHEN plan = 'free' THEN 'free'",
		"ELSE 'legacy_one_time'",
		"SET status = 'cancelled' WHERE status = 'canceled'",
		"'pending_payment', 'active', 'renewal_pending', 'past_due', 'grace_period'",
		"'cancellation_scheduled', 'cancelled', 'expired', 'suspended', 'reconciliation_required'",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_subscriptions_provider_identity",
		"WHERE provider_subscription_id IS NOT NULL",
	)
}

func TestSubscriptionLifecycleMigrationAddsBillingAuditCommandsAndSanitizedInbox(t *testing.T) {
	body := migrationSQL(t, "000055_subscription_lifecycle.up.sql")
	requireSQLFragments(t, body,
		"CREATE TABLE subscription_billing_records",
		"amount_minor BIGINT NOT NULL",
		"receipt_reference VARCHAR(80) NOT NULL",
		"quota_period_start TIMESTAMPTZ",
		"quota_period_end TIMESTAMPTZ",
		"CREATE TABLE subscription_audit_records",
		"action VARCHAR(80) NOT NULL",
		"sanitized_code VARCHAR(80) NOT NULL",
		"CREATE TABLE subscription_commands",
		"request_hash CHAR(64) NOT NULL",
		"UNIQUE (business_id, actor_user_id, action, idempotency_key)",
		"ADD COLUMN IF NOT EXISTS provider_mode VARCHAR(16)",
		"ADD COLUMN IF NOT EXISTS payload_hash CHAR(64)",
		"ADD COLUMN IF NOT EXISTS signature_verified BOOLEAN NOT NULL DEFAULT FALSE",
		"ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS provider_occurred_at TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS processing_status VARCHAR(32)",
		"ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS sanitized_error_code VARCHAR(80)",
		"ADD COLUMN IF NOT EXISTS business_id UUID",
		"ADD COLUMN IF NOT EXISTS subscription_id UUID",
		"ADD COLUMN IF NOT EXISTS replay_count INTEGER NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS last_replayed_at TIMESTAMPTZ",
		"UNIQUE (provider_mode, razorpay_event_id)",
	)
	for _, forbidden := range []string{"raw_payload", "raw_body", "webhook_signature"} {
		if containsSQLFragment(body, forbidden) {
			t.Fatalf("migration must not persist %s", forbidden)
		}
	}
}

func TestSubscriptionLifecycleDownMigrationRestoresLegacyCompatibility(t *testing.T) {
	body := migrationSQL(t, "000055_subscription_lifecycle.down.sql")
	requireSQLFragments(t, body,
		"DROP TABLE IF EXISTS subscription_commands",
		"DROP TABLE IF EXISTS subscription_audit_records",
		"DROP TABLE IF EXISTS subscription_billing_records",
		"SET status = 'canceled' WHERE status = 'cancelled'",
		"DROP COLUMN IF EXISTS provider_subscription_id",
		"DROP COLUMN IF EXISTS billing_mode",
	)
}

func containsSQLFragment(body, fragment string) bool {
	return len(fragment) > 0 && len(body) >= len(fragment) && stringContains(body, fragment)
}

func stringContains(body, fragment string) bool {
	for index := 0; index+len(fragment) <= len(body); index++ {
		if body[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
