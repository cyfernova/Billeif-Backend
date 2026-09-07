resource "aws_sqs_queue" "bulk_import_dlq" {
  name                      = "${local.resource_prefix}-bulk-import-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

resource "aws_sqs_queue" "bulk_import" {
  name                       = "${local.resource_prefix}-bulk-import-queue"
  message_retention_seconds  = 604800
  visibility_timeout_seconds = 720
  receive_wait_time_seconds  = 20
  sqs_managed_sse_enabled    = true
}

resource "aws_sqs_queue_redrive_allow_policy" "bulk_import_dlq" {
  queue_url = aws_sqs_queue.bulk_import_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue"
    sourceQueueArns   = [aws_sqs_queue.bulk_import.arn]
  })
}

resource "aws_sqs_queue_redrive_policy" "bulk_import" {
  queue_url = aws_sqs_queue.bulk_import.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.bulk_import_dlq.arn
    maxReceiveCount     = 5
  })
}

data "aws_iam_policy_document" "bulk_import_queue" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["sqs:*"]
    resources = [aws_sqs_queue.bulk_import.arn]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

data "aws_iam_policy_document" "bulk_import_dlq" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["sqs:*"]
    resources = [aws_sqs_queue.bulk_import_dlq.arn]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_sqs_queue_policy" "bulk_import" {
  queue_url = aws_sqs_queue.bulk_import.id
  policy    = data.aws_iam_policy_document.bulk_import_queue.json
}

resource "aws_sqs_queue_policy" "bulk_import_dlq" {
  queue_url = aws_sqs_queue.bulk_import_dlq.id
  policy    = data.aws_iam_policy_document.bulk_import_dlq.json
}

resource "aws_cloudwatch_metric_alarm" "bulk_import_queue_age" {
  alarm_name          = "${local.resource_prefix}-bulk-import-queue-age"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "ApproximateAgeOfOldestMessage"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = 300
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif bulk import queue oldest message exceeds five minutes"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    QueueName = aws_sqs_queue.bulk_import.name
  }
}

resource "aws_cloudwatch_metric_alarm" "bulk_import_dlq_messages" {
  alarm_name          = "${local.resource_prefix}-bulk-import-dlq-messages"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif bulk import dead-letter queue has messages"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    QueueName = aws_sqs_queue.bulk_import_dlq.name
  }
}

resource "aws_cloudwatch_log_group" "lambda_bulk_import" {
  name              = "/aws/lambda/${local.resource_prefix}-bulk-import"
  retention_in_days = var.log_retention_days
}

resource "aws_iam_role" "bulk_import" {
  name                 = "${local.resource_prefix}-bulk-import-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "bulk_import_basic" {
  role       = aws_iam_role.bulk_import.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "bulk_import_vpc_access" {
  role       = aws_iam_role.bulk_import.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

data "aws_iam_policy_document" "bulk_import_scheduler_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "bulk_import_scheduler" {
  name                 = "${local.resource_prefix}-bulk-import-scheduler-role"
  assume_role_policy   = data.aws_iam_policy_document.bulk_import_scheduler_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

data "aws_iam_policy_document" "bulk_import_worker" {
  statement {
    sid       = "BulkImportDatabaseHost"
    effect    = "Allow"
    actions   = ["ssm:GetParameters"]
    resources = [local.db_host_ssm_parameter_arn]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }
  }

  statement {
    sid       = "BulkImportDatabaseSecret"
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_db_instance.main.master_user_secret[0].secret_arn]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }
  }

  statement {
    sid       = "BulkImportDatabaseSecretKMS"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.application_secrets.arn]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["secretsmanager.${var.aws_region}.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:EncryptionContext:SecretARN"
      values   = [aws_db_instance.main.master_user_secret[0].secret_arn]
    }
  }

  statement {
    sid    = "BulkImportQueue"
    effect = "Allow"
    actions = [
      "sqs:ChangeMessageVisibility",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:ReceiveMessage",
      "sqs:SendMessage",
    ]
    resources = [aws_sqs_queue.bulk_import.arn]
  }

  statement {
    sid       = "BulkImportPendingObjectLifecycle"
    effect    = "Allow"
    actions   = ["s3:DeleteObject", "s3:GetObject"]
    resources = ["${aws_s3_bucket.invoices_pdf.arn}/pending/*"]
  }

  statement {
    sid       = "BulkImportResultArtifactLifecycle"
    effect    = "Allow"
    actions   = ["s3:DeleteObject", "s3:PutObject"]
    resources = ["${aws_s3_bucket.invoices_pdf.arn}/bulk-import-results/*"]
  }
}

