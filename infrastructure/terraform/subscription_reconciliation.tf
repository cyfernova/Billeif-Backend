resource "aws_cloudwatch_log_group" "lambda_subscription_reconciler" {
  name              = "/aws/lambda/${local.resource_prefix}-subscription-reconciler"
  retention_in_days = var.log_retention_days
}

resource "aws_iam_role" "subscription_reconciler" {
  name                 = "${local.resource_prefix}-sub-recon-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "subscription_reconciler_basic" {
  role       = aws_iam_role.subscription_reconciler.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "subscription_reconciler_vpc" {
  role       = aws_iam_role.subscription_reconciler.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

data "aws_iam_policy_document" "subscription_reconciler" {
  statement {
    sid       = "SubscriptionDatabaseHost"
    effect    = "Allow"
    actions   = ["ssm:GetParameters"]
    resources = [local.db_host_ssm_parameter_arn]
  }

  statement {
    sid    = "SubscriptionSecrets"
    effect = "Allow"
    actions = [
      "secretsmanager:DescribeSecret",
      "secretsmanager:GetSecretValue",
    ]
    resources = [
      aws_db_instance.main.master_user_secret[0].secret_arn,
      aws_secretsmanager_secret.razorpay.arn,
    ]
  }

  statement {
    sid    = "SubscriptionSecretKMS"
    effect = "Allow"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey",
    ]
    resources = [aws_kms_key.application_secrets.arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["secretsmanager.${var.aws_region}.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "subscription_reconciler" {
  name   = "${local.resource_prefix}-subscription-reconciler-policy"
  role   = aws_iam_role.subscription_reconciler.id
  policy = data.aws_iam_policy_document.subscription_reconciler.json
}

resource "aws_lambda_function" "subscription_reconciler" {
  function_name    = "${local.resource_prefix}-subscription-reconciler"
  role             = aws_iam_role.subscription_reconciler.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.subscription_reconciler
  source_code_hash = local.lambda_artifact_hashes.subscription_reconciler
  memory_size      = 256
  timeout          = 60

  reserved_concurrent_executions = local.background_processing_enabled ? (var.enable_lambda_reserved_concurrency ? 1 : null) : 0

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
      RAZORPAY_SECRET_ARN     = aws_secretsmanager_secret.razorpay.arn
    }
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.subscription_reconciler)
      error_message = "Missing subscription reconciler artifact. Run make package-lambda-subscription-reconciler before Terraform."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.lambda_subscription_reconciler,
    aws_iam_role_policy.subscription_reconciler,
  ]
}

resource "aws_sqs_queue" "subscription_reconciler_scheduler_dlq" {
  name                      = "${local.resource_prefix}-subscription-reconciler-scheduler-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

data "aws_iam_policy_document" "subscription_scheduler_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "subscription_scheduler" {
  name                 = "${local.resource_prefix}-sub-recon-scheduler-role"
  assume_role_policy   = data.aws_iam_policy_document.subscription_scheduler_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

data "aws_iam_policy_document" "subscription_scheduler" {
  statement {
    sid       = "InvokeSubscriptionReconciler"
    effect    = "Allow"
    actions   = ["lambda:InvokeFunction"]
    resources = [aws_lambda_function.subscription_reconciler.arn]
  }
  statement {
    sid       = "SendSubscriptionFailures"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.subscription_reconciler_scheduler_dlq.arn]
  }
}

resource "aws_iam_role_policy" "subscription_scheduler" {
  name   = "${local.resource_prefix}-subscription-reconciler-scheduler-policy"
  role   = aws_iam_role.subscription_scheduler.id
  policy = data.aws_iam_policy_document.subscription_scheduler.json
}

resource "aws_scheduler_schedule" "subscription_reconciler" {
  name                         = "${local.resource_prefix}-sub-recon-five-minute"
  description                  = "${local.resource_prefix} bounded subscription lifecycle reconciliation"
  schedule_expression          = "rate(5 minutes)"
  schedule_expression_timezone = "UTC"
  state                        = local.background_processing_enabled ? "ENABLED" : "DISABLED"
  flexible_time_window { mode = "OFF" }
  target {
    arn      = aws_lambda_function.subscription_reconciler.arn
    role_arn = aws_iam_role.subscription_scheduler.arn
    input    = "{}"
    dead_letter_config { arn = aws_sqs_queue.subscription_reconciler_scheduler_dlq.arn }
    retry_policy {
      maximum_event_age_in_seconds = 900
      maximum_retry_attempts       = 2
    }
  }
  depends_on = [aws_iam_role_policy.subscription_scheduler]
}

resource "aws_cloudwatch_metric_alarm" "subscription_reconciler_errors" {
  alarm_name          = "${local.resource_prefix}-subscription-reconciler-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = { FunctionName = aws_lambda_function.subscription_reconciler.function_name }
}

resource "aws_cloudwatch_metric_alarm" "subscription_reconciliation_failed" {
  alarm_name          = "${local.resource_prefix}-subscription-reconciliation-failed"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Failed"
  namespace           = "Billeif/SubscriptionLifecycle"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = { Environment = var.environment }
}
