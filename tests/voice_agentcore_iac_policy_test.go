package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVoiceAgentCoreIaCStaysCostGatedAndHasNoPaidAddOns(t *testing.T) {
	t.Parallel()

	terraformDir := filepath.Join("..", "infrastructure", "terraform")
	var source strings.Builder
	for _, name := range []string{"voice_agentcore.tf", "voice_agentcore_iam.tf", "voice_agentcore_mmds.tf"} {
		body, err := os.ReadFile(filepath.Join(terraformDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		source.Write(body)
	}
	combined := source.String()

	for _, required := range []string{
		`count = var.enable_voice ? 1 : 0`,
		`resource "awscc_kinesisvideo_signaling_channel" "voice"`,
		`count = var.enable_voice ? 12 : 0`,
		`request_header_allowlist = ["Authorization"]`,
		`allowed_clients = [aws_cognito_user_pool_client.phone.id]`,
		`metadataConfiguration`,
		`requireMMDSV2 = true`,
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("AgentCore voice IaC is missing %q", required)
		}
	}

	for _, forbidden := range []string{
		`resource "aws_nat_gateway"`,
		`resource "aws_vpc_endpoint"`,
		`resource "aws_bedrockagentcore_gateway"`,
		`resource "aws_bedrockagentcore_memory"`,
		`resource "aws_bedrockagentcore_browser"`,
		`resource "aws_bedrockagentcore_code_interpreter"`,
		`resource "aws_bedrockagentcore_web_search"`,
		`SARVAM_API_KEY`,
		`allowed_audience`,
		`allowed_scopes`,
		`bedrock-agentcore:*`,
		`secretsmanager:*`,
		`dynamodb:*`,
	} {
		if strings.Contains(combined, forbidden) {
			t.Errorf("AgentCore voice IaC contains forbidden cost/security surface %q", forbidden)
		}
	}
}

func TestVoiceAgentCoreDeploymentPublishesOnlyBehindManualGate(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatalf("read deploy workflow: %v", err)
	}
	source := string(body)
	for _, required := range []string{
		`if: env.TF_VAR_enable_voice == 'true'`,
		`VOICE_AGENTCORE_IMAGE_TAG is required when voice is enabled`,
		`AWS CLI 2.35 or newer is required`,
		`-target=aws_ecr_repository.voice_agentcore`,
		`docker buildx build --platform linux/arm64`,
		`--tag "${repository_url}:${TF_VAR_voice_agentcore_image_tag}"`,
		`--push .`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("gated AgentCore deployment plumbing is missing %q", required)
		}
	}
	if strings.Contains(source, "voice-runtime:latest") {
		t.Fatal("AgentCore deployment must never publish a mutable latest tag")
	}
}
