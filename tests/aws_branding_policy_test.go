package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTerraformUsesBilleifBrandingAndVerifiedSESSenderContract(t *testing.T) {
	terraform := readTerraformSources(t)

	for _, required := range []string{
		`default     = "billeif"`,
		`resource_prefix = "${var.project_name}-${var.environment}"`,
		`cognito_hosted_ui_domain_prefix`,
		`"^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$"`,
		`strcontains(lower(trimspace(var.cognito_domain_prefix)), "billeif")`,
		`aws_cognito_resource_server`,
		`variable "ses_verified_sender"`,
		`email = var.ses_verified_sender`,
		`local.resource_prefix`,
	} {
		if !strings.Contains(terraform, required) {
			t.Errorf("Terraform branding contract is missing %q", required)
		}
	}

	for _, forbidden := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)invoice[-_ ]?(backend|platform)`),
		regexp.MustCompile(`(?i)@[^\s"']+\.local`),
	} {
		if match := forbidden.FindString(terraform); match != "" {
			t.Errorf("Terraform contains forbidden legacy AWS-facing literal %q", match)
		}
	}

	variables := readTerraformFile(t, "variables.tf")
	sesVariable := regexp.MustCompile(`(?s)variable\s+"ses_verified_sender"\s*\{.*?\n\}`).FindString(variables)
	if regexp.MustCompile(`(?s)default\s*=`).MatchString(sesVariable) {
		t.Error("ses_verified_sender must be caller-supplied and cannot have a default")
	}

	for _, want := range []string{
		`${local.resource_prefix}-users-sessions`,
		`${local.resource_prefix}-invoice-processing-queue`,
		`${local.resource_prefix}-ses-config`,
		`${local.resource_prefix}-api-http`,
		`${local.resource_prefix}-postgres`,
		`${local.resource_prefix}-websocket`,
	} {
		if !strings.Contains(terraform, want) {
			t.Errorf("stable AWS resource name does not use the Billeif project/environment prefix %q", want)
		}
	}
}

func TestTerraformBrandingPreservesPublicInterfaceNames(t *testing.T) {
	outputs := readTerraformFile(t, "outputs.tf")
	lambda := readTerraformFile(t, "lambda.tf")
	cognito := readTerraformFile(t, "cognito.tf")

	for _, required := range []string{
		`output "cognito_domain"`,
		`output "cognito_callback_urls"`,
		`output "cognito_logout_urls"`,
		`output "websocket_connections_table"`,
		`output "voice_sessions_table"`,
	} {
		if !strings.Contains(outputs, required) {
			t.Errorf("public Terraform output interface changed or is missing %q", required)
		}
	}

	for _, required := range []string{
		`COGNITO_DOMAIN`,
		`WEBSOCKET_CONNECTIONS_TABLE`,
		`VOICE_SESSIONS_TABLE`,
		`cognito_additional_callback_urls`,
		`cognito_additional_logout_urls`,
	} {
		if !strings.Contains(lambda+"\n"+cognito, required) {
			t.Errorf("public runtime or URL input interface changed or is missing %q", required)
		}
	}
}

func readTerraformSources(t *testing.T) string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "infrastructure", "terraform", "*.tf"))
	if err != nil {
		t.Fatalf("glob Terraform sources: %v", err)
	}

	var source strings.Builder
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		source.Write(body)
		source.WriteByte('\n')
	}
	return source.String()
}

func readTerraformFile(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "infrastructure", "terraform", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}
