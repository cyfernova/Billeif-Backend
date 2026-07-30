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
	if len(manifest.Entries) != 92 {
		t.Fatalf("embedded entry count = %d, want 92", len(manifest.Entries))
	}
	if manifest.LatestVersion != 46 {
		t.Fatalf("embedded latest version = %d, want 46", manifest.LatestVersion)
	}

	up, err := fs.Glob(Embedded, "*.up.sql")
	if err != nil {
		t.Fatalf("glob embedded up migrations: %v", err)
	}
	down, err := fs.Glob(Embedded, "*.down.sql")
	if err != nil {
		t.Fatalf("glob embedded down migrations: %v", err)
	}
	if len(up) != 46 || len(down) != 46 {
		t.Fatalf("embedded migration pairs = %d up/%d down, want 46/46", len(up), len(down))
	}
}
