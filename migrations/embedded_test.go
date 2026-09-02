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
	if len(manifest.Entries) != 120 {
		t.Fatalf("embedded entry count = %d, want 120", len(manifest.Entries))
	}
	if manifest.LatestVersion != 60 {
		t.Fatalf("embedded latest version = %d, want 60", manifest.LatestVersion)
	}

	up, err := fs.Glob(Embedded, "*.up.sql")
	if err != nil {
		t.Fatalf("glob embedded up migrations: %v", err)
	}
	down, err := fs.Glob(Embedded, "*.down.sql")
	if err != nil {
		t.Fatalf("glob embedded down migrations: %v", err)
	}
	if len(up) != 60 || len(down) != 60 {
		t.Fatalf("embedded migration pairs = %d up/%d down, want 60/60", len(up), len(down))
	}
}
