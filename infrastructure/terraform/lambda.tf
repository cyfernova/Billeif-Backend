locals {
  lambda_artifacts = {
    api_http    = "${var.lambda_artifact_dir}/http.zip"
    a2a_stream  = "${var.lambda_artifact_dir}/a2a-stream.zip"
    sqs_invoice = "${var.lambda_artifact_dir}/sqs-invoice.zip"
    sqs_payment = "${var.lambda_artifact_dir}/sqs-payment.zip"
    ws_handler  = "${var.lambda_artifact_dir}/ws.zip"
  }

  common_lambda_env = {
    ENVIRONMENT                         = var.environment
    LOG_LEVEL                           = "info"
    LOG_FORMAT                          = "json"
    SERVER_PORT                         = "8080"
    AWS_REGION                          = var.aws_region
    DATABASE_HOST                       = aws_db_instance.main.address
    DATABASE_PORT                       = tostring(aws_db_instance.main.port)
    DATABASE_NAME                       = var.db_name
    DATABASE_SSL_MODE                   = "require"
    DATABASE_USER_SSM_PARAM             = aws_ssm_parameter.db_username.name
    DATABASE_PASSWORD_SSM_PARAM         = aws_ssm_parameter.db_password.name
    S3_BUCKET_LOGOS                     = aws_s3_bucket.business_logos.id
    S3_BUCKET_INVOICES                  = aws_s3_bucket.invoices_pdf.id
    S3_BUCKET_PRODUCTS                  = aws_s3_bucket.product_images.id
    S3_BUCKET_EMAIL_SINK                = aws_s3_bucket.email_sink.id
    SQS_INVOICE_QUEUE                   = aws_sqs_queue.invoice_processing.url
    SQS_PAYMENT_QUEUE                   = aws_sqs_queue.payment_processing.url
    COGNITO_USER_POOL_ID                = aws_cognito_user_pool.main.id
    COGNITO_CLIENT_ID                   = aws_cognito_user_pool_client.main.id
    COGNITO_REGION                      = var.aws_region
    JWT_ACCESS_TOKEN_EXPIRY             = "1h"
    JWT_REFRESH_TOKEN_EXPIRY            = "720h"
    WEBSOCKET_CONNECTIONS_TABLE         = aws_dynamodb_table.ws_connections.name
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

resource "aws_cloudwatch_log_group" "lambda_ws_handler" {
  name              = "/aws/lambda/${var.project_name}-ws-handler"
  retention_in_days = var.log_retention_days
}

resource "aws_lambda_function" "api_http" {
  function_name    = "${var.project_name}-api-http"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.api_http
  source_code_hash = filebase64sha256(local.lambda_artifacts.api_http)
  memory_size      = 1024
  timeout          = 30

  reserved_concurrent_executions = 10

  environment {
    variables = merge(local.common_lambda_env, {
      WEBSOCKET_API_ENDPOINT = aws_apigatewayv2_stage.websocket_default.invoke_url
      SERVER_BASE_URL        = aws_api_gateway_stage.main.invoke_url
    })
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
  source_code_hash = filebase64sha256(local.lambda_artifacts.a2a_stream)
  memory_size      = 1024
  timeout          = 60

  reserved_concurrent_executions = 5

  environment {
    variables = merge(local.common_lambda_env, {
      WEBSOCKET_API_ENDPOINT = aws_apigatewayv2_stage.websocket_default.invoke_url
      SERVER_BASE_URL        = aws_api_gateway_stage.main.invoke_url
    })
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
  source_code_hash = filebase64sha256(local.lambda_artifacts.sqs_invoice)
  memory_size      = 512
  timeout          = 60

  reserved_concurrent_executions = 2

  environment {
    variables = merge(local.common_lambda_env, {
      WEBSOCKET_API_ENDPOINT = aws_apigatewayv2_stage.websocket_default.invoke_url
    })
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
  source_code_hash = filebase64sha256(local.lambda_artifacts.sqs_payment)
  memory_size      = 512
  timeout          = 60

  reserved_concurrent_executions = 2

  environment {
    variables = merge(local.common_lambda_env, {
      WEBSOCKET_API_ENDPOINT = aws_apigatewayv2_stage.websocket_default.invoke_url
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_sqs_payment]
}

resource "aws_lambda_function" "ws_handler" {
  function_name    = "${var.project_name}-ws-handler"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.ws_handler
  source_code_hash = filebase64sha256(local.lambda_artifacts.ws_handler)
  memory_size      = 256
  timeout          = 15

  reserved_concurrent_executions = 5

  environment {
    variables = {
      ENVIRONMENT                 = var.environment
      LOG_LEVEL                   = "info"
      LOG_FORMAT                  = "json"
      AWS_REGION                  = var.aws_region
      AWS_ENDPOINT                = ""
      COGNITO_USER_POOL_ID        = aws_cognito_user_pool.main.id
      COGNITO_CLIENT_ID           = aws_cognito_user_pool_client.main.id
      COGNITO_REGION              = var.aws_region
      WEBSOCKET_API_ENDPOINT      = aws_apigatewayv2_stage.websocket_default.invoke_url
      WEBSOCKET_CONNECTIONS_TABLE = aws_dynamodb_table.ws_connections.name
    }
  }

  depends_on = [aws_cloudwatch_log_group.lambda_ws_handler]
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
