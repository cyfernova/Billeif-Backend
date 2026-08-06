mock_provider "aws" {
  override_during = plan

  mock_data "aws_ami" {
    defaults = {
      id = "ami-0billeifnat"
    }
  }

  mock_data "aws_iam_policy_document" {
    override_during = plan
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
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

  override_data {
    target = data.aws_availability_zone.voice_agentcore[0]
    values = {
      name    = "ap-south-1a"
      zone_id = "aps1-az1"
    }
  }

  override_data {
    target = data.aws_availability_zone.voice_agentcore[1]
    values = {
      name    = "ap-south-1b"
      zone_id = "aps1-az2"
    }
  }

  override_resource {
    target = aws_bedrockagentcore_agent_runtime.voice[0]
    values = {
      agent_runtime_arn     = "arn:aws:bedrock-agentcore:ap-south-1:928282274753:runtime/billeif_test_voice"
      agent_runtime_id      = "billeif-test-voice-runtime"
      agent_runtime_version = "1"
    }
  }

  override_resource {
    target = aws_bedrockagentcore_agent_runtime_endpoint.voice_staging[0]
    values = {
      agent_runtime_endpoint_arn = "arn:aws:bedrock-agentcore:ap-south-1:928282274753:runtime/billeif_test_voice/runtime-endpoint/STAGING"
      agent_runtime_version      = "2"
      name                       = "STAGING"
    }
  }

  override_resource {
    target = aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0]
    values = {
      agent_runtime_endpoint_arn = "arn:aws:bedrock-agentcore:ap-south-1:928282274753:runtime/billeif_test_voice/runtime-endpoint/PROD"
      agent_runtime_version      = "2"
      name                       = "PROD"
    }
  }

  override_resource {
    target = aws_apigatewayv2_api.http
    values = {
      id = "billeif-test-http-api"
    }
  }

  override_resource {
    target = aws_secretsmanager_secret.sarvam
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:billeif-test-sarvam"
    }
  }

  override_resource {
    target = aws_security_group.voice_agentcore[0]
    values = {
      id = "sg-voice-agentcore"
    }
  }

  override_resource {
    target = aws_subnet.private[0]
    values = {
      id = "subnet-private-a"
    }
  }

  override_resource {
    target = aws_subnet.private[1]
    values = {
      id = "subnet-private-b"
    }
  }

  override_resource {
    target = aws_kms_key.application_secrets
    values = {
      arn    = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
      key_id = "application-secrets"
    }
  }

  override_resource {
    target = aws_dynamodb_table.voice_sessions
    values = {
      arn  = "arn:aws:dynamodb:ap-south-1:928282274753:table/billeif-test-voice-sessions"
      name = "billeif-test-voice-sessions"
    }
  }

  override_resource {
    target = aws_lambda_function.voice_reconciler[0]
    values = {
      arn           = "arn:aws:lambda:ap-south-1:928282274753:function:billeif-test-voice-reconciler"
      function_name = "billeif-test-voice-reconciler"
    }
  }

  override_resource {
    target = aws_iam_role.voice_reconciler_scheduler[0]
    values = {
      arn  = "arn:aws:iam::928282274753:role/billeif-test-voice-reconciler-scheduler-role"
      id   = "billeif-test-voice-reconciler-scheduler-role"
      name = "billeif-test-voice-reconciler-scheduler-role"
    }
  }

  override_resource {
    target = aws_scheduler_schedule.voice_reconciler[0]
    values = {
      arn = "arn:aws:scheduler:ap-south-1:928282274753:schedule/default/billeif-test-voice-reconciler-minute"
    }
  }

  override_resource {
    target = aws_db_instance.main
    values = {
      address = "database.internal"
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret/rds-managed"
      }]
    }
  }

  override_resource {
    target = aws_instance.nat[0]
    values = {
      id                           = "i-billeifnat"
      primary_network_interface_id = "eni-billeifnat"
    }
  }
}

