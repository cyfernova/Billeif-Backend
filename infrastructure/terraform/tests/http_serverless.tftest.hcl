mock_provider "aws" {
  override_during = plan

  mock_data "aws_iam_policy_document" {
    override_during = plan
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  mock_resource "aws_lambda_invocation" {
    defaults = {
      result = "{\"status\":\"applied\",\"version\":53,\"latest_version\":53,\"dirty\":false,\"manifest_checksum\":\"e6b4bef1df19096e4ab93d3aec502445f0ba0c25af134cb2b2bef09b8aa1af9f\"}"
    }
  }

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "928282274753"
      arn        = "arn:aws:iam::928282274753:user/terraform-test"
      user_id    = "AIDATEST1234567890"
    }
  }

  override_data {
    target = data.aws_partition.current
    values = {
      partition = "aws"
    }
  }

  override_resource {
    target          = aws_api_gateway_rest_api.main
    override_during = plan
    values = {
      id               = "test-rest-api"
      root_resource_id = "test-root-resource"
      execution_arn    = "arn:aws:execute-api:ap-south-1:928282274753:test-rest-api"
    }
  }

  override_resource {
    target          = aws_apigatewayv2_api.http
    override_during = plan
    values = {
      id            = "test-http-api"
      api_endpoint  = "https://test-http-api.execute-api.ap-south-1.amazonaws.com"
      execution_arn = "arn:aws:execute-api:ap-south-1:928282274753:test-http-api"
      protocol_type = "HTTP"
    }
  }

  override_resource {
    target          = aws_kms_key.application_secrets
    override_during = plan
    values = {
      arn    = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
      key_id = "application-secrets"
    }
  }

  override_resource {
    target          = aws_db_instance.main
    override_during = plan
    values = {
      id         = "db-INTERNALRESOURCEID"
      identifier = "billeif-test-postgres"
      address    = "database.internal"
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:rds-managed"
      }]
    }
  }

  override_resource {
    target          = aws_iam_role.lambda_exec
    override_during = plan
    values = {
      arn = "arn:aws:iam::928282274753:role/billeif-test-lambda-exec-role"
      id  = "billeif-test-lambda-exec-role"
    }
  }

  override_resource {
    target          = aws_iam_role.lambda_http_exec
    override_during = plan
    values = {
      arn = "arn:aws:iam::928282274753:role/billeif-test-lambda-http-exec-role"
      id  = "billeif-test-lambda-http-exec-role"
    }
  }

  override_resource {
    target          = aws_s3_bucket.business_logos
    override_during = plan
    values = {
      arn = "arn:aws:s3:::billeif-test-928282274753-business-logos"
    }
  }

  override_resource {
    target          = aws_s3_bucket.invoices_pdf
    override_during = plan
    values = {
      arn = "arn:aws:s3:::billeif-test-928282274753-invoices-pdf"
    }
  }

  override_resource {
    target          = aws_s3_bucket.product_images
    override_during = plan
    values = {
      arn = "arn:aws:s3:::billeif-test-928282274753-product-images"
    }
  }

  override_resource {
    target          = aws_s3_bucket.email_sink
    override_during = plan
    values = {
      arn = "arn:aws:s3:::billeif-test-928282274753-email-sink"
    }
  }

  override_resource {
    target          = aws_dynamodb_table.ws_connections
    override_during = plan
    values = {
      arn = "arn:aws:dynamodb:ap-south-1:928282274753:table/billeif-test-ws-connections"
    }
  }

  override_resource {
    target          = aws_dynamodb_table.phone_auth_cooldowns
    override_during = plan
    values = {
      arn = "arn:aws:dynamodb:ap-south-1:928282274753:table/billeif-test-phone-auth-cooldowns"
    }
  }

  override_resource {
    target          = aws_apigatewayv2_api.websocket
    override_during = plan
    values = {
      execution_arn = "arn:aws:execute-api:ap-south-1:928282274753:test-websocket-api"
    }
  }

  override_data {
    target          = data.aws_iam_policy_document.lambda_app
    override_during = plan
    values = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Sid\":\"SharedA2A\",\"Effect\":\"Allow\",\"Action\":\"sqs:SendMessage\",\"Resource\":\"arn:aws:sqs:ap-south-1:928282274753:a2a\"}]}"
    }
  }

  override_data {
    target          = data.aws_iam_policy_document.lambda_http_app
    override_during = plan
    values = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Sid\":\"HTTP\",\"Effect\":\"Allow\",\"Action\":\"sqs:SendMessage\",\"Resource\":\"arn:aws:sqs:ap-south-1:928282274753:http\"}]}"
    }
  }

  override_resource {
    target          = aws_security_group.database_migrator
    override_during = plan
    values = {
      id = "sg-database-migrator"
    }
  }

  override_resource {
    target          = aws_security_group.lambda
    override_during = plan
    values = {
      id = "sg-application-lambda"
    }
  }

  override_resource {
    target          = aws_subnet.private[0]
    override_during = plan
    values = {
      id = "subnet-private-a"
    }
  }

  override_resource {
    target          = aws_subnet.private[1]
    override_during = plan
    values = {
      id = "subnet-private-b"
    }
  }
}

