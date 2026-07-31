package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDeployWorkflowLaunchSafetyPolicy(t *testing.T) {
	workflow := loadWorkflow(t)
	root := documentRoot(t, workflow)

	triggers := requiredMap(t, root, "on")
	requiredMap(t, triggers, "pull_request")
	push := requiredMap(t, triggers, "push")
	requireBranches(t, push, "main", "master")

	rootPermissions := requiredMap(t, root, "permissions")
	requireScalar(t, rootPermissions, "contents", "read")
	if mappingValue(rootPermissions, "id-token") != nil {
		t.Fatal("workflow-level permissions must not grant id-token: write")
	}

	concurrency := requiredMap(t, root, "concurrency")
	requireScalar(t, concurrency, "cancel-in-progress", "false")

	if env := mappingValue(root, "env"); env != nil && containsSecretTerraformVariable(env) {
		t.Fatal("workflow/global environment must not expose secret Terraform variables")
	}

	jobs := requiredMap(t, root, "jobs")
	verify := requiredMap(t, jobs, "verify")
	if env := mappingValue(verify, "env"); env != nil && containsSecretTerraformVariable(env) {
		t.Fatal("verification job must not expose secret Terraform variables")
	}

	secretScan := requiredMap(t, jobs, "secret-scan")
	requireSecretScan(t, secretScan)
	requireTerraformVersion(t, verify, "1.13.5")
	requireTerraformTest(t, verify)

	deploy := requiredMap(t, jobs, "deploy")
	deployIf := requiredScalar(t, deploy, "if")
	if deployIf != "github.event_name == 'workflow_dispatch'" {
		t.Fatalf("deploy must require explicit workflow dispatch until the OIDC role is configured, got if: %q", deployIf)
	}

	deployPermissions := requiredMap(t, deploy, "permissions")
	requireScalar(t, deployPermissions, "contents", "read")
	requireScalar(t, deployPermissions, "id-token", "write")
	requireTerraformVersion(t, deploy, "1.13.5")
	requireOIDCOnlyDeploy(t, deploy)
	if !hasNullProfileEnvironment(deploy) {
		t.Fatal("deploy must set TF_VAR_aws_profile to Terraform null for OIDC credentials")
	}
	requireDeployTerraformInputs(t, root, deploy)
}

func TestDeployWorkflowBackgroundProcessingDefaultsToDisabled(t *testing.T) {
	workflow := loadWorkflow(t)
	root := documentRoot(t, workflow)
	env := requiredMap(t, root, "env")

	const want = "${{ vars.ENABLE_BACKGROUND_PROCESSING == 'true' && 'true' || 'false' }}"
	if got := requiredScalar(t, env, "TF_VAR_enable_background_processing"); got != want {
		t.Fatalf("TF_VAR_enable_background_processing = %q, want strict ENABLE_BACKGROUND_PROCESSING mapping %q", got, want)
	}
}

func requireDeployTerraformInputs(t *testing.T, root, deploy *yaml.Node) {
	t.Helper()
	required := []string{
		"TF_VAR_alert_email",
		"TF_VAR_alert_email_subscription_confirmed",
		"TF_VAR_db_allowed_cidr",
		"TF_VAR_enable_application",
		"TF_VAR_llm_api_url",
		"TF_VAR_llm_model",
		"TF_VAR_deepseek_base_url",
		"TF_VAR_deepseek_model",
		"TF_VAR_ses_verified_identity",
		"TF_VAR_ses_sender_email",
	}
	rootEnv := mappingValue(root, "env")
	deployEnv := mappingValue(deploy, "env")
	for _, name := range required {
		value := mappingValue(deployEnv, name)
		if value == nil {
			value = mappingValue(rootEnv, name)
		}
		if value == nil || strings.TrimSpace(value.Value) == "" {
			t.Fatalf("deploy must map required Terraform input %s", name)
		}
	}
}

func loadWorkflow(t *testing.T) *yaml.Node {
	t.Helper()
	path := filepath.Join("..", ".github", "workflows", "deploy.yml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read deploy workflow: %v", err)
	}

	var workflow yaml.Node
	if err := yaml.Unmarshal(contents, &workflow); err != nil {
		t.Fatalf("parse deploy workflow: %v", err)
	}
	return &workflow
}

func documentRoot(t *testing.T, document *yaml.Node) *yaml.Node {
	t.Helper()
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		t.Fatalf("workflow must contain one YAML document, got kind %d with %d nodes", document.Kind, len(document.Content))
	}
	if document.Content[0].Kind != yaml.MappingNode {
		t.Fatalf("workflow root must be a mapping, got kind %d", document.Content[0].Kind)
	}
	return document.Content[0]
}

func requiredMap(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	value := mappingValue(mapping, key)
	if value == nil {
		t.Fatalf("workflow is missing %q", key)
	}
	if value.Kind != yaml.MappingNode {
		t.Fatalf("workflow %q must be a mapping, got kind %d", key, value.Kind)
	}
	return value
}

func requiredScalar(t *testing.T, mapping *yaml.Node, key string) string {
	t.Helper()
	value := mappingValue(mapping, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		t.Fatalf("workflow %q must be a scalar", key)
	}
	return value.Value
}

