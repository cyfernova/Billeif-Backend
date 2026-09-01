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
      result = "{\"status\":\"applied\",\"version\":55,\"latest_version\":55,\"dirty\":false,\"manifest_checksum\":\"6f0a41a9dd9a52992bdffdf283ca96b7138f13d35e5066b7938803ceafdac5e6\"}"
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
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:rds-managed"
      }]
    }
  }

  override_resource {
    target          = aws_sqs_queue.invoice_processing
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-invoice-processing-queue"
      url = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-invoice-processing-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.invoice_processing_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-invoice-processing-dlq"
      id  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-invoice-processing-dlq"
    }
  }

  override_resource {
    target          = aws_sqs_queue.gst_processing
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-gst-processing-queue"
      id  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-gst-processing-queue"
      url = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-gst-processing-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.gst_processing_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-gst-processing-dlq"
      id  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-gst-processing-dlq"
    }
  }

  override_resource {
    target          = aws_sqs_queue.bargaining_negotiation
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-bargaining-negotiation-queue"
      id  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-bargaining-negotiation-queue"
      url = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-bargaining-negotiation-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.bargaining_negotiation_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-bargaining-negotiation-dlq"
      id  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-bargaining-negotiation-dlq"
    }
  }

  override_resource {
    target          = aws_sns_topic.alerts
    override_during = plan
    values = {
      arn = "arn:aws:sns:ap-south-1:928282274753:billeif-test-test-alerts"
    }
  }

  override_resource {
    target          = aws_sns_topic.low_stock_alerts
    override_during = plan
    values = {
      arn = "arn:aws:sns:ap-south-1:928282274753:billeif-test-test-low-stock-alerts"
    }
  }

  override_resource {
    target          = aws_sns_topic.ses_events
    override_during = plan
    values = {
      arn = "arn:aws:sns:ap-south-1:928282274753:billeif-test-test-ses-email-events"
    }
  }

  override_resource {
    target          = aws_sqs_queue.email_delivery
    override_during = plan
    values = {
      arn  = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-email-delivery-queue"
      url  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-email-delivery-queue"
      name = "billeif-test-test-email-delivery-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.email_delivery_dlq
    override_during = plan
    values = {
      arn  = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-email-delivery-dlq"
      url  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-email-delivery-dlq"
      name = "billeif-test-test-email-delivery-dlq"
    }
  }

  override_resource {
    target          = aws_sqs_queue.ses_feedback
    override_during = plan
    values = {
      arn  = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-ses-feedback-queue"
      url  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-ses-feedback-queue"
      name = "billeif-test-test-ses-feedback-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.ses_feedback_dlq
    override_during = plan
    values = {
      arn  = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-ses-feedback-dlq"
      url  = "https://sqs.ap-south-1.amazonaws.com/928282274753/billeif-test-test-ses-feedback-dlq"
      name = "billeif-test-test-ses-feedback-dlq"
    }
  }

  override_resource {
    target          = aws_lambda_function.outbox_dispatcher
    override_during = plan
    values = {
      arn = "arn:aws:lambda:ap-south-1:928282274753:function:billeif-test-test-outbox-dispatcher"
    }
  }

  override_resource {
    target          = aws_iam_role.outbox_scheduler
    override_during = plan
    values = {
      arn = "arn:aws:iam::928282274753:role/billeif-test-test-outbox-scheduler-exec-role"
    }
  }

  override_resource {
    target          = aws_sqs_queue.outbox_dispatcher_scheduler_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-outbox-dispatcher-scheduler-dlq"
    }
  }

  override_resource {
    target          = aws_lambda_function.recurring_invoices
    override_during = plan
    values = {
      arn = "arn:aws:lambda:ap-south-1:928282274753:function:billeif-test-test-recurring-invoices"
    }
  }

  override_resource {
    target          = aws_iam_role.recurring_invoices_scheduler
    override_during = plan
    values = {
      arn = "arn:aws:iam::928282274753:role/billeif-test-test-recurring-invoices-scheduler-role"
    }
  }

  override_resource {
    target          = aws_sqs_queue.recurring_invoices_scheduler_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-recurring-invoices-scheduler-dlq"
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

  assert {
    condition = (
      local.lambda_artifacts.recurring_invoices == "${var.lambda_artifact_dir}/recurring-invoices.zip" &&
      local.lambda_artifact_hashes.recurring_invoices != null &&
      aws_lambda_function.recurring_invoices.function_name == "${local.resource_prefix}-recurring-invoices" &&
      aws_lambda_function.recurring_invoices.runtime == "provided.al2023" &&
      aws_lambda_function.recurring_invoices.architectures[0] == "arm64" &&
      aws_lambda_function.recurring_invoices.memory_size == 256 &&
      aws_lambda_function.recurring_invoices.timeout == 60 &&
      aws_lambda_function.recurring_invoices.reserved_concurrent_executions == 0 &&
      toset(aws_lambda_function.recurring_invoices.vpc_config[0].subnet_ids) == toset(aws_subnet.private[*].id) &&
      toset(aws_lambda_function.recurring_invoices.vpc_config[0].security_group_ids) == toset([aws_security_group.lambda.id]) &&
      toset(keys(aws_lambda_function.recurring_invoices.environment[0].variables)) == toset([
        "ENVIRONMENT", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_HOST_SSM_PARAM", "DATABASE_SECRET_ARN",
        "DATABASE_PORT", "DATABASE_NAME", "DATABASE_SSL_MODE"
      ])
    )
    error_message = "The disabled recurring invoice generator must be a private, ARM64, database-only Lambda with one bounded execution."
  }

  assert {
    condition = (
      aws_scheduler_schedule.recurring_invoices.schedule_expression == "rate(1 minute)" &&
      aws_scheduler_schedule.recurring_invoices.state == "DISABLED" &&
      aws_scheduler_schedule.recurring_invoices.flexible_time_window[0].mode == "OFF" &&
      aws_scheduler_schedule.recurring_invoices.target[0].arn == aws_lambda_function.recurring_invoices.arn &&
      aws_scheduler_schedule.recurring_invoices.target[0].role_arn == aws_iam_role.recurring_invoices_scheduler.arn &&
      aws_scheduler_schedule.recurring_invoices.target[0].dead_letter_config[0].arn == aws_sqs_queue.recurring_invoices_scheduler_dlq.arn &&
      aws_scheduler_schedule.recurring_invoices.target[0].retry_policy[0].maximum_event_age_in_seconds == 300 &&
      aws_scheduler_schedule.recurring_invoices.target[0].retry_policy[0].maximum_retry_attempts == 3 &&
      aws_sqs_queue.recurring_invoices_scheduler_dlq.sqs_managed_sse_enabled &&
      aws_sqs_queue.recurring_invoices_scheduler_dlq.message_retention_seconds == 1209600
    )
    error_message = "The recurring invoice schedule must be gated, retry-bounded, and retain failures in an encrypted 14-day DLQ."
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

  assert {
    condition = (
      aws_iam_role.recurring_invoices.name == "${local.resource_prefix}-recurring-invoices-exec-role" &&
      aws_iam_role_policy_attachment.recurring_invoices_basic.policy_arn == "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole" &&
      aws_iam_role_policy_attachment.recurring_invoices_vpc_access.policy_arn == "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole" &&
      length([for statement in data.aws_iam_policy_document.recurring_invoices.statement : statement if statement.sid == "RecurringInvoiceParameters" && length(statement.actions) == 1 && contains(statement.actions, "ssm:GetParameters") && length(statement.resources) == 1 && contains(statement.resources, local.db_host_ssm_parameter_arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.recurring_invoices.statement : statement if statement.sid == "RecurringInvoiceSecret" && length(statement.actions) == 2 && contains(statement.actions, "secretsmanager:DescribeSecret") && contains(statement.actions, "secretsmanager:GetSecretValue") && length(statement.resources) == 1 && contains(statement.resources, aws_db_instance.main.master_user_secret[0].secret_arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.recurring_invoices.statement : statement if statement.sid == "RecurringInvoiceSecretKMS" && length(statement.actions) == 2 && contains(statement.actions, "kms:Decrypt") && contains(statement.actions, "kms:DescribeKey") && length(statement.resources) == 1 && contains(statement.resources, aws_kms_key.application_secrets.arn)]) == 1 &&
      aws_iam_role.recurring_invoices_scheduler.name == "${local.resource_prefix}-recurring-invoices-scheduler-role" &&
      length([for statement in data.aws_iam_policy_document.recurring_invoices_scheduler.statement : statement if statement.sid == "InvokeRecurringInvoices" && length(statement.actions) == 1 && contains(statement.actions, "lambda:InvokeFunction") && length(statement.resources) == 1 && contains(statement.resources, aws_lambda_function.recurring_invoices.arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.recurring_invoices_scheduler.statement : statement if statement.sid == "SendRecurringInvoiceFailures" && length(statement.actions) == 1 && contains(statement.actions, "sqs:SendMessage") && length(statement.resources) == 1 && contains(statement.resources, aws_sqs_queue.recurring_invoices_scheduler_dlq.arn)]) == 1
    )
    error_message = "Recurring invoice execution and scheduling must use distinct roles scoped to the database, one Lambda, and one failure queue."
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
      length([for statement in data.aws_iam_policy_document.lambda_app.statement : statement if statement.sid == "SESAndSNS" && contains(statement.actions, "sns:Publish") && contains(statement.resources, aws_sns_topic.alerts.arn) && contains(statement.resources, aws_sns_topic.low_stock_alerts.arn) && !contains(statement.resources, aws_sns_topic.ses_events.arn)]) == 1 &&
      length([for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement if statement.sid == "HTTPQueueSend" && toset(statement.actions) == toset(["sqs:SendMessage"]) && toset(statement.resources) == toset([aws_sqs_queue.invoice_processing.arn, aws_sqs_queue.gst_processing.arn, aws_sqs_queue.bargaining_negotiation.arn, aws_sqs_queue.email_delivery.arn])]) == 1 &&
      length([for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement if statement.sid == "HTTPEmailSend" && toset(statement.actions) == toset(["ses:SendEmail"]) && toset(statement.resources) == toset([local.ses_verified_identity_arn])]) == 1 &&
      alltrue([for action in flatten([for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement.actions]) : !contains(["ses:SendRawEmail", "sns:Publish", "sqs:ChangeMessageVisibility", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ReceiveMessage"], action)]) &&
      toset(aws_ses_event_destination.to_sns.matching_types) == toset(["bounce", "complaint", "delivery"])
    )
    error_message = "The email delivery role must only read final PDFs, receive its queue, and call SES SendRawEmail."
  }
}

run "outbox_dispatcher_schedule_enables_only_with_application" {
  command = plan

  variables {
    enable_application                 = true
    enable_lambda_reserved_concurrency = true
    alert_email                        = "alerts@example.com"
    alert_email_subscription_confirmed = true
  }

  assert {
    condition = (
      aws_lambda_function.outbox_dispatcher.reserved_concurrent_executions == 1 &&
      aws_scheduler_schedule.outbox_dispatcher.state == "ENABLED" &&
      aws_lambda_function.recurring_invoices.reserved_concurrent_executions == 1 &&
      aws_scheduler_schedule.recurring_invoices.state == "ENABLED"
    )
    error_message = "Reviewed application enablement must release exactly one dispatcher execution and enable its schedule."
  }
}

run "low_quota_activation_keeps_request_paths_unreserved_and_background_off" {
  command = plan

  variables {
    enable_application                 = true
    enable_background_processing       = false
    enable_lambda_reserved_concurrency = false
    alert_email                        = "alerts@example.com"
    alert_email_subscription_confirmed = true
  }

  assert {
    condition = (
      aws_lambda_function.api_http.reserved_concurrent_executions == null &&
      aws_lambda_function.a2a_stream.reserved_concurrent_executions == null &&
      aws_lambda_function.ws_handler.reserved_concurrent_executions == null &&
      aws_lambda_function.custom_sms_sender.reserved_concurrent_executions == null &&
      aws_lambda_function.database_migrator.reserved_concurrent_executions == null &&
      aws_lambda_function.outbox_dispatcher.reserved_concurrent_executions == 0 &&
      aws_lambda_function.recurring_invoices.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_invoice.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_gst.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_bargaining.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_email_delivery.reserved_concurrent_executions == 0 &&
      aws_lambda_function.sqs_ses_feedback.reserved_concurrent_executions == 0
    )
    error_message = "Low-quota activation must leave request handlers and migrations in the shared pool while hard-throttling voice and background workers."
  }

  assert {
    condition = (
      length(aws_lambda_event_source_mapping.invoice_queue) == 0 &&
      length(aws_lambda_event_source_mapping.gst_queue) == 0 &&
      length(aws_lambda_event_source_mapping.bargaining_queue) == 0 &&
      length(aws_lambda_event_source_mapping.email_delivery_queue) == 0 &&
      length(aws_lambda_event_source_mapping.ses_feedback_queue) == 0 &&
      aws_scheduler_schedule.outbox_dispatcher.state == "DISABLED" &&
      aws_scheduler_schedule.recurring_invoices.state == "DISABLED" &&
      aws_cloudwatch_metric_alarm.outbox_oldest_pending_age.treat_missing_data == "notBreaching"
    )
    error_message = "Low-quota activation must keep every background trigger disabled and avoid alarming on missing outbox telemetry."
  }

  assert {
    condition = (
      aws_apigatewayv2_stage.http.default_route_settings[0].throttling_rate_limit == 10 &&
      aws_apigatewayv2_stage.http.default_route_settings[0].throttling_burst_limit == 20 &&
      aws_api_gateway_method_settings.main.settings[0].throttling_rate_limit == 10 &&
      aws_api_gateway_method_settings.main.settings[0].throttling_burst_limit == 20 &&
      aws_apigatewayv2_stage.websocket_default.default_route_settings[0].throttling_rate_limit == 10 &&
      aws_apigatewayv2_stage.websocket_default.default_route_settings[0].throttling_burst_limit == 20
    )
    error_message = "Low-quota activation must cap HTTP, REST, and WebSocket routes at ten requests per second with a twenty-request startup burst."
  }
}

run "email_delivery_enablement_uses_mapping_cap_without_low_reserved_concurrency" {
  command = plan

  variables {
    enable_application                 = true
    alert_email                        = "alerts@example.com"
    alert_email_subscription_confirmed = true
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

run "ses_feedback_is_raw_cost_capped_and_least_privilege" {
  command = plan

  assert {
    condition = (
      aws_sqs_queue.ses_feedback.name == "${local.resource_prefix}-ses-feedback-queue" &&
      aws_sqs_queue.ses_feedback.message_retention_seconds == 345600 &&
      aws_sqs_queue.ses_feedback.visibility_timeout_seconds == 180 &&
      aws_sqs_queue.ses_feedback.sqs_managed_sse_enabled &&
      aws_sqs_queue.ses_feedback_dlq.name == "${local.resource_prefix}-ses-feedback-dlq" &&
      aws_sqs_queue.ses_feedback_dlq.message_retention_seconds == 1209600 &&
      aws_sqs_queue.ses_feedback_dlq.sqs_managed_sse_enabled &&
      jsondecode(aws_sqs_queue_redrive_policy.ses_feedback.redrive_policy).maxReceiveCount == 5
    )
    error_message = "The Billeif SES feedback queue must be encrypted, retained for four days, use a 180-second visibility timeout, and redrive after five receives to its 14-day DLQ."
  }

  assert {
    condition = (
      aws_sns_topic_subscription.ses_feedback.topic_arn == aws_sns_topic.ses_events.arn &&
      aws_sns_topic_subscription.ses_feedback.endpoint == aws_sqs_queue.ses_feedback.arn &&
      aws_sns_topic_subscription.ses_feedback.protocol == "sqs" &&
      aws_sns_topic_subscription.ses_feedback.raw_message_delivery &&
      jsondecode(aws_sns_topic_subscription.ses_feedback.redrive_policy).deadLetterTargetArn == aws_sqs_queue.ses_feedback_dlq.arn &&
      length([for statement in data.aws_iam_policy_document.ses_feedback_queue.statement : statement if statement.sid == "AllowSESEventTopic" && length(statement.actions) == 1 && contains(statement.actions, "sqs:SendMessage") && length(statement.resources) == 1 && contains(statement.resources, aws_sqs_queue.ses_feedback.arn) && anytrue([for principal in statement.principals : contains(principal.identifiers, "sns.amazonaws.com")])]) == 1 &&
      length([for statement in data.aws_iam_policy_document.ses_feedback_dlq.statement : statement if statement.sid == "AllowSESEventTopicRedrive" && length(statement.actions) == 1 && contains(statement.actions, "sqs:SendMessage") && length(statement.resources) == 1 && contains(statement.resources, aws_sqs_queue.ses_feedback_dlq.arn)]) == 1
    )
    error_message = "SES feedback must use raw SNS delivery with an exact-topic/account queue policy and a subscription DLQ."
  }

  assert {
    condition = (
      local.lambda_artifacts.sqs_ses_feedback == "${var.lambda_artifact_dir}/sqs-ses-feedback.zip" &&
      local.lambda_artifact_hashes.sqs_ses_feedback != null &&
      aws_lambda_function.sqs_ses_feedback.function_name == "${local.resource_prefix}-sqs-ses-feedback" &&
      aws_lambda_function.sqs_ses_feedback.runtime == "provided.al2023" &&
      aws_lambda_function.sqs_ses_feedback.architectures[0] == "arm64" &&
      aws_lambda_function.sqs_ses_feedback.memory_size == 256 &&
      aws_lambda_function.sqs_ses_feedback.timeout == 30 &&
      aws_lambda_function.sqs_ses_feedback.reserved_concurrent_executions == 0 &&
      length(aws_lambda_event_source_mapping.ses_feedback_queue) == 0 &&
      toset(keys(aws_lambda_function.sqs_ses_feedback.environment[0].variables)) == toset([
        "ENVIRONMENT", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_HOST_SSM_PARAM", "DATABASE_SECRET_ARN",
        "DATABASE_PORT", "DATABASE_NAME", "DATABASE_SSL_MODE", "SES_SENDING_ACCOUNT_ID", "SES_CONFIGURATION_SET"
      ])
    )
    error_message = "The disabled Billeif SES feedback Lambda must be minimal, private, ARM64, and hard-throttled."
  }

  assert {
    condition = (
      length([for statement in data.aws_iam_policy_document.ses_feedback.statement : statement if statement.sid == "SESFeedbackQueue" && !contains(statement.actions, "sqs:SendMessage") && length(statement.resources) == 1 && contains(statement.resources, aws_sqs_queue.ses_feedback.arn)]) == 1 &&
      length(flatten([for statement in data.aws_iam_policy_document.ses_feedback.statement : statement.actions])) == 9 &&
      alltrue([for action in flatten([for statement in data.aws_iam_policy_document.ses_feedback.statement : statement.actions]) : !startswith(action, "ses:") && !startswith(action, "sns:") && !startswith(action, "s3:")])
    )
    error_message = "The SES feedback role may only resolve the database and consume its own queue."
  }

  assert {
    condition = alltrue([
      for alarm in [
        aws_cloudwatch_metric_alarm.lambda_ses_feedback_errors,
        aws_cloudwatch_metric_alarm.lambda_ses_feedback_throttles,
        aws_cloudwatch_metric_alarm.lambda_ses_feedback_duration,
        aws_cloudwatch_metric_alarm.worker_queue_age["ses_feedback"],
        aws_cloudwatch_metric_alarm.worker_dlq_messages["ses_feedback"],
      ] :
      alarm.evaluation_periods == 3 &&
      alarm.datapoints_to_alarm == 2 &&
      alarm.treat_missing_data == "notBreaching"
    ])
    error_message = "Every Billeif SES feedback alarm must use 2-of-3 evaluation and treat missing data as non-breaching."
  }
}

run "ses_feedback_enablement_uses_zero_window_mapping_cap" {
  command = plan

  variables {
    enable_application                 = true
    alert_email                        = "alerts@example.com"
    alert_email_subscription_confirmed = true
  }

  assert {
    condition = (
      aws_lambda_function.sqs_ses_feedback.reserved_concurrent_executions == null &&
      aws_lambda_event_source_mapping.ses_feedback_queue[0].batch_size == 10 &&
      aws_lambda_event_source_mapping.ses_feedback_queue[0].maximum_batching_window_in_seconds == 0 &&
      contains(aws_lambda_event_source_mapping.ses_feedback_queue[0].function_response_types, "ReportBatchItemFailures") &&
      aws_lambda_event_source_mapping.ses_feedback_queue[0].scaling_config[0].maximum_concurrency == 2
    )
    error_message = "Enabled Billeif SES feedback must use zero batching delay, partial-batch responses, and a two-concurrency event-source cap."
  }
}

run "active_worker_queues_are_retained_redriven_and_cost_capped" {
  command = plan

  variables {
    enable_application                 = true
    alert_email                        = "alerts@example.com"
    alert_email_subscription_confirmed = true
  }

  assert {
    condition = (
      aws_sqs_queue.invoice_processing.message_retention_seconds == 604800 &&
      aws_sqs_queue.gst_processing.message_retention_seconds == 345600 &&
      aws_sqs_queue.bargaining_negotiation.message_retention_seconds == 345600 &&
      aws_sqs_queue.bargaining_negotiation.visibility_timeout_seconds == 365 &&
      aws_sqs_queue.invoice_processing_dlq.message_retention_seconds == 1209600 &&
      aws_sqs_queue.gst_processing_dlq.message_retention_seconds == 1209600 &&
      aws_sqs_queue.bargaining_negotiation_dlq.message_retention_seconds == 1209600 &&
      jsondecode(aws_sqs_queue_redrive_policy.invoice_processing.redrive_policy).maxReceiveCount == 5 &&
      jsondecode(aws_sqs_queue_redrive_policy.gst_processing.redrive_policy).maxReceiveCount == 5 &&
      jsondecode(aws_sqs_queue_redrive_policy.bargaining_negotiation.redrive_policy).maxReceiveCount == 5
    )
    error_message = "Invoice work must retain seven days; other jobs four days; all worker DLQs must retain fourteen days and redrive after five receives."
  }

  assert {
    condition = (
      aws_lambda_function.sqs_bargaining.timeout == 60 &&
      aws_lambda_event_source_mapping.invoice_queue[0].scaling_config[0].maximum_concurrency == 2 &&
      aws_lambda_event_source_mapping.gst_queue[0].scaling_config[0].maximum_concurrency == 2 &&
      aws_lambda_event_source_mapping.bargaining_queue[0].scaling_config[0].maximum_concurrency == 2 &&
      aws_lambda_event_source_mapping.bargaining_queue[0].batch_size == 1 &&
      aws_lambda_event_source_mapping.bargaining_queue[0].maximum_batching_window_in_seconds == 0 &&
      contains(aws_lambda_event_source_mapping.invoice_queue[0].function_response_types, "ReportBatchItemFailures") &&
      contains(aws_lambda_event_source_mapping.gst_queue[0].function_response_types, "ReportBatchItemFailures") &&
      contains(aws_lambda_event_source_mapping.bargaining_queue[0].function_response_types, "ReportBatchItemFailures")
    )
    error_message = "Every active queue must have its own partial-batch worker capped at two concurrent invocations; bargaining processes one round in a 60-second invocation."
  }
}

run "monthly_budget_warns_at_actual_and_forecast_thresholds" {
  command = plan

  variables {
    alert_email = "alerts@example.com"
  }

  assert {
    condition = (
      aws_budgets_budget.monthly_cost.name == "${local.resource_prefix}-monthly-cost" &&
      aws_budgets_budget.monthly_cost.budget_type == "COST" &&
      aws_budgets_budget.monthly_cost.limit_amount == "125" &&
      aws_budgets_budget.monthly_cost.limit_unit == "USD" &&
      aws_budgets_budget.monthly_cost.time_unit == "MONTHLY" &&
      length(aws_budgets_budget.monthly_cost.notification) == 4 &&
      contains([for notification in aws_budgets_budget.monthly_cost.notification : "${notification.notification_type}:${notification.threshold}"], "ACTUAL:50") &&
      contains([for notification in aws_budgets_budget.monthly_cost.notification : "${notification.notification_type}:${notification.threshold}"], "ACTUAL:80") &&
      contains([for notification in aws_budgets_budget.monthly_cost.notification : "${notification.notification_type}:${notification.threshold}"], "ACTUAL:100") &&
      contains([for notification in aws_budgets_budget.monthly_cost.notification : "${notification.notification_type}:${notification.threshold}"], "FORECASTED:100") &&
      alltrue([for notification in aws_budgets_budget.monthly_cost.notification : notification.comparison_operator == "GREATER_THAN" && notification.threshold_type == "PERCENTAGE" && contains(notification.subscriber_email_addresses, var.alert_email)])
    )
    error_message = "Billeif must have a $125 monthly cost budget with actual 50/80/100 percent and forecasted 100 percent email alerts."
  }
}

run "application_enablement_requires_an_alert_recipient" {
  command = plan

  variables {
    enable_application = true
  }

  expect_failures = [aws_budgets_budget.monthly_cost]
}

run "application_enablement_requires_confirmed_alert_subscription" {
  command = plan

  variables {
    enable_application = true
    alert_email        = "alerts@example.com"
  }

  expect_failures = [aws_budgets_budget.monthly_cost]
}

run "standard_resolution_operational_alarms_use_two_of_three" {
  command = plan

  variables {
    enable_application                 = true
    enable_background_processing       = true
    alert_email                        = "alerts@example.com"
    alert_email_subscription_confirmed = true
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.lambda_api_errors.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.lambda_api_errors.datapoints_to_alarm == 2 &&
      aws_cloudwatch_metric_alarm.lambda_api_errors.treat_missing_data == "notBreaching" &&
      aws_cloudwatch_metric_alarm.lambda_api_throttles.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.lambda_api_throttles.datapoints_to_alarm == 2 &&
      aws_cloudwatch_metric_alarm.lambda_api_throttles.treat_missing_data == "notBreaching" &&
      aws_cloudwatch_metric_alarm.lambda_api_duration.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.lambda_api_duration.datapoints_to_alarm == 2 &&
      aws_cloudwatch_metric_alarm.lambda_api_duration.extended_statistic == "p95" &&
      aws_cloudwatch_metric_alarm.lambda_api_duration.threshold == 1500 &&
      aws_cloudwatch_metric_alarm.lambda_api_duration.treat_missing_data == "notBreaching"
    )
    error_message = "API error, throttle, and p95 launch-SLO duration alarms must use explicit two-of-three standard-resolution evaluation."
  }

  assert {
    condition = alltrue([
      for alarm in [
        aws_cloudwatch_metric_alarm.lambda_ws_errors,
        aws_cloudwatch_metric_alarm.lambda_invoice_errors,
        aws_cloudwatch_metric_alarm.lambda_gst_errors
      ] : alarm.treat_missing_data == "notBreaching"
    ])
    error_message = "Sparse Lambda error alarms must treat missing metrics as non-breaching instead of entering INSUFFICIENT_DATA."
  }

  assert {
    condition = (
      length(aws_cloudwatch_metric_alarm.worker_queue_age) == 5 &&
      length(aws_cloudwatch_metric_alarm.worker_dlq_messages) == 5 &&
      alltrue([for alarm in aws_cloudwatch_metric_alarm.worker_queue_age : alarm.evaluation_periods == 3 && alarm.datapoints_to_alarm == 2 && alarm.period == 60 && alarm.threshold == 300 && alarm.treat_missing_data == "notBreaching"]) &&
      alltrue([for alarm in aws_cloudwatch_metric_alarm.worker_dlq_messages : alarm.evaluation_periods == 3 && alarm.datapoints_to_alarm == 2 && alarm.period == 60 && alarm.treat_missing_data == "notBreaching"])
    )
    error_message = "Every active worker queue and DLQ must have explicit two-of-three age/depth monitoring."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.rds_cpu_high.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.rds_cpu_high.datapoints_to_alarm == 2 &&
      aws_cloudwatch_metric_alarm.rds_cpu_high.threshold == 70 &&
      aws_cloudwatch_metric_alarm.rds_connections_high.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.rds_memory_low.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.rds_storage_low.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.rds_cpu_credits_low.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.rds_connections_high.treat_missing_data == "notBreaching" &&
      aws_cloudwatch_metric_alarm.rds_memory_low.treat_missing_data == "breaching" &&
      aws_cloudwatch_metric_alarm.rds_storage_low.treat_missing_data == "breaching" &&
      aws_cloudwatch_metric_alarm.rds_cpu_credits_low.treat_missing_data == "notBreaching"
    )
    error_message = "RDS connections, CPU, memory, storage, and burst credits must use explicit two-of-three monitoring."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.outbox_oldest_pending_age.metric_name == "OldestPendingAgeSeconds" &&
      aws_cloudwatch_metric_alarm.outbox_oldest_pending_age.namespace == "Billeif/Outbox" &&
      aws_cloudwatch_metric_alarm.outbox_oldest_pending_age.threshold == 300 &&
      aws_cloudwatch_metric_alarm.outbox_oldest_pending_age.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.outbox_oldest_pending_age.datapoints_to_alarm == 2 &&
      aws_cloudwatch_metric_alarm.outbox_oldest_pending_age.treat_missing_data == "notBreaching"
    )
    error_message = "The Billeif outbox oldest-pending-age metric must alarm after two of three five-minute breaches without treating an empty outbox as a failure."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.lambda_recurring_invoices_errors.metric_name == "Errors" &&
      aws_cloudwatch_metric_alarm.lambda_recurring_invoices_errors.namespace == "AWS/Lambda" &&
      aws_cloudwatch_metric_alarm.lambda_recurring_invoices_errors.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.lambda_recurring_invoices_errors.datapoints_to_alarm == 2 &&
      aws_cloudwatch_metric_alarm.lambda_recurring_invoices_errors.treat_missing_data == "notBreaching" &&
      aws_cloudwatch_metric_alarm.recurring_invoice_failed_runs.metric_name == "Failed" &&
      aws_cloudwatch_metric_alarm.recurring_invoice_failed_runs.namespace == "Billeif/RecurringInvoices" &&
      aws_cloudwatch_metric_alarm.recurring_invoice_failed_runs.threshold == 0 &&
      aws_cloudwatch_metric_alarm.recurring_invoice_failed_runs.evaluation_periods == 3 &&
      aws_cloudwatch_metric_alarm.recurring_invoice_failed_runs.datapoints_to_alarm == 2 &&
      aws_cloudwatch_metric_alarm.recurring_invoice_failed_runs.treat_missing_data == "notBreaching"
    )
    error_message = "Recurring invoice runtime and failed-run metrics must use explicit two-of-three alarms."
  }
}
