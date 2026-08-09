locals {
  background_processing_enabled = var.enable_application && var.enable_background_processing

  # Construct invoke URLs from API IDs to avoid circular dependencies
  # (lambdas need stage URL, but stages depend on lambdas via deployments).
  http_api_invoke_url               = "https://${aws_apigatewayv2_api.http.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
  rest_api_invoke_url               = "https://${aws_api_gateway_rest_api.main.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
  websocket_api_invoke_url          = "wss://${aws_apigatewayv2_api.websocket.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
  websocket_management_api_endpoint = "https://${aws_apigatewayv2_api.websocket.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"

  lambda_artifacts = {
    api_http           = "${var.lambda_artifact_dir}/http.zip"
    a2a_stream         = "${var.lambda_artifact_dir}/a2a-stream.zip"
    sqs_invoice        = "${var.lambda_artifact_dir}/sqs-invoice.zip"
    sqs_gst            = "${var.lambda_artifact_dir}/sqs-gst.zip"
    sqs_bargaining     = "${var.lambda_artifact_dir}/sqs-bargaining.zip"
    ws_handler         = "${var.lambda_artifact_dir}/ws.zip"
    custom_sms_sender  = "${var.lambda_artifact_dir}/custom-sms-sender.zip"
    outbox             = "${var.lambda_artifact_dir}/outbox.zip"
    sqs_email_delivery = "${var.lambda_artifact_dir}/sqs-email-delivery.zip"
    sqs_ses_feedback   = "${var.lambda_artifact_dir}/sqs-ses-feedback.zip"
  }

  lambda_artifact_hashes = {
    for name, path in local.lambda_artifacts :
    name => fileexists(path) ? filebase64sha256(path) : null
  }

  lambda_artifact_hex_hashes = {
    for name, path in local.lambda_artifacts :
    name => fileexists(path) ? filesha256(path) : null
  }

  common_lambda_env = {
    ENVIRONMENT                      = var.environment
    LOG_LEVEL                        = "info"
    LOG_FORMAT                       = "json"
    SERVER_PORT                      = "8080"
    DATABASE_PORT                    = tostring(var.db_port)
    DATABASE_NAME                    = var.db_name
    DATABASE_SSL_MODE                = "require"
    ALLOWED_ORIGINS                  = join(",", distinct(concat([local.http_api_invoke_url], var.allowed_origins)))
    S3_BUCKET_LOGOS                  = aws_s3_bucket.business_logos.id
    S3_BUCKET_INVOICES               = aws_s3_bucket.invoices_pdf.id
    S3_BUCKET_PRODUCTS               = aws_s3_bucket.product_images.id
    S3_BUCKET_EMAIL_SINK             = aws_s3_bucket.email_sink.id
    SQS_INVOICE_QUEUE                = aws_sqs_queue.invoice_processing.url
    SQS_EMAIL_DELIVERY_QUEUE         = aws_sqs_queue.email_delivery.url
    SQS_GST_QUEUE                    = aws_sqs_queue.gst_processing.url
    SQS_BARGAINING_QUEUE             = aws_sqs_queue.bargaining_negotiation.url
    COGNITO_USER_POOL_ID             = aws_cognito_user_pool.main.id
    COGNITO_CLIENT_ID                = aws_cognito_user_pool_client.main.id
    COGNITO_DOMAIN                   = local.cognito_runtime_domain
    COGNITO_REGION                   = var.aws_region
    COGNITO_PHONE_USER_POOL_ID       = aws_cognito_user_pool.phone.id
    COGNITO_PHONE_CLIENT_ID          = aws_cognito_user_pool_client.phone.id
    COGNITO_PHONE_REGION             = "ap-south-1"
    COGNITO_PHONE_OTP_COOLDOWN_TABLE = aws_dynamodb_table.phone_auth_cooldowns.name
    JWT_ACCESS_TOKEN_EXPIRY          = "1h"
    JWT_REFRESH_TOKEN_EXPIRY         = "720h"
    WEBSOCKET_CONNECTIONS_TABLE      = aws_dynamodb_table.ws_connections.name
    LLM_API_URL                      = var.llm_api_url
    LLM_MODEL                        = var.llm_model
    EXA_BASE_URL                     = var.exa_base_url
    EXA_TIMEOUT                      = tostring(var.exa_timeout)
    GST_LOOKUP_BASE_URL              = var.gst_lookup_base_url
    GST_LOOKUP_TIMEOUT               = tostring(var.gst_lookup_timeout)
    DEEPSEEK_BASE_URL                = var.deepseek_base_url
    DEEPSEEK_MODEL                   = var.deepseek_model
    MCP_SERVER_URL                   = var.mcp_server_url
  }

  voice_http_lambda_env = local.voice_agentcore_runtime_enabled ? {
    AGENTCORE_RUNTIME_ARN             = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn
    AGENTCORE_RUNTIME_QUALIFIER       = var.promote_voice_agentcore_prod ? "PROD" : "STAGING"
    VOICE_ADMISSION_ENABLED           = tostring(var.enable_voice)
    VOICE_GLOBAL_CAPACITY_LIMIT       = "100"
    VOICE_KVS_CHANNEL_COUNT           = "12"
    VOICE_PER_USER_CAPACITY_LIMIT     = "1"
    VOICE_PROTOCOL_VERSION            = "1"
    VOICE_ROLLOUT_INTERNAL_SUB_HASHES = join(",", sort(tolist(var.voice_rollout_internal_sub_hashes)))
    VOICE_ROLLOUT_STAGE               = local.voice_effective_rollout_stage
    VOICE_SESSION_IDEMPOTENCY_TTL     = "24h"
    VOICE_SESSION_LEASE_DURATION      = "2m"
    VOICE_SESSION_LEASE_INDEX_NAME    = "gsi2"
    VOICE_SESSION_MAX_DURATION        = "55m"
    VOICE_SESSION_ROTATE_AFTER        = "52m"
    VOICE_SESSIONS_TABLE_NAME         = aws_dynamodb_table.voice_sessions.name
  } : {}

  database_runtime_env = {
    DATABASE_HOST_SSM_PARAM = local.db_host_ssm_parameter_name
    DATABASE_SECRET_ARN     = aws_db_instance.main.master_user_secret[0].secret_arn
  }

  http_secret_env = merge(local.database_runtime_env, {
    CREDENTIAL_ENCRYPTION_SECRET_ARN = aws_secretsmanager_secret.credential_encryption.arn
    RAZORPAY_SECRET_ARN              = aws_secretsmanager_secret.razorpay.arn
    LLM_SECRET_ARN                   = aws_secretsmanager_secret.llm.arn
    EXA_SECRET_ARN                   = aws_secretsmanager_secret.exa.arn
    GST_LOOKUP_SECRET_ARN            = aws_secretsmanager_secret.gst_lookup.arn
    GST_PROVIDER_SECRET_ARN          = aws_secretsmanager_secret.gst_provider.arn
    DEEPSEEK_SECRET_ARN              = aws_secretsmanager_secret.deepseek.arn
    SARVAM_SECRET_ARN                = aws_secretsmanager_secret.sarvam.arn
  })

  http_cursor_secret_env = {
    INVOICE_CURSOR_HMAC_SECRET_ARN = aws_secretsmanager_secret.billeif_invoice_cursor_hmac.arn
  }

  worker_secret_env = {
    invoice = merge(local.database_runtime_env, {
      CREDENTIAL_ENCRYPTION_SECRET_ARN = aws_secretsmanager_secret.credential_encryption.arn
    })
    gst = merge(local.database_runtime_env, {
      CREDENTIAL_ENCRYPTION_SECRET_ARN = aws_secretsmanager_secret.credential_encryption.arn
      GST_PROVIDER_SECRET_ARN          = aws_secretsmanager_secret.gst_provider.arn
    })
    bargaining = merge(local.database_runtime_env, {
      CREDENTIAL_ENCRYPTION_SECRET_ARN = aws_secretsmanager_secret.credential_encryption.arn
      LLM_SECRET_ARN                   = aws_secretsmanager_secret.llm.arn
      EXA_SECRET_ARN                   = aws_secretsmanager_secret.exa.arn
    })
  }
}

