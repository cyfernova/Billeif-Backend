package tests

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDeadPaymentWorkflowAndInvoiceSequenceInfrastructureIsAbsent(t *testing.T) {
	removedResources := []string{
		"aws_cloudwatch_log_group.lambda_sqs_payment",
		"aws_cloudwatch_metric_alarm.lambda_payment_errors",
		"aws_dynamodb_table.invoice_sequences",
		"aws_lambda_event_source_mapping.payment_queue",
		"aws_lambda_function.sqs_payment",
		"aws_sns_topic.payment_notifications",
		"aws_sns_topic.workflow_notifications",
		"aws_sns_topic_subscription.workflow_to_sqs",
		"aws_sqs_queue.payment_processing",
		"aws_sqs_queue.payment_processing_dlq",
		"aws_sqs_queue.workflow_runs",
		"aws_sqs_queue.workflow_runs_dlq",
		"aws_sqs_queue_policy.workflow_runs",
		"aws_sqs_queue_redrive_allow_policy.payment_processing_dlq",
		"aws_sqs_queue_redrive_allow_policy.workflow_runs_dlq",
		"aws_sqs_queue_redrive_policy.payment_processing",
		"aws_sqs_queue_redrive_policy.workflow_runs",
	}
	resources := terraformResourceLabels(t)
	for _, removed := range removedResources {
		if containsString(resources, removed) {
			t.Errorf("dead Terraform resource remains in the plan graph: %s", removed)
		}
	}

	for _, removed := range []string{
		"lambda_sqs_payment_arn",
		"payment_processing_queue_url",
		"workflow_runs_queue_url",
	} {
		if containsString(terraformOutputKeys(t), removed) {
			t.Errorf("dead Terraform output remains: %s", removed)
		}
	}
	if containsString(terraformEnvironmentKeys(t), "SQS_PAYMENT_QUEUE") {
		t.Error("dead payment queue URL remains in a Lambda environment")
	}

	terraform := readTerraformSources(t)
	for _, suffix := range []string{
		"-invoice-sequences",
		"-payment-notifications",
		"-payment-processing-dlq",
		"-payment-processing-queue",
		"-sqs-payment",
		"-workflow-notifications",
		"-workflow-runs-dlq",
		"-workflow-runs-queue",
	} {
		if strings.Contains(terraform, suffix) {
			t.Errorf("dead AWS physical-name suffix remains in Terraform: %s", suffix)
		}
	}
	for _, reference := range []string{
		"aws_dynamodb_table.invoice_sequences",
		"aws_lambda_function.sqs_payment",
		"aws_sns_topic.payment_notifications",
		"aws_sns_topic.workflow_notifications",
		"aws_sqs_queue.payment_processing",
		"aws_sqs_queue.workflow_runs",
	} {
		if strings.Contains(terraform, reference) {
			t.Errorf("dead Terraform reference remains: %s", reference)
		}
	}
}

func TestDeadPaymentWorkerBuildSurfaceIsAbsent(t *testing.T) {
	repoRoot := filepath.Join("..")
	for _, removedPath := range []string{
		filepath.Join(repoRoot, "cmd", "lambda", "sqs-payment", "main.go"),
		filepath.Join(repoRoot, "infrastructure", "terraform", "tests", "fixtures", "lambda", "sqs-payment.zip"),
	} {
		_, err := os.Stat(removedPath)
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("dead payment worker path still exists: %s", removedPath)
		}
	}

	makefile, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	targetPattern := regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+)\s*:`)
	for _, match := range targetPattern.FindAllStringSubmatch(string(makefile), -1) {
		if match[1] == "build-lambda-sqs-payment" {
			t.Error("dead payment worker Make target remains")
		}
	}
	if strings.Contains(string(makefile), "sqs-payment.zip") ||
		strings.Contains(string(makefile), "$(LAMBDA_BUILD_DIR)/sqs-payment") ||
		strings.Contains(string(makefile), "./cmd/lambda/sqs-payment") {
		t.Error("dead payment worker build/package path remains in Makefile recipes")
	}
}

func TestActivePaymentAPIAndPersistenceRemain(t *testing.T) {
	repoRoot := filepath.Join("..")
	for _, activePath := range []string{
		filepath.Join("internal", "handlers", "payment_handler.go"),
		filepath.Join("internal", "services", "payment_service.go"),
		filepath.Join("internal", "repositories", "interfaces", "repositories.go"),
		filepath.Join("internal", "repositories", "postgres", "payment_repo.go"),
		filepath.Join("internal", "models", "payment.go"),
	} {
		info, err := os.Stat(filepath.Join(repoRoot, activePath))
		if err != nil {
			t.Errorf("active payment path must remain (%s): %v", activePath, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("active payment path is empty: %s", activePath)
		}
	}

	if !containsString(terraformResourceLabels(t), "aws_dynamodb_table.payments_cache") {
		t.Error("active payment cache table must remain")
	}
}
