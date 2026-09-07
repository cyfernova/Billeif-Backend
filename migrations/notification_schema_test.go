package migrationbundle

import "testing"

func TestNotificationMigrationEnforcesTenantUserAndEventIdentity(t *testing.T) {
	body := migrationSQL(t, "000051_notifications.up.sql")
	requireSQLFragments(t, body,
		"CREATE TABLE notifications",
		"business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE",
		"user_id VARCHAR(255) NOT NULL",
		"source_event_key VARCHAR(255) NOT NULL",
		"UNIQUE (business_id, user_id, source_event_key)",
		"idx_notifications_business_user_created",
		"ON notifications (business_id, user_id, created_at DESC, id DESC)",
		"idx_notifications_business_user_unread",
		"WHERE read_at IS NULL",
	)
}

func TestNotificationDownMigrationOnlyDropsInboxState(t *testing.T) {
	body := migrationSQL(t, "000051_notifications.down.sql")
	requireSQLFragments(t, body, "DROP TABLE IF EXISTS notifications")
}