resource "aws_cloudwatch_log_group" "lambda_api_http" {
  name              = "/aws/lambda/${local.resource_prefix}-api-http"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_a2a_stream" {
  name              = "/aws/lambda/${local.resource_prefix}-a2a-stream"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_sqs_invoice" {
  name              = "/aws/lambda/${local.resource_prefix}-sqs-invoice"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_sqs_gst" {
  name              = "/aws/lambda/${local.resource_prefix}-sqs-gst"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_ws_handler" {
  name              = "/aws/lambda/${local.resource_prefix}-ws-handler"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_outbox_dispatcher" {
  name              = "/aws/lambda/${local.resource_prefix}-outbox-dispatcher"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_sqs_email_delivery" {
  name              = "/aws/lambda/${local.resource_prefix}-sqs-email-delivery"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_sqs_ses_feedback" {
  name              = "/aws/lambda/${local.resource_prefix}-sqs-ses-feedback"
  retention_in_days = var.log_retention_days
}

resource "aws_lambda_function" "outbox_dispatcher" {
  function_name    = "${local.resource_prefix}-outbox-dispatcher"
  role             = aws_iam_role.outbox_dispatcher.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.outbox
  source_code_hash = local.lambda_artifact_hashes.outbox
  memory_size      = 256
  timeout          = 45

  reserved_concurrent_executions = local.background_processing_enabled ? (var.enable_lambda_reserved_concurrency ? 1 : null) : 0

  environment {
    variables = {
      ENVIRONMENT              = var.environment
      LOG_LEVEL                = "info"
      LOG_FORMAT               = "json"
      DATABASE_HOST_SSM_PARAM  = local.db_host_ssm_parameter_name
      DATABASE_SECRET_ARN      = aws_db_instance.main.master_user_secret[0].secret_arn
      DATABASE_PORT            = tostring(var.db_port)
      DATABASE_NAME            = var.db_name
      DATABASE_SSL_MODE        = "require"
      SQS_INVOICE_QUEUE        = aws_sqs_queue.invoice_processing.url
      SQS_EMAIL_DELIVERY_QUEUE = aws_sqs_queue.email_delivery.url
    }
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.outbox)
      error_message = "Missing Billeif outbox Lambda artifact ${local.lambda_artifacts.outbox}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_outbox_dispatcher,
    aws_iam_role_policy.outbox_dispatcher,
    aws_ssm_parameter.db_host,
  ]
}