func requireScalar(t *testing.T, mapping *yaml.Node, key, want string) {
	t.Helper()
	if got := requiredScalar(t, mapping, key); got != want {
		t.Fatalf("workflow %q = %q, want %q", key, got, want)
	}
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func requireBranches(t *testing.T, trigger *yaml.Node, wants ...string) {
	t.Helper()
	branches := mappingValue(trigger, "branches")
	if branches == nil || branches.Kind != yaml.SequenceNode {
		t.Fatal("push trigger must explicitly include protected branches")
	}
	for _, want := range wants {
		found := false
		for _, branch := range branches.Content {
			if branch.Value == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("push trigger must include %q", want)
		}
	}
}

func requireSecretScan(t *testing.T, job *yaml.Node) {
	t.Helper()
	steps := mappingValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("secret-scan job must have steps")
	}

	checkoutWithFullHistory := false
	explicitIntroducedCommitScan := false
	checksum := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, step := range steps.Content {
		if step.Kind != yaml.MappingNode {
			continue
		}
		uses := mappingValue(step, "uses")
		if uses != nil && strings.HasPrefix(uses.Value, "actions/checkout@") {
			with := mappingValue(step, "with")
			checkoutWithFullHistory = with != nil && mappingValue(with, "fetch-depth") != nil && mappingValue(with, "fetch-depth").Value == "0"
		}

		run := mappingValue(step, "run")
		env := mappingValue(step, "env")
		if run == nil || env == nil {
			continue
		}
		version := mappingValue(env, "GITLEAKS_VERSION")
		digest := mappingValue(env, "GITLEAKS_SHA256")
		if version == nil || digest == nil || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version.Value) || !checksum.MatchString(digest.Value) {
			continue
		}
		script := run.Value
		explicitIntroducedCommitScan = strings.Contains(script, "github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}") &&
			strings.Contains(script, "sha256sum --check") &&
			strings.Contains(script, "gitleaks detect") &&
			strings.Contains(script, "--source .") &&
			strings.Contains(script, `--log-opts="${scan_range}"`) &&
			strings.Contains(script, "GITHUB_EVENT_NAME") &&
			strings.Contains(script, "GITHUB_BASE_REF") &&
			strings.Contains(script, ".gitleaks.toml") &&
			strings.Contains(script, "--redact") &&
			strings.Contains(script, "--exit-code 1")
	}
	if !checkoutWithFullHistory {
		t.Fatal("secret-scan must check out full Git history with fetch-depth: 0")
	}
	if !explicitIntroducedCommitScan {
		t.Fatal("secret-scan must checksum a pinned Gitleaks release and scan only commits introduced by the event")
	}
}

func requireTerraformVersion(t *testing.T, job *yaml.Node, want string) {
	t.Helper()
	steps := mappingValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("Terraform job must have steps")
	}
	for _, step := range steps.Content {
		uses := mappingValue(step, "uses")
		if uses == nil || !strings.HasPrefix(uses.Value, "hashicorp/setup-terraform@") {
			continue
		}
		with := mappingValue(step, "with")
		if with != nil && mappingValue(with, "terraform_version") != nil && mappingValue(with, "terraform_version").Value == want {
			return
		}
	}
	t.Fatalf("Terraform job must use Terraform %s", want)
}

func requireTerraformTest(t *testing.T, job *yaml.Node) {
	t.Helper()
	steps := mappingValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("Terraform verification job must have steps")
	}
	for _, step := range steps.Content {
		if run := mappingValue(step, "run"); run != nil && strings.Contains(run.Value, `terraform -chdir="${TERRAFORM_DIR}" test`) {
			return
		}
	}
	t.Fatal("verification job must execute terraform test")
}

func containsSecretTerraformVariable(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.MappingNode {
		for index := 0; index < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if strings.HasPrefix(key.Value, "TF_VAR_") && strings.Contains(value.Value, "secrets.") {
				return true
			}
			if containsSecretTerraformVariable(value) {
				return true
			}
		}
	}
	for _, child := range node.Content {
		if containsSecretTerraformVariable(child) {
			return true
		}
	}
	return false
}

func requireOIDCOnlyDeploy(t *testing.T, deploy *yaml.Node) {
	t.Helper()
	if containsNodeValue(deploy, "AWS_ACCESS_KEY_ID") || containsNodeValue(deploy, "AWS_SECRET_ACCESS_KEY") || containsNodeValue(deploy, "AWS_SESSION_TOKEN") {
		t.Fatal("deploy must not include a static AWS credential fallback")
	}

	steps := mappingValue(deploy, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("deploy must have steps")
	}

	configuredOIDC := false
	failClosed := false
	for _, step := range steps.Content {
		if step.Kind != yaml.MappingNode {
			continue
		}
		if uses := mappingValue(step, "uses"); uses != nil && strings.HasPrefix(uses.Value, "aws-actions/configure-aws-credentials@") {
			with := mappingValue(step, "with")
			configuredOIDC = with != nil && mappingValue(with, "role-to-assume") != nil
		}
		if run := mappingValue(step, "run"); run != nil && strings.Contains(run.Value, "TF_STATE_BUCKET") && strings.Contains(run.Value, "TF_STATE_LOCK_TABLE") && strings.Contains(run.Value, "AWS_DEPLOY_ROLE_ARN") && strings.Contains(run.Value, "exit 1") {
			failClosed = true
		}
	}
	if !configuredOIDC {
		t.Fatal("deploy must configure AWS credentials by assuming the OIDC role")
	}
	if !failClosed {
		t.Fatal("deploy configuration checks must fail closed for the role, state bucket, and lock table")
	}
}

func hasNullProfileEnvironment(deploy *yaml.Node) bool {
	steps := mappingValue(deploy, "steps")
	if steps == nil {
		return false
	}
	for _, step := range steps.Content {
		env := mappingValue(step, "env")
		if env != nil {
			if profile := mappingValue(env, "TF_VAR_aws_profile"); profile != nil && profile.Value == "null" {
				return true
			}
		}
	}
	return false
}

func containsNodeValue(node *yaml.Node, want string) bool {
	if node == nil {
		return false
	}
	if node.Value == want {
		return true
	}
	for _, child := range node.Content {
		if containsNodeValue(child, want) {
			return true
		}
	}
	return false
}
