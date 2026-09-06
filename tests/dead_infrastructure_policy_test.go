package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"invoice-backend/internal/handlers"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
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
	contract := activePaymentContract()
	for _, capability := range []string{
		"payment-api",
		"payment-model",
		"payment-postgres-repository",
		"payment-repository-contract",
		"payment-schema",
		"razorpay-checkout",
		"razorpay-payment-attempt-model",
		"razorpay-payment-processor",
		"razorpay-webhook",
		"razorpay-webhook-schema",
	} {
		if !contract[capability] {
			t.Errorf("active payment capability must remain: %s", capability)
		}
	}

	if !containsString(terraformResourceLabels(t), "aws_dynamodb_table.payments_cache") {
		t.Error("active payment cache table must remain")
	}
}

func TestRemovedInfrastructureIsAbsentFromBothMockedPlans(t *testing.T) {
	plans, err := mockedPlanResourceAddresses()
	if err != nil {
		t.Fatal(err)
	}

	for _, run := range []string{
		"foundation_migrates_while_application_is_fail_closed",
		"reviewed_enablement_activates_stable_application_resources_after_migration",
	} {
		addresses, ok := plans[run]
		if !ok {
			t.Fatalf("mocked Terraform output did not contain plan for %s", run)
		}
		if len(addresses) == 0 {
			t.Fatalf("mocked Terraform plan for %s contained no resource changes", run)
		}
		for _, removed := range []string{
			"aws_dynamodb_table.invoice_sequences",
			"aws_lambda_event_source_mapping.payment_queue",
			"aws_lambda_function.sqs_payment",
			"aws_sqs_queue.payment_processing",
			"aws_sqs_queue.payment_processing_dlq",
			"aws_sqs_queue.workflow_runs",
			"aws_sqs_queue.workflow_runs_dlq",
		} {
			if containsString(addresses, removed) {
				t.Errorf("%s mocked plan contains removed resource %s", run, removed)
			}
		}
	}
}

func TestTerraformMockEnvironmentIsHermetic(t *testing.T) {
	environment := terraformTestEnvironment([]string{
		"PATH=/usr/bin",
		"TF_VAR_exa_base_url=",
		"TF_VAR_voice_ws_max_frame_bytes=not-a-number",
		"AWS_PROFILE=unexpected",
		"TF_IN_AUTOMATION=0",
	})

	got := make(map[string]string, len(environment))
	for _, variable := range environment {
		key, value, ok := strings.Cut(variable, "=")
		if ok {
			got[key] = value
		}
	}
	if got["PATH"] != "/usr/bin" {
		t.Fatalf("PATH = %q, want preserved", got["PATH"])
	}
	if got["AWS_PROFILE"] != "default" {
		t.Fatalf("AWS_PROFILE = %q, want default", got["AWS_PROFILE"])
	}
	if got["TF_IN_AUTOMATION"] != "1" {
		t.Fatalf("TF_IN_AUTOMATION = %q, want 1", got["TF_IN_AUTOMATION"])
	}
	for key := range got {
		if strings.HasPrefix(key, "TF_VAR_") {
			t.Fatalf("mock Terraform environment leaked %s", key)
		}
	}
}

func activePaymentContract() map[string]bool {
	paymentHandler := reflect.TypeOf((*handlers.PaymentHandler)(nil))
	razorpayHandler := reflect.TypeOf((*handlers.RazorpayPaymentHandler)(nil))
	paymentService := reflect.TypeOf((*services.PaymentService)(nil))
	razorpayService := reflect.TypeOf((*services.RazorpayPaymentService)(nil))
	paymentProcessor := reflect.TypeOf((*services.PaymentProcessorService)(nil))
	paymentRepository := reflect.TypeOf((*interfaces.PaymentRepository)(nil)).Elem()

	return map[string]bool{
		"payment-api": methodsExist(paymentHandler, "Create", "Get", "List", "Update", "Delete") &&
			methodsExist(paymentService, "Create", "GetByBusiness", "ListByBusiness", "UpdateByBusiness", "DeleteByBusiness") &&
			functionExists(handlers.NewPaymentHandler) &&
			functionExists(services.NewPaymentService),
		"payment-model":               (&models.Payment{}).TableName() == "payments",
		"payment-postgres-repository": functionExists(postgres.NewPaymentRepository),
		"payment-repository-contract": methodsExist(
			paymentRepository,
			"Create",
			"GetByID",
			"GetByBusinessID",
			"GetByInvoiceID",
			"Update",
			"Delete",
		),
		"payment-schema": migrationContains(
			"000007_payments.up.sql",
			"CREATE TABLE IF NOT EXISTS payments",
			"business_id UUID NOT NULL",
			"invoice_id UUID NOT NULL",
			"CREATE INDEX idx_payments_business_id",
			"CREATE INDEX idx_payments_invoice_id",
		),
		"razorpay-checkout": methodsExist(razorpayHandler, "CreateOrder", "VerifyPayment") &&
			methodsExist(razorpayService, "CreateOrder", "VerifyPayment") &&
			functionExists(handlers.NewRazorpayPaymentHandler) &&
			functionExists(services.NewRazorpayPaymentService),
		"razorpay-payment-attempt-model": models.PaymentAttempt{}.TableName() == "payment_attempts",
		"razorpay-payment-processor": methodsExist(
			paymentProcessor,
			"ProcessPayment",
			"AuthorizePayment",
			"CapturePayment",
			"FailPayment",
			"RefundPayment",
		) && functionExists(services.NewPaymentProcessorService),
		"razorpay-webhook": methodsExist(razorpayHandler, "Webhook") &&
			methodsExist(razorpayService, "HandleWebhook") &&
			models.RazorpayWebhookEvent{}.TableName() == "razorpay_webhook_events",
		"razorpay-webhook-schema": migrationContains(
			"000041_add_razorpay_payment_attempts.up.sql",
			"CREATE TABLE IF NOT EXISTS payment_attempts",
			"razorpay_order_id VARCHAR(100) UNIQUE",
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_attempts_user_business_idempotency",
			"CREATE TABLE IF NOT EXISTS razorpay_webhook_events",
			"razorpay_event_id VARCHAR(160) NOT NULL UNIQUE",
		),
	}
}

