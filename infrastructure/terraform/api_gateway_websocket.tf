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

resource "aws_apigatewayv2_deployment" "websocket" {
  api_id = aws_apigatewayv2_api.websocket.id

  triggers = {
    redeploy = sha1(jsonencode([
      aws_apigatewayv2_route.ws_connect.id,
      aws_apigatewayv2_route.ws_disconnect.id,
      aws_apigatewayv2_route.ws_default.id
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_apigatewayv2_stage" "websocket_default" {
  api_id        = aws_apigatewayv2_api.websocket.id
  name          = "$default"
  auto_deploy   = false
  deployment_id = aws_apigatewayv2_deployment.websocket.id
}

resource "aws_lambda_permission" "allow_websocket_lambda" {
  statement_id  = "AllowExecutionFromAPIGatewayWebSocket"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws_handler.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.websocket.execution_arn}/*"
}
