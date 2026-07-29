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
	if len(manifest.Entries) != 86 {
		t.Fatalf("embedded entry count = %d, want 86", len(manifest.Entries))
	}
	if manifest.LatestVersion != 43 {
		t.Fatalf("embedded latest version = %d, want 43", manifest.LatestVersion)
	}

	up, err := fs.Glob(Embedded, "*.up.sql")
	if err != nil {
		t.Fatalf("glob embedded up migrations: %v", err)
	}
	down, err := fs.Glob(Embedded, "*.down.sql")
	if err != nil {
		t.Fatalf("glob embedded down migrations: %v", err)
	}
	if len(up) != 43 || len(down) != 43 {
		t.Fatalf("embedded migration pairs = %d up/%d down, want 43/43", len(up), len(down))
	}
}
