package migrationbundle

import (
	"io/fs"
	"testing"
)

func TestEmbeddedBundleContainsEveryRootNumberedMigration(t *testing.T) {
	manifest, err := Verify(Embedded, ".")
	if err != nil {
		t.Fatalf("Verify(Embedded) error = %v", err)
	}
	if len(manifest.Entries) != 90 {
		t.Fatalf("embedded entry count = %d, want 90", len(manifest.Entries))
	}
	if manifest.LatestVersion != 45 {
		t.Fatalf("embedded latest version = %d, want 45", manifest.LatestVersion)
	}

	up, err := fs.Glob(Embedded, "*.up.sql")
	if err != nil {
		t.Fatalf("glob embedded up migrations: %v", err)
	}
	down, err := fs.Glob(Embedded, "*.down.sql")
	if err != nil {
		t.Fatalf("glob embedded down migrations: %v", err)
	}
	if len(up) != 45 || len(down) != 45 {
		t.Fatalf("embedded migration pairs = %d up/%d down, want 45/45", len(up), len(down))
	}
}
