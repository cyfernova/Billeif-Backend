locals {
  # Construct invoke URLs from API IDs to avoid circular dependencies
  # (lambdas need stage URL, but stages depend on lambdas via deployments).
  rest_api_invoke_url               = "https://${aws_api_gateway_rest_api.main.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
  websocket_api_invoke_url          = "wss://${aws_apigatewayv2_api.websocket.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
  websocket_management_api_endpoint = "https://${aws_apigatewayv2_api.websocket.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"

  lambda_artifacts = {
    api_http          = "${var.lambda_artifact_dir}/http.zip"
    a2a_stream        = "${var.lambda_artifact_dir}/a2a-stream.zip"
    sqs_invoice       = "${var.lambda_artifact_dir}/sqs-invoice.zip"
    sqs_payment       = "${var.lambda_artifact_dir}/sqs-payment.zip"
    sqs_gst           = "${var.lambda_artifact_dir}/sqs-gst.zip"
    sqs_bargaining    = "${var.lambda_artifact_dir}/sqs-bargaining.zip"
    ws_handler        = "${var.lambda_artifact_dir}/ws.zip"
    voice_session     = "${var.lambda_artifact_dir}/voice-session.zip"
    custom_sms_sender = "${var.lambda_artifact_dir}/custom-sms-sender.zip"
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
    ENVIRONMENT                               = var.environment
    LOG_LEVEL                                 = "info"
    LOG_FORMAT                                = "json"
    SERVER_PORT                               = "8080"
    DATABASE_HOST_SSM_PARAM                   = local.db_host_ssm_parameter_name
    DATABASE_PORT                             = tostring(var.db_port)
    DATABASE_NAME                             = var.db_name
    DATABASE_SSL_MODE                         = "require"
    DATABASE_USER_SSM_PARAM                   = local.db_username_ssm_parameter_name
    DATABASE_PASSWORD_SSM_PARAM               = local.db_password_ssm_parameter_name
    RAZORPAY_KEY_ID_SSM_PARAM                 = local.razorpay_key_id_ssm_parameter_name
    RAZORPAY_KEY_SECRET_SSM_PARAM             = local.razorpay_key_secret_ssm_parameter_name
    RAZORPAY_WEBHOOK_SECRET_SSM_PARAM         = local.razorpay_webhook_secret_ssm_parameter_name
    ALLOWED_ORIGINS                           = local.rest_api_invoke_url
    CREDENTIAL_ENCRYPTION_KEY                 = var.credential_encryption_key
    S3_BUCKET_LOGOS                           = aws_s3_bucket.business_logos.id
    S3_BUCKET_INVOICES                        = aws_s3_bucket.invoices_pdf.id
    S3_BUCKET_PRODUCTS                        = aws_s3_bucket.product_images.id
    S3_BUCKET_EMAIL_SINK                      = aws_s3_bucket.email_sink.id
    SQS_INVOICE_QUEUE                         = aws_sqs_queue.invoice_processing.url
    SQS_PAYMENT_QUEUE                         = aws_sqs_queue.payment_processing.url
    SQS_GST_QUEUE                             = aws_sqs_queue.gst_processing.url
    SQS_BARGAINING_QUEUE                      = aws_sqs_queue.bargaining_negotiation.url
    COGNITO_USER_POOL_ID                      = aws_cognito_user_pool.main.id
    COGNITO_CLIENT_ID                         = aws_cognito_user_pool_client.main.id
    COGNITO_DOMAIN                            = "${aws_cognito_user_pool_domain.main.domain}.auth.${var.aws_region}.amazoncognito.com"
    COGNITO_REGION                            = var.aws_region
    COGNITO_PHONE_USER_POOL_ID                = aws_cognito_user_pool.phone.id
    COGNITO_PHONE_CLIENT_ID                   = aws_cognito_user_pool_client.phone.id
    COGNITO_PHONE_REGION                      = "ap-south-1"
    COGNITO_PHONE_OTP_COOLDOWN_TABLE          = aws_dynamodb_table.phone_auth_cooldowns.name
    JWT_ACCESS_TOKEN_EXPIRY                   = "1h"
    JWT_REFRESH_TOKEN_EXPIRY                  = "720h"
    WEBSOCKET_CONNECTIONS_TABLE               = aws_dynamodb_table.ws_connections.name
    LLM_API_KEY                               = var.llm_api_key
    LLM_API_URL                               = var.llm_api_url
    LLM_MODEL                                 = var.llm_model
    GST_LOOKUP_BASE_URL                       = var.gst_lookup_base_url
    GST_LOOKUP_API_KEY                        = var.gst_lookup_api_key
    GST_LOOKUP_TIMEOUT                        = tostring(var.gst_lookup_timeout)
    DEEPGRAM_API_KEY                          = var.deepgram_api_key
    DEEPGRAM_VOICE_AGENT_URL                  = var.deepgram_voice_agent_url
    DEEPGRAM_VOICE_LISTEN_MODEL               = var.deepgram_voice_listen_model
    DEEPGRAM_VOICE_SPEAK_MODEL                = var.deepgram_voice_speak_model
    DEEPGRAM_VOICE_INPUT_ENCODING             = var.deepgram_voice_input_encoding
    DEEPGRAM_VOICE_INPUT_SAMPLE_RATE          = tostring(var.deepgram_voice_input_sample_rate)
    DEEPGRAM_VOICE_OUTPUT_ENCODING            = var.deepgram_voice_output_encoding
    DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE         = tostring(var.deepgram_voice_output_sample_rate)
    DEEPSEEK_API_KEY                          = var.deepseek_api_key
    DEEPSEEK_BASE_URL                         = var.deepseek_base_url
    DEEPSEEK_MODEL                            = var.deepseek_model
    VOICE_WS_MAX_SESSION_SECONDS              = tostring(var.voice_ws_max_session_seconds)
    VOICE_WS_PING_INTERVAL_SECONDS            = tostring(var.voice_ws_ping_interval_seconds)
    VOICE_WS_WRITE_TIMEOUT_SECONDS            = tostring(var.voice_ws_write_timeout_seconds)
    VOICE_WS_MAX_FRAME_BYTES                  = tostring(var.voice_ws_max_frame_bytes)
    VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER = tostring(var.voice_ws_max_concurrent_sessions_per_user)
    VOICE_WS_EVENT_POLL_INTERVAL_MS           = tostring(var.voice_ws_event_poll_interval_ms)
    VOICE_WS_EVENT_TTL_SECONDS                = tostring(var.voice_ws_event_ttl_seconds)
    VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES         = tostring(var.voice_ws_max_outbound_chunk_bytes)
    VOICE_WS_PROVIDER_READY_TIMEOUT_SECONDS   = tostring(var.voice_ws_provider_ready_timeout_seconds)
    VOICE_SESSIONS_TABLE                      = aws_dynamodb_table.voice_sessions.name
  }
}

