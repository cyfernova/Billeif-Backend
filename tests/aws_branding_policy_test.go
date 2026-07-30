package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// These manifests are the exact public Terraform surface captured from
// pre-task commit 98622ad. They are intentionally static: this test must not
// shell out to Git, so a future change has to make its interface delta explicit.
const preTaskResourceManifest = `
aws_api_gateway_account.main
aws_api_gateway_authorizer.cognito
aws_api_gateway_deployment.main
aws_api_gateway_integration.api_v1_a2a_message_stream_options
aws_api_gateway_integration.api_v1_a2a_message_stream_post
aws_api_gateway_integration.api_v1_a2a_task_subscribe_get
aws_api_gateway_integration.api_v1_a2a_task_subscribe_options
aws_api_gateway_integration.api_v1_auth_google_options
aws_api_gateway_integration.api_v1_auth_google_post
aws_api_gateway_integration.api_v1_auth_phone_logout_options
aws_api_gateway_integration.api_v1_auth_phone_logout_post
aws_api_gateway_integration.api_v1_auth_phone_public_options
aws_api_gateway_integration.api_v1_auth_phone_public_post
aws_api_gateway_integration.api_v1_auth_proxy_any
aws_api_gateway_integration.api_v1_auth_public_options
aws_api_gateway_integration.api_v1_auth_public_post
aws_api_gateway_integration.api_v1_public_proxy_any
aws_api_gateway_integration.api_v1_webhooks_razorpay_post
aws_api_gateway_integration.health_get
aws_api_gateway_integration.proxy_any
aws_api_gateway_integration.proxy_options
aws_api_gateway_integration.root_any
aws_api_gateway_integration.swagger_proxy_any
aws_api_gateway_integration.well_known_proxy_any
aws_api_gateway_method.api_v1_a2a_message_stream_options
aws_api_gateway_method.api_v1_a2a_message_stream_post
aws_api_gateway_method.api_v1_a2a_task_subscribe_get
aws_api_gateway_method.api_v1_a2a_task_subscribe_options
aws_api_gateway_method.api_v1_auth_google_options
aws_api_gateway_method.api_v1_auth_google_post
aws_api_gateway_method.api_v1_auth_phone_logout_options
aws_api_gateway_method.api_v1_auth_phone_logout_post
aws_api_gateway_method.api_v1_auth_phone_public_options
aws_api_gateway_method.api_v1_auth_phone_public_post
aws_api_gateway_method.api_v1_auth_proxy_any
aws_api_gateway_method.api_v1_auth_public_options
aws_api_gateway_method.api_v1_auth_public_post
aws_api_gateway_method.api_v1_public_proxy_any
aws_api_gateway_method.api_v1_webhooks_razorpay_post
aws_api_gateway_method.health_get
aws_api_gateway_method.proxy_any
aws_api_gateway_method.proxy_options
aws_api_gateway_method.root_any
aws_api_gateway_method.swagger_proxy_any
aws_api_gateway_method.well_known_proxy_any
aws_api_gateway_method_settings.main
aws_api_gateway_resource.api
aws_api_gateway_resource.api_v1
aws_api_gateway_resource.api_v1_a2a
aws_api_gateway_resource.api_v1_a2a_message_stream
aws_api_gateway_resource.api_v1_a2a_task_id
aws_api_gateway_resource.api_v1_a2a_task_subscribe
aws_api_gateway_resource.api_v1_a2a_tasks
aws_api_gateway_resource.api_v1_auth
aws_api_gateway_resource.api_v1_auth_google
aws_api_gateway_resource.api_v1_auth_phone
aws_api_gateway_resource.api_v1_auth_phone_logout
aws_api_gateway_resource.api_v1_auth_phone_public
aws_api_gateway_resource.api_v1_auth_proxy
aws_api_gateway_resource.api_v1_auth_public
aws_api_gateway_resource.api_v1_public
aws_api_gateway_resource.api_v1_public_proxy
aws_api_gateway_resource.api_v1_webhooks
aws_api_gateway_resource.api_v1_webhooks_razorpay
aws_api_gateway_resource.health
aws_api_gateway_resource.proxy
aws_api_gateway_resource.swagger
aws_api_gateway_resource.swagger_proxy
aws_api_gateway_resource.well_known
aws_api_gateway_resource.well_known_proxy
aws_api_gateway_rest_api.main
aws_api_gateway_stage.main
aws_apigatewayv2_api.websocket
aws_apigatewayv2_deployment.websocket
aws_apigatewayv2_integration.websocket_lambda
aws_apigatewayv2_route.ws_connect
aws_apigatewayv2_route.ws_default
aws_apigatewayv2_route.ws_disconnect
aws_apigatewayv2_route.ws_voice_audio
aws_apigatewayv2_route.ws_voice_control
aws_apigatewayv2_route.ws_voice_start
aws_apigatewayv2_stage.websocket_default
aws_cloudwatch_dashboard.main
aws_cloudwatch_log_group.cognito_phone_custom_sms
aws_cloudwatch_log_group.lambda_a2a_stream
aws_cloudwatch_log_group.lambda_api_http
aws_cloudwatch_log_group.lambda_sqs_bargaining
aws_cloudwatch_log_group.lambda_sqs_gst
aws_cloudwatch_log_group.lambda_sqs_invoice
aws_cloudwatch_log_group.lambda_sqs_payment
aws_cloudwatch_log_group.lambda_voice_session
aws_cloudwatch_log_group.lambda_ws_handler
aws_cloudwatch_log_group.rest_api_access
aws_cloudwatch_log_group.websocket_api_access
aws_cloudwatch_log_metric_filter.threat_detection
aws_cloudwatch_metric_alarm.lambda_api_errors
aws_cloudwatch_metric_alarm.lambda_gst_errors
aws_cloudwatch_metric_alarm.lambda_invoice_errors
aws_cloudwatch_metric_alarm.lambda_payment_errors
aws_cloudwatch_metric_alarm.lambda_ws_errors
aws_cloudwatch_metric_alarm.rds_cpu_high
aws_cloudwatch_metric_alarm.rds_storage_low
aws_cloudwatch_metric_alarm.threat_detection
aws_cognito_user_group.accountant
aws_cognito_user_group.admin
aws_cognito_user_group.viewer
aws_cognito_user_pool.main
aws_cognito_user_pool.phone
aws_cognito_user_pool_client.main
aws_cognito_user_pool_client.phone
aws_cognito_user_pool_domain.main
aws_db_instance.main
aws_db_subnet_group.public
aws_dynamodb_table.customers_cache
aws_dynamodb_table.invoice_sequences
aws_dynamodb_table.invoices_cache
aws_dynamodb_table.ledger_cache
aws_dynamodb_table.mfa_codes
aws_dynamodb_table.password_reset_tokens
aws_dynamodb_table.payments_cache
aws_dynamodb_table.phone_auth_cooldowns
aws_dynamodb_table.products_cache
aws_dynamodb_table.refresh_tokens
aws_dynamodb_table.users_sessions
aws_dynamodb_table.vendors_cache
aws_dynamodb_table.voice_sessions
aws_dynamodb_table.ws_connections
aws_eip.nat
aws_iam_instance_profile.rds_tunnel
aws_iam_role.apigateway_cloudwatch
aws_iam_role.cognito_phone_custom_sms
aws_iam_role.cognito_phone_sms
aws_iam_role.lambda_exec
aws_iam_role.lambda_voice_exec
aws_iam_role.lambda_websocket_exec
aws_iam_role.lambda_worker_exec
aws_iam_role.rds_tunnel
aws_iam_role.sns_feedback
aws_iam_role_policy.cognito_phone_custom_sms
aws_iam_role_policy.cognito_phone_sms
aws_iam_role_policy.lambda_app
aws_iam_role_policy.lambda_voice_app
aws_iam_role_policy.lambda_websocket_app
aws_iam_role_policy.lambda_worker_app
aws_iam_role_policy.sns_feedback
aws_iam_role_policy_attachment.apigateway_cloudwatch
aws_iam_role_policy_attachment.cognito_phone_custom_sms_basic
aws_iam_role_policy_attachment.lambda_basic
aws_iam_role_policy_attachment.lambda_voice_basic
aws_iam_role_policy_attachment.lambda_vpc_access
aws_iam_role_policy_attachment.lambda_websocket_basic
aws_iam_role_policy_attachment.lambda_worker_basic
aws_iam_role_policy_attachment.lambda_worker_vpc_access
aws_iam_role_policy_attachment.rds_tunnel_ssm
aws_instance.rds_tunnel
aws_internet_gateway.main
aws_kms_alias.application_secrets
aws_kms_alias.cognito_phone_custom_sms
aws_kms_key.application_secrets
aws_kms_key.cognito_phone_custom_sms
aws_lambda_event_source_mapping.bargaining_queue
aws_lambda_event_source_mapping.gst_queue
aws_lambda_event_source_mapping.invoice_queue
aws_lambda_event_source_mapping.payment_queue
aws_lambda_function.a2a_stream
aws_lambda_function.api_http
aws_lambda_function.custom_sms_sender
aws_lambda_function.sqs_bargaining
aws_lambda_function.sqs_gst
aws_lambda_function.sqs_invoice
aws_lambda_function.sqs_payment
aws_lambda_function.voice_session
aws_lambda_function.ws_handler
aws_lambda_permission.allow_rest_a2a_stream
aws_lambda_permission.allow_rest_api_http
aws_lambda_permission.allow_websocket_lambda
aws_lambda_permission.cognito_phone_custom_sms
aws_nat_gateway.main
aws_route_table.private
aws_route_table.public
aws_route_table_association.private
aws_route_table_association.public
aws_s3_bucket.business_logos
aws_s3_bucket.email_sink
aws_s3_bucket.integration_data
aws_s3_bucket.invoices_pdf
aws_s3_bucket.lambda_artifacts
aws_s3_bucket.product_images
aws_s3_bucket_lifecycle_configuration.business_logos
aws_s3_bucket_lifecycle_configuration.lambda_artifacts
aws_s3_bucket_public_access_block.business_logos
aws_s3_bucket_public_access_block.email_sink
aws_s3_bucket_public_access_block.integration_data
aws_s3_bucket_public_access_block.invoices_pdf
aws_s3_bucket_public_access_block.lambda_artifacts
aws_s3_bucket_public_access_block.product_images
aws_s3_bucket_server_side_encryption_configuration.business_logos
aws_s3_bucket_server_side_encryption_configuration.email_sink
aws_s3_bucket_server_side_encryption_configuration.integration_data
aws_s3_bucket_server_side_encryption_configuration.invoices_pdf
aws_s3_bucket_server_side_encryption_configuration.lambda_artifacts
aws_s3_bucket_server_side_encryption_configuration.product_images
aws_s3_bucket_versioning.business_logos
aws_s3_bucket_versioning.invoices_pdf
aws_s3_bucket_versioning.lambda_artifacts
aws_s3_object.api_http_lambda_artifact
aws_s3_object.sqs_bargaining_lambda_artifact
aws_secretsmanager_secret.apns
aws_secretsmanager_secret.credential_encryption
aws_secretsmanager_secret.deepgram
aws_secretsmanager_secret.deepseek
aws_secretsmanager_secret.exa
aws_secretsmanager_secret.fcm
aws_secretsmanager_secret.google_oauth
aws_secretsmanager_secret.gst_lookup
aws_secretsmanager_secret.gst_provider
aws_secretsmanager_secret.legacy_jwt
aws_secretsmanager_secret.llm
aws_secretsmanager_secret.razorpay
aws_security_group.lambda
aws_security_group.rds
aws_security_group.rds_tunnel
aws_ses_configuration_set.main
aws_ses_email_identity.main
aws_ses_event_destination.to_sns
aws_sns_topic.alerts
aws_sns_topic.low_stock_alerts
aws_sns_topic.payment_notifications
aws_sns_topic.ses_events
aws_sns_topic.workflow_notifications
aws_sns_topic_subscription.alerts_email
aws_sns_topic_subscription.workflow_to_sqs
aws_sqs_queue.bargaining_negotiation
aws_sqs_queue.bargaining_negotiation_dlq
aws_sqs_queue.gst_processing
aws_sqs_queue.gst_processing_dlq
aws_sqs_queue.invoice_processing
aws_sqs_queue.invoice_processing_dlq
aws_sqs_queue.payment_processing
aws_sqs_queue.payment_processing_dlq
aws_sqs_queue.workflow_runs
aws_sqs_queue.workflow_runs_dlq
aws_sqs_queue_policy.workflow_runs
aws_sqs_queue_redrive_allow_policy.bargaining_negotiation_dlq
aws_sqs_queue_redrive_allow_policy.gst_processing_dlq
aws_sqs_queue_redrive_allow_policy.invoice_processing_dlq
aws_sqs_queue_redrive_allow_policy.payment_processing_dlq
aws_sqs_queue_redrive_allow_policy.workflow_runs_dlq
aws_sqs_queue_redrive_policy.bargaining_negotiation
aws_sqs_queue_redrive_policy.gst_processing
aws_sqs_queue_redrive_policy.invoice_processing
aws_sqs_queue_redrive_policy.payment_processing
aws_sqs_queue_redrive_policy.workflow_runs
aws_ssm_parameter.db_host
aws_subnet.private
aws_subnet.public
aws_vpc.main
`

