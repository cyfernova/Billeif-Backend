package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionDeploymentUsesProtectedOIDCAndBoundedWorkloadRoles(t *testing.T) {
	workflow := readRepositoryFile(t, ".github", "workflows", "deploy.yml")
	for _, required := range []string{
		"environment: production",
		"role-session-name: github-${{ github.run_id }}",
		"role-to-assume: ${{ secrets.AWS_DEPLOY_ROLE_ARN }}",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("production deployment workflow is missing %q", required)
		}
	}

	bootstrap := readRepositoryFile(t, "infrastructure", "bootstrap", "github-actions-deploy-role.yaml")
	for _, required := range []string{
		"repo:cyfernova/Billeif-Backend:environment:production",
		"token.actions.githubusercontent.com:repository_id",
		"1122461982",
		"token.actions.githubusercontent.com:repository_owner_id",
		"236051948",
		"token.actions.githubusercontent.com:workflow",
		"Terraform Deploy",
		"token.actions.githubusercontent.com:ref",
		"refs/heads/main",
		"workload-boundary",
		"github-actions-production-role",
		"DenyDeploymentRoleSelfMutation",
		"DenyBoundaryRemoval",
	} {
		if !strings.Contains(bootstrap, required) {
			t.Errorf("bootstrap template is missing %q", required)
		}
	}

	terraformDir := filepath.Join(repositoryRoot(t), "infrastructure", "terraform")
	entries, err := os.ReadDir(terraformDir)
	if err != nil {
		t.Fatalf("read terraform directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tf") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(terraformDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		text := string(contents)
		for offset := 0; ; {
			start := strings.Index(text[offset:], `resource "aws_iam_role" `)
			if start < 0 {
				break
			}
			start += offset
			block, end := terraformBlock(t, text, start)
			offset = end
			if !strings.Contains(block, "permissions_boundary = local.workload_permissions_boundary_arn") {
				t.Errorf("%s contains an IAM workload role without the production permissions boundary: %s", entry.Name(), strings.SplitN(block, "\n", 2)[0])
			}
		}
	}
}

func terraformBlock(t *testing.T, text string, start int) (string, int) {
	t.Helper()
	open := strings.Index(text[start:], "{")
	if open < 0 {
		t.Fatal("terraform resource has no opening brace")
	}
	open += start
	depth := 0
	for index := open; index < len(text); index++ {
		switch text[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : index+1], index + 1
			}
		}
	}
	t.Fatal("terraform resource has no closing brace")
	return "", len(text)
}