mock_provider "aws" {
  alias           = "ap_south_1"
  override_during = plan

  override_resource {
    target = aws_cognito_user_pool.phone
    values = {
      id = "ap-south-1_voicephone"
    }
  }

  override_resource {
    target = aws_cognito_user_pool_client.phone
    values = {
      id = "voice-phone-client"
    }
  }
}

mock_provider "aws" {
  alias           = "us_east_1"
  override_during = plan
}

mock_provider "awscc" {
  override_during = plan

  mock_resource "awscc_kinesisvideo_signaling_channel" {
    defaults = {
      arn  = "arn:aws:kinesisvideo:ap-south-1:928282274753:channel/mock/0"
      type = "SINGLE_MASTER"
    }
  }
}

mock_provider "external" {
  override_during = plan

  override_data {
    target = data.external.voice_agentcore_version[0]
    values = {
      result = {
        agent_runtime_version = "2"
      }
    }
  }
}

variables {
  project_name                          = "billeif-test"
  environment                           = "test"
  lambda_artifact_dir                   = "tests/fixtures/lambda"
  migration_lambda_artifact_path        = "tests/fixtures/lambda/http.zip"
  voice_reconciler_lambda_artifact_path = "tests/fixtures/lambda/http.zip"
  llm_api_url                           = "https://llm.example.test/chat/completions"
  llm_model                             = "test-model"
  deepseek_base_url                     = "https://voice-llm.example.test/v1"
  deepseek_model                        = "voice-test-model"
  ses_verified_identity                 = "billeif.example"
  ses_sender_email                      = "notifications@billeif.example"
  db_allowed_cidr                       = "10.0.0.0/24"
}

run "voice_reconciler_creates_no_resources_when_voice_is_disabled" {
  command = plan

  assert {
    condition = (
      var.enable_voice == false &&
      var.provision_voice_infrastructure == false &&
      length(aws_lambda_function.voice_reconciler) == 0 &&
      length(aws_cloudwatch_log_group.lambda_voice_reconciler) == 0 &&
      length(aws_iam_role.voice_reconciler) == 0 &&
      length(aws_iam_role.voice_reconciler_scheduler) == 0 &&
      length(aws_scheduler_schedule.voice_reconciler) == 0
    )
    error_message = "Disabled voice must create no reconciler Lambda, IAM, logging, or scheduler resources."
  }
}

run "voice_reconciler_is_bounded_arm64_and_scheduled_each_minute" {
  command = plan

  variables {
    provision_voice_infrastructure = true
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release        = "release-0123456789abcdef"
  }

  assert {
    condition = (
      length(aws_lambda_function.voice_reconciler) == 1 &&
      aws_lambda_function.voice_reconciler[0].runtime == "provided.al2023" &&
      aws_lambda_function.voice_reconciler[0].architectures[0] == "arm64" &&
      aws_lambda_function.voice_reconciler[0].memory_size == 128 &&
      aws_lambda_function.voice_reconciler[0].timeout == 15 &&
      aws_lambda_function.voice_reconciler[0].reserved_concurrent_executions == 1 &&
      length(aws_lambda_function.voice_reconciler[0].vpc_config) == 0
    )
    error_message = "The voice reconciler must be a serial 128 MiB ARM64 Lambda with a short timeout and no VPC attachment."
  }

  assert {
    condition = (
      toset(keys(aws_lambda_function.voice_reconciler[0].environment[0].variables)) == toset([
        "AGENTCORE_RUNTIME_ARN",
        "AGENTCORE_RUNTIME_QUALIFIER",
        "ENVIRONMENT",
        "VOICE_RECONCILER_BATCH_SIZE",
        "VOICE_SESSION_LEASE_INDEX_NAME",
        "VOICE_SESSIONS_TABLE_NAME",
      ]) &&
      aws_lambda_function.voice_reconciler[0].environment[0].variables.AGENTCORE_RUNTIME_QUALIFIER == "STAGING" &&
      aws_lambda_function.voice_reconciler[0].environment[0].variables.ENVIRONMENT == "test" &&
      aws_lambda_function.voice_reconciler[0].environment[0].variables.VOICE_RECONCILER_BATCH_SIZE == "10" &&
      aws_lambda_function.voice_reconciler[0].environment[0].variables.VOICE_SESSION_LEASE_INDEX_NAME == "gsi2"
    )
    error_message = "The voice reconciler environment must contain only bounded non-secret identifiers."
  }

  assert {
    condition = (
      length(aws_scheduler_schedule.voice_reconciler) == 1 &&
      aws_scheduler_schedule.voice_reconciler[0].schedule_expression == "rate(1 minute)" &&
      aws_scheduler_schedule.voice_reconciler[0].schedule_expression_timezone == "UTC" &&
      aws_scheduler_schedule.voice_reconciler[0].flexible_time_window[0].mode == "OFF" &&
      aws_scheduler_schedule.voice_reconciler[0].target[0].arn == aws_lambda_function.voice_reconciler[0].arn &&
      aws_scheduler_schedule.voice_reconciler[0].target[0].role_arn == aws_iam_role.voice_reconciler_scheduler[0].arn &&
      length(aws_scheduler_schedule.voice_reconciler[0].target[0].dead_letter_config) == 0 &&
      aws_scheduler_schedule.voice_reconciler[0].target[0].retry_policy[0].maximum_event_age_in_seconds == 300 &&
      aws_scheduler_schedule.voice_reconciler[0].target[0].retry_policy[0].maximum_retry_attempts == 3
    )
    error_message = "The reconciler must run each minute with bounded native retries and no unmeasured SQS queue."
  }
}