func mockedPlanResourceAddresses() (map[string][]string, error) {
	terraform, err := exec.LookPath("terraform")
	if err != nil {
		return nil, fmt.Errorf("find terraform executable: %w", err)
	}

	terraformDir := filepath.Join("..", "infrastructure", "terraform")
	testConfig, err := os.ReadFile(filepath.Join(terraformDir, "tests", "migrator.tftest.hcl"))
	if err != nil {
		return nil, fmt.Errorf("read mocked terraform test: %w", err)
	}
	if !bytes.Contains(testConfig, []byte(`mock_provider "aws"`)) {
		return nil, errors.New("refusing to execute Terraform test without the AWS mock provider")
	}
	if regexp.MustCompile(`(?m)^\s*command\s*=\s*apply\s*$`).Match(testConfig) {
		return nil, errors.New("refusing to execute a Terraform apply test")
	}

	command := exec.Command(
		terraform,
		"test",
		"-filter="+filepath.Join("tests", "migrator.tftest.hcl"),
		"-json",
		"-verbose",
	)
	command.Dir = terraformDir
	command.Env = terraformTestEnvironment(os.Environ())

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("capture terraform test stdout: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start mocked terraform test: %w", err)
	}

	type resourceChange struct {
		Address string `json:"address"`
	}
	type terraformTestEvent struct {
		Diagnostic struct {
			Severity string `json:"severity"`
			Summary  string `json:"summary"`
			Detail   string `json:"detail"`
		} `json:"diagnostic"`
		Type     string `json:"type"`
		TestRun  string `json:"@testrun"`
		TestPlan struct {
			ResourceChanges []resourceChange `json:"resource_changes"`
		} `json:"test_plan"`
	}

	wantedRuns := map[string]bool{
		"foundation_migrates_while_application_is_fail_closed":                       true,
		"reviewed_enablement_activates_stable_application_resources_after_migration": true,
	}
	plans := make(map[string][]string, len(wantedRuns))
	var diagnostics []string
	decoder := json.NewDecoder(stdout)
	for {
		var event terraformTestEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			_ = command.Process.Kill()
			_ = command.Wait()
			return nil, fmt.Errorf("decode mocked terraform test output: %w", err)
		}
		if event.Type == "diagnostic" && event.Diagnostic.Severity == "error" {
			diagnostics = append(diagnostics, event.Diagnostic.Summary+": "+event.Diagnostic.Detail)
		}
		if event.Type != "test_plan" || !wantedRuns[event.TestRun] {
			continue
		}
		for _, resource := range event.TestPlan.ResourceChanges {
			plans[event.TestRun] = append(plans[event.TestRun], normalizeResourceAddress(resource.Address))
		}
		plans[event.TestRun] = sortedStrings(plans[event.TestRun])
	}

	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf("mocked terraform test failed: %w: %s %s", err, strings.TrimSpace(stderr.String()), strings.Join(diagnostics, "; "))
	}
	return plans, nil
}

func terraformTestEnvironment(base []string) []string {
	environment := make([]string, 0, len(base)+2)
	for _, variable := range base {
		key, _, ok := strings.Cut(variable, "=")
		if !ok || strings.HasPrefix(key, "TF_VAR_") || key == "AWS_PROFILE" || key == "TF_IN_AUTOMATION" {
			continue
		}
		environment = append(environment, variable)
	}
	return append(environment, "AWS_PROFILE=default", "TF_IN_AUTOMATION=1")
}

func sortedStrings(values []string) []string {
	sort.Strings(values)
	return values
}

func methodsExist(value reflect.Type, methods ...string) bool {
	if value == nil {
		return false
	}
	for _, method := range methods {
		if _, ok := value.MethodByName(method); !ok {
			return false
		}
	}
	return true
}

func functionExists(value interface{}) bool {
	return reflect.TypeOf(value).Kind() == reflect.Func
}

func migrationContains(name string, fragments ...string) bool {
	contents, err := os.ReadFile(filepath.Join("..", "migrations", name))
	if err != nil {
		return false
	}
	for _, fragment := range fragments {
		if !bytes.Contains(contents, []byte(fragment)) {
			return false
		}
	}
	return true
}

var resourceInstanceSuffix = regexp.MustCompile(`\[[^]]+\]`)

func normalizeResourceAddress(address string) string {
	return resourceInstanceSuffix.ReplaceAllString(address, "")
}