mock_provider "aws" {
  alias           = "ap_south_1"
  override_during = plan
}

mock_provider "aws" {
  alias           = "us_east_1"
  override_during = plan
}

mock_provider "awscc" {
  override_during = plan
}

variables {
  project_name                       = "billeif-test"
  environment                        = "test"
  lambda_artifact_dir                = "tests/fixtures/lambda"
  migration_lambda_artifact_path     = "tests/fixtures/lambda/http.zip"
  llm_api_url                        = "https://llm.example.test/chat/completions"
  llm_model                          = "test-model"
  deepseek_base_url                  = "https://voice-llm.example.test/v1"
  deepseek_model                     = "voice-test-model"
  ses_verified_identity              = "billeif.example"
  ses_sender_email                   = "notifications@billeif.example"
  db_allowed_cidr                    = "10.0.0.0/24"
  enable_application                 = true
  enable_lambda_reserved_concurrency = true
  alert_email                        = "alerts@example.com"
  alert_email_subscription_confirmed = true
}

run "ordinary_http_uses_payload_v1_with_bounded_execution" {
  command = plan

  assert {
    condition = (
      aws_apigatewayv2_api.http.protocol_type == "HTTP" &&
      aws_apigatewayv2_api.http.name == "${local.resource_prefix}-http-api" &&
      aws_apigatewayv2_integration.http_lambda.integration_type == "AWS_PROXY" &&
      aws_apigatewayv2_integration.http_lambda.integration_method == "POST" &&
      aws_apigatewayv2_integration.http_lambda.payload_format_version == "1.0" &&
      aws_apigatewayv2_integration.http_lambda.timeout_milliseconds == 29000 &&
      aws_apigatewayv2_route.http_default.route_key == "$default" &&
      aws_apigatewayv2_route.http_default.authorization_type == "NONE"
    )
    error_message = "Ordinary Billeif routes must use an unauthenticated-at-gateway HTTP API payload-v1 proxy so Gin retains dual-Cognito authorization."
  }

  assert {
    condition = (
      aws_lambda_function.api_http.architectures[0] == "arm64" &&
      aws_lambda_function.api_http.runtime == "provided.al2023" &&
      aws_lambda_function.api_http.timeout == 28 &&
      aws_lambda_function.api_http.environment[0].variables["SERVER_BASE_URL"] == local.http_api_invoke_url &&
      length(aws_lambda_permission.allow_http_api_http) == 1
    )
    error_message = "The ordinary HTTP Lambda must be ARM64, bounded to 28 seconds, use the HTTP base URL, and be invokable only after reviewed enablement."
  }

  assert {
    condition = (
      aws_apigatewayv2_stage.http.auto_deploy == true &&
      aws_apigatewayv2_stage.http.default_route_settings[0].detailed_metrics_enabled == false &&
      aws_cloudwatch_log_group.http_api_access.retention_in_days == 14 &&
      can(jsondecode(aws_apigatewayv2_stage.http.access_log_settings[0].format)) &&
      strcontains(aws_apigatewayv2_stage.http.access_log_settings[0].format, "$context.requestId") &&
      strcontains(aws_apigatewayv2_stage.http.access_log_settings[0].format, "$context.routeKey")
    )
    error_message = "The HTTP API stage must auto-deploy with standard metrics only and 14-day structured access logs."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.http_api_5xx.metric_name == "5xx" &&
      aws_cloudwatch_metric_alarm.http_api_5xx.namespace == "AWS/ApiGateway" &&
      aws_cloudwatch_metric_alarm.http_api_5xx.dimensions.ApiId == aws_apigatewayv2_api.http.id &&
      aws_cloudwatch_metric_alarm.http_api_5xx.dimensions.Stage == aws_apigatewayv2_stage.http.name &&
      aws_cloudwatch_metric_alarm.http_api_5xx.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.http_api_5xx.datapoints_to_alarm == 2 &&
      aws_cloudwatch_metric_alarm.http_api_5xx.treat_missing_data == "notBreaching" &&
      aws_cloudwatch_metric_alarm.http_api_latency.metric_name == "Latency" &&
      aws_cloudwatch_metric_alarm.http_api_latency.extended_statistic == "p95" &&
      aws_cloudwatch_metric_alarm.http_api_latency.threshold == 1500 &&
      aws_cloudwatch_metric_alarm.http_api_latency.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.http_api_latency.datapoints_to_alarm == 2
    )
    error_message = "Billeif HTTP API handled 5xx responses and stage p95 latency must have standard two-of-three alarms."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.rds_cpu_high.dimensions.DBInstanceIdentifier == aws_db_instance.main.identifier &&
      aws_cloudwatch_metric_alarm.rds_storage_low.dimensions.DBInstanceIdentifier == aws_db_instance.main.identifier &&
      aws_cloudwatch_metric_alarm.rds_connections_high.dimensions.DBInstanceIdentifier == aws_db_instance.main.identifier &&
      aws_cloudwatch_metric_alarm.rds_memory_low.dimensions.DBInstanceIdentifier == aws_db_instance.main.identifier &&
      aws_cloudwatch_metric_alarm.rds_cpu_credits_low.dimensions.DBInstanceIdentifier == aws_db_instance.main.identifier &&
      strcontains(aws_cloudwatch_dashboard.main.dashboard_body, aws_db_instance.main.identifier) &&
      !strcontains(aws_cloudwatch_dashboard.main.dashboard_body, aws_db_instance.main.id)
    )
    error_message = "RDS alarms and dashboards must select metrics by DBInstanceIdentifier, not by the internal RDS resource ID."
  }
}

