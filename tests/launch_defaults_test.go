package tests

import (
	"os"
	"strings"
	"testing"
)

func TestLocalTerraformDefaultsTargetBilleifMumbaiAccount(t *testing.T) {
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	source := string(makefile)
	for _, required := range []string{
		"AWS_PROFILE ?= default",
		"TF_BACKEND_BUCKET ?= billeif-terraform-state-928282274753-ap-south-1",
		"TF_BACKEND_REGION ?= ap-south-1",
		"TF_BACKEND_KEY ?= billeif/dev/terraform.tfstate",
		"--profile $(AWS_PROFILE)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("local Terraform launch defaults are missing %q", required)
		}
	}
	for _, forbidden := range []string{"830283279729", "us-east-1"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("local Terraform launch defaults retain stale AWS target %q", forbidden)
		}
	}
}

func TestLambdaArtifactsWaitForBucketVersioning(t *testing.T) {
	terraform, err := os.ReadFile("../infrastructure/terraform/s3.tf")
	if err != nil {
		t.Fatalf("read S3 Terraform: %v", err)
	}
	source := string(terraform)
	resources := []string{
		"api_http_lambda_artifact",
		"sqs_bargaining_lambda_artifact",
		"sqs_invoice_lambda_artifact",
		"sqs_gst_lambda_artifact",
		"ws_lambda_artifact",
		"custom_sms_sender_lambda_artifact",
	}
	for _, name := range resources {
		startToken := `resource "aws_s3_object" "` + name + `"`
		start := strings.Index(source, startToken)
		if start < 0 {
			t.Fatalf("S3 Terraform is missing %s", startToken)
		}
		end := strings.Index(source[start+len(startToken):], `resource "`)
		block := source[start:]
		if end >= 0 {
			block = source[start : start+len(startToken)+end]
		}
		if !strings.Contains(block, "depends_on") || !strings.Contains(block, "aws_s3_bucket_versioning.lambda_artifacts") {
			t.Fatalf("%s must wait for Lambda artifact bucket versioning before upload", name)
		}
	}
}

func TestAllGoLambdaBuildsAreStrippedAndReproducible(t *testing.T) {
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	source := string(makefile)
	targets := []string{
		"build-lambda-http",
		"build-lambda-a2a-stream",
		"build-lambda-sqs-invoice",
		"build-lambda-sqs-email-delivery",
		"build-lambda-sqs-ses-feedback",
		"build-lambda-sqs-gst",
		"build-lambda-sqs-bargaining",
		"build-lambda-ws",
		"build-lambda-outbox",
		"build-lambda-migrator",
	}
	for _, target := range targets {
		startToken := target + ":"
		start := strings.Index(source, startToken)
		if start < 0 {
			t.Fatalf("Makefile is missing %s", target)
		}
		end := strings.Index(source[start+len(startToken):], "\nbuild-lambda-")
		block := source[start:]
		if end >= 0 {
			block = source[start : start+len(startToken)+end]
		}
		if !strings.Contains(block, "go build -trimpath") ||
			!strings.Contains(block, `-ldflags="-s -w -buildid="`) {
			t.Fatalf("%s must strip symbols and local build paths", target)
		}
	}
}

func TestAllLambdaArchivesAreDeterministic(t *testing.T) {
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	source := string(makefile)
	for _, artifact := range []string{
		"http",
		"a2a-stream",
		"sqs-invoice",
		"sqs-email-delivery",
		"sqs-ses-feedback",
		"sqs-gst",
		"sqs-bargaining",
		"ws",
		"outbox",
		"migrator",
	} {
		for _, required := range []string{
			"rm -f $(LAMBDA_BUILD_DIR)/" + artifact + ".zip",
			"TZ=UTC touch -t 198001010000 $(LAMBDA_BUILD_DIR)/" + artifact + "/bootstrap",
			"TZ=UTC zip -q -X -j ../" + artifact + ".zip bootstrap",
		} {
			if !strings.Contains(source, required) {
				t.Fatalf("%s packaging must contain %q", artifact, required)
			}
		}
	}
	for _, required := range []string{
		"rm -f $(LAMBDA_BUILD_DIR)/custom-sms-sender.zip",
		"rm -f $(LAMBDA_BUILD_DIR)/custom-sms-sender/node_modules/.modules.yaml $(LAMBDA_BUILD_DIR)/custom-sms-sender/node_modules/.pnpm-workspace-state-v1.json",
		"find $(LAMBDA_BUILD_DIR)/custom-sms-sender -exec touch -t 198001010000 {} +",
		"find -L . -type f -print | LC_ALL=C sort | TZ=UTC zip -q -X ../custom-sms-sender.zip -@",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("custom SMS packaging must contain %q", required)
		}
	}
}