const preTaskOutputManifest = `
client_id
cloudwatch_dashboard_url
cognito_callback_urls
cognito_domain
cognito_logout_urls
cognito_region
db_host_ssm_parameter
google_oauth_secret_arn
google_oauth_secret_name
gst_processing_queue_url
invoice_processing_queue_url
lambda_a2a_stream_arn
lambda_api_http_arn
lambda_sqs_gst_arn
lambda_sqs_invoice_arn
lambda_sqs_payment_arn
lambda_voice_session_arn
lambda_ws_handler_arn
payment_processing_queue_url
phone_auth_cooldown_table
phone_client_id
phone_user_pool_id
public_subnet_ids
razorpay_secret_arn
razorpay_webhook_url
rds_address
rds_database_name
rds_endpoint
rds_master_user_secret_arn
rds_port
rds_tunnel_instance_id
rest_api_url
s3_bucket_invoices
s3_bucket_logos
s3_bucket_products
sns_alerts_topic_arn
user_pool_id
voice_realtime_input_sample_rate
voice_realtime_output_sample_rate
voice_realtime_ws_url
voice_sessions_table
vpc_id
websocket_api_url
websocket_connections_table
workflow_runs_queue_url
`

const preTaskEnvironmentManifest = `
ALLOWED_ORIGINS
AWS_ENDPOINT
COGNITO_CLIENT_ID
COGNITO_DOMAIN
COGNITO_PHONE_CLIENT_ID
COGNITO_PHONE_OTP_COOLDOWN_TABLE
COGNITO_PHONE_REGION
COGNITO_PHONE_USER_POOL_ID
COGNITO_REGION
COGNITO_USER_POOL_ID
CREDENTIAL_ENCRYPTION_SECRET_ARN
DATABASE_HOST_SSM_PARAM
DATABASE_NAME
DATABASE_PORT
DATABASE_SECRET_ARN
DATABASE_SSL_MODE
DEEPGRAM_SECRET_ARN
DEEPGRAM_VOICE_AGENT_URL
DEEPGRAM_VOICE_INPUT_ENCODING
DEEPGRAM_VOICE_INPUT_SAMPLE_RATE
DEEPGRAM_VOICE_LISTEN_MODEL
DEEPGRAM_VOICE_OUTPUT_ENCODING
DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE
DEEPGRAM_VOICE_SPEAK_MODEL
DEEPSEEK_BASE_URL
DEEPSEEK_MODEL
DEEPSEEK_SECRET_ARN
ENVIRONMENT
EXA_BASE_URL
EXA_SECRET_ARN
EXA_TIMEOUT
GST_LOOKUP_BASE_URL
GST_LOOKUP_SECRET_ARN
GST_LOOKUP_TIMEOUT
GST_PROVIDER_SECRET_ARN
INDIA_AUTH_MESSAGE_TEMPLATE
INDIA_AUTH_TEMPLATE_ID
INDIA_DLT_ENTITY_ID
INDIA_SENDER_ID
INDIA_SIGNUP_MESSAGE_TEMPLATE
INDIA_SIGNUP_TEMPLATE_ID
JWT_ACCESS_TOKEN_EXPIRY
JWT_REFRESH_TOKEN_EXPIRY
KEY_ARN
KEY_ID
LLM_API_URL
LLM_MODEL
LLM_SECRET_ARN
LOG_FORMAT
LOG_LEVEL
MCP_SERVER_URL
RAZORPAY_SECRET_ARN
S3_BUCKET_EMAIL_SINK
S3_BUCKET_INVOICES
S3_BUCKET_LOGOS
S3_BUCKET_PRODUCTS
SES_CONFIGURATION_SET
SES_SENDER_EMAIL
SERVER_BASE_URL
SERVER_PORT
SMS_REGION
SQS_BARGAINING_QUEUE
SQS_EMAIL_DELIVERY_QUEUE
SQS_GST_QUEUE
SQS_INVOICE_QUEUE
SQS_PAYMENT_QUEUE
VOICE_SESSIONS_TABLE
VOICE_SESSION_WORKER_FUNCTION_NAME
VOICE_WS_EVENT_POLL_INTERVAL_MS
VOICE_WS_EVENT_TTL_SECONDS
VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER
VOICE_WS_MAX_FRAME_BYTES
VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES
VOICE_WS_MAX_SESSION_SECONDS
VOICE_WS_PING_INTERVAL_SECONDS
VOICE_WS_PROVIDER_READY_TIMEOUT_SECONDS
VOICE_WS_WRITE_TIMEOUT_SECONDS
WEBSOCKET_API_ENDPOINT
WEBSOCKET_CONNECTIONS_TABLE
`