run "http_execution_role_is_dedicated_and_least_privilege" {
  command = plan

  assert {
    condition = (
      aws_iam_role_policy.lambda_http_app.role == aws_iam_role.lambda_http_exec.id &&
      aws_iam_role_policy.lambda_app.role == aws_iam_role.lambda_exec.id &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPQueueSend"
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_app.statement : statement
        if statement.sid == "HTTPQueueSend"
      ]) == 0
    )
    error_message = "The HTTP role must consume its dedicated policy document without changing the shared A2A role policy."
  }

  assert {
    condition = (
      length(data.aws_iam_policy_document.lambda_http_app.statement) == 16 &&
      toset(flatten([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement.actions
        ])) == toset([
        "dynamodb:DeleteItem",
        "dynamodb:PutItem",
        "dynamodb:Query",
        "dynamodb:Scan",
        "execute-api:ManageConnections",
        "kms:Decrypt",
        "s3:DeleteObject",
        "s3:GetObject",
        "s3:ListBucket",
        "s3:PutObject",
        "secretsmanager:GetSecretValue",
        "ses:SendEmail",
        "sqs:SendMessage",
        "ssm:GetParameters",
      ])
    )
    error_message = "The HTTP inline policy must expose only operations reached by the ordinary HTTP runtime."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
      if statement.sid == "HTTPDatabaseHostParameter" &&
      toset(statement.actions) == toset(["ssm:GetParameters"]) &&
      toset(statement.resources) == toset([local.db_host_ssm_parameter_arn]) &&
      length([
        for condition in statement.condition : condition
        if condition.test == "Bool" &&
        condition.variable == "aws:SecureTransport" &&
        toset(condition.values) == toset(["true"])
      ]) == 1
    ]) == 1
    error_message = "HTTP SSM access must read only the database host parameter over secure transport."
  }

  assert {
    condition = (
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPBusinessAssetWrites" &&
        toset(statement.actions) == toset(["s3:PutObject"]) &&
        toset(statement.resources) == toset([
          "${aws_s3_bucket.business_logos.arn}/logos/*",
          "${aws_s3_bucket.business_logos.arn}/profile-pictures/*",
        ])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPProductImageWrites" &&
        toset(statement.actions) == toset(["s3:PutObject"]) &&
        toset(statement.resources) == toset(["${aws_s3_bucket.product_images.arn}/products/*"])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPFinalInvoiceReads" &&
        toset(statement.actions) == toset(["s3:GetObject"]) &&
        toset(statement.resources) == toset(["${aws_s3_bucket.invoices_pdf.arn}/invoices/*/*/v*/final.pdf"])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPInvoiceAssetWrites" &&
        toset(statement.actions) == toset(["s3:PutObject"]) &&
        toset(statement.resources) == toset([
          "${aws_s3_bucket.invoices_pdf.arn}/bulk-jobs/*",
          "${aws_s3_bucket.invoices_pdf.arn}/gst/*",
          "${aws_s3_bucket.invoices_pdf.arn}/signature-profiles/*",
          "${aws_s3_bucket.invoices_pdf.arn}/????????-????-????-????-????????????/????????/????????-????-????-????-????????????",
        ])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPDriveAssetDeletes" &&
        toset(statement.actions) == toset(["s3:DeleteObject"]) &&
        toset(statement.resources) == toset(["${aws_s3_bucket.invoices_pdf.arn}/????????-????-????-????-????????????/????????/????????-????-????-????-????????????"])
      ]) == 1
    )
    error_message = "HTTP S3 access must be action- and object-pattern-specific with no unscoped resource."
  }

  assert {
    condition = (
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPEmailCaptureObjects" &&
        toset(statement.actions) == toset(["s3:GetObject", "s3:PutObject"]) &&
        toset(statement.resources) == toset(["${aws_s3_bucket.email_sink.arn}/emails/*"])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPEmailCaptureList" &&
        toset(statement.actions) == toset(["s3:ListBucket"]) &&
        toset(statement.resources) == toset([aws_s3_bucket.email_sink.arn]) &&
        length([
          for condition in statement.condition : condition
          if condition.test == "StringLike" &&
          condition.variable == "s3:prefix" &&
          toset(condition.values) == toset(["emails/*"])
        ]) == 1
      ]) == 1
    )
    error_message = "Current admin email inspection must retain only the email sink emails prefix."
  }

  assert {
    condition = (
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPWebSocketTable" &&
        toset(statement.actions) == toset(["dynamodb:DeleteItem", "dynamodb:Scan"]) &&
        toset(statement.resources) == toset([aws_dynamodb_table.ws_connections.arn])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPWebSocketUserIndex" &&
        toset(statement.actions) == toset(["dynamodb:Query"]) &&
        toset(statement.resources) == toset(["${aws_dynamodb_table.ws_connections.arn}/index/user_id-index"])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPPhoneAuthCooldown" &&
        toset(statement.actions) == toset(["dynamodb:PutItem"]) &&
        toset(statement.resources) == toset([aws_dynamodb_table.phone_auth_cooldowns.arn])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
        if statement.sid == "HTTPWebSocketManageConnections" &&
        toset(statement.actions) == toset(["execute-api:ManageConnections"]) &&
        toset(statement.resources) == toset(["${aws_apigatewayv2_api.websocket.execution_arn}/${var.environment}/POST/@connections/*"])
      ]) == 1
    )
    error_message = "HTTP DynamoDB and WebSocket permissions must match the exact table, index, and stage routes used by management endpoints."
  }

  assert {
    condition = (
      aws_dynamodb_table.ws_connections.ttl[0].attribute_name == "ttl" &&
      aws_dynamodb_table.ws_connections.ttl[0].enabled == true
    )
    error_message = "WebSocket connection records must enable DynamoDB TTL for stale connection cleanup."
  }
}