resource "aws_lambda_function" "sqs_email_delivery" {
  function_name    = "${local.resource_prefix}-sqs-email-delivery"
  role             = aws_iam_role.email_delivery.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.sqs_email_delivery
  source_code_hash = local.lambda_artifact_hashes.sqs_email_delivery
  memory_size      = 512
  timeout          = 60

  reserved_concurrent_executions = local.background_processing_enabled ? null : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = {
      ENVIRONMENT             = var.environment
      LOG_LEVEL               = "info"
      LOG_FORMAT              = "json"
      DATABASE_HOST_SSM_PARAM = local.db_host_ssm_parameter_name
      DATABASE_SECRET_ARN     = aws_db_instance.main.master_user_secret[0].secret_arn
      DATABASE_PORT           = tostring(var.db_port)
      DATABASE_NAME           = var.db_name
      DATABASE_SSL_MODE       = "require"
      S3_BUCKET_INVOICES      = aws_s3_bucket.invoices_pdf.id
      SES_SENDER_EMAIL        = var.ses_sender_email
      SES_CONFIGURATION_SET   = aws_ses_configuration_set.main.name
    }
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.sqs_email_delivery)
      error_message = "Missing Billeif email delivery Lambda artifact ${local.lambda_artifacts.sqs_email_delivery}. Run make package-lambda-email-delivery from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_sqs_email_delivery,
    aws_iam_role_policy.email_delivery,
    aws_ssm_parameter.db_host,
  ]
}

resource "aws_lambda_function" "sqs_ses_feedback" {
  function_name    = "${local.resource_prefix}-sqs-ses-feedback"
  role             = aws_iam_role.ses_feedback.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.sqs_ses_feedback
  source_code_hash = local.lambda_artifact_hashes.sqs_ses_feedback
  memory_size      = 256
  timeout          = 30

  reserved_concurrent_executions = local.background_processing_enabled ? null : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = {
      ENVIRONMENT             = var.environment
      LOG_LEVEL               = "info"
      LOG_FORMAT              = "json"
      DATABASE_HOST_SSM_PARAM = local.db_host_ssm_parameter_name
      DATABASE_SECRET_ARN     = aws_db_instance.main.master_user_secret[0].secret_arn
      DATABASE_PORT           = tostring(var.db_port)
      DATABASE_NAME           = var.db_name
      DATABASE_SSL_MODE       = "require"
      SES_SENDING_ACCOUNT_ID  = data.aws_caller_identity.current.account_id
      SES_CONFIGURATION_SET   = aws_ses_configuration_set.main.name
    }
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.sqs_ses_feedback)
      error_message = "Missing Billeif SES feedback Lambda artifact ${local.lambda_artifacts.sqs_ses_feedback}. Run make package-lambda-ses-feedback from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_sqs_ses_feedback,
    aws_iam_role_policy.ses_feedback,
    aws_ssm_parameter.db_host,
  ]
}