resource "aws_cloudwatch_log_group" "lambda_api_http" {
  name              = "/aws/lambda/${var.project_name}-api-http"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_a2a_stream" {
  name              = "/aws/lambda/${var.project_name}-a2a-stream"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_sqs_invoice" {
  name              = "/aws/lambda/${var.project_name}-sqs-invoice"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_sqs_payment" {
  name              = "/aws/lambda/${var.project_name}-sqs-payment"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_sqs_gst" {
  name              = "/aws/lambda/${var.project_name}-sqs-gst"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_ws_handler" {
  name              = "/aws/lambda/${var.project_name}-ws-handler"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_voice_session" {
  name              = "/aws/lambda/${var.voice_session_lambda_function_name}"
  retention_in_days = var.log_retention_days
}

resource "aws_lambda_function" "api_http" {
  function_name     = "${var.project_name}-api-http"
  role              = aws_iam_role.lambda_exec.arn
  runtime           = "provided.al2023"
  handler           = "bootstrap"
  architectures     = ["arm64"]
  s3_bucket         = aws_s3_bucket.lambda_artifacts.id
  s3_key            = aws_s3_object.api_http_lambda_artifact.key
  s3_object_version = aws_s3_object.api_http_lambda_artifact.version_id
  source_code_hash  = local.lambda_artifact_hashes.api_http
  memory_size       = 1024
  timeout           = 500

  reserved_concurrent_executions = var.enable_lambda_reserved_concurrency ? 10 : null

  environment {
    variables = merge(local.common_lambda_env, {
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
      condition     = fileexists(local.lambda_artifacts.api_http)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.api_http}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [aws_cloudwatch_log_group.lambda_api_http]
}

resource "aws_lambda_function" "a2a_stream" {
  function_name    = "${var.project_name}-a2a-stream"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.a2a_stream
  source_code_hash = local.lambda_artifact_hashes.a2a_stream
  memory_size      = 1024
  timeout          = 60

  reserved_concurrent_executions = var.enable_lambda_reserved_concurrency ? 5 : null

  environment {
    variables = merge(local.common_lambda_env, {
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

  depends_on = [aws_cloudwatch_log_group.lambda_a2a_stream]
}

resource "aws_lambda_function" "sqs_invoice" {
  function_name    = "${var.project_name}-sqs-invoice"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.sqs_invoice
  source_code_hash = local.lambda_artifact_hashes.sqs_invoice
  memory_size      = 512
  timeout          = 60

  reserved_concurrent_executions = var.enable_lambda_reserved_concurrency ? 2 : null

  environment {
    variables = merge(local.common_lambda_env, {
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

  depends_on = [aws_cloudwatch_log_group.lambda_sqs_invoice]
}

resource "aws_lambda_function" "sqs_payment" {
  function_name    = "${var.project_name}-sqs-payment"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.sqs_payment
  source_code_hash = local.lambda_artifact_hashes.sqs_payment
  memory_size      = 512
  timeout          = 60

  reserved_concurrent_executions = var.enable_lambda_reserved_concurrency ? 2 : null

  environment {
    variables = merge(local.common_lambda_env, {
      WEBSOCKET_API_ENDPOINT = local.websocket_api_invoke_url
    })
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.sqs_payment)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.sqs_payment}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [aws_cloudwatch_log_group.lambda_sqs_payment]
}

resource "aws_lambda_function" "sqs_gst" {
  function_name    = "${var.project_name}-sqs-gst"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.sqs_gst
  source_code_hash = local.lambda_artifact_hashes.sqs_gst
  memory_size      = 512
  timeout          = 60

  reserved_concurrent_executions = var.enable_lambda_reserved_concurrency ? 2 : null

  environment {
    variables = merge(local.common_lambda_env, {
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

  depends_on = [aws_cloudwatch_log_group.lambda_sqs_gst]
}

resource "aws_cloudwatch_log_group" "lambda_sqs_bargaining" {
  name              = "/aws/lambda/${var.project_name}-sqs-bargaining"
  retention_in_days = var.log_retention_days
}

resource "aws_lambda_function" "sqs_bargaining" {
  function_name     = "${var.project_name}-sqs-bargaining"
  role              = aws_iam_role.lambda_exec.arn
  runtime           = "provided.al2023"
  handler           = "bootstrap"
  architectures     = ["arm64"]
  s3_bucket         = aws_s3_bucket.lambda_artifacts.id
  s3_key            = aws_s3_object.sqs_bargaining_lambda_artifact.key
  s3_object_version = aws_s3_object.sqs_bargaining_lambda_artifact.version_id
  source_code_hash  = local.lambda_artifact_hashes.sqs_bargaining
  memory_size       = 1024
  timeout           = 350

  reserved_concurrent_executions = var.enable_lambda_reserved_concurrency ? 5 : null

  environment {
    variables = merge(local.common_lambda_env, {
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

  depends_on = [aws_cloudwatch_log_group.lambda_sqs_bargaining]
}

resource "aws_lambda_function" "ws_handler" {
  function_name    = "${var.project_name}-ws-handler"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.ws_handler
  source_code_hash = local.lambda_artifact_hashes.ws_handler
  memory_size      = 256
  timeout          = 15

  reserved_concurrent_executions = var.enable_lambda_reserved_concurrency ? 5 : null

  environment {
    variables = merge(local.common_lambda_env, {
      AWS_ENDPOINT                       = ""
      WEBSOCKET_API_ENDPOINT             = local.websocket_management_api_endpoint
      VOICE_SESSION_WORKER_FUNCTION_NAME = aws_lambda_function.voice_session.function_name
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

  depends_on = [aws_cloudwatch_log_group.lambda_ws_handler]
}

resource "aws_lambda_function" "voice_session" {
  function_name    = var.voice_session_lambda_function_name
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.voice_session
  source_code_hash = local.lambda_artifact_hashes.voice_session
  memory_size      = var.voice_session_lambda_memory_size
  timeout          = var.voice_session_lambda_timeout_seconds

  reserved_concurrent_executions = var.enable_lambda_reserved_concurrency ? var.voice_session_reserved_concurrency : null

  environment {
    variables = merge(local.common_lambda_env, {
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
      condition     = fileexists(local.lambda_artifacts.voice_session)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.voice_session}. Run make package-lambda from the repository root before running Terraform."
    }
  }

  depends_on = [aws_cloudwatch_log_group.lambda_voice_session]
}

resource "aws_lambda_event_source_mapping" "invoice_queue" {
  event_source_arn                   = aws_sqs_queue.invoice_processing.arn
  function_name                      = aws_lambda_function.sqs_invoice.arn
  batch_size                         = 10
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 5
}

resource "aws_lambda_event_source_mapping" "payment_queue" {
  event_source_arn                   = aws_sqs_queue.payment_processing.arn
  function_name                      = aws_lambda_function.sqs_payment.arn
  batch_size                         = 10
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 5
}

resource "aws_lambda_event_source_mapping" "gst_queue" {
  event_source_arn                   = aws_sqs_queue.gst_processing.arn
  function_name                      = aws_lambda_function.sqs_gst.arn
  batch_size                         = 10
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 5
}

resource "aws_lambda_event_source_mapping" "bargaining_queue" {
  event_source_arn                   = aws_sqs_queue.bargaining_negotiation.arn
  function_name                      = aws_lambda_function.sqs_bargaining.arn
  batch_size                         = 10
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 5
}