run "rest_api_is_streaming_only_without_detailed_metrics" {
  command = plan

  assert {
    condition = (
      aws_api_gateway_integration.api_v1_a2a_message_stream_post.response_transfer_mode == "STREAM" &&
      aws_api_gateway_integration.api_v1_a2a_task_subscribe_get.response_transfer_mode == "STREAM" &&
      aws_api_gateway_method_settings.main.settings[0].metrics_enabled == false &&
      aws_cloudwatch_log_group.rest_api_access.retention_in_days == 14 &&
      can(jsondecode(aws_api_gateway_stage.main.access_log_settings[0].format))
    )
    error_message = "REST must retain exactly the streaming integrations, standard service metrics, and 14-day structured access logs."
  }
}

run "launch_safe_throttling_allows_mobile_startup_bursts" {
  command = plan

  variables {
    enable_lambda_reserved_concurrency = false
  }

  assert {
    condition = (
      aws_apigatewayv2_stage.http.default_route_settings[0].throttling_burst_limit == 20 &&
      aws_apigatewayv2_stage.http.default_route_settings[0].throttling_rate_limit == 10 &&
      aws_api_gateway_method_settings.main.settings[0].throttling_burst_limit == 20 &&
      aws_api_gateway_method_settings.main.settings[0].throttling_rate_limit == 10 &&
      aws_apigatewayv2_stage.websocket_default.default_route_settings[0].throttling_burst_limit == 20 &&
      aws_apigatewayv2_stage.websocket_default.default_route_settings[0].throttling_rate_limit == 10
    )
    error_message = "Launch-safe API Gateway throttling must absorb a normal mobile startup burst while retaining a low steady-state cost cap."
  }

  assert {
    condition = (
      contains(split(",", aws_lambda_function.api_http.environment[0].variables["ALLOWED_ORIGINS"]), "https://billeif.com") &&
      contains(split(",", aws_lambda_function.api_http.environment[0].variables["ALLOWED_ORIGINS"]), "https://www.billeif.com") &&
      contains(split(",", aws_lambda_function.api_http.environment[0].variables["ALLOWED_ORIGINS"]), local.http_api_invoke_url)
    )
    error_message = "The application Lambda must allow both Billeif website origins and its invoke URL without using wildcard CORS."
  }
}
