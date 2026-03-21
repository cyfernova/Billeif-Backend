resource "aws_api_gateway_rest_api" "main" {
  name        = "${var.project_name}-rest-api"
  description = "REST API for ${var.project_name} Lambda backend"
}

locals {
  a2a_stream_invoke_uri = "arn:aws:apigateway:${var.aws_region}:lambda:path/2021-11-15/functions/${aws_lambda_function.a2a_stream.arn}/response-streaming-invocations"
}

resource "aws_api_gateway_method" "root_any" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_rest_api.main.root_resource_id
  http_method   = "ANY"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "root_any" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_rest_api.main.root_resource_id
  http_method             = aws_api_gateway_method.root_any.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_resource" "proxy" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = "{proxy+}"
}

resource "aws_api_gateway_method" "proxy_any" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.proxy.id
  http_method   = "ANY"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "proxy_any" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.proxy.id
  http_method             = aws_api_gateway_method.proxy_any.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

# Dedicated SSE stream endpoint mapping for latest A2A HTTP+JSON streaming.
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
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_a2a_message_stream.id
  http_method   = "POST"
  authorization = "NONE"
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
  parent_id   = aws_api_gateway_resource.api_v1_a2a_tasks.id
  path_part   = "{taskId}:subscribe"
}

resource "aws_api_gateway_method" "api_v1_a2a_task_subscribe_get" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_a2a_task_subscribe.id
  http_method   = "GET"
  authorization = "NONE"
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
      aws_api_gateway_integration.root_any.id,
      aws_api_gateway_integration.proxy_any.id,
      aws_api_gateway_integration.api_v1_a2a_message_stream_post.id,
      aws_api_gateway_integration.api_v1_a2a_task_subscribe_get.id
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_api_gateway_stage" "main" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  deployment_id = aws_api_gateway_deployment.main.id
  stage_name    = var.environment
}

resource "aws_lambda_permission" "allow_rest_api_http" {
  statement_id  = "AllowExecutionFromAPIGatewayRestApi"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api_http.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*/*"
}

resource "aws_lambda_permission" "allow_rest_a2a_stream" {
  statement_id  = "AllowExecutionFromAPIGatewayRestA2AStream"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.a2a_stream.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*/*"
}
