data "aws_iam_policy_document" "outbox_scheduler_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "outbox_scheduler" {
  name                 = "${local.resource_prefix}-outbox-scheduler-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.outbox_scheduler_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

data "aws_iam_policy_document" "outbox_scheduler" {
  statement {
    sid       = "InvokeOutboxDispatcher"
    effect    = "Allow"
    actions   = ["lambda:InvokeFunction"]
    resources = [aws_lambda_function.outbox_dispatcher.arn]
  }

  statement {
    sid       = "SendOutboxDispatcherFailures"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.outbox_dispatcher_scheduler_dlq.arn]
  }
}

resource "aws_iam_role_policy" "outbox_scheduler" {
  name   = "${local.resource_prefix}-outbox-scheduler-policy"
  role   = aws_iam_role.outbox_scheduler.id
  policy = data.aws_iam_policy_document.outbox_scheduler.json
}

resource "aws_sqs_queue" "outbox_dispatcher_scheduler_dlq" {
  name                      = "${local.resource_prefix}-outbox-dispatcher-scheduler-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

resource "aws_scheduler_schedule" "outbox_dispatcher" {
  name                         = "${local.resource_prefix}-outbox-dispatcher-minute"
  description                  = "${local.resource_prefix} Billeif outbox dispatcher schedule"
  schedule_expression          = "rate(1 minute)"
  schedule_expression_timezone = "UTC"
  state                        = local.background_processing_enabled ? "ENABLED" : "DISABLED"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.outbox_dispatcher.arn
    role_arn = aws_iam_role.outbox_scheduler.arn
    input    = "{}"

    dead_letter_config {
      arn = aws_sqs_queue.outbox_dispatcher_scheduler_dlq.arn
    }

    retry_policy {
      maximum_event_age_in_seconds = 300
      maximum_retry_attempts       = 3
    }
  }

  depends_on = [aws_iam_role_policy.outbox_scheduler]
}

data "aws_iam_policy_document" "outbox_scheduler_dlq" {
  statement {
    sid       = "AllowBilleifSchedulerFailures"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.outbox_dispatcher_scheduler_dlq.arn]

    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_scheduler_schedule.outbox_dispatcher.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "outbox_dispatcher_scheduler_dlq" {
  queue_url = aws_sqs_queue.outbox_dispatcher_scheduler_dlq.id
  policy    = data.aws_iam_policy_document.outbox_scheduler_dlq.json
}
