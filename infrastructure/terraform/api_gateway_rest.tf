resource "aws_api_gateway_rest_api" "main" {
  name               = "${local.resource_prefix}-rest-api"
  description        = "A2A response-streaming REST API for ${local.resource_prefix}"
  binary_media_types = ["application/octet-stream"]
}

resource "aws_api_gateway_authorizer" "cognito" {
  name            = "${local.resource_prefix}-cognito"
  rest_api_id     = aws_api_gateway_rest_api.main.id
  type            = "COGNITO_USER_POOLS"
  provider_arns   = [aws_cognito_user_pool.main.arn, aws_cognito_user_pool.phone.arn]
  identity_source = "method.request.header.Authorization"
}

locals {
  a2a_stream_invoke_uri = "arn:aws:apigateway:${var.aws_region}:lambda:path/2021-11-15/functions/${aws_lambda_function.a2a_stream.arn}/response-streaming-invocations"
}

resource "aws_api_gateway_resource" "api" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = "api"
}

resource "aws_api_gateway_resource" "api_v1" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api.id
  path_part   = "v1"
}

resource "aws_api_gateway_resource" "api_v1_a2a" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1.id
  path_part   = "a2a"
}

resource "aws_api_gateway_resource" "api_v1_a2a_message_stream" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_a2a.id
  path_part   = "message:stream"
}

resource "aws_api_gateway_method" "api_v1_a2a_message_stream_post" {
  rest_api_id          = aws_api_gateway_rest_api.main.id
  resource_id          = aws_api_gateway_resource.api_v1_a2a_message_stream.id
  http_method          = "POST"
  authorization        = "COGNITO_USER_POOLS"
  authorizer_id        = aws_api_gateway_authorizer.cognito.id
  authorization_scopes = ["aws.cognito.signin.user.admin"]
}

resource "aws_api_gateway_integration" "api_v1_a2a_message_stream_post" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_a2a_message_stream.id
  http_method             = aws_api_gateway_method.api_v1_a2a_message_stream_post.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.a2a_stream_invoke_uri
  response_transfer_mode  = "STREAM"
}

resource "aws_api_gateway_resource" "api_v1_a2a_tasks" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_a2a.id
  path_part   = "tasks"
}

resource "aws_api_gateway_resource" "api_v1_a2a_task_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_a2a_tasks.id
  path_part   = "{taskId}"
}

resource "aws_api_gateway_resource" "api_v1_a2a_task_subscribe" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_a2a_task_id.id
  path_part   = "subscribe"
}

resource "aws_api_gateway_method" "api_v1_a2a_task_subscribe_get" {
  rest_api_id          = aws_api_gateway_rest_api.main.id
  resource_id          = aws_api_gateway_resource.api_v1_a2a_task_subscribe.id
  http_method          = "GET"
  authorization        = "COGNITO_USER_POOLS"
  authorizer_id        = aws_api_gateway_authorizer.cognito.id
  authorization_scopes = ["aws.cognito.signin.user.admin"]
}

resource "aws_api_gateway_integration" "api_v1_a2a_task_subscribe_get" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_a2a_task_subscribe.id
  http_method             = aws_api_gateway_method.api_v1_a2a_task_subscribe_get.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.a2a_stream_invoke_uri
  response_transfer_mode  = "STREAM"
}

resource "aws_api_gateway_deployment" "main" {
  rest_api_id = aws_api_gateway_rest_api.main.id

  triggers = {
    redeploy = sha1(jsonencode([
      aws_api_gateway_integration.api_v1_a2a_message_stream_post.id,
      aws_api_gateway_integration.api_v1_a2a_task_subscribe_get.id,
      aws_api_gateway_rest_api.main.binary_media_types,
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_cloudwatch_log_group" "rest_api_access" {
  name              = "/aws/apigateway/${local.resource_prefix}-rest"
  retention_in_days = 14
}

resource "aws_api_gateway_stage" "main" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  deployment_id = aws_api_gateway_deployment.main.id
  stage_name    = var.environment

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.rest_api_access.arn
    format = jsonencode({
      requestId      = "$context.requestId"
      sourceIp       = "$context.identity.sourceIp"
      requestTime    = "$context.requestTime"
      httpMethod     = "$context.httpMethod"
      resourcePath   = "$context.resourcePath"
      status         = "$context.status"
      responseLength = "$context.responseLength"
    })
  }

  depends_on = [aws_api_gateway_account.main]
}

resource "aws_api_gateway_method_settings" "main" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  stage_name  = aws_api_gateway_stage.main.stage_name
  method_path = "*/*"

  settings {
    logging_level          = "ERROR"
    metrics_enabled        = false
    data_trace_enabled     = false
    throttling_burst_limit = local.api_gateway_throttling_burst_limit
    throttling_rate_limit  = local.api_gateway_throttling_rate_limit
  }
}

resource "aws_lambda_permission" "allow_rest_a2a_stream" {
  count = var.enable_application ? 1 : 0

  statement_id  = "AllowExecutionFromAPIGatewayRestA2AStream"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.a2a_stream.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*/*"
}
