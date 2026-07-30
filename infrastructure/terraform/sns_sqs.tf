resource "aws_sns_topic" "low_stock_alerts" {
  name = "${local.resource_prefix}-low-stock-alerts"
}

# IAM Role for SNS Feedback Logging
resource "aws_iam_role" "sns_feedback" {
  name = "${local.resource_prefix}-sns-feedback"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_role_policy" "sns_feedback" {
  name = "${local.resource_prefix}-sns-feedback-policy"
  role = aws_iam_role.sns_feedback.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:PutMetricFilter",
          "logs:PutRetentionPolicy"
        ]
        Resource = "*"
      }
    ]
  })
}

resource "aws_sqs_queue" "invoice_processing" {
  name                       = "${local.resource_prefix}-invoice-processing-queue"
  message_retention_seconds  = 604800
  visibility_timeout_seconds = var.worker_queue_visibility_timeout_seconds
  sqs_managed_sse_enabled    = true
}

resource "aws_sqs_queue" "gst_processing" {
  name                       = "${local.resource_prefix}-gst-processing-queue"
  message_retention_seconds  = 345600
  visibility_timeout_seconds = var.worker_queue_visibility_timeout_seconds
  sqs_managed_sse_enabled    = true
}

resource "aws_sqs_queue" "email_delivery_dlq" {
  name                      = "${local.resource_prefix}-email-delivery-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

resource "aws_sqs_queue" "email_delivery" {
  name                       = "${local.resource_prefix}-email-delivery-queue"
  message_retention_seconds  = 345600
  visibility_timeout_seconds = var.worker_queue_visibility_timeout_seconds
  sqs_managed_sse_enabled    = true
}

resource "aws_sqs_queue_redrive_allow_policy" "email_delivery_dlq" {
  queue_url = aws_sqs_queue.email_delivery_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue"
    sourceQueueArns   = [aws_sqs_queue.email_delivery.arn]
  })
}

resource "aws_sqs_queue_redrive_policy" "email_delivery" {
  queue_url = aws_sqs_queue.email_delivery.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.email_delivery_dlq.arn
    maxReceiveCount     = 5
  })
}

data "aws_iam_policy_document" "email_delivery_queue" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["sqs:*"]
    resources = [aws_sqs_queue.email_delivery.arn]

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

data "aws_iam_policy_document" "email_delivery_dlq" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["sqs:*"]
    resources = [aws_sqs_queue.email_delivery_dlq.arn]

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

resource "aws_sqs_queue_policy" "email_delivery" {
  queue_url = aws_sqs_queue.email_delivery.id
  policy    = data.aws_iam_policy_document.email_delivery_queue.json
}

resource "aws_sqs_queue_policy" "email_delivery_dlq" {
  queue_url = aws_sqs_queue.email_delivery_dlq.id
  policy    = data.aws_iam_policy_document.email_delivery_dlq.json
}

resource "aws_sqs_queue" "ses_feedback_dlq" {
  name                      = "${local.resource_prefix}-ses-feedback-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

resource "aws_sqs_queue" "ses_feedback" {
  name                       = "${local.resource_prefix}-ses-feedback-queue"
  message_retention_seconds  = 345600
  visibility_timeout_seconds = 180
  sqs_managed_sse_enabled    = true
}

resource "aws_sqs_queue_redrive_allow_policy" "ses_feedback_dlq" {
  queue_url = aws_sqs_queue.ses_feedback_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue"
    sourceQueueArns   = [aws_sqs_queue.ses_feedback.arn]
  })
}

resource "aws_sqs_queue_redrive_policy" "ses_feedback" {
  queue_url = aws_sqs_queue.ses_feedback.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.ses_feedback_dlq.arn
    maxReceiveCount     = 5
  })
}

data "aws_iam_policy_document" "ses_feedback_queue" {
  statement {
    sid       = "AllowSESEventTopic"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.ses_feedback.arn]

    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.ses_events.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }

  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["sqs:*"]
    resources = [aws_sqs_queue.ses_feedback.arn]

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

data "aws_iam_policy_document" "ses_feedback_dlq" {
  statement {
    sid       = "AllowSESEventTopicRedrive"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.ses_feedback_dlq.arn]

    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.ses_events.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }

  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["sqs:*"]
    resources = [aws_sqs_queue.ses_feedback_dlq.arn]

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

resource "aws_sqs_queue_policy" "ses_feedback" {
  queue_url = aws_sqs_queue.ses_feedback.id
  policy    = data.aws_iam_policy_document.ses_feedback_queue.json
}

resource "aws_sqs_queue_policy" "ses_feedback_dlq" {
  queue_url = aws_sqs_queue.ses_feedback_dlq.id
  policy    = data.aws_iam_policy_document.ses_feedback_dlq.json
}

resource "aws_sns_topic_subscription" "ses_feedback" {
  topic_arn            = aws_sns_topic.ses_events.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.ses_feedback.arn
  raw_message_delivery = true
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.ses_feedback_dlq.arn
  })

  depends_on = [
    aws_sqs_queue_policy.ses_feedback,
    aws_sqs_queue_policy.ses_feedback_dlq,
  ]
}

# Autonomous Bargaining Negotiation Queue
resource "aws_sqs_queue" "bargaining_negotiation" {
  name                       = "${local.resource_prefix}-bargaining-negotiation-queue"
  message_retention_seconds  = 345600
  visibility_timeout_seconds = 365
  sqs_managed_sse_enabled    = true
}

resource "aws_sqs_queue" "bargaining_negotiation_dlq" {
  name                      = "${local.resource_prefix}-bargaining-negotiation-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

resource "aws_sqs_queue_redrive_allow_policy" "bargaining_negotiation_dlq" {
  queue_url = aws_sqs_queue.bargaining_negotiation_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.bargaining_negotiation.arn]
  })
}

resource "aws_sqs_queue_redrive_policy" "bargaining_negotiation" {
  queue_url = aws_sqs_queue.bargaining_negotiation.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.bargaining_negotiation_dlq.arn
    maxReceiveCount     = 5
  })
}

resource "aws_sqs_queue" "invoice_processing_dlq" {
  name                      = "${local.resource_prefix}-invoice-processing-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

resource "aws_sqs_queue" "gst_processing_dlq" {
  name                      = "${local.resource_prefix}-gst-processing-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

resource "aws_sqs_queue_redrive_allow_policy" "invoice_processing_dlq" {
  queue_url = aws_sqs_queue.invoice_processing_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.invoice_processing.arn]
  })
}

resource "aws_sqs_queue_redrive_allow_policy" "gst_processing_dlq" {
  queue_url = aws_sqs_queue.gst_processing_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.gst_processing.arn]
  })
}

resource "aws_sqs_queue_redrive_policy" "invoice_processing" {
  queue_url = aws_sqs_queue.invoice_processing.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.invoice_processing_dlq.arn
    maxReceiveCount     = 5
  })
}

resource "aws_sqs_queue_redrive_policy" "gst_processing" {
  queue_url = aws_sqs_queue.gst_processing.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.gst_processing_dlq.arn
    maxReceiveCount     = 5
  })
}