resource "aws_lambda_function" "api_http" {
  function_name     = "${local.resource_prefix}-api-http"
  role              = aws_iam_role.lambda_http_exec.arn
  runtime           = "provided.al2023"
  handler           = "bootstrap"
  architectures     = ["arm64"]
  s3_bucket         = aws_s3_bucket.lambda_artifacts.id
  s3_key            = aws_s3_object.api_http_lambda_artifact.key
  s3_object_version = aws_s3_object.api_http_lambda_artifact.version_id
  source_code_hash  = local.lambda_artifact_hashes.api_http
  memory_size       = 1024
  timeout           = 28

  reserved_concurrent_executions = var.enable_application ? (var.enable_lambda_reserved_concurrency ? 10 : null) : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = merge(local.common_lambda_env, local.http_secret_env, local.http_cursor_secret_env, local.voice_http_lambda_env, {
      WEBSOCKET_API_ENDPOINT = local.websocket_api_invoke_url
      SERVER_BASE_URL        = local.http_api_invoke_url
    })
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.api_http)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.api_http}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_api_http,
    aws_bedrockagentcore_agent_runtime_endpoint.voice_prod,
    aws_iam_role_policy.lambda_http_app,
    aws_iam_role_policy.invoice_cursor_http,
    aws_iam_role_policy_attachment.lambda_http_basic,
    aws_iam_role_policy_attachment.lambda_http_vpc_access,
  ]
}

resource "aws_lambda_function" "a2a_stream" {
  function_name    = "${local.resource_prefix}-a2a-stream"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.a2a_stream
  source_code_hash = local.lambda_artifact_hashes.a2a_stream
  memory_size      = 1024
  timeout          = 60

  reserved_concurrent_executions = var.enable_application ? (var.enable_lambda_reserved_concurrency ? 5 : null) : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = merge(local.common_lambda_env, local.http_secret_env, {
      WEBSOCKET_API_ENDPOINT = local.websocket_api_invoke_url
      SERVER_BASE_URL        = local.rest_api_invoke_url
    })
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.a2a_stream)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.a2a_stream}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_a2a_stream,
  ]
}

resource "aws_lambda_function" "sqs_invoice" {
  function_name     = "${local.resource_prefix}-sqs-invoice"
  role              = aws_iam_role.lambda_worker_exec["invoice"].arn
  runtime           = "provided.al2023"
  handler           = "bootstrap"
  architectures     = ["arm64"]
  s3_bucket         = aws_s3_bucket.lambda_artifacts.id
  s3_key            = aws_s3_object.sqs_invoice_lambda_artifact.key
  s3_object_version = aws_s3_object.sqs_invoice_lambda_artifact.version_id
  source_code_hash  = local.lambda_artifact_hashes.sqs_invoice
  memory_size       = 512
  timeout           = 60

  reserved_concurrent_executions = local.background_processing_enabled ? (var.enable_lambda_reserved_concurrency ? 2 : null) : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = merge(local.common_lambda_env, local.worker_secret_env.invoice, {
      WEBSOCKET_API_ENDPOINT = local.websocket_api_invoke_url
    })
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.sqs_invoice)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.sqs_invoice}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_sqs_invoice,
  ]
}

resource "aws_lambda_function" "sqs_gst" {
  function_name     = "${local.resource_prefix}-sqs-gst"
  role              = aws_iam_role.lambda_worker_exec["gst"].arn
  runtime           = "provided.al2023"
  handler           = "bootstrap"
  architectures     = ["arm64"]
  s3_bucket         = aws_s3_bucket.lambda_artifacts.id
  s3_key            = aws_s3_object.sqs_gst_lambda_artifact.key
  s3_object_version = aws_s3_object.sqs_gst_lambda_artifact.version_id
  source_code_hash  = local.lambda_artifact_hashes.sqs_gst
  memory_size       = 512
  timeout           = 60

  reserved_concurrent_executions = local.background_processing_enabled ? (var.enable_lambda_reserved_concurrency ? 2 : null) : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = merge(local.common_lambda_env, local.worker_secret_env.gst, {
      WEBSOCKET_API_ENDPOINT = local.websocket_api_invoke_url
    })
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.sqs_gst)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.sqs_gst}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_sqs_gst,
  ]
}

