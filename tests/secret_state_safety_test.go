package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTerraformAndShellDoNotCarrySecretValues(t *testing.T) {
	terraformFiles, err := filepath.Glob("../infrastructure/terraform/*.tf")
	if err != nil {
		t.Fatalf("glob Terraform files: %v", err)
	}
	var terraformSource strings.Builder
	for _, name := range terraformFiles {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		terraformSource.Write(body)
		terraformSource.WriteByte('\n')
	}
	source := terraformSource.String()

	for _, forbidden := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)resource\s+"aws_secretsmanager_secret_version"`),
		regexp.MustCompile(`(?m)data\s+"aws_secretsmanager_secret_version"`),
		regexp.MustCompile(`(?m)\bsecret_(string|binary)\s*=`),
		regexp.MustCompile(`(?m)type\s*=\s*"SecureString"`),
		regexp.MustCompile(`(?m)^\s*password\s*=`),
		regexp.MustCompile(`(?m)variable\s+"(db_password|credential_encryption_key|jwt_secret|razorpay_key_id|razorpay_key_secret|razorpay_webhook_secret|google_client_id|google_client_secret|fcm_api_key|apns_private_key|apns_certificate|llm_api_key|exa_api_key|gst_lookup_api_key|deepgram_api_key|deepseek_api_key)"`),
	} {
		if match := forbidden.FindString(source); match != "" {
			t.Fatalf("Terraform secret-state safety violation matched %q", match)
		}
	}

	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	command := "secretsmanager " + "get-secret-value"
	batchCommand := "secretsmanager " + "batch-get-secret-value"
	if strings.Contains(string(makefile), command) || strings.Contains(string(makefile), batchCommand) {
		t.Fatal("Makefile must not invoke Secrets Manager value retrieval commands")
	}
	for _, required := range []string{
		"command -v asm-exec",
		"asm-exec -- env",
		"{{resolve:secretsmanager:$$DB_SECRET_ARN:SecretString:username}}",
		"{{resolve:secretsmanager:$$DB_SECRET_ARN:SecretString:password}}",
	} {
		if !strings.Contains(string(makefile), required) {
			t.Fatalf("Makefile is missing secret-safe migration behavior %q", required)
		}
	}
}

func TestKMSAndReaderPoliciesAreScopedForApplicationSecrets(t *testing.T) {
	secretsSource, err := os.ReadFile("../infrastructure/terraform/secrets.tf")
	if err != nil {
		t.Fatalf("read secrets Terraform: %v", err)
	}
	for _, required := range []string{
		`enable_key_rotation     = true`,
		`sid       = "AccountAdministration"`,
		`:root"`,
		`variable = "kms:ViaService"`,
		`values   = ["secretsmanager.ap-south-1.amazonaws.com"]`,
		`name          = "alias/${var.project_name}-${var.environment}-application-secrets"`,
	} {
		if !strings.Contains(string(secretsSource), required) {
			t.Fatalf("application secret KMS policy is missing %q", required)
		}
	}

	iamSource, err := os.ReadFile("../infrastructure/terraform/iam.tf")
	if err != nil {
		t.Fatalf("read IAM Terraform: %v", err)
	}
	for _, required := range []string{
		`"secretsmanager:GetSecretValue"`,
		`"secretsmanager:DescribeSecret"`,
		`resources = local.application_runtime_secret_arns`,
		`resources = [aws_kms_key.application_secrets.arn]`,
		`variable = "aws:SecureTransport"`,
	} {
		if !strings.Contains(string(iamSource), required) {
			t.Fatalf("runtime IAM policy is missing %q", required)
		}
	}
	if regexp.MustCompile(`resources\s*=\s*\[\s*"\*"\s*\]`).Match(iamSource) {
		t.Fatal("runtime IAM contains a wildcard resource")
	}
}
