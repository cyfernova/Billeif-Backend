package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMigrationBundleHasOneRootSourceAndNoImperativeTerraformRunner(t *testing.T) {
	rootMigrations, err := filepath.Glob("../migrations/*.sql")
	if err != nil {
		t.Fatalf("glob root migrations: %v", err)
	}
	if len(rootMigrations) != 100 {
		t.Fatalf("root migration SQL count = %d, want 100", len(rootMigrations))
	}

	var duplicateSQL []string
	err = filepath.WalkDir("../infrastructure", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			duplicateSQL = append(duplicateSQL, name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk infrastructure SQL: %v", err)
	}
	if len(duplicateSQL) != 0 {
		t.Fatalf("migration SQL must exist only in the root bundle; duplicates: %v", duplicateSQL)
	}

	terraform := readTerraformSources(t)
	migrationTerraform, err := os.ReadFile("../infrastructure/terraform/migrations.tf")
	if err != nil {
		t.Fatalf("read migration Terraform: %v", err)
	}
	if match := regexp.MustCompile(`(?m)provisioner\s+"local-exec"`).FindString(string(migrationTerraform)); match != "" {
		t.Fatalf("imperative migration runner found: %q", match)
	}
	for _, forbidden := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)resource\s+"null_resource"\s+"[^"]*migrat`),
		regexp.MustCompile(`\btimestamp\s*\(`),
	} {
		if match := forbidden.FindString(terraform); match != "" {
			t.Fatalf("imperative or always-run migration trigger found: %q", match)
		}
	}
	for _, required := range []string{
		`resource "aws_lambda_invocation" "database_migrations"`,
		`resource "aws_lambda_function" "database_migrator"`,
		`variable "enable_application"`,
		`default     = false`,
	} {
		if !strings.Contains(terraform, required) {
			t.Fatalf("migration Terraform is missing %q", required)
		}
	}
}

func TestMigrationBuildUsesStrippedARM64ArtifactAndVerifiesChecksums(t *testing.T) {
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	source := string(makefile)
	for _, required := range []string{
		"build-lambda-migrator:",
		"GOOS=linux GOARCH=arm64 CGO_ENABLED=0",
		"go build -trimpath",
		`-ldflags="-s -w -buildid="`,
		"package-lambda-migrator:",
		"TZ=UTC zip -q -X -j",
		"migration-manifest-verify:",
		"shasum -a 256 -c manifest.sha256",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("migration build/checksum target is missing %q", required)
		}
	}
}