resource "aws_cloudwatch_log_group" "lambda_sqs_bargaining" {
  name              = "/aws/lambda/${local.resource_prefix}-sqs-bargaining"
  retention_in_days = var.log_retention_days
}

resource "aws_lambda_function" "sqs_bargaining" {
  function_name     = "${local.resource_prefix}-sqs-bargaining"
  role              = aws_iam_role.lambda_worker_exec["bargaining"].arn
  runtime           = "provided.al2023"
  handler           = "bootstrap"
  architectures     = ["arm64"]
  s3_bucket         = aws_s3_bucket.lambda_artifacts.id
  s3_key            = aws_s3_object.sqs_bargaining_lambda_artifact.key
  s3_object_version = aws_s3_object.sqs_bargaining_lambda_artifact.version_id
  source_code_hash  = local.lambda_artifact_hashes.sqs_bargaining
  memory_size       = 1024
  timeout           = 60

  reserved_concurrent_executions = local.background_processing_enabled ? (var.enable_lambda_reserved_concurrency ? 5 : null) : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = merge(local.common_lambda_env, local.worker_secret_env.bargaining, {
      WEBSOCKET_API_ENDPOINT = local.websocket_api_invoke_url
    })
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.sqs_bargaining)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.sqs_bargaining}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_sqs_bargaining,
  ]
}

resource "aws_lambda_function" "ws_handler" {
  function_name     = "${local.resource_prefix}-ws-handler"
  role              = aws_iam_role.lambda_websocket_exec.arn
  runtime           = "provided.al2023"
  handler           = "bootstrap"
  architectures     = ["arm64"]
  s3_bucket         = aws_s3_bucket.lambda_artifacts.id
  s3_key            = aws_s3_object.ws_lambda_artifact.key
  s3_object_version = aws_s3_object.ws_lambda_artifact.version_id
  source_code_hash  = local.lambda_artifact_hashes.ws_handler
  memory_size       = 256
  timeout           = 15

  reserved_concurrent_executions = var.enable_application ? (var.enable_lambda_reserved_concurrency ? 5 : null) : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = merge(local.common_lambda_env, local.database_runtime_env, {
      AWS_ENDPOINT           = ""
      WEBSOCKET_API_ENDPOINT = local.websocket_management_api_endpoint
    })
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.ws_handler)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.ws_handler}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_ws_handler,
  ]
}

resource "aws_lambda_event_source_mapping" "invoice_queue" {
  count = local.background_processing_enabled ? 1 : 0

  event_source_arn                   = aws_sqs_queue.invoice_processing.arn
  function_name                      = aws_lambda_function.sqs_invoice.arn
  batch_size                         = 10
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 5

  scaling_config {
    maximum_concurrency = 2
  }
}

resource "aws_lambda_event_source_mapping" "gst_queue" {
  count = local.background_processing_enabled ? 1 : 0

  event_source_arn                   = aws_sqs_queue.gst_processing.arn
  function_name                      = aws_lambda_function.sqs_gst.arn
  batch_size                         = 10
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 5

  scaling_config {
    maximum_concurrency = 2
  }
}

resource "aws_lambda_event_source_mapping" "bargaining_queue" {
  count = local.background_processing_enabled ? 1 : 0

  event_source_arn                   = aws_sqs_queue.bargaining_negotiation.arn
  function_name                      = aws_lambda_function.sqs_bargaining.arn
  batch_size                         = 1
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 0

  scaling_config {
    maximum_concurrency = 2
  }
}

resource "aws_lambda_event_source_mapping" "email_delivery_queue" {
  count = local.background_processing_enabled ? 1 : 0

  event_source_arn                   = aws_sqs_queue.email_delivery.arn
  function_name                      = aws_lambda_function.sqs_email_delivery.arn
  batch_size                         = 1
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 0

  scaling_config {
    maximum_concurrency = 2
  }
}

resource "aws_lambda_event_source_mapping" "ses_feedback_queue" {
  count = local.background_processing_enabled ? 1 : 0

  event_source_arn                   = aws_sqs_queue.ses_feedback.arn
  function_name                      = aws_lambda_function.sqs_ses_feedback.arn
  batch_size                         = 10
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 0

  scaling_config {
    maximum_concurrency = 2
  }
}
