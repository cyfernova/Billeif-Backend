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
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:123456789012:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:rds-managed"
      }]
    }
  }

  override_resource {
    target          = aws_sqs_queue.invoice_processing
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:123456789012:billeif-test-test-invoice-processing-queue"
      url = "https://sqs.ap-south-1.amazonaws.com/123456789012/billeif-test-test-invoice-processing-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.email_delivery
    override_during = plan
    values = {
      arn  = "arn:aws:sqs:ap-south-1:123456789012:billeif-test-test-email-delivery-queue"
      url  = "https://sqs.ap-south-1.amazonaws.com/123456789012/billeif-test-test-email-delivery-queue"
      name = "billeif-test-test-email-delivery-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.email_delivery_dlq
    override_during = plan
    values = {
      arn  = "arn:aws:sqs:ap-south-1:123456789012:billeif-test-test-email-delivery-dlq"
      url  = "https://sqs.ap-south-1.amazonaws.com/123456789012/billeif-test-test-email-delivery-dlq"
      name = "billeif-test-test-email-delivery-dlq"
    }
  }

  override_resource {
    target          = aws_lambda_function.outbox_dispatcher
    override_during = plan
    values = {
      arn = "arn:aws:lambda:ap-south-1:123456789012:function:billeif-test-test-outbox-dispatcher"
    }
  }

  override_resource {
    target          = aws_iam_role.outbox_scheduler
    override_during = plan
    values = {
      arn = "arn:aws:iam::123456789012:role/billeif-test-test-outbox-scheduler-exec-role"
    }
  }

  override_resource {
    target          = aws_sqs_queue.outbox_dispatcher_scheduler_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:123456789012:billeif-test-test-outbox-dispatcher-scheduler-dlq"
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
  project_name                   = "billeif-test"
  environment                    = "test"
  lambda_artifact_dir            = "tests/fixtures/lambda"
  migration_lambda_artifact_path = "tests/fixtures/lambda/http.zip"
  llm_api_url                    = "https://llm.example.test/chat/completions"
  llm_model                      = "test-model"
  deepseek_base_url              = "https://voice-llm.example.test/v1"
  deepseek_model                 = "voice-test-model"
  ses_verified_identity          = "billeif.example"
  ses_sender_email               = "notifications@billeif.example"
  db_allowed_cidr                = "10.0.0.0/24"
}

run "outbox_dispatcher_is_private_serial_and_fail_closed" {
  command = plan

  assert {
    condition = (
      local.lambda_artifacts.outbox == "${var.lambda_artifact_dir}/outbox.zip" &&
      local.lambda_artifact_hashes.outbox != null &&
      aws_lambda_function.outbox_dispatcher.function_name == "${local.resource_prefix}-outbox-dispatcher" &&
      aws_lambda_function.outbox_dispatcher.runtime == "provided.al2023" &&
      aws_lambda_function.outbox_dispatcher.architectures[0] == "arm64" &&
      aws_lambda_function.outbox_dispatcher.memory_size == 256 &&
      aws_lambda_function.outbox_dispatcher.timeout == 45 &&
      aws_lambda_function.outbox_dispatcher.reserved_concurrent_executions == 0 &&
      toset(aws_lambda_function.outbox_dispatcher.vpc_config[0].subnet_ids) == toset(aws_subnet.private[*].id) &&
      toset(aws_lambda_function.outbox_dispatcher.vpc_config[0].security_group_ids) == toset([aws_security_group.lambda.id])
    )
    error_message = "The disabled Billeif outbox dispatcher must be packaged, private, ARM64, and hard-throttled."
  }

  assert {
    condition = (
      aws_cloudwatch_log_group.lambda_outbox_dispatcher.name == "/aws/lambda/${local.resource_prefix}-outbox-dispatcher" &&
      toset(keys(aws_lambda_function.outbox_dispatcher.environment[0].variables)) == toset([
        "ENVIRONMENT", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_HOST_SSM_PARAM", "DATABASE_SECRET_ARN",
        "DATABASE_PORT", "DATABASE_NAME", "DATABASE_SSL_MODE", "SQS_INVOICE_QUEUE", "SQS_EMAIL_DELIVERY_QUEUE"
      ]) &&
      aws_lambda_function.outbox_dispatcher.environment[0].variables.ENVIRONMENT == var.environment &&
      aws_lambda_function.outbox_dispatcher.environment[0].variables.SQS_INVOICE_QUEUE == aws_sqs_queue.invoice_processing.url &&
      aws_lambda_function.outbox_dispatcher.environment[0].variables.SQS_EMAIL_DELIVERY_QUEUE == aws_sqs_queue.email_delivery.url
    )
    error_message = "The outbox dispatcher environment must contain only logging and database or invoice-queue runtime settings."
  }

  assert {
    condition = (
      aws_scheduler_schedule.outbox_dispatcher.schedule_expression == "rate(1 minute)" &&
      aws_scheduler_schedule.outbox_dispatcher.state == "DISABLED" &&
      aws_scheduler_schedule.outbox_dispatcher.flexible_time_window[0].mode == "OFF" &&
      aws_scheduler_schedule.outbox_dispatcher.target[0].arn == aws_lambda_function.outbox_dispatcher.arn &&
      aws_scheduler_schedule.outbox_dispatcher.target[0].role_arn == aws_iam_role.outbox_scheduler.arn &&
      aws_scheduler_schedule.outbox_dispatcher.target[0].dead_letter_config[0].arn == aws_sqs_queue.outbox_dispatcher_scheduler_dlq.arn &&
      aws_scheduler_schedule.outbox_dispatcher.target[0].retry_policy[0].maximum_event_age_in_seconds == 300 &&
      aws_scheduler_schedule.outbox_dispatcher.target[0].retry_policy[0].maximum_retry_attempts == 3 &&
      aws_sqs_queue.outbox_dispatcher_scheduler_dlq.sqs_managed_sse_enabled &&
      aws_sqs_queue.outbox_dispatcher_scheduler_dlq.message_retention_seconds == 1209600
    )
    error_message = "The disabled dispatcher schedule must run every minute without a flexible window and retain bounded failures in an encrypted Billeif DLQ."
  }
}

run "outbox_dispatcher_iam_is_dedicated_and_least_privilege" {
  command = plan

  assert {
    condition = (
      aws_iam_role.outbox_dispatcher.name == "${local.resource_prefix}-outbox-dispatcher-exec-role" &&
      aws_iam_role_policy_attachment.outbox_dispatcher_basic.policy_arn == "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole" &&
      aws_iam_role_policy_attachment.outbox_dispatcher_vpc_access.policy_arn == "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole" &&
      length([for statement in data.aws_iam_policy_document.outbox_dispatcher.statement : statement if statement.sid == "OutboxQueues" && length(statement.actions) == 1 && contains(statement.actions, "sqs:SendMessage") && length(statement.resources) == 2 && contains(statement.resources, aws_sqs_queue.invoice_processing.arn) && contains(statement.resources, aws_sqs_queue.email_delivery.arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.outbox_dispatcher.statement : statement if statement.sid == "OutboxParameters" && length(statement.actions) == 1 && contains(statement.actions, "ssm:GetParameters") && length(statement.resources) == 1 && contains(statement.resources, local.db_host_ssm_parameter_arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.outbox_dispatcher.statement : statement if statement.sid == "OutboxSecret" && length(statement.actions) == 2 && contains(statement.actions, "secretsmanager:DescribeSecret") && contains(statement.actions, "secretsmanager:GetSecretValue") && length(statement.resources) == 1 && contains(statement.resources, aws_db_instance.main.master_user_secret[0].secret_arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.outbox_dispatcher.statement : statement if statement.sid == "OutboxSecretKMS" && length(statement.actions) == 2 && contains(statement.actions, "kms:Decrypt") && contains(statement.actions, "kms:DescribeKey") && length(statement.resources) == 1 && contains(statement.resources, aws_kms_key.application_secrets.arn)]) == 1
    )
    error_message = "The dispatcher role must be distinct and limited to database runtime access plus SendMessage to the invoice queue."
  }

  assert {
    condition = (
      aws_iam_role.outbox_scheduler.name == "${local.resource_prefix}-outbox-scheduler-exec-role" &&
      length([for statement in data.aws_iam_policy_document.outbox_scheduler_assume_role.statement : statement if anytrue([for principal in statement.principals : contains(principal.identifiers, "scheduler.amazonaws.com")])]) == 1 &&
      length([for statement in data.aws_iam_policy_document.outbox_scheduler.statement : statement if statement.sid == "InvokeOutboxDispatcher" && length(statement.actions) == 1 && contains(statement.actions, "lambda:InvokeFunction") && length(statement.resources) == 1 && contains(statement.resources, aws_lambda_function.outbox_dispatcher.arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.outbox_scheduler.statement : statement if statement.sid == "SendOutboxDispatcherFailures" && length(statement.actions) == 1 && contains(statement.actions, "sqs:SendMessage") && length(statement.resources) == 1 && contains(statement.resources, aws_sqs_queue.outbox_dispatcher_scheduler_dlq.arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.outbox_scheduler_dlq.statement : statement if length(statement.actions) == 1 && contains(statement.actions, "sqs:SendMessage") && anytrue([for principal in statement.principals : contains(principal.identifiers, "scheduler.amazonaws.com")]) && length(statement.resources) == 1 && contains(statement.resources, aws_sqs_queue.outbox_dispatcher_scheduler_dlq.arn)]) == 1
    )
    error_message = "The scheduler may invoke only the outbox Lambda and write failures only to its Billeif DLQ."
  }
}

run "email_delivery_worker_is_cost_capped_and_least_privilege" {
  command = plan

  assert {
    condition = (
      aws_sqs_queue.email_delivery.name == "${local.resource_prefix}-email-delivery-queue" &&
      aws_sqs_queue.email_delivery.message_retention_seconds == 345600 &&
      aws_sqs_queue.email_delivery.visibility_timeout_seconds == 365 &&
      aws_sqs_queue.email_delivery.sqs_managed_sse_enabled &&
      aws_sqs_queue.email_delivery_dlq.message_retention_seconds == 1209600 &&
      aws_sqs_queue.email_delivery_dlq.sqs_managed_sse_enabled &&
      jsondecode(aws_sqs_queue_redrive_policy.email_delivery.redrive_policy).maxReceiveCount == 5
    )
    error_message = "The Billeif email delivery queue must be encrypted, bounded, and use its dedicated 14-day DLQ."
  }

  assert {
    condition = (
      aws_lambda_function.sqs_email_delivery.function_name == "${local.resource_prefix}-sqs-email-delivery" &&
      aws_lambda_function.sqs_email_delivery.runtime == "provided.al2023" &&
      aws_lambda_function.sqs_email_delivery.architectures[0] == "arm64" &&
      aws_lambda_function.sqs_email_delivery.memory_size == 512 &&
      aws_lambda_function.sqs_email_delivery.timeout == 60 &&
      aws_lambda_function.sqs_email_delivery.reserved_concurrent_executions == 0 &&
      length(aws_lambda_event_source_mapping.email_delivery_queue) == 0
    )
    error_message = "The disabled Billeif email delivery worker must be hard-throttled and the enabled mapping capped at two single-record batches."
  }

  assert {
    condition = (
      length([for statement in data.aws_iam_policy_document.email_delivery.statement : statement if statement.sid == "EmailDeliveryFinalPDF" && length(statement.actions) == 1 && contains(statement.actions, "s3:GetObject") && length(statement.resources) == 1]) == 1 &&
      length([for statement in data.aws_iam_policy_document.email_delivery.statement : statement if statement.sid == "EmailDeliverySES" && length(statement.actions) == 1 && contains(statement.actions, "ses:SendRawEmail") && length(statement.resources) == 2]) == 1 &&
      length([for statement in data.aws_iam_policy_document.email_delivery.statement : statement if statement.sid == "EmailDeliveryQueue" && !contains(statement.actions, "sqs:SendMessage") && length(statement.resources) == 1 && contains(statement.resources, aws_sqs_queue.email_delivery.arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.lambda_app.statement : statement if statement.sid == "EmailDeliveryQueueSend" && length(statement.actions) == 1 && contains(statement.actions, "sqs:SendMessage") && length(statement.resources) == 1 && contains(statement.resources, aws_sqs_queue.email_delivery.arn)]) == 1 &&
      toset(aws_ses_event_destination.to_sns.matching_types) == toset(["bounce", "complaint", "delivery"])
    )
    error_message = "The email delivery role must only read final PDFs, receive its queue, and call SES SendRawEmail."
  }
}

run "outbox_dispatcher_schedule_enables_only_with_application" {
  command = plan

  variables {
    enable_application = true
  }

  assert {
    condition = (
      aws_lambda_function.outbox_dispatcher.reserved_concurrent_executions == 1 &&
      aws_scheduler_schedule.outbox_dispatcher.state == "ENABLED"
    )
    error_message = "Reviewed application enablement must release exactly one dispatcher execution and enable its schedule."
  }
}

run "email_delivery_enablement_uses_mapping_cap_without_low_reserved_concurrency" {
  command = plan

  variables {
    enable_application = true
  }

  assert {
    condition = (
      aws_lambda_function.sqs_email_delivery.reserved_concurrent_executions == null &&
      aws_lambda_event_source_mapping.email_delivery_queue[0].batch_size == 1 &&
      aws_lambda_event_source_mapping.email_delivery_queue[0].maximum_batching_window_in_seconds == 0 &&
      aws_lambda_event_source_mapping.email_delivery_queue[0].scaling_config[0].maximum_concurrency == 2
    )
    error_message = "Enabled Billeif email delivery must rely on the two-concurrency event-source cap without configuring an unsafe reserved concurrency below five."
  }
}
