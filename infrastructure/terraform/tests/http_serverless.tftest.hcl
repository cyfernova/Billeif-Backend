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
      result = "{\"status\":\"applied\",\"version\":45,\"latest_version\":45,\"dirty\":false,\"manifest_checksum\":\"be1d51afbbb4e42563b767d03efc375a71fb7027f54d089bbf71792c62814452\"}"
    }
  }

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "123456789012"
      arn        = "arn:aws:iam::123456789012:user/terraform-test"
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
      execution_arn    = "arn:aws:execute-api:ap-south-1:123456789012:test-rest-api"
    }
  }

  override_resource {
    target          = aws_apigatewayv2_api.http
    override_during = plan
    values = {
      id            = "test-http-api"
      api_endpoint  = "https://test-http-api.execute-api.ap-south-1.amazonaws.com"
      execution_arn = "arn:aws:execute-api:ap-south-1:123456789012:test-http-api"
      protocol_type = "HTTP"
    }
  }

  override_resource {
    target          = aws_kms_key.application_secrets
    override_during = plan
    values = {
      arn    = "arn:aws:kms:ap-south-1:123456789012:key/application-secrets"
      key_id = "application-secrets"
    }
  }

  override_resource {
    target          = aws_db_instance.main
    override_during = plan
    values = {
      address = "database.internal"
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:123456789012:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:rds-managed"
      }]
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
