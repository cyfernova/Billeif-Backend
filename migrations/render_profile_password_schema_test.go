package migrationbundle

import (
	"strings"
	"testing"
)

func TestRenderProfilePasswordCiphertextMigrationPreservesLegacyCompatibility(t *testing.T) {
	up := migrationSQL(t, "000047_encrypt_render_profile_passwords.up.sql")
	requireSQLFragments(t, up,
		"ALTER TABLE render_profiles",
		"ADD COLUMN password_ciphertext TEXT",
		"COMMENT ON COLUMN render_profiles.password",
		"legacy plaintext compatibility",
		"COMMENT ON COLUMN render_profiles.password_ciphertext",
		"AES-256-GCM",
	)
	for _, forbidden := range []string{
		"SET password_ciphertext = password",
		"DROP COLUMN password",
		"RENAME COLUMN password",
	} {
		if strings.Contains(strings.ToUpper(up), strings.ToUpper(forbidden)) {
			t.Fatalf("up migration contains unsafe legacy transformation %q", forbidden)
		}
	}

	down := migrationSQL(t, "000047_encrypt_render_profile_passwords.down.sql")
	requireSQLFragments(t, down,
		"DO $$",
		"IF EXISTS",
		"password_ciphertext IS NOT NULL",
		"RAISE EXCEPTION",
		"ALTER TABLE render_profiles",
		"DROP COLUMN password_ciphertext",
	)
	if strings.Index(strings.ToUpper(down), "RAISE EXCEPTION") >
		strings.Index(strings.ToUpper(down), "DROP COLUMN PASSWORD_CIPHERTEXT") {
		t.Fatal("down migration drops ciphertext before its fail-stop rollback guard")
	}
	for _, forbidden := range []string{
		"SET password = password_ciphertext",
		"RENAME COLUMN password_ciphertext TO password",
	} {
		if strings.Contains(strings.ToUpper(down), strings.ToUpper(forbidden)) {
			t.Fatalf("down migration would persist ciphertext as an ordinary password: %q", forbidden)
		}
	}
}
