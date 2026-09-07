package migrationbundle

import "testing"

func TestWebSocketTicketMigrationStoresOnlyScopedDigestState(t *testing.T) {
	body := migrationSQL(t, "000050_websocket_tickets.up.sql")
	requireSQLFragments(t, body,
		"CREATE TABLE websocket_tickets",
		"ticket_digest CHAR(64) NOT NULL",
		"subject VARCHAR(255) NOT NULL",
		"business_id UUID NOT NULL REFERENCES business_profiles(id)",
		"expires_at TIMESTAMPTZ NOT NULL",
		"consumed_at TIMESTAMPTZ",
		"CHECK (ticket_digest ~ '^[0-9a-f]{64}$')",
		"CHECK (expires_at > created_at)",
		"CREATE UNIQUE INDEX ux_websocket_tickets_digest",
		"WHERE consumed_at IS NULL",
	)
}

func TestWebSocketTicketDownMigrationOnlyDropsTicketState(t *testing.T) {
	body := migrationSQL(t, "000050_websocket_tickets.down.sql")
	requireSQLFragments(t, body, "DROP TABLE IF EXISTS websocket_tickets")
}
