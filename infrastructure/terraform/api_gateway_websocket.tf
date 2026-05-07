resource "aws_apigatewayv2_api" "websocket" {
  name                       = "${var.project_name}-websocket"
  protocol_type              = "WEBSOCKET"
  route_selection_expression = "$request.body.action"
}

resource "aws_apigatewayv2_integration" "websocket_lambda" {
  api_id                 = aws_apigatewayv2_api.websocket.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.ws_handler.invoke_arn
  integration_method     = "POST"
  payload_format_version = "1.0"
}

resource "aws_apigatewayv2_route" "ws_connect" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$connect"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_lambda.id}"
}

resource "aws_apigatewayv2_route" "ws_disconnect" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$disconnect"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_lambda.id}"
}

resource "aws_apigatewayv2_route" "ws_default" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$default"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_lambda.id}"
}

resource "aws_apigatewayv2_route" "ws_voice_start" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "voice.start"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_lambda.id}"
}

resource "aws_apigatewayv2_route" "ws_voice_audio" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "voice.audio"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_lambda.id}"
}

resource "aws_apigatewayv2_route" "ws_voice_control" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "voice.control"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_lambda.id}"
}

resource "aws_apigatewayv2_deployment" "websocket" {
  api_id = aws_apigatewayv2_api.websocket.id

  triggers = {
    redeploy = sha1(jsonencode([
      aws_apigatewayv2_route.ws_connect.id,
      aws_apigatewayv2_route.ws_disconnect.id,
      aws_apigatewayv2_route.ws_default.id,
      aws_apigatewayv2_route.ws_voice_start.id,
      aws_apigatewayv2_route.ws_voice_audio.id,
      aws_apigatewayv2_route.ws_voice_control.id
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }

}

resource "aws_cloudwatch_log_group" "websocket_api_access" {
  name              = "/aws/apigateway/${var.project_name}-websocket"
  retention_in_days = var.log_retention_days
}

resource "aws_apigatewayv2_stage" "websocket_default" {
  api_id        = aws_apigatewayv2_api.websocket.id
  name          = var.environment
  auto_deploy   = false
  deployment_id = aws_apigatewayv2_deployment.websocket.id

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.websocket_api_access.arn
    format = jsonencode({
      connectionId            = "$context.connectionId"
      errorMessage            = "$context.error.message"
      eventType               = "$context.eventType"
      integrationErrorMessage = "$context.integrationErrorMessage"
      requestId               = "$context.requestId"
      routeKey                = "$context.routeKey"
      status                  = "$context.status"
    })
  }

  lifecycle {
    create_before_destroy = true
  }

  depends_on = [aws_api_gateway_account.main]
}

resource "aws_lambda_permission" "allow_websocket_lambda" {
  statement_id  = "AllowExecutionFromAPIGatewayWebSocket"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws_handler.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.websocket.execution_arn}/*"
}