resource "aws_iam_role_policy" "bulk_import" {
  name   = "${local.resource_prefix}-bulk-import-policy"
  role   = aws_iam_role.bulk_import.id
  policy = data.aws_iam_policy_document.bulk_import_worker.json
}

data "aws_iam_policy_document" "bulk_import_scheduler" {
  statement {
    sid       = "InvokeBulkImportMaintenance"
    effect    = "Allow"
    actions   = ["lambda:InvokeFunction"]
    resources = [aws_lambda_function.bulk_import.arn]
  }

  statement {
    sid       = "SendBulkImportMaintenanceFailures"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.bulk_import_dlq.arn]
  }
}

resource "aws_iam_role_policy" "bulk_import_scheduler" {
  name   = "${local.resource_prefix}-bulk-import-scheduler-policy"
  role   = aws_iam_role.bulk_import_scheduler.id
  policy = data.aws_iam_policy_document.bulk_import_scheduler.json
}

resource "aws_lambda_function" "bulk_import" {
  function_name    = "${local.resource_prefix}-bulk-import"
  role             = aws_iam_role.bulk_import.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.bulk_import
  source_code_hash = local.lambda_artifact_hashes.bulk_import
  memory_size      = 512
  timeout          = 120

  reserved_concurrent_executions = local.background_processing_enabled ? (var.enable_lambda_reserved_concurrency ? 2 : null) : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = {
      ENVIRONMENT             = var.environment
      LOG_LEVEL               = "info"
      LOG_FORMAT              = "json"
      DATABASE_HOST_SSM_PARAM = local.db_host_ssm_parameter_name
      DATABASE_SECRET_ARN     = aws_db_instance.main.master_user_secret[0].secret_arn
      DATABASE_PORT           = tostring(var.db_port)
      DATABASE_NAME           = var.db_name
      DATABASE_SSL_MODE       = "require"
      S3_BUCKET_DRIVE         = aws_s3_bucket.invoices_pdf.id
      S3_BUCKET_INVOICES      = aws_s3_bucket.invoices_pdf.id
      SQS_BULK_IMPORT_QUEUE   = aws_sqs_queue.bulk_import.url
    }
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.bulk_import)
      error_message = "Missing Billeif bulk import Lambda artifact ${local.lambda_artifacts.bulk_import}. Run make package-lambda-bulk-import from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_bulk_import,
    aws_iam_role_policy.bulk_import,
    aws_iam_role_policy_attachment.bulk_import_basic,
    aws_iam_role_policy_attachment.bulk_import_vpc_access,
    aws_ssm_parameter.db_host,
  ]
}

resource "aws_lambda_event_source_mapping" "bulk_import_queue" {
  count = local.background_processing_enabled ? 1 : 0

  event_source_arn                   = aws_sqs_queue.bulk_import.arn
  function_name                      = aws_lambda_function.bulk_import.arn
  batch_size                         = 5
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 5

  scaling_config {
    maximum_concurrency = 2
  }
}

resource "aws_scheduler_schedule" "bulk_import_maintenance" {
  name                         = "${local.resource_prefix}-bulk-import-maintenance"
  description                  = "${local.resource_prefix} Billeif bulk import recovery and retention schedule"
  schedule_expression          = "rate(5 minutes)"
  schedule_expression_timezone = "UTC"
  state                        = local.background_processing_enabled ? "ENABLED" : "DISABLED"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.bulk_import.arn
    role_arn = aws_iam_role.bulk_import_scheduler.arn
    input    = jsonencode({ Records = [] })

    dead_letter_config {
      arn = aws_sqs_queue.bulk_import_dlq.arn
    }

    retry_policy {
      maximum_event_age_in_seconds = 300
      maximum_retry_attempts       = 3
    }
  }

  depends_on = [aws_iam_role_policy.bulk_import_scheduler]
}
