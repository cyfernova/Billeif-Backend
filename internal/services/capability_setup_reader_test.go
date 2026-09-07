package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDBCapabilityBusinessSetupReaderIsTenantScopedAndSecretSafe(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:capability-setup?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE business_profiles (id TEXT PRIMARY KEY, gst_registered BOOLEAN, gstin TEXT, deleted_at DATETIME)`,
		`CREATE TABLE gst_integration_accounts (id TEXT PRIMARY KEY, business_id TEXT, provider TEXT, encrypted_credentials TEXT, deleted_at DATETIME)`,
		`CREATE TABLE whatsapp_configs (id TEXT PRIMARY KEY, business_id TEXT, phone_number_id TEXT, access_token TEXT, enabled BOOLEAN, deleted_at DATETIME)`,
		`CREATE TABLE email_accounts (id TEXT PRIMARY KEY, business_id TEXT, status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE storefronts (id TEXT PRIMARY KEY, business_id TEXT, allow_online_payment BOOLEAN, deleted_at DATETIME)`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO business_profiles (id, gst_registered, gstin) VALUES ('biz-a', TRUE, '29ABCDE1234F1Z5'), ('biz-b', TRUE, '27ABCDE1234F1Z7')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO gst_integration_accounts VALUES ('gst-a', 'biz-a', 'cleartax', 'ciphertext-secret', NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO whatsapp_configs VALUES ('wa-a', 'biz-a', 'phone-account-secret', 'token-secret', TRUE, NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO email_accounts VALUES ('email-a', 'biz-a', 'connected', NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO storefronts VALUES ('store-a', 'biz-a', TRUE, NULL)`).Error)

	reader := NewDBCapabilityBusinessSetupReader(db)
	setupA, err := reader.ReadCapabilityBusinessSetup(context.Background(), "biz-a")
	require.NoError(t, err)
	require.Equal(t, CapabilityBusinessSetup{
		BusinessExists: true, GST: true, WhatsApp: true, Email: true, Voice: true, StorefrontPayments: true,
	}, setupA)

	setupB, err := reader.ReadCapabilityBusinessSetup(context.Background(), "biz-b")
	require.NoError(t, err)
	require.True(t, setupB.BusinessExists)
	require.False(t, setupB.GST, "another business's integration account must not satisfy setup")
	require.False(t, setupB.WhatsApp)
	require.False(t, setupB.Email)
	require.False(t, setupB.StorefrontPayments)
}
