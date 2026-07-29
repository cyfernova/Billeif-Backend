package tests

import (
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

const preTaskLambdaEnvironmentManifest = `
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
JWT_ACCESS_TOKEN_EXPIRY
JWT_REFRESH_TOKEN_EXPIRY
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
SERVER_BASE_URL
SERVER_PORT
SQS_BARGAINING_QUEUE
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
		`resources = [local.ses_verified_identity_arn]`,
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
	wantResources = removeManifestEntry(wantResources, "aws_ses_email_identity.main")
	wantResources = append(wantResources, "aws_cognito_resource_server.main")
	assertExactManifest(t, "Terraform resource labels", terraformResourceLabels(t), wantResources)
	assertExactManifest(t, "Terraform output keys", terraformOutputKeys(t), manifestLines(preTaskOutputManifest))
	assertExactManifest(t, "Lambda environment keys", terraformLambdaEnvironmentKeys(t), manifestLines(preTaskLambdaEnvironmentManifest))

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
	"aws_lambda_permission.function_name":                       {"aws_lambda_function."},
	"aws_s3_bucket.bucket":                                      {"local.bucket_prefix"},
	"aws_s3_bucket_lifecycle_configuration.bucket":              {"aws_s3_bucket."},
	"aws_s3_bucket_public_access_block.bucket":                  {"aws_s3_bucket."},
	"aws_s3_bucket_server_side_encryption_configuration.bucket": {"aws_s3_bucket."},
	"aws_s3_bucket_versioning.bucket":                           {"aws_s3_bucket."},
	"aws_s3_object.bucket":                                      {"aws_s3_bucket."},
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
var terraformNameAttributeDeclaration = regexp.MustCompile(`^\s*(name|bucket|identifier|function_name|log_group_name|alarm_name|dashboard_name|domain|stage_name)\s*=\s*(.+?)\s*$`)
var terraformLambdaEnvironmentDeclaration = regexp.MustCompile(`^\s*([A-Z][A-Z0-9_]+)\s*=`)

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

func terraformLambdaEnvironmentKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for _, line := range strings.Split(readTerraformFile(t, "lambda.tf"), "\n") {
		if match := terraformLambdaEnvironmentDeclaration.FindStringSubmatch(line); match != nil {
			keys = append(keys, match[1])
		}
	}
	return sortedUnique(keys)
}

func terraformAWSNameAttributes(t *testing.T) []terraformAWSNameAttribute {
	t.Helper()
	var attributes []terraformAWSNameAttribute
	for _, source := range terraformSourceFiles(t) {
		resourceType := ""
		depth := 0
		for _, line := range strings.Split(source, "\n") {
			if resourceType == "" {
				if match := terraformResourceDeclaration.FindStringSubmatch(line); match != nil {
					resourceType = match[1]
					depth = terraformBraceDelta(line)
				}
				continue
			}

			if depth == 1 {
				if match := terraformNameAttributeDeclaration.FindStringSubmatch(line); match != nil {
					attributes = append(attributes, terraformAWSNameAttribute{resourceType: resourceType, name: match[1], expression: match[2]})
				}
			}
			depth += terraformBraceDelta(line)
			if depth == 0 {
				resourceType = ""
			}
		}
	}
	return attributes
}

func terraformBraceDelta(line string) int {
	return strings.Count(line, "{") - strings.Count(line, "}")
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
