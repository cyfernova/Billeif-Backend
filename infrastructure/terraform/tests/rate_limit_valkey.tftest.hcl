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
      result = "{\"status\":\"applied\",\"version\":48,\"latest_version\":48,\"dirty\":false,\"manifest_checksum\":\"9a2241873f45c1cf45b4ab15787a8f7407f7024e2d1306bb8ad0f2425680ceba\"}"
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
      address = "database.internal"
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:rds-managed"
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
    target          = aws_vpc.main
    override_during = plan
    values = {
      id = "vpc-billeif-test"
    }
  }

  override_resource {
    target          = aws_iam_role.lambda_http_exec
    override_during = plan
    values = {
      id  = "billeif-test-test-lambda-http-exec-role"
      arn = "arn:aws:iam::928282274753:role/billeif-test-test-lambda-http-exec-role"
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
    target          = aws_security_group.rate_limit_valkey
    override_during = plan
    values = {
      id = "sg-rate-limit-valkey"
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

  override_resource {
    target          = aws_elasticache_serverless_cache.rate_limit
    override_during = plan
    values = {
      arn = "arn:aws:elasticache:ap-south-1:928282274753:serverlesscache:billeif-test-test-rate-limit"
      endpoint = [{
        address = "billeif-test-test-rate-limit.serverless.aps1.cache.amazonaws.com"
        port    = 6379
      }]
    }
  }

  override_resource {
    target          = aws_elasticache_user.rate_limit_http
    override_during = plan
    values = {
      arn = "arn:aws:elasticache:ap-south-1:928282274753:user:billeif-test-test-http"
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

run "rate_limit_backend_is_private_tls_valkey_with_iam_only_rbac" {
  command = plan

  assert {
    condition = (
      aws_elasticache_serverless_cache.rate_limit.engine == "valkey" &&
      aws_elasticache_serverless_cache.rate_limit.major_engine_version == "7" &&
      aws_elasticache_serverless_cache.rate_limit.name == "${local.resource_prefix}-rate-limit" &&
      aws_elasticache_serverless_cache.rate_limit.user_group_id == aws_elasticache_user_group.rate_limit.user_group_id &&
      toset(aws_elasticache_serverless_cache.rate_limit.subnet_ids) == toset(aws_subnet.private[*].id) &&
      toset(aws_elasticache_serverless_cache.rate_limit.security_group_ids) == toset([aws_security_group.rate_limit_valkey.id]) &&
      aws_elasticache_serverless_cache.rate_limit.cache_usage_limits[0].data_storage[0].maximum == 1 &&
      aws_elasticache_serverless_cache.rate_limit.cache_usage_limits[0].ecpu_per_second[0].maximum == 1000
    )
    error_message = "Rate limiting must use the bounded private Valkey 7 serverless cache and IAM-only user group."
  }

  assert {
    condition = (
      aws_elasticache_user.rate_limit_http.engine == "valkey" &&
      aws_elasticache_user.rate_limit_http.user_id == "${local.resource_prefix}-http" &&
      aws_elasticache_user.rate_limit_http.user_name == aws_elasticache_user.rate_limit_http.user_id &&
      aws_elasticache_user.rate_limit_http.authentication_mode[0].type == "iam" &&
      aws_elasticache_user.rate_limit_http.access_string == "on ~{billeif-rate-limit}:* -@all +@connection +cluster|slots +get +incr +pexpire +pttl +eval +evalsha" &&
      toset(aws_elasticache_user_group.rate_limit.user_ids) == toset([aws_elasticache_user.rate_limit_http.user_id])
    )
    error_message = "The cache must expose only the short-lived IAM user and only the commands and key namespace used by rate limiting."
  }

  assert {
    condition = (
      length(aws_security_group.rate_limit_valkey.ingress) == 1 &&
      one(aws_security_group.rate_limit_valkey.ingress).from_port == 6379 &&
      one(aws_security_group.rate_limit_valkey.ingress).to_port == 6380 &&
      one(aws_security_group.rate_limit_valkey.ingress).protocol == "tcp" &&
      toset(one(aws_security_group.rate_limit_valkey.ingress).security_groups) == toset([aws_security_group.lambda.id]) &&
      length(coalesce(one(aws_security_group.rate_limit_valkey.ingress).cidr_blocks, [])) == 0
    )
    error_message = "Valkey ingress must allow both serverless cache ports only from the application Lambda security group."
  }
}

run "http_lambda_alone_receives_required_backend_configuration" {
  command = plan

  assert {
    condition = (
      aws_lambda_function.api_http.environment[0].variables.REDIS_HOST == aws_elasticache_serverless_cache.rate_limit.endpoint[0].address &&
      aws_lambda_function.api_http.environment[0].variables.REDIS_PORT == tostring(aws_elasticache_serverless_cache.rate_limit.endpoint[0].port) &&
      aws_lambda_function.api_http.environment[0].variables.REDIS_USER_ID == aws_elasticache_user.rate_limit_http.user_id &&
      aws_lambda_function.api_http.environment[0].variables.REDIS_CACHE_NAME == aws_elasticache_serverless_cache.rate_limit.name &&
      aws_lambda_function.api_http.environment[0].variables.REDIS_TLS_ENABLED == "true" &&
      aws_lambda_function.api_http.environment[0].variables.REDIS_IAM_AUTH_ENABLED == "true" &&
      aws_lambda_function.api_http.environment[0].variables.REDIS_CLUSTER_MODE == "true" &&
      aws_lambda_function.api_http.environment[0].variables.RATE_LIMIT_DECISION_TIMEOUT == "2s" &&
      !contains(keys(aws_lambda_function.api_http.environment[0].variables), "REDIS_PASSWORD") &&
      !contains(keys(aws_lambda_function.api_http.environment[0].variables), "RATE_LIMIT_TRUSTED_PROXY_CIDR") &&
      alltrue([
        for variables in [
          aws_lambda_function.a2a_stream.environment[0].variables,
          aws_lambda_function.sqs_invoice.environment[0].variables,
          aws_lambda_function.sqs_gst.environment[0].variables,
          aws_lambda_function.sqs_bargaining.environment[0].variables,
          aws_lambda_function.ws_handler.environment[0].variables,
        ] : !contains(keys(variables), "REDIS_HOST")
      ])
    )
    error_message = "Only the HTTP runtime may receive the complete TLS/IAM Valkey connection configuration, without a static credential or trusted proxy override."
  }
}

run "http_lambda_connect_permission_is_exact_and_vpc_bound" {
  command = plan

  assert {
    condition = (
      aws_iam_role_policy.rate_limit_connect.role == aws_iam_role.lambda_http_exec.id &&
      jsondecode(aws_iam_role_policy.rate_limit_connect.policy).Statement[0].Action == ["elasticache:Connect"] &&
      toset(jsondecode(aws_iam_role_policy.rate_limit_connect.policy).Statement[0].Resource) == toset([
        aws_elasticache_serverless_cache.rate_limit.arn,
        aws_elasticache_user.rate_limit_http.arn,
      ]) &&
      jsondecode(aws_iam_role_policy.rate_limit_connect.policy).Statement[0].Condition.StringEquals["aws:SourceVpc"] == aws_vpc.main.id
    )
    error_message = "Only the HTTP Lambda role may connect, and only to the exact cache/user from this VPC."
  }
}
