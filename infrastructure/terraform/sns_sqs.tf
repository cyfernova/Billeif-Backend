resource "aws_sns_topic" "low_stock_alerts" {
  name = "low-stock-alerts"
}

resource "aws_sns_topic" "payment_notifications" {
  name = "payment-notifications"
}

# Workflow Notifications Topic
resource "aws_sns_topic" "workflow_notifications" {
  name = "workflow-notifications"

  tags = {
    Name = "workflow-notifications"
  }
}

# Mobile Push Notifications - FCM (Android)
# Note: FCM API Key should be stored in AWS Secrets Manager for production
resource "aws_sns_platform_application" "fcm" {
  count = var.fcm_api_key != "" ? 1 : 0

  name                = "invoice-backend-fcm"
  platform            = "GCM"
  platform_credential = var.fcm_api_key

  success_feedback_role_arn    = aws_iam_role.sns_feedback.arn
  failure_feedback_role_arn    = aws_iam_role.sns_feedback.arn
  success_feedback_sample_rate = 100
}

# Mobile Push Notifications - APNs (iOS)
# Note: APNs credentials should be stored in AWS Secrets Manager for production
resource "aws_sns_platform_application" "apns" {
  count = var.apns_private_key != "" && var.apns_certificate != "" ? 1 : 0

  name                = "invoice-backend-apns"
  platform            = var.apns_sandbox ? "APNS_SANDBOX" : "APNS"
  platform_credential = var.apns_private_key
  platform_principal  = var.apns_certificate

  success_feedback_role_arn    = aws_iam_role.sns_feedback.arn
  failure_feedback_role_arn    = aws_iam_role.sns_feedback.arn
  success_feedback_sample_rate = 100
}

# IAM Role for SNS Feedback Logging
resource "aws_iam_role" "sns_feedback" {
  name = "invoice-backend-sns-feedback"

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
  name = "sns-feedback-policy"
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
  name                       = "invoice-processing-queue"
  message_retention_seconds  = 86400
  visibility_timeout_seconds = var.worker_queue_visibility_timeout_seconds
}

resource "aws_sqs_queue" "payment_processing" {
  name                       = "payment-processing-queue"
  message_retention_seconds  = 86400
  visibility_timeout_seconds = var.worker_queue_visibility_timeout_seconds
}

# Workflow Run Queue
resource "aws_sqs_queue" "workflow_runs" {
  name                       = "workflow-runs-queue"
  message_retention_seconds  = 86400
  visibility_timeout_seconds = 60
}

resource "aws_sqs_queue" "invoice_processing_dlq" {
  name = "invoice-processing-dlq"
}

resource "aws_sqs_queue" "payment_processing_dlq" {
  name = "payment-processing-dlq"
}

resource "aws_sqs_queue" "workflow_runs_dlq" {
  name = "workflow-runs-dlq"
}

resource "aws_sqs_queue_redrive_allow_policy" "invoice_processing_dlq" {
  queue_url = aws_sqs_queue.invoice_processing_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.invoice_processing.arn]
  })
}

resource "aws_sqs_queue_redrive_allow_policy" "payment_processing_dlq" {
  queue_url = aws_sqs_queue.payment_processing_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.payment_processing.arn]
  })
}

resource "aws_sqs_queue_redrive_allow_policy" "workflow_runs_dlq" {
  queue_url = aws_sqs_queue.workflow_runs_dlq.id
  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.workflow_runs.arn]
  })
}

resource "aws_sqs_queue_redrive_policy" "invoice_processing" {
  queue_url = aws_sqs_queue.invoice_processing.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.invoice_processing_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue_redrive_policy" "payment_processing" {
  queue_url = aws_sqs_queue.payment_processing.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.payment_processing_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue_redrive_policy" "workflow_runs" {
  queue_url = aws_sqs_queue.workflow_runs.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.workflow_runs_dlq.arn
    maxReceiveCount     = 3
  })
}

# SNS to SQS subscription for workflow notifications
resource "aws_sns_topic_subscription" "workflow_to_sqs" {
  topic_arn = aws_sns_topic.workflow_notifications.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.workflow_runs.arn
}

resource "aws_sqs_queue_policy" "workflow_runs" {
  queue_url = aws_sqs_queue.workflow_runs.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.workflow_runs.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_sns_topic.workflow_notifications.arn
          }
        }
      }
    ]
  })
}
