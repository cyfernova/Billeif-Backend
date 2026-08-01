locals {
  api_gateway_throttling_burst_limit = var.enable_lambda_reserved_concurrency ? 100 : 20
  api_gateway_throttling_rate_limit  = var.enable_lambda_reserved_concurrency ? 50 : 10
}

resource "aws_apigatewayv2_api" "http" {
  name          = "${local.resource_prefix}-http-api"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "http_lambda" {
  api_id                 = aws_apigatewayv2_api.http.id
  integration_type       = "AWS_PROXY"
  integration_method     = "POST"
  integration_uri        = aws_lambda_function.api_http.invoke_arn
  payload_format_version = "1.0"
  timeout_milliseconds   = 29000
}

# Authentication remains in Gin because it accepts both Billeif Cognito pools.
resource "aws_apigatewayv2_route" "http_default" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "$default"
  authorization_type = "NONE"
  target             = "integrations/${aws_apigatewayv2_integration.http_lambda.id}"
}

resource "aws_cloudwatch_log_group" "http_api_access" {
  name              = "/aws/apigateway/${local.resource_prefix}-http"
  retention_in_days = 14
}

resource "aws_apigatewayv2_stage" "http" {
  api_id      = aws_apigatewayv2_api.http.id
  name        = var.environment
  auto_deploy = true

  default_route_settings {
    detailed_metrics_enabled = false
    throttling_burst_limit   = local.api_gateway_throttling_burst_limit
    throttling_rate_limit    = local.api_gateway_throttling_rate_limit
  }

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.http_api_access.arn
    format = jsonencode({
      requestId      = "$context.requestId"
      sourceIp       = "$context.identity.sourceIp"
      requestTime    = "$context.requestTime"
      httpMethod     = "$context.httpMethod"
      routeKey       = "$context.routeKey"
      status         = "$context.status"
      responseLength = "$context.responseLength"
    })
  }
}

resource "aws_lambda_permission" "allow_http_api_http" {
  count = var.enable_application ? 1 : 0

  statement_id  = "AllowExecutionFromAPIGatewayHttpApi"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api_http.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}
