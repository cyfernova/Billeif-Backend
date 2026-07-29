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
      result = "{\"status\":\"applied\",\"version\":43,\"latest_version\":43,\"dirty\":false,\"manifest_checksum\":\"e7c51b8069e0785e8d2881a4eb06070c3899107ad55a72166e86e26a7979e936\"}"
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
  enable_lambda_reserved_concurrency = true
}

run "foundation_migrates_while_application_is_fail_closed" {
  command = plan

  assert {
    condition     = var.enable_application == false
    error_message = "The clean deployment must default to application execution disabled."
  }

  assert {
    condition = (
      aws_lambda_function.database_migrator.runtime == "provided.al2023" &&
      aws_lambda_function.database_migrator.architectures[0] == "arm64" &&
      aws_lambda_function.database_migrator.reserved_concurrent_executions == 1 &&
      toset(aws_lambda_function.database_migrator.vpc_config[0].subnet_ids) == toset(aws_subnet.private[*].id) &&
      toset(aws_lambda_function.database_migrator.vpc_config[0].security_group_ids) == toset([aws_security_group.database_migrator.id])
    )
    error_message = "The migration Lambda must be ARM64 AL2023, VPC-attached, and concurrency-one."
  }

  assert {
    condition = (
      length(keys(aws_lambda_function.database_migrator.environment[0].variables)) == 5 &&
      alltrue([
        for key in keys(aws_lambda_function.database_migrator.environment[0].variables) :
        contains([
          "DATABASE_HOST_SSM_PARAM", "DATABASE_SECRET_ARN",
          "DATABASE_PORT", "DATABASE_NAME", "DATABASE_SSL_MODE"
        ], key)
      ])
    )
    error_message = "The migration Lambda environment must contain identifiers and non-secret database configuration only."
  }

  assert {
    condition = (
      length(keys(jsondecode(aws_lambda_invocation.database_migrations.input))) == 1 &&
      jsondecode(aws_lambda_invocation.database_migrations.input).manifest_checksum == local.migration_manifest_checksum &&
      aws_lambda_invocation.database_migrations.lifecycle_scope == "CREATE_ONLY" &&
      length(keys(aws_lambda_invocation.database_migrations.triggers)) == 2 &&
      aws_lambda_invocation.database_migrations.triggers.artifact_checksum == local.migration_artifact_checksum &&
      aws_lambda_invocation.database_migrations.triggers.manifest_checksum == local.migration_manifest_checksum
    )
    error_message = "The synchronous invocation must carry only the checksum and re-run only for artifact/manifest changes."
  }

  assert {
    condition = (
      aws_lambda_function.api_http.reserved_concurrent_executions == 0 &&
      aws_lambda_function.a2a_stream.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_invoice.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_payment.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_gst.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_bargaining.reserved_concurrent_executions == 0 &&
      aws_lambda_function.ws_handler.reserved_concurrent_executions == 0 &&
      aws_lambda_function.voice_session.reserved_concurrent_executions == 0 &&
      aws_lambda_function.custom_sms_sender.reserved_concurrent_executions == 0
    )
    error_message = "Every ordinary application Lambda must be hard-throttled while application execution is disabled."
  }

  assert {
    condition = (
      length(aws_lambda_event_source_mapping.invoice_queue) == 0 &&
      length(aws_lambda_event_source_mapping.payment_queue) == 0 &&
      length(aws_lambda_event_source_mapping.gst_queue) == 0 &&
      length(aws_lambda_event_source_mapping.bargaining_queue) == 0 &&
      length(aws_lambda_permission.allow_rest_api_http) == 0 &&
      length(aws_lambda_permission.allow_rest_a2a_stream) == 0 &&
      length(aws_lambda_permission.allow_websocket_lambda) == 0 &&
      length(aws_lambda_permission.cognito_phone_custom_sms) == 0
    )
    error_message = "API, WebSocket, Cognito, and SQS invocation paths must be absent while disabled."
  }
}

