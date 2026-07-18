resource "aws_api_gateway_rest_api" "main" {
  name               = "${var.project_name}-rest-api"
  description        = "REST API for ${var.project_name} Lambda backend"
  binary_media_types = ["multipart/form-data", "application/octet-stream", "audio/mp4", "audio/mpeg", "audio/wav", "audio/webm", "audio/x-caf"]
}

resource "aws_api_gateway_authorizer" "cognito" {
  name            = "${var.project_name}-cognito"
  rest_api_id     = aws_api_gateway_rest_api.main.id
  type            = "COGNITO_USER_POOLS"
  provider_arns   = [aws_cognito_user_pool.main.arn, aws_cognito_user_pool.phone.arn]
  identity_source = "method.request.header.Authorization"
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

resource "aws_api_gateway_resource" "health" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = "health"
}

resource "aws_api_gateway_method" "health_get" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.health.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "health_get" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.health.id
  http_method             = aws_api_gateway_method.health_get.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_resource" "swagger" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = "swagger"
}

resource "aws_api_gateway_resource" "swagger_proxy" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.swagger.id
  path_part   = "{swaggerProxy+}"
}

resource "aws_api_gateway_method" "swagger_proxy_any" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.swagger_proxy.id
  http_method   = "ANY"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "swagger_proxy_any" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.swagger_proxy.id
  http_method             = aws_api_gateway_method.swagger_proxy_any.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_resource" "well_known" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = ".well-known"
}

resource "aws_api_gateway_resource" "well_known_proxy" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.well_known.id
  path_part   = "{wellKnownProxy+}"
}

resource "aws_api_gateway_method" "well_known_proxy_any" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.well_known_proxy.id
  http_method   = "ANY"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "well_known_proxy_any" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.well_known_proxy.id
  http_method             = aws_api_gateway_method.well_known_proxy_any.http_method
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
  rest_api_id          = aws_api_gateway_rest_api.main.id
  resource_id          = aws_api_gateway_resource.proxy.id
  http_method          = "ANY"
  authorization        = "COGNITO_USER_POOLS"
  authorizer_id        = aws_api_gateway_authorizer.cognito.id
  authorization_scopes = ["aws.cognito.signin.user.admin"]
}

resource "aws_api_gateway_integration" "proxy_any" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.proxy.id
  http_method             = aws_api_gateway_method.proxy_any.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

# Browser preflight must reach the app CORS middleware without requiring a token.
resource "aws_api_gateway_method" "proxy_options" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.proxy.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "proxy_options" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.proxy.id
  http_method             = aws_api_gateway_method.proxy_options.http_method
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

resource "aws_api_gateway_resource" "api_v1_auth" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1.id
  path_part   = "auth"
}

resource "aws_api_gateway_resource" "api_v1_auth_proxy" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_auth.id
  path_part   = "{authProxy+}"
}

resource "aws_api_gateway_method" "api_v1_auth_proxy_any" {
  rest_api_id          = aws_api_gateway_rest_api.main.id
  resource_id          = aws_api_gateway_resource.api_v1_auth_proxy.id
  http_method          = "ANY"
  authorization        = "COGNITO_USER_POOLS"
  authorizer_id        = aws_api_gateway_authorizer.cognito.id
  authorization_scopes = ["aws.cognito.signin.user.admin"]
}

resource "aws_api_gateway_integration" "api_v1_auth_proxy_any" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_proxy.id
  http_method             = aws_api_gateway_method.api_v1_auth_proxy_any.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

locals {
  public_auth_direct_paths = toset([
    "register",
    "login",
    "logout",
    "refresh",
    "forgot-password",
    "reset-password",
    "verify-email",
    "resend-verification"
  ])
  public_phone_auth_paths = toset([
    "register",
    "confirm",
    "resend-confirmation",
    "login",
    "verify-login",
    "refresh"
  ])
}

resource "aws_api_gateway_resource" "api_v1_auth_public" {
  for_each    = local.public_auth_direct_paths
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_auth.id
  path_part   = each.value
}

resource "aws_api_gateway_method" "api_v1_auth_public_post" {
  for_each      = local.public_auth_direct_paths
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_auth_public[each.key].id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_auth_public_post" {
  for_each                = local.public_auth_direct_paths
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_public[each.key].id
  http_method             = aws_api_gateway_method.api_v1_auth_public_post[each.key].http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_method" "api_v1_auth_public_options" {
  for_each      = local.public_auth_direct_paths
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_auth_public[each.key].id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_auth_public_options" {
  for_each                = local.public_auth_direct_paths
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_public[each.key].id
  http_method             = aws_api_gateway_method.api_v1_auth_public_options[each.key].http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_resource" "api_v1_auth_phone" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_auth.id
  path_part   = "phone"
}

resource "aws_api_gateway_resource" "api_v1_auth_phone_public" {
  for_each    = local.public_phone_auth_paths
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_auth_phone.id
  path_part   = each.value
}

resource "aws_api_gateway_method" "api_v1_auth_phone_public_post" {
  for_each      = local.public_phone_auth_paths
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_auth_phone_public[each.key].id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_auth_phone_public_post" {
  for_each                = local.public_phone_auth_paths
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_phone_public[each.key].id
  http_method             = aws_api_gateway_method.api_v1_auth_phone_public_post[each.key].http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_method" "api_v1_auth_phone_public_options" {
  for_each      = local.public_phone_auth_paths
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_auth_phone_public[each.key].id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_auth_phone_public_options" {
  for_each                = local.public_phone_auth_paths
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_phone_public[each.key].id
  http_method             = aws_api_gateway_method.api_v1_auth_phone_public_options[each.key].http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_resource" "api_v1_auth_phone_logout" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_auth_phone.id
  path_part   = "logout"
}