run "voice_reconciler_iam_is_dedicated_and_least_privilege" {
  command = plan

  variables {
    provision_voice_infrastructure = true
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release        = "release-0123456789abcdef"
  }

  assert {
    condition = (
      length([for statement in data.aws_iam_policy_document.voice_reconciler[0].statement : statement if statement.sid == "QueryExpiredVoiceLeases" && toset(statement.actions) == toset(["dynamodb:Query"]) && toset(statement.resources) == toset(["${aws_dynamodb_table.voice_sessions.arn}/index/gsi2"])]) == 1 &&
      length([for statement in data.aws_iam_policy_document.voice_reconciler[0].statement : statement if statement.sid == "ReconcileVoiceSessionState" && toset(statement.actions) == toset(["dynamodb:GetItem", "dynamodb:TransactWriteItems", "dynamodb:UpdateItem"]) && toset(statement.resources) == toset([aws_dynamodb_table.voice_sessions.arn])]) == 1 &&
      length([for statement in data.aws_iam_policy_document.voice_reconciler[0].statement : statement if statement.sid == "StopExpiredVoiceRuntime" && toset(statement.actions) == toset(["bedrock-agentcore:StopRuntimeSession"]) && toset(statement.resources) == toset([aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, aws_bedrockagentcore_agent_runtime_endpoint.voice_staging[0].agent_runtime_endpoint_arn])]) == 1 &&
      alltrue([for statement in data.aws_iam_policy_document.voice_reconciler[0].statement : !contains(statement.actions, "dynamodb:PutItem") && !contains(statement.actions, "secretsmanager:GetSecretValue")])
    )
    error_message = "The reconciler role must query only the lease index, conditionally update only the voice table, and stop only the owned runtime."
  }

  assert {
    condition = (
      length([for statement in data.aws_iam_policy_document.voice_reconciler_scheduler[0].statement : statement if statement.sid == "InvokeVoiceReconciler" && toset(statement.actions) == toset(["lambda:InvokeFunction"]) && toset(statement.resources) == toset([aws_lambda_function.voice_reconciler[0].arn])]) == 1 &&
      alltrue([for statement in data.aws_iam_policy_document.voice_reconciler_scheduler[0].statement : !contains(statement.actions, "sqs:SendMessage")])
    )
    error_message = "The scheduler role may only invoke the reconciler and must not add SQS permissions without measured need."
  }
}