run "migrator_iam_and_database_ingress_are_exact" {
  command = plan

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.database_migrator.statement : statement
      if statement.sid == "MigrationVPC" &&
      length(statement.actions) == 6 &&
      alltrue([
        for action in [
          "ec2:AssignPrivateIpAddresses",
          "ec2:CreateNetworkInterface",
          "ec2:DeleteNetworkInterface",
          "ec2:DescribeNetworkInterfaces",
          "ec2:DescribeSubnets",
          "ec2:UnassignPrivateIpAddresses",
        ] : contains(statement.actions, action)
      ])
    ]) == 1
    error_message = "The VPC-attached migrator must have the complete exact Lambda ENI permission set."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.database_migrator.statement : statement
      if statement.sid == "MigrationSecret" &&
      length(statement.resources) == 1 &&
      contains(statement.resources, aws_db_instance.main.master_user_secret[0].secret_arn) &&
      length(statement.actions) == 2 &&
      contains(statement.actions, "secretsmanager:GetSecretValue") &&
      contains(statement.actions, "secretsmanager:DescribeSecret")
    ]) == 1
    error_message = "The migrator may read only the exact RDS-managed secret."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.database_migrator.statement : statement
      if statement.sid == "MigrationSecretKMS" &&
      length(statement.resources) == 1 &&
      contains(statement.resources, aws_kms_key.application_secrets.arn) &&
      length([
        for condition in statement.condition : condition
        if condition.variable == "kms:ViaService" &&
        contains(condition.values, "secretsmanager.ap-south-1.amazonaws.com")
      ]) == 1 &&
      length([
        for condition in statement.condition : condition
        if condition.variable == "kms:EncryptionContext:SecretARN" &&
        contains(condition.values, aws_db_instance.main.master_user_secret[0].secret_arn)
      ]) == 1
    ]) == 1
    error_message = "The migrator KMS grant must be exact and restricted through Mumbai Secrets Manager."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.database_migrator.statement : statement
      if statement.sid == "MigrationParameters" &&
      length(statement.actions) == 1 &&
      contains(statement.actions, "ssm:GetParameters") &&
      length(statement.resources) == 1 &&
      contains(statement.resources, local.db_host_ssm_parameter_arn)
    ]) == 1
    error_message = "The migrator may batch-read only the exact non-secret database host parameter."
  }

  assert {
    condition = (
      one([
        for ingress in aws_security_group.rds.ingress : ingress
        if ingress.description == "PostgreSQL from database migrator"
      ]).from_port == var.db_port &&
      one([
        for ingress in aws_security_group.rds.ingress : ingress
        if ingress.description == "PostgreSQL from database migrator"
      ]).to_port == var.db_port &&
      toset(one([
        for ingress in aws_security_group.rds.ingress : ingress
        if ingress.description == "PostgreSQL from database migrator"
      ]).security_groups) == toset([aws_security_group.database_migrator.id])
    )
    error_message = "RDS must allow PostgreSQL only from the dedicated migrator security group."
  }
}

run "reviewed_enablement_activates_stable_application_resources_after_migration" {
  command = plan

  variables {
    enable_application = true
  }

  assert {
    condition = (
      aws_lambda_function.api_http.reserved_concurrent_executions == 10 &&
      aws_lambda_function.a2a_stream.reserved_concurrent_executions == 5 &&
      aws_lambda_function.sqs_invoice.reserved_concurrent_executions == 2 &&
      aws_lambda_function.sqs_payment.reserved_concurrent_executions == 2 &&
      aws_lambda_function.sqs_gst.reserved_concurrent_executions == 2 &&
      aws_lambda_function.sqs_bargaining.reserved_concurrent_executions == 5 &&
      aws_lambda_function.ws_handler.reserved_concurrent_executions == 5
    )
    error_message = "Reviewed enablement must activate the stable ordinary Lambda resources."
  }

  assert {
    condition = (
      length(aws_lambda_event_source_mapping.invoice_queue) == 1 &&
      length(aws_lambda_event_source_mapping.payment_queue) == 1 &&
      length(aws_lambda_event_source_mapping.gst_queue) == 1 &&
      length(aws_lambda_event_source_mapping.bargaining_queue) == 1 &&
      length(aws_lambda_permission.allow_rest_api_http) == 1 &&
      length(aws_lambda_permission.allow_rest_a2a_stream) == 1 &&
      length(aws_lambda_permission.allow_websocket_lambda) == 1 &&
      length(aws_lambda_permission.cognito_phone_custom_sms) == 1
    )
    error_message = "Reviewed enablement must activate API, WebSocket, Cognito, and SQS invoke paths."
  }

  assert {
    condition = alltrue([
      aws_lambda_function.api_http.tags.MigrationChecksum == local.application_migration_checksum,
      aws_lambda_function.a2a_stream.tags.MigrationChecksum == local.application_migration_checksum,
      aws_lambda_function.sqs_invoice.tags.MigrationChecksum == local.application_migration_checksum,
      aws_lambda_function.sqs_payment.tags.MigrationChecksum == local.application_migration_checksum,
      aws_lambda_function.sqs_gst.tags.MigrationChecksum == local.application_migration_checksum,
      aws_lambda_function.sqs_bargaining.tags.MigrationChecksum == local.application_migration_checksum,
      aws_lambda_function.ws_handler.tags.MigrationChecksum == local.application_migration_checksum,
      aws_lambda_function.voice_session.tags.MigrationChecksum == local.application_migration_checksum,
      aws_lambda_function.custom_sms_sender.tags.MigrationChecksum == local.application_migration_checksum,
    ])
    error_message = "Every stable application Lambda update must consume the successful migration invocation checksum."
  }
}
