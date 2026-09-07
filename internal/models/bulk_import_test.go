package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBulkImportArtifactsHideStorageCoordinates(t *testing.T) {
	artifact, err := json.Marshal(BulkJobArtifact{
		FileKey:  "bulk-imports/business/user/private.csv",
		Metadata: `{"bucket":"private"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(artifact)
	if strings.Contains(body, "bulk-imports/") || strings.Contains(body, "bucket") || strings.Contains(body, "file_key") {
		t.Fatalf("storage coordinates leaked in JSON: %s", body)
	}
}

func TestBulkImportLifecycleStatesAreDistinct(t *testing.T) {
	states := []string{
		BulkJobStatusValidating,
		BulkJobStatusValidated,
		BulkJobStatusCommitQueued,
		BulkJobStatusCommitting,
		BulkJobStatusCompleted,
		BulkJobStatusFailed,
		BulkJobStatusCanceled,
		BulkJobStatusExpired,
	}
	seen := make(map[string]struct{}, len(states))
	for _, state := range states {
		if state == "" {
			t.Fatal("bulk import lifecycle state is empty")
		}
		if _, exists := seen[state]; exists {
			t.Fatalf("duplicate bulk import lifecycle state %q", state)
		}
		seen[state] = struct{}{}
	}
}