func TestTerraformUsesBilleifBrandingAndVerifiedSESSenderContract(t *testing.T) {
	terraform := readTerraformSources(t)

	for _, required := range []string{
		`default     = "billeif"`,
		`resource_prefix = "${var.project_name}-${var.environment}"`,
		`length(local.resource_prefix) <= 29`,
		`cognito_hosted_ui_domain_prefix`,
		`"^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$"`,
		`strcontains(lower(var.cognito_domain_prefix), "billeif")`,
		`aws_cognito_resource_server`,
		`variable "ses_verified_identity"`,
		`variable "ses_sender_email"`,
		`ses_verified_identity_arn`,
		`values   = [var.ses_sender_email]`,
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
	for _, variable := range []string{"ses_verified_identity", "ses_sender_email"} {
		if terraformVariableHasDefault(variables, variable) {
			t.Errorf("%s must be caller-supplied and cannot have a default", variable)
		}
	}
	if strings.Contains(terraform, `resource "aws_ses_email_identity"`) {
		t.Error("Terraform must not create or verify an SES identity; it must reference the caller-supplied verified identity ARN")
	}
	for _, variable := range []string{"project_name", "environment", "user_pool_name", "client_name", "phone_user_pool_name", "phone_client_name", "cognito_domain_prefix"} {
		if !strings.Contains(variables, `var.`+variable+` == trimspace(var.`+variable+`)`) {
			t.Errorf("%s must reject surrounding whitespace instead of normalizing it", variable)
		}
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

func terraformVariableHasDefault(source, variable string) bool {
	start := strings.Index(source, `variable "`+variable+`"`)
	if start == -1 {
		return false
	}

	remainder := source[start:]
	next := strings.Index(remainder[len(`variable "`+variable+`"`):], `variable "`)
	if next == -1 {
		return regexp.MustCompile(`(?m)^\s*default\s*=`).MatchString(remainder)
	}
	return regexp.MustCompile(`(?m)^\s*default\s*=`).MatchString(remainder[:len(`variable "`+variable+`"`)+next])
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

func TestTerraformBrandingHasOnlyApprovedInterfaceAndNameDeltas(t *testing.T) {
	wantResources := manifestLines(preTaskResourceManifest)
	for _, removed := range []string{
		"aws_cloudwatch_log_group.lambda_sqs_payment",
		"aws_cloudwatch_metric_alarm.lambda_payment_errors",
		"aws_dynamodb_table.invoice_sequences",
		"aws_lambda_event_source_mapping.payment_queue",
		"aws_lambda_function.sqs_payment",
		"aws_ses_email_identity.main",
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
	} {
		wantResources = removeManifestEntry(wantResources, removed)
	}
	wantResources = append(wantResources,
		"aws_cognito_resource_server.main",
		"aws_cloudwatch_log_group.database_migrator",
		"aws_cloudwatch_log_group.lambda_outbox_dispatcher",
		"aws_cloudwatch_log_group.lambda_sqs_email_delivery",
		"aws_cloudwatch_metric_alarm.email_delivery_dlq_messages",
		"aws_cloudwatch_metric_alarm.email_delivery_queue_age",
		"aws_cloudwatch_metric_alarm.lambda_email_delivery_duration",
		"aws_cloudwatch_metric_alarm.lambda_email_delivery_errors",
		"aws_cloudwatch_metric_alarm.lambda_email_delivery_throttles",
		"aws_iam_role.database_migrator",
		"aws_iam_role.email_delivery",
		"aws_iam_role.outbox_dispatcher",
		"aws_iam_role.outbox_scheduler",
		"aws_iam_role_policy.database_migrator",
		"aws_iam_role_policy.email_delivery",
		"aws_iam_role_policy.outbox_dispatcher",
		"aws_iam_role_policy.outbox_scheduler",
		"aws_iam_role_policy_attachment.outbox_dispatcher_basic",
		"aws_iam_role_policy_attachment.outbox_dispatcher_vpc_access",
		"aws_iam_role_policy_attachment.email_delivery_basic",
		"aws_iam_role_policy_attachment.email_delivery_vpc_access",
		"aws_lambda_event_source_mapping.email_delivery_queue",
		"aws_lambda_function.database_migrator",
		"aws_lambda_function.outbox_dispatcher",
		"aws_lambda_function.sqs_email_delivery",
		"aws_lambda_invocation.database_migrations",
		"aws_scheduler_schedule.outbox_dispatcher",
		"aws_security_group.database_migrator",
		"aws_sns_topic_policy.ses_events",
		"aws_sqs_queue.email_delivery",
		"aws_sqs_queue.email_delivery_dlq",
		"aws_sqs_queue.outbox_dispatcher_scheduler_dlq",
		"aws_sqs_queue_policy.email_delivery",
		"aws_sqs_queue_policy.email_delivery_dlq",
		"aws_sqs_queue_policy.outbox_dispatcher_scheduler_dlq",
		"aws_sqs_queue_redrive_allow_policy.email_delivery_dlq",
		"aws_sqs_queue_redrive_policy.email_delivery",
	)
	assertExactManifest(t, "Terraform resource labels", terraformResourceLabels(t), wantResources)
	wantOutputs := manifestLines(preTaskOutputManifest)
	for _, removed := range []string{"lambda_sqs_payment_arn", "payment_processing_queue_url", "workflow_runs_queue_url"} {
		wantOutputs = removeManifestEntry(wantOutputs, removed)
	}
	wantOutputs = append(wantOutputs, "email_delivery_queue_url", "lambda_sqs_email_delivery_arn")
	assertExactManifest(t, "Terraform output keys", terraformOutputKeys(t), wantOutputs)
	wantEnvironment := removeManifestEntry(manifestLines(preTaskEnvironmentManifest), "SQS_PAYMENT_QUEUE")
	assertExactManifest(t, "Terraform environment keys", terraformEnvironmentKeys(t), wantEnvironment)

	providers := readTerraformFile(t, "providers.tf")
	if !strings.Contains(providers, `required_version = ">= 1.13.0, < 2.0.0"`) {
		t.Error("Terraform must require a version that supports the cross-variable resource-prefix validation")
	}

	for _, attribute := range terraformAWSNameAttributes(t) {
		allow, ok := stableAWSNameAttributeAllowlist[attribute.resourceType+"."+attribute.name]
		if !ok {
			t.Errorf("unclassified AWS name-bearing attribute %s.%s = %q; add an explicit allowlist entry", attribute.resourceType, attribute.name, attribute.expression)
			continue
		}
		if !containsOne(attribute.expression, allow) {
			t.Errorf("AWS name-bearing attribute %s.%s = %q does not use an approved expression %q", attribute.resourceType, attribute.name, attribute.expression, allow)
		}
	}
	if failures := stableNameLocalDefinitionFailures(terraformAWSNameAttributes(t), terraformLocalDefinitions(t)); len(failures) > 0 {
		t.Errorf("stable AWS name locals must resolve to Billeif-derived or explicit approved definitions:\n%s", strings.Join(failures, "\n"))
	}
}

func TestTerraformStableNameLocalValidationRejectsUnbrandedDefinition(t *testing.T) {
	failures := stableNameLocalDefinitionFailures(
		[]terraformAWSNameAttribute{{resourceType: "aws_sqs_queue", name: "name", expression: "local.unbranded_queue_name"}},
		map[string]string{"unbranded_queue_name": `"legacy-queue"`},
	)
	if len(failures) == 0 {
		t.Fatal("an unbranded local used as a stable AWS name must be rejected")
	}
}

func TestTerraformEnvironmentKeyScannerIncludesEveryTerraformSource(t *testing.T) {
	keys := terraformEnvironmentKeysFromSources([]string{
		`resource "aws_lambda_function" "http" {
  environment {
    variables = {
      HTTP_KEY = "value"
    }
  }
}`,
		`resource "aws_lambda_function" "phone" {
  environment {
    variables = {
      PHONE_KEY = "value"
    }
  }
}`,
	})
	for _, want := range []string{"HTTP_KEY", "PHONE_KEY"} {
		if !containsString(keys, want) {
			t.Errorf("environment key from a Terraform source was missed: %s", want)
		}
	}
}

func TestTerraformEnvironmentKeyScannerIgnoresUnrelatedUppercaseMapKeys(t *testing.T) {
	keys := terraformEnvironmentKeysFromSources([]string{`
locals {
  unrelated_metadata = {
    NOT_A_RUNTIME_ENV_KEY = "value"
  }
}

resource "aws_lambda_function" "http" {
  environment {
    variables = {
      REAL_ENV_KEY = "value"
    }
  }
}`})
	if containsString(keys, "NOT_A_RUNTIME_ENV_KEY") {
		t.Fatal("uppercase keys outside Lambda environment.variables must not enter the runtime environment manifest")
	}
	if !containsString(keys, "REAL_ENV_KEY") {
		t.Fatal("a Lambda environment.variables key must enter the runtime environment manifest")
	}
}

func TestTerraformEnvironmentKeyScannerIgnoresLambdaResourceInBlockComment(t *testing.T) {
	source := `
/*
resource "aws_lambda_function" "fake" {
  environment {
    variables = {
      FAKE_BLOCK_COMMENT_KEY = "value"
    }
  }
}
*/
resource "aws_lambda_function" "real" {
  environment {
    variables = {
      REAL_BLOCK_COMMENT_KEY = "value"
    }
  }
}`
	if bodies := terraformLambdaResourceBodies(source); len(bodies) != 1 {
		t.Fatalf("only the real Lambda resource outside the block comment should be discovered, got %d", len(bodies))
	}
	keys := terraformEnvironmentKeysFromSources([]string{source})
	if containsString(keys, "FAKE_BLOCK_COMMENT_KEY") {
		t.Fatal("a Lambda environment key inside a block comment must not enter the runtime environment manifest")
	}
	if !containsString(keys, "REAL_BLOCK_COMMENT_KEY") {
		t.Fatal("a real Lambda resource adjacent to a block comment must still be discovered")
	}
}

func TestTerraformEnvironmentKeyScannerIgnoresLambdaResourcesInLineComments(t *testing.T) {
	for name, source := range map[string]string{
		"slash": `
// resource "aws_lambda_function" "fake" { environment { variables = { FAKE_SLASH_COMMENT_KEY = "value" } } }
resource "aws_lambda_function" "real" {
  environment {
    variables = {
      REAL_SLASH_COMMENT_KEY = "value"
    }
  }
}`,
		"hash": `
# resource "aws_lambda_function" "fake" { environment { variables = { FAKE_HASH_COMMENT_KEY = "value" } } }
resource "aws_lambda_function" "real" {
  environment {
    variables = {
      REAL_HASH_COMMENT_KEY = "value"
    }
  }
}`,
	} {
		t.Run(name, func(t *testing.T) {
			if bodies := terraformLambdaResourceBodies(source); len(bodies) != 1 {
				t.Fatalf("only the real Lambda resource outside the %s line comment should be discovered, got %d", name, len(bodies))
			}
			keys := terraformEnvironmentKeysFromSources([]string{source})
			fakeKey := "FAKE_" + strings.ToUpper(name) + "_COMMENT_KEY"
			realKey := "REAL_" + strings.ToUpper(name) + "_COMMENT_KEY"
			if containsString(keys, fakeKey) {
				t.Fatalf("a Lambda environment key inside a %s line comment must not enter the runtime environment manifest", name)
			}
			if !containsString(keys, realKey) {
				t.Fatalf("a real Lambda resource adjacent to a %s line comment must still be discovered", name)
			}
		})
	}
}

func TestTerraformEnvironmentKeyScannerIgnoresLambdaResourceInIndentedHeredoc(t *testing.T) {
	source := `
locals {
  fake_lambda = <<-EOT
resource "aws_lambda_function" "fake" {
  environment {
    variables = {
      FAKE_HEREDOC_KEY = "value"
    }
  }
}
shell_function {
  EOT
}
resource "aws_lambda_function" "real" {
  environment {
    variables = {
      REAL_HEREDOC_KEY = "value"
    }
  }
}`
	if bodies := terraformLambdaResourceBodies(source); len(bodies) != 1 {
		t.Fatalf("only the real Lambda resource outside the indented heredoc should be discovered, got %d", len(bodies))
	}
	keys := terraformEnvironmentKeysFromSources([]string{source})
	if containsString(keys, "FAKE_HEREDOC_KEY") {
		t.Fatal("a Lambda environment key inside an indented heredoc must not enter the runtime environment manifest")
	}
	if !containsString(keys, "REAL_HEREDOC_KEY") {
		t.Fatal("a real Lambda resource adjacent to an indented heredoc must still be discovered")
	}
}

func TestTerraformEnvironmentKeyScannerIgnoresLambdaResourceInQuotedString(t *testing.T) {
	source := `
locals {
  fake_lambda = "resource \"aws_lambda_function\" \"fake\" { environment { variables = { FAKE_QUOTED_STRING_KEY = \"value\" } } }"
}
resource "aws_lambda_function" "real" {
  environment {
    variables = {
      REAL_QUOTED_STRING_KEY = "value"
    }
  }
}`
	if bodies := terraformLambdaResourceBodies(source); len(bodies) != 1 {
		t.Fatalf("only the real Lambda resource outside the ordinary quoted string should be discovered, got %d", len(bodies))
	}
	keys := terraformEnvironmentKeysFromSources([]string{source})
	if containsString(keys, "FAKE_QUOTED_STRING_KEY") {
		t.Fatal("a Lambda environment key inside an ordinary quoted string must not enter the runtime environment manifest")
	}
	if !containsString(keys, "REAL_QUOTED_STRING_KEY") {
		t.Fatal("a real Lambda resource adjacent to an ordinary quoted string must still be discovered")
	}
}

func TestTerraformStableNameLocalValidationRejectsConditionalBadBranch(t *testing.T) {
	failures := stableNameLocalDefinitionFailures(
		[]terraformAWSNameAttribute{{resourceType: "aws_cognito_user_pool_client", name: "name", expression: "local.cognito_web_client_name"}},
		map[string]string{
			"cognito_web_client_name": `var.client_name != "" ? var.client_name : "legacy-client"`,
		},
	)
	if len(failures) == 0 {
		t.Fatal("an unbranded conditional output branch must be rejected")
	}
}

func TestTerraformStableNameLocalValidationRejectsCoalesceBadBranch(t *testing.T) {
	failures := stableNameLocalDefinitionFailures(
		[]terraformAWSNameAttribute{{resourceType: "aws_cognito_resource_server", name: "name", expression: "local.cognito_resource_server_name"}},
		map[string]string{
			"cognito_resource_server_name": `coalesce(local.resource_prefix, "legacy-resource-server")`,
			"resource_prefix":              `"${var.project_name}-${var.environment}"`,
		},
	)
	if len(failures) == 0 {
		t.Fatal("an unbranded coalesce fallback branch must be rejected")
	}
}

func TestTerraformStableNameLocalValidationRejectsListAndMapBadBranches(t *testing.T) {
	for name, definition := range map[string]string{
		"list": `[local.resource_prefix, "legacy-resource-server"]`,
		"map":  `{safe = local.resource_prefix, unsafe = "legacy-resource-server"}`,
	} {
		t.Run(name, func(t *testing.T) {
			failures := stableNameLocalDefinitionFailures(
				[]terraformAWSNameAttribute{{resourceType: "aws_cognito_resource_server", name: "name", expression: "local.cognito_resource_server_name"}},
				map[string]string{
					"cognito_resource_server_name": definition,
					"resource_prefix":              `"${var.project_name}-${var.environment}"`,
				},
			)
			if len(failures) == 0 {
				t.Fatal("an unbranded list or map output branch must be rejected")
			}
		})
	}
}

func TestTerraformStableNameLocalValidationRejectsNestedBadBranch(t *testing.T) {
	failures := stableNameLocalDefinitionFailures(
		[]terraformAWSNameAttribute{{resourceType: "aws_cognito_resource_server", name: "identifier", expression: "local.cognito_resource_server_id"}},
		map[string]string{
			"cognito_resource_server_id": `local.nested_name`,
			"nested_name":                `var.use_safe_name ? local.resource_prefix : "legacy-api"`,
			"resource_prefix":            `"${var.project_name}-${var.environment}"`,
		},
	)
	if len(failures) == 0 {
		t.Fatal("an unbranded branch in a nested local must be rejected")
	}
}

func TestTerraformStableNameLocalValidationReportsCyclesDeterministically(t *testing.T) {
	attributes := []terraformAWSNameAttribute{{resourceType: "aws_cognito_resource_server", name: "identifier", expression: "local.cognito_resource_server_id"}}
	definitions := map[string]string{
		"cognito_resource_server_id": `local.first_name`,
		"first_name":                 `local.second_name`,
		"second_name":                `local.first_name`,
	}
	first := stableNameLocalDefinitionFailures(attributes, definitions)
	second := stableNameLocalDefinitionFailures(attributes, definitions)
	if len(first) == 0 || strings.Join(first, "\n") != strings.Join(second, "\n") {
		t.Fatalf("cyclic local definitions must fail deterministically, got %q then %q", first, second)
	}
}

// Every top-level Terraform field that identifies a stable AWS resource has an
// explicit expression allowlist. The exceptions preserve stable API stage and
// Cognito group interfaces, while all other entries are Billeif project and
// environment names or validated caller overrides.
var stableAWSNameAttributeAllowlist = map[string][]string{
	"aws_api_gateway_authorizer.name":                           {"local.resource_prefix"},
	"aws_api_gateway_method_settings.stage_name":                {"aws_api_gateway_stage.main.stage_name"},
	"aws_api_gateway_rest_api.name":                             {"local.resource_prefix"},
	"aws_api_gateway_stage.stage_name":                          {"var.environment"},
	"aws_apigatewayv2_api.name":                                 {"local.resource_prefix"},
	"aws_apigatewayv2_stage.name":                               {"var.environment"},
	"aws_cloudwatch_dashboard.dashboard_name":                   {"local.resource_prefix"},
	"aws_cloudwatch_log_group.name":                             {"local.resource_prefix", "local.voice_session_lambda_name"},
	"aws_cloudwatch_log_metric_filter.log_group_name":           {"each.value"},
	"aws_cloudwatch_log_metric_filter.name":                     {"local.resource_prefix"},
	"aws_cloudwatch_metric_alarm.alarm_name":                    {"local.resource_prefix"},
	"aws_cognito_resource_server.identifier":                    {"local.cognito_resource_server_id"},
	"aws_cognito_resource_server.name":                          {"local.cognito_resource_server_name"},
	"aws_cognito_user_group.name":                               {`"admin"`, `"accountant"`, `"viewer"`},
	"aws_cognito_user_pool.name":                                {"local.cognito_"},
	"aws_cognito_user_pool_client.name":                         {"local.cognito_"},
	"aws_cognito_user_pool_domain.domain":                       {"local.cognito_hosted_ui_domain_prefix"},
	"aws_db_instance.identifier":                                {"local.resource_prefix"},
	"aws_db_subnet_group.name":                                  {"local.resource_prefix"},
	"aws_dynamodb_table.name":                                   {"local.resource_prefix", "local.phone_auth_cooldown_table_name", "local.websocket_connections_table", "local.voice_sessions_table_name"},
	"aws_eip.domain":                                            {`"vpc"`},
	"aws_iam_instance_profile.name":                             {"local.resource_prefix"},
	"aws_iam_role.name":                                         {"local.resource_prefix"},
	"aws_iam_role_policy.name":                                  {"local.resource_prefix"},
	"aws_kms_alias.name":                                        {"var.project_name"},
	"aws_lambda_event_source_mapping.function_name":             {"aws_lambda_function."},
	"aws_lambda_function.function_name":                         {"local.resource_prefix", "local.voice_session_lambda_name"},
	"aws_lambda_invocation.function_name":                       {"aws_lambda_function."},
	"aws_lambda_permission.function_name":                       {"aws_lambda_function."},
	"aws_s3_bucket.bucket":                                      {"local.bucket_prefix"},
	"aws_s3_bucket_lifecycle_configuration.bucket":              {"aws_s3_bucket."},
	"aws_s3_bucket_public_access_block.bucket":                  {"aws_s3_bucket."},
	"aws_s3_bucket_server_side_encryption_configuration.bucket": {"aws_s3_bucket."},
	"aws_s3_bucket_versioning.bucket":                           {"aws_s3_bucket."},
	"aws_s3_object.bucket":                                      {"aws_s3_bucket."},
	"aws_scheduler_schedule.name":                               {"local.resource_prefix"},
	"aws_secretsmanager_secret.name":                            {"var.project_name"},
	"aws_security_group.name":                                   {"local.resource_prefix"},
	"aws_ses_configuration_set.name":                            {"local.resource_prefix"},
	"aws_ses_event_destination.name":                            {"local.resource_prefix"},
	"aws_sns_topic.name":                                        {"local.resource_prefix"},
	"aws_sqs_queue.name":                                        {"local.resource_prefix"},
	"aws_ssm_parameter.name":                                    {"local.db_host_ssm_parameter_name"},
}

type terraformAWSNameAttribute struct {
	resourceType string
	name         string
	expression   string
}

var terraformResourceDeclaration = regexp.MustCompile(`(?m)^resource\s+"([^"]+)"\s+"([^"]+)"\s*\{`)
var terraformOutputDeclaration = regexp.MustCompile(`(?m)^output\s+"([^"]+)"\s*\{`)
var terraformLocalReference = regexp.MustCompile(`\blocal\.([A-Za-z0-9_]+)\b`)

var terraformStableNameAttributes = []string{
	"name",
	"bucket",
	"identifier",
	"function_name",
	"log_group_name",
	"alarm_name",
	"dashboard_name",
	"domain",
	"stage_name",
}

func terraformResourceLabels(t *testing.T) []string {
	t.Helper()
	var labels []string
	for _, source := range terraformSourceFiles(t) {
		for _, match := range terraformResourceDeclaration.FindAllStringSubmatch(source, -1) {
			labels = append(labels, match[1]+"."+match[2])
		}
	}
	return sortedUnique(labels)
}

func terraformOutputKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for _, source := range terraformSourceFiles(t) {
		for _, match := range terraformOutputDeclaration.FindAllStringSubmatch(source, -1) {
			keys = append(keys, match[1])
		}
	}
	return sortedUnique(keys)
}

func terraformEnvironmentKeys(t *testing.T) []string {
	t.Helper()
	return terraformEnvironmentKeysFromSources(terraformSourceFiles(t))
}

func terraformEnvironmentKeysFromSources(sources []string) []string {
	var keys []string
	localDefinitions := terraformLocalDefinitionsFromSources(sources)
	for _, source := range sources {
		for _, resourceBody := range terraformLambdaResourceBodies(source) {
			for _, environmentBody := range terraformTopLevelBlocks(resourceBody, "environment") {
				variables, exists := terraformTopLevelAssignments(environmentBody)["variables"]
				if !exists {
					continue
				}
				keys = append(keys, terraformEnvironmentMapKeys(variables, localDefinitions, make(map[string]bool))...)
			}
		}
	}
	return sortedUnique(keys)
}

func terraformLocalDefinitions(t *testing.T) map[string]string {
	t.Helper()
	return terraformLocalDefinitionsFromSources(terraformSourceFiles(t))
}

func terraformLocalDefinitionsFromSources(sources []string) map[string]string {
	definitions := make(map[string]string)
	for _, source := range sources {
		for _, localsBody := range terraformTopLevelBlocks(source, "locals") {
			for name, expression := range terraformTopLevelAssignments(localsBody) {
				definitions[name] = expression
			}
		}
	}
	return definitions
}

func terraformLambdaResourceBodies(source string) []string {
	var bodies []string
	depth := 0
	for index := 0; index < len(source); {
		if next, skipped := terraformSkipQuotedCommentOrHeredoc(source, index); skipped {
			index = next
			continue
		}
		switch source[index] {
		case '{', '(', '[':
			depth++
			index++
			continue
		case '}', ')', ']':
			if depth > 0 {
				depth--
			}
			index++
			continue
		}
		if depth != 0 || !terraformIdentifierStart(source[index]) {
			index++
			continue
		}

		start, end := index, terraformIdentifierEnd(source, index)
		if source[start:end] != "resource" {
			index = end
			continue
		}
		resourceType, next, ok := terraformQuotedLabel(source, terraformSkipWhitespace(source, end))
		if !ok {
			index = end
			continue
		}
		_, next, ok = terraformQuotedLabel(source, terraformSkipWhitespace(source, next))
		if !ok {
			index = end
			continue
		}
		openingBrace := terraformSkipWhitespace(source, next)
		if openingBrace >= len(source) || source[openingBrace] != '{' {
			index = end
			continue
		}
		closingBrace := terraformMatchingDelimiter(source, openingBrace, '{', '}')
		if closingBrace == -1 {
			return bodies
		}
		if resourceType == "aws_lambda_function" {
			bodies = append(bodies, source[openingBrace+1:closingBrace])
		}
		index = closingBrace + 1
	}
	return bodies
}

func terraformQuotedLabel(source string, index int) (string, int, bool) {
	if index >= len(source) || source[index] != '"' {
		return "", index, false
	}
	start := index + 1
	for index = start; index < len(source); index++ {
		if source[index] == '\\' {
			index++
			continue
		}
		if source[index] == '"' {
			return source[start:index], index + 1, true
		}
	}
	return "", len(source), false
}

func terraformEnvironmentMapKeys(expression string, localDefinitions map[string]string, visiting map[string]bool) []string {
	expression = terraformTrimOuterParentheses(strings.TrimSpace(expression))
	if name, arguments, isCall := terraformFunctionArguments(expression); isCall && (name == "merge" || name == "coalesce" || name == "try") {
		var keys []string
		for _, argument := range arguments {
			keys = append(keys, terraformEnvironmentMapKeys(argument, localDefinitions, visiting)...)
		}
		return sortedUnique(keys)
	}
	if terraformIsDelimited(expression, '{', '}') {
		keys := make([]string, 0)
		for key := range terraformTopLevelAssignments(expression[1 : len(expression)-1]) {
			keys = append(keys, key)
		}
		return sortedUnique(keys)
	}

	path := terraformLocalReferencePath(expression)
	if len(path) == 0 || visiting[strings.Join(path, ".")] {
		return nil
	}
	definition, exists := localDefinitions[path[0]]
	if !exists {
		return nil
	}
	visitingKey := strings.Join(path, ".")
	visiting[visitingKey] = true
	defer delete(visiting, visitingKey)
	for _, field := range path[1:] {
		if !terraformIsDelimited(definition, '{', '}') {
			return nil
		}
		value, exists := terraformTopLevelAssignments(definition[1 : len(definition)-1])[field]
		if !exists {
			return nil
		}
		definition = value
	}
	return terraformEnvironmentMapKeys(definition, localDefinitions, visiting)
}

func terraformLocalReferencePath(expression string) []string {
	expression = strings.TrimSpace(expression)
	if !strings.HasPrefix(expression, "local.") {
		return nil
	}
	path := strings.Split(strings.TrimPrefix(expression, "local."), ".")
	for _, segment := range path {
		if segment == "" || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`).MatchString(segment) {
			return nil
		}
	}
	return path
}

func terraformTopLevelBlocks(source, name string) []string {
	var bodies []string
	depth := 0
	for index := 0; index < len(source); {
		if next, skipped := terraformSkipQuotedCommentOrHeredoc(source, index); skipped {
			index = next
			continue
		}
		switch source[index] {
		case '{', '(', '[':
			depth++
			index++
			continue
		case '}', ')', ']':
			if depth > 0 {
				depth--
			}
			index++
			continue
		}
		if depth == 0 && terraformIdentifierStart(source[index]) {
			start, end := index, terraformIdentifierEnd(source, index)
			if source[start:end] == name {
				next := terraformSkipWhitespace(source, end)
				if next < len(source) && source[next] == '{' {
					closing := terraformMatchingDelimiter(source, next, '{', '}')
					if closing != -1 {
						bodies = append(bodies, source[next+1:closing])
						index = closing + 1
						continue
					}
				}
			}
			index = end
			continue
		}
		index++
	}
	return bodies
}

func terraformTopLevelAssignments(body string) map[string]string {
	type topLevelEntry struct {
		name       string
		valueStart int
		start      int
		assignment bool
	}
	var entries []topLevelEntry
	depth := 0
	for index := 0; index < len(body); {
		if next, skipped := terraformSkipQuotedCommentOrHeredoc(body, index); skipped {
			index = next
			continue
		}
		switch body[index] {
		case '{', '(', '[':
			depth++
			index++
			continue
		case '}', ')', ']':
			if depth > 0 {
				depth--
			}
			index++
			continue
		}
		if depth == 0 && terraformIdentifierStart(body[index]) {
			start, end := index, terraformIdentifierEnd(body, index)
			next := terraformSkipWhitespace(body, end)
			if next < len(body) && body[next] == '=' && (next+1 == len(body) || body[next+1] != '=') {
				entries = append(entries, topLevelEntry{name: body[start:end], valueStart: next + 1, start: start, assignment: true})
			} else if next < len(body) && body[next] == '{' {
				entries = append(entries, topLevelEntry{start: start})
			}
			index = end
			continue
		}
		index++
	}
	assignments := make(map[string]string)
	for index, entry := range entries {
		if !entry.assignment {
			continue
		}
		end := len(body)
		if index+1 < len(entries) {
			end = entries[index+1].start
		}
		assignments[entry.name] = strings.TrimRight(strings.TrimSpace(body[entry.valueStart:end]), ",")
	}
	return assignments
}

func terraformMatchingDelimiter(source string, opening int, open, close byte) int {
	depth := 0
	for index := opening; index < len(source); {
		if next, skipped := terraformSkipQuotedCommentOrHeredoc(source, index); skipped {
			index = next
			continue
		}
		if source[index] == open {
			depth++
		} else if source[index] == close {
			depth--
			if depth == 0 {
				return index
			}
		}
		index++
	}
	return -1
}

func terraformSkipQuotedCommentOrHeredoc(source string, index int) (int, bool) {
	if source[index] == '"' {
		index++
		for index < len(source) {
			if source[index] == '\\' {
				index += 2
				continue
			}
			if source[index] == '"' {
				return index + 1, true
			}
			index++
		}
		return index, true
	}
	if source[index] == '#' || (source[index] == '/' && index+1 < len(source) && source[index+1] == '/') {
		for index < len(source) && source[index] != '\n' {
			index++
		}
		return index, true
	}
	if source[index] == '/' && index+1 < len(source) && source[index+1] == '*' {
		index += 2
		for index+1 < len(source) && !(source[index] == '*' && source[index+1] == '/') {
			index++
		}
		if index+1 < len(source) {
			return index + 2, true
		}
		return len(source), true
	}
	if source[index] == '<' && index+1 < len(source) && source[index+1] == '<' {
		if end, ok := terraformHeredocEnd(source, index); ok {
			return end, true
		}
	}
	return index, false
}

func terraformHeredocEnd(source string, index int) (int, bool) {
	delimiterStart := index + 2
	indented := delimiterStart < len(source) && source[delimiterStart] == '-'
	if indented {
		delimiterStart++
	}
	if delimiterStart >= len(source) || !terraformIdentifierStart(source[delimiterStart]) {
		return index, false
	}
	delimiterEnd := terraformIdentifierEnd(source, delimiterStart)
	delimiter := source[delimiterStart:delimiterEnd]
	lineEnd := delimiterEnd
	for lineEnd < len(source) && (source[lineEnd] == ' ' || source[lineEnd] == '\t' || source[lineEnd] == '\r') {
		lineEnd++
	}
	if lineEnd >= len(source) || source[lineEnd] != '\n' {
		return index, false
	}

	for lineStart := lineEnd + 1; lineStart <= len(source); {
		nextLine := strings.IndexByte(source[lineStart:], '\n')
		if nextLine == -1 {
			nextLine = len(source)
		} else {
			nextLine += lineStart
		}
		candidate := strings.TrimSuffix(source[lineStart:nextLine], "\r")
		candidate = strings.TrimRight(candidate, " \t")
		if indented {
			candidate = strings.TrimLeft(candidate, " \t")
		}
		if candidate == delimiter {
			if nextLine < len(source) {
				return nextLine + 1, true
			}
			return nextLine, true
		}
		if nextLine == len(source) {
			return len(source), true
		}
		lineStart = nextLine + 1
	}
	return len(source), true
}

func terraformSkipWhitespace(source string, index int) int {
	for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n') {
		index++
	}
	return index
}

func terraformIdentifierStart(character byte) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character == '_'
}

func terraformIdentifierEnd(source string, index int) int {
	for index < len(source) && (terraformIdentifierStart(source[index]) || source[index] >= '0' && source[index] <= '9' || source[index] == '-') {
		index++
	}
	return index
}

func terraformTrimOuterParentheses(expression string) string {
	for terraformIsDelimited(expression, '(', ')') {
		expression = strings.TrimSpace(expression[1 : len(expression)-1])
	}
	return expression
}

func terraformIsDelimited(expression string, open, close byte) bool {
	expression = strings.TrimSpace(expression)
	return len(expression) >= 2 && expression[0] == open && terraformMatchingDelimiter(expression, 0, open, close) == len(expression)-1
}

func terraformFunctionArguments(expression string) (string, []string, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" || !terraformIdentifierStart(expression[0]) {
		return "", nil, false
	}
	end := terraformIdentifierEnd(expression, 0)
	opening := terraformSkipWhitespace(expression, end)
	if opening >= len(expression) || expression[opening] != '(' || terraformMatchingDelimiter(expression, opening, '(', ')') != len(expression)-1 {
		return "", nil, false
	}
	return expression[:end], terraformSplitTopLevel(expression[opening+1:len(expression)-1], ','), true
}

func terraformSplitTopLevel(source string, delimiter byte) []string {
	var values []string
	start, depth := 0, 0
	for index := 0; index < len(source); {
		if next, skipped := terraformSkipQuotedCommentOrHeredoc(source, index); skipped {
			index = next
			continue
		}
		switch source[index] {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			if depth > 0 {
				depth--
			}
		case delimiter:
			if depth == 0 {
				values = append(values, strings.TrimSpace(source[start:index]))
				start = index + 1
			}
		}
		index++
	}
	if value := strings.TrimSpace(source[start:]); value != "" {
		values = append(values, value)
	}
	return values
}

type stableNameLocalDefinitionContract struct {
	explicitValidatedOverrides []string
	derivedTokenSets           [][]string
}

var stableNameLocalDefinitionContracts = map[string]stableNameLocalDefinitionContract{
	"resource_prefix": {
		derivedTokenSets: [][]string{{"var.project_name", "var.environment"}},
	},
	"cognito_web_user_pool_name": {
		explicitValidatedOverrides: []string{"var.user_pool_name"},
	},
	"cognito_web_client_name": {
		explicitValidatedOverrides: []string{"var.client_name"},
	},
	"cognito_native_user_pool_name": {
		explicitValidatedOverrides: []string{"var.phone_user_pool_name"},
	},
	"cognito_native_client_name": {
		explicitValidatedOverrides: []string{"var.phone_client_name"},
	},
	"cognito_hosted_ui_domain_prefix": {
		explicitValidatedOverrides: []string{"var.cognito_domain_prefix"},
		derivedTokenSets:           [][]string{{`"billeif-`, "var.environment"}},
	},
	"phone_auth_cooldown_table_name": {
		explicitValidatedOverrides: []string{"var.phone_auth_cooldown_table_name"},
	},
	"websocket_connections_table": {
		explicitValidatedOverrides: []string{"var.websocket_connections_table"},
	},
	"voice_sessions_table_name": {
		explicitValidatedOverrides: []string{"var.voice_sessions_table_name"},
	},
	"voice_session_lambda_name": {
		explicitValidatedOverrides: []string{"var.voice_session_lambda_function_name"},
	},
	"db_host_ssm_parameter_name": {
		derivedTokenSets: [][]string{{"var.project_name", "var.environment"}},
	},
}

func stableNameLocalDefinitionFailures(attributes []terraformAWSNameAttribute, definitions map[string]string) []string {
	var failures []string
	validated := make(map[string]error)
	for _, attribute := range attributes {
		for _, match := range terraformLocalReference.FindAllStringSubmatch(attribute.expression, -1) {
			name := match[1]
			if _, alreadyValidated := validated[name]; !alreadyValidated {
				validated[name] = validateStableNameLocalDefinition(name, definitions, stableNameLocalDefinitionContracts[name], nil)
			}
			if err := validated[name]; err != nil {
				failures = append(failures, fmt.Sprintf("%s.%s references local.%s: %v", attribute.resourceType, attribute.name, name, err))
			}
		}
	}
	return sortedUnique(failures)
}

func validateStableNameLocalDefinition(name string, definitions map[string]string, inheritedContract stableNameLocalDefinitionContract, path []string) error {
	for index, ancestor := range path {
		if ancestor == name {
			return fmt.Errorf("cyclic local definition: %s", strings.Join(append(path[index:], name), " -> "))
		}
	}
	definition, exists := definitions[name]
	if !exists {
		return fmt.Errorf("definition is missing")
	}
	contract := inheritedContract
	if declaredContract, declared := stableNameLocalDefinitionContracts[name]; declared {
		contract = declaredContract
	}
	return validateStableNameExpression(definition, definitions, contract, append(path, name))
}

func validateStableNameExpression(expression string, definitions map[string]string, contract stableNameLocalDefinitionContract, path []string) error {
	expression = terraformTrimOuterParentheses(strings.TrimSpace(expression))
	if trueBranch, falseBranch, conditional := terraformConditionalBranches(expression); conditional {
		if err := validateStableNameExpression(trueBranch, definitions, contract, path); err != nil {
			return fmt.Errorf("conditional true branch: %w", err)
		}
		if err := validateStableNameExpression(falseBranch, definitions, contract, path); err != nil {
			return fmt.Errorf("conditional false branch: %w", err)
		}
		return nil
	}
	if function, arguments, isCall := terraformFunctionArguments(expression); isCall && (function == "coalesce" || function == "try") {
		for index, argument := range arguments {
			if err := validateStableNameExpression(argument, definitions, contract, path); err != nil {
				return fmt.Errorf("%s branch %d: %w", function, index+1, err)
			}
		}
		return nil
	}
	if terraformIsDelimited(expression, '[', ']') {
		for index, item := range terraformSplitTopLevel(expression[1:len(expression)-1], ',') {
			if err := validateStableNameExpression(item, definitions, contract, path); err != nil {
				return fmt.Errorf("list branch %d: %w", index+1, err)
			}
		}
		return nil
	}
	if terraformIsDelimited(expression, '{', '}') {
		assignments := terraformTopLevelAssignments(expression[1 : len(expression)-1])
		keys := make([]string, 0, len(assignments))
		for key := range assignments {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := validateStableNameExpression(assignments[key], definitions, contract, path); err != nil {
				return fmt.Errorf("map branch %s: %w", key, err)
			}
		}
		return nil
	}

	for _, match := range terraformLocalReference.FindAllStringSubmatch(expression, -1) {
		if err := validateStableNameLocalDefinition(match[1], definitions, contract, path); err != nil {
			return fmt.Errorf("dependency local.%s: %w", match[1], err)
		}
	}
	if terraformLocalReference.FindString(expression) != "" {
		return nil
	}
	if containsString(contract.explicitValidatedOverrides, expression) {
		return nil
	}
	for _, derivedTokens := range contract.derivedTokenSets {
		if containsAll(expression, derivedTokens) {
			return nil
		}
	}
	return fmt.Errorf("unbranded output expression %q", expression)
}

func terraformConditionalBranches(expression string) (string, string, bool) {
	questionIndex, nestedQuestions, depth := -1, 0, 0
	for index := 0; index < len(expression); {
		if next, skipped := terraformSkipQuotedCommentOrHeredoc(expression, index); skipped {
			index = next
			continue
		}
		switch expression[index] {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			if depth > 0 {
				depth--
			}
		case '?':
			if depth == 0 {
				if questionIndex == -1 {
					questionIndex = index
				} else {
					nestedQuestions++
				}
			}
		case ':':
			if depth == 0 && questionIndex != -1 {
				if nestedQuestions == 0 {
					return strings.TrimSpace(expression[questionIndex+1 : index]), strings.TrimSpace(expression[index+1:]), true
				}
				nestedQuestions--
			}
		}
		index++
	}
	return "", "", false
}

func terraformAWSNameAttributes(t *testing.T) []terraformAWSNameAttribute {
	t.Helper()
	var attributes []terraformAWSNameAttribute
	for _, source := range terraformSourceFiles(t) {
		for _, match := range terraformResourceDeclaration.FindAllStringSubmatchIndex(source, -1) {
			resourceType := source[match[2]:match[3]]
			openingBrace := strings.LastIndex(source[match[0]:match[1]], "{") + match[0]
			closingBrace := terraformMatchingDelimiter(source, openingBrace, '{', '}')
			if closingBrace == -1 {
				continue
			}
			assignments := terraformTopLevelAssignments(source[openingBrace+1 : closingBrace])
			for _, name := range terraformStableNameAttributes {
				if expression, exists := assignments[name]; exists {
					attributes = append(attributes, terraformAWSNameAttribute{resourceType: resourceType, name: name, expression: expression})
				}
			}
		}
	}
	return attributes
}

func terraformSourceFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "infrastructure", "terraform", "*.tf"))
	if err != nil {
		t.Fatalf("glob Terraform sources: %v", err)
	}
	var sources []string
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		sources = append(sources, string(body))
	}
	return sources
}

func manifestLines(manifest string) []string {
	return sortedUnique(strings.Fields(manifest))
}

func removeManifestEntry(entries []string, entry string) []string {
	var remaining []string
	for _, candidate := range entries {
		if candidate != entry {
			remaining = append(remaining, candidate)
		}
	}
	return remaining
}

func sortedUnique(entries []string) []string {
	set := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		set[entry] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for entry := range set {
		result = append(result, entry)
	}
	sort.Strings(result)
	return result
}

func assertExactManifest(t *testing.T, name string, got, want []string) {
	t.Helper()
	got = sortedUnique(got)
	want = sortedUnique(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s changed unexpectedly\nwant:\n%s\n\ngot:\n%s", name, strings.Join(want, "\n"), strings.Join(got, "\n"))
	}
}

func containsOne(expression string, allowed []string) bool {
	for _, token := range allowed {
		if strings.Contains(expression, token) {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsAll(expression string, required []string) bool {
	for _, token := range required {
		if !strings.Contains(expression, token) {
			return false
		}
	}
	return true
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
