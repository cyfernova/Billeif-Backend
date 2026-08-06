package tests

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestNoLegacyRealtimeProviderInActiveProjectFiles(t *testing.T) {
	root := repositoryRoot(t)
	legacyProvider := strings.Join([]string{"deep", "gram"}, "")
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(legacyProvider))
	projectFiles := activeProjectFiles(t, root)
	var hits []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if excludedLegacyPolicyPath(filepath.ToSlash(relativePath), entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !projectFiles[filepath.ToSlash(relativePath)] {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(contents, 0) >= 0 {
			return nil
		}
		if pattern.Match(contents) {
			hits = append(hits, filepath.ToSlash(relativePath))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk project files: %v", err)
	}
	if len(hits) > 0 {
		sort.Strings(hits)
		t.Fatalf("legacy realtime provider references remain in active project files:\n%s", strings.Join(hits, "\n"))
	}
}

func activeProjectFiles(t *testing.T, root string) map[string]bool {
	t.Helper()
	command := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list active project files: %v", err)
	}
	files := make(map[string]bool)
	for _, path := range bytes.Split(output, []byte{0}) {
		if len(path) > 0 {
			files[string(path)] = true
		}
	}
	return files
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %q does not contain go.mod: %v", root, err)
	}
	return root
}

func excludedLegacyPolicyPath(path string, isDirectory bool) bool {
	if path == "." {
		return false
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		switch part {
		case ".git", "vendor", "node_modules", ".cache", ".terraform", "build", "dist", "coverage", "tmp", "bin":
			return true
		}
	}

	if isDirectory {
		return false
	}
	if strings.HasPrefix(path, "migrations/") && strings.HasSuffix(path, ".sql") {
		return true
	}
	if path == "tests/no_deepgram_policy_test.go" {
		return true
	}
	for _, historicalDocument := range []string{
		"docs/adr/ADR-agentcore-sarvam-realtime-voice.md",
		"docs/architecture/voice-migration-inventory.md",
		"docs/superpowers/plans/2026-08-06-agentcore-sarvam-voice-migration.md",
	} {
		if path == historicalDocument {
			return true
		}
	}
	return false
}
