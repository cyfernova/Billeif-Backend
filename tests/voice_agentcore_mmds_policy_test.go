package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVoiceAgentCoreMMDSBridgeCarriesTheCompleteDesiredRuntimeConfiguration(t *testing.T) {
	t.Parallel()

	source := readVoiceAgentCoreMMDSFile(t, "voice_agentcore_mmds.tf")
	for _, required := range []string{
		`resource "terraform_data" "voice_agentcore_mmdsv2"`,
		`triggers_replace`,
		`aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_version`,
		`var.voice_agentcore_release`,
		`agentRuntimeArtifact`,
		`aws_ecr_repository.voice_agentcore[0].repository_url`,
		`var.voice_agentcore_image_tag`,
		`roleArn`,
		`aws_iam_role.voice_agentcore_runtime[0].arn`,
		`networkConfiguration`,
		`securityGroups = [aws_security_group.voice_agentcore[0].id]`,
		`subnets        = aws_subnet.private[*].id`,
		`authorizerConfiguration`,
		`aws_cognito_user_pool.phone.id`,
		`allowedClients`,
		`aws_cognito_user_pool_client.phone.id`,
		`requestHeaderConfiguration`,
		`requestHeaderAllowlist = ["Authorization"]`,
		`protocolConfiguration`,
		`serverProtocol = "HTTP"`,
		`lifecycleConfiguration`,
		`idleRuntimeSessionTimeout = 120`,
		`maxLifetime               = 3600`,
		`environmentVariables = local.voice_agentcore_runtime_environment`,
		`metadataConfiguration`,
		`requireMMDSV2 = true`,
		`clientToken`,
		`provisioner "local-exec"`,
		`AGENTCORE_UPDATE_INPUT`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("MMDSv2 bridge is missing %q", required)
		}
	}

	for _, forbidden := range []string{
		"SARVAM_API_KEY",
		"secretsmanager get-secret-value",
		"secretsmanager batch-get-secret-value",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("MMDSv2 bridge must not carry secret values or retrieve credentials: found %q", forbidden)
		}
	}
}

func TestVoiceAgentCoreMMDSEndpointsConsumeThePostBridgeVersion(t *testing.T) {
	t.Parallel()

	source := readVoiceAgentCoreMMDSFile(t, "voice_agentcore_mmds.tf")
	for _, required := range []string{
		`data "external" "voice_agentcore_version"`,
		`depends_on = [terraform_data.voice_agentcore_mmdsv2]`,
		`agent_runtime_version`,
		`voice_agentcore_endpoint_version`,
		`data.external.voice_agentcore_version[0].result.agent_runtime_version`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("post-MMDSv2 runtime-version lookup is missing %q", required)
		}
	}
}

func TestVoiceAgentCoreMMDSScriptIsStrictAuditableAndSecretSafe(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "infrastructure", "terraform", "scripts", "ensure-agentcore-mmdsv2.sh")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	source := string(body)

	for _, required := range []string{
		"set -Eeuo pipefail",
		"umask 077",
		"mktemp",
		"trap cleanup",
		"aws --version",
		"2.35",
		"bedrock-agentcore-control update-agent-runtime",
		"bedrock-agentcore-control get-agent-runtime",
		"--cli-input-json",
		"metadataConfiguration.requireMMDSV2",
		"agentRuntimeVersion",
		"READY",
		"agent_runtime_version",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("MMDSv2 helper is missing %q", required)
		}
	}

	for _, forbidden := range []string{
		"set -x",
		"eval ",
		"get-secret-value",
		"batch-get-secret-value",
		"SARVAM_API_KEY",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("MMDSv2 helper contains forbidden behavior %q", forbidden)
		}
	}

	command := exec.Command("bash", "-n", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("MMDSv2 helper shell syntax is invalid: %v: %s", err, output)
	}
}

func TestVoiceAgentCoreMMDSScriptWaitsThroughStaleReadySnapshot(t *testing.T) {
	t.Parallel()

	binDir := t.TempDir()
	statePath := filepath.Join(binDir, "aws-state")
	awsPath := filepath.Join(binDir, "aws")
	sleepPath := filepath.Join(binDir, "sleep")
	if err := os.WriteFile(awsPath, []byte(`#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${1:-}" == "--version" ]]; then
  printf 'aws-cli/2.35.0 Python/3.13.0 Darwin/24.0 source/arm64\n'
  exit 0
fi
count=0
if [[ -f "${FAKE_AWS_STATE}" ]]; then
  read -r count <"${FAKE_AWS_STATE}"
fi
count=$((count + 1))
printf '%s\n' "${count}" >"${FAKE_AWS_STATE}"
if [[ "${count}" -eq 1 ]]; then
  printf '1\tREADY\tFalse\n'
else
  printf '2\tREADY\tTrue\n'
fi
`), 0o755); err != nil {
		t.Fatalf("write fake aws: %v", err)
	}
	if err := os.WriteFile(sleepPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake sleep: %v", err)
	}

	path := filepath.Join("..", "infrastructure", "terraform", "scripts", "ensure-agentcore-mmdsv2.sh")
	command := exec.Command("bash", path, "read-version", "Voice_runtime-ABCDEFGHIJ", "ap-south-1", "bridge-id")
	command.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_AWS_STATE="+statePath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("MMDSv2 helper must poll through a stale READY,false snapshot: %v: %s", err, output)
	}
	if string(output) != "{\"agent_runtime_version\":\"2\"}\n" {
		t.Fatalf("unexpected post-update runtime version output %q", output)
	}
}

func readVoiceAgentCoreMMDSFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "infrastructure", "terraform", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}
