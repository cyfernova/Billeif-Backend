package migrationbundle

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

const (
	canonicalInvoiceMigrationVersion = 44
	releasedManifestPrefixDigest     = "e7c51b8069e0785e8d2881a4eb06070c3899107ad55a72166e86e26a7979e936"
)

func TestCanonicalInvoiceMigrationIsAppendedWithoutChangingReleasedMigrations(t *testing.T) {
	manifest, err := Verify(Embedded, ".")
	if err != nil {
		t.Fatalf("Verify(Embedded) error = %v", err)
	}
	if manifest.LatestVersion != canonicalInvoiceMigrationVersion {
		t.Fatalf("latest version = %d, want %d", manifest.LatestVersion, canonicalInvoiceMigrationVersion)
	}
	if len(manifest.Entries) != canonicalInvoiceMigrationVersion*2 {
		t.Fatalf("entry count = %d, want %d", len(manifest.Entries), canonicalInvoiceMigrationVersion*2)
	}

	body, err := fs.ReadFile(Embedded, ManifestFilename)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	prefix := strings.Join(lines[:86], "\n") + "\n"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(prefix))); got != releasedManifestPrefixDigest {
		t.Fatalf("released migration manifest prefix digest = %s, want %s", got, releasedManifestPrefixDigest)
	}
	if got := manifest.Entries[86].Name; got != "000044_canonical_invoice_foundations.down.sql" {
		t.Fatalf("new down migration = %q", got)
	}
	if got := manifest.Entries[87].Name; got != "000044_canonical_invoice_foundations.up.sql" {
		t.Fatalf("new up migration = %q", got)
	}
}

func TestCanonicalInvoiceUpMigrationContainsRequiredSchemaContracts(t *testing.T) {
	body := migrationSQL(t, "000044_canonical_invoice_foundations.up.sql")
	requireSQLFragments(t, body,
		"ADD COLUMN timezone",
		"ALTER COLUMN invoice_no DROP NOT NULL",
		"ALTER COLUMN customer_id DROP NOT NULL",
		"ADD COLUMN issued_at",
		"ADD COLUMN origin",
		"seller_snapshot",
		"buyer_snapshot",
		"CREATE TABLE document_sequences",
		"CHECK (last_number BETWEEN 0 AND 999999)",
		"CREATE TABLE api_idempotency_keys",
		"request_hash",
		"CREATE TABLE outbox_events",
		"lease_owner",
		"lease_expires_at",
		"ADD COLUMN kind",
		"source_invoice_version",
		"object_key",
		"idx_document_render_jobs_final_invoice_version",
		"ADD COLUMN render_job_id",
		"idx_bargaining_rounds_negotiation_round_unique",
		"idx_invoices_business_created_cursor",
	)
}

func TestCanonicalInvoiceDownMigrationOnlyReversesNewSlice(t *testing.T) {
	body := migrationSQL(t, "000044_canonical_invoice_foundations.down.sql")
	requireSQLFragments(t, body,
		"DROP TABLE IF EXISTS outbox_events",
		"DROP TABLE IF EXISTS api_idempotency_keys",
		"DROP TABLE IF EXISTS document_sequences",
		"DROP COLUMN IF EXISTS timezone",
		"DROP COLUMN IF EXISTS origin",
		"ALTER COLUMN customer_id SET NOT NULL",
		"ALTER COLUMN invoice_no SET NOT NULL",
	)
	for _, forbidden := range []string{
		"DROP TABLE invoices",
		"DROP TABLE document_render_jobs",
		"DROP TABLE email_deliveries",
		"DROP TABLE bargaining_rounds",
	} {
		if strings.Contains(strings.ToUpper(body), strings.ToUpper(forbidden)) {
			t.Fatalf("down migration contains forbidden predecessor change %q", forbidden)
		}
	}
}

func migrationSQL(t *testing.T, name string) string {
	t.Helper()
	body, err := fs.ReadFile(Embedded, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func requireSQLFragments(t *testing.T, body string, fragments ...string) {
	t.Helper()
	upper := strings.ToUpper(body)
	for _, fragment := range fragments {
		if !strings.Contains(upper, strings.ToUpper(fragment)) {
			t.Errorf("migration missing SQL contract %q", fragment)
		}
	}
}