resource "aws_api_gateway_method" "api_v1_auth_phone_logout_post" {
  rest_api_id          = aws_api_gateway_rest_api.main.id
  resource_id          = aws_api_gateway_resource.api_v1_auth_phone_logout.id
  http_method          = "POST"
  authorization        = "COGNITO_USER_POOLS"
  authorizer_id        = aws_api_gateway_authorizer.cognito.id
  authorization_scopes = ["aws.cognito.signin.user.admin"]
}

resource "aws_api_gateway_integration" "api_v1_auth_phone_logout_post" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_phone_logout.id
  http_method             = aws_api_gateway_method.api_v1_auth_phone_logout_post.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_method" "api_v1_auth_phone_logout_options" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_auth_phone_logout.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_auth_phone_logout_options" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_phone_logout.id
  http_method             = aws_api_gateway_method.api_v1_auth_phone_logout_options.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

# Google sign-in accepts a Cognito ID token and remains protected at the gateway.
resource "aws_api_gateway_resource" "api_v1_auth_google" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_auth.id
  path_part   = "google"
}

resource "aws_api_gateway_method" "api_v1_auth_google_post" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_auth_google.id
  http_method   = "POST"
  authorization = "COGNITO_USER_POOLS"
  authorizer_id = aws_api_gateway_authorizer.cognito.id
}

resource "aws_api_gateway_integration" "api_v1_auth_google_post" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_google.id
  http_method             = aws_api_gateway_method.api_v1_auth_google_post.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_method" "api_v1_auth_google_options" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_auth_google.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_auth_google_options" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_auth_google.id
  http_method             = aws_api_gateway_method.api_v1_auth_google_options.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_resource" "api_v1_public" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1.id
  path_part   = "public"
}

resource "aws_api_gateway_resource" "api_v1_public_proxy" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_public.id
  path_part   = "{publicProxy+}"
}

resource "aws_api_gateway_method" "api_v1_public_proxy_any" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_public_proxy.id
  http_method   = "ANY"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_public_proxy_any" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_public_proxy.id
  http_method             = aws_api_gateway_method.api_v1_public_proxy_any.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_resource" "api_v1_webhooks" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1.id
  path_part   = "webhooks"
}

resource "aws_api_gateway_resource" "api_v1_webhooks_razorpay" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.api_v1_webhooks.id
  path_part   = "razorpay"
}

resource "aws_api_gateway_method" "api_v1_webhooks_razorpay_post" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_webhooks_razorpay.id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_webhooks_razorpay_post" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_webhooks_razorpay.id
  http_method             = aws_api_gateway_method.api_v1_webhooks_razorpay_post.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
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

resource "aws_api_gateway_method" "api_v1_a2a_message_stream_options" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_a2a_message_stream.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_a2a_message_stream_options" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_a2a_message_stream.id
  http_method             = aws_api_gateway_method.api_v1_a2a_message_stream_options.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
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

resource "aws_api_gateway_method" "api_v1_a2a_task_subscribe_options" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.api_v1_a2a_task_subscribe.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "api_v1_a2a_task_subscribe_options" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.api_v1_a2a_task_subscribe.id
  http_method             = aws_api_gateway_method.api_v1_a2a_task_subscribe_options.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api_http.invoke_arn
}

resource "aws_api_gateway_deployment" "main" {
  rest_api_id = aws_api_gateway_rest_api.main.id

  triggers = {
    redeploy = sha1(jsonencode([
      aws_api_gateway_integration.root_any.id,
      aws_api_gateway_integration.health_get.id,
      aws_api_gateway_integration.swagger_proxy_any.id,
      aws_api_gateway_integration.well_known_proxy_any.id,
      aws_api_gateway_integration.proxy_any.id,
      aws_api_gateway_integration.proxy_options.id,
      aws_api_gateway_integration.api_v1_auth_proxy_any.id,
      values(aws_api_gateway_integration.api_v1_auth_public_post)[*].id,
      values(aws_api_gateway_integration.api_v1_auth_public_options)[*].id,
      values(aws_api_gateway_integration.api_v1_auth_phone_public_post)[*].id,
      values(aws_api_gateway_integration.api_v1_auth_phone_public_options)[*].id,
      aws_api_gateway_integration.api_v1_auth_phone_logout_post.id,
      aws_api_gateway_integration.api_v1_auth_phone_logout_options.id,
      aws_api_gateway_integration.api_v1_auth_google_post.id,
      aws_api_gateway_integration.api_v1_auth_google_options.id,
      aws_api_gateway_integration.api_v1_public_proxy_any.id,
      aws_api_gateway_integration.api_v1_webhooks_razorpay_post.id,
      aws_api_gateway_integration.api_v1_a2a_message_stream_post.id,
      aws_api_gateway_integration.api_v1_a2a_message_stream_options.id,
      aws_api_gateway_integration.api_v1_a2a_task_subscribe_get.id,
      aws_api_gateway_integration.api_v1_a2a_task_subscribe_options.id,
      aws_api_gateway_rest_api.main.binary_media_types
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_cloudwatch_log_group" "rest_api_access" {
  name              = "/aws/apigateway/${var.project_name}-rest"
  retention_in_days = var.log_retention_days
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
    metrics_enabled        = true
    data_trace_enabled     = false
    throttling_burst_limit = 100
    throttling_rate_limit  = 50
  }
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
