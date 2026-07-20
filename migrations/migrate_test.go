package migrations

import (
	"sort"
	"strings"
	"testing"
)

func TestEmbeddedUpMigrationsAreCompleteAndOrdered(t *testing.T) {
	files, err := UpFiles()
	if err != nil {
		t.Fatalf("list embedded migrations: %v", err)
	}

	if len(files) != 43 {
		t.Fatalf("expected 43 up migrations, got %d", len(files))
	}
	if files[0] != "000001_users.up.sql" {
		t.Fatalf("unexpected first migration: %q", files[0])
	}
	if files[len(files)-1] != "000043_add_llm_chat_history.up.sql" {
		t.Fatalf("unexpected last migration: %q", files[len(files)-1])
	}
	if !sort.StringsAreSorted(files) {
		t.Fatal("embedded migrations are not sorted")
	}
	for _, file := range files {
		if !strings.HasSuffix(file, ".up.sql") {
			t.Fatalf("unexpected embedded migration file %q", file)
		}
	}
}
