resource "aws_sns_topic" "low_stock_alerts" {
  name = "low-stock-alerts"
}

resource "aws_sns_topic" "payment_notifications" {
  name = "payment-notifications"
}

resource "aws_sqs_queue" "invoice_processing" {
  name                       = "invoice-processing-queue"
  message_retention_seconds  = 86400
  visibility_timeout_seconds = 30
}

resource "aws_sqs_queue" "payment_processing" {
  name                       = "payment-processing-queue"
  message_retention_seconds  = 86400
  visibility_timeout_seconds = 30
}

resource "aws_sqs_queue" "invoice_processing_dlq" {
  name = "invoice-processing-dlq"
}

resource "aws_sqs_queue" "payment_processing_dlq" {
  name = "payment-processing-dlq"
}

resource "aws_sqs_queue_redrive_allow_policy" "invoice_processing" {
  queue_url = aws_sqs_queue.invoice_processing.id
  redrive_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.invoice_processing_dlq.arn]
  })
}

resource "aws_sqs_queue_redrive_allow_policy" "payment_processing" {
  queue_url = aws_sqs_queue.payment_processing.id
  redrive_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.payment_processing_dlq.arn]
  })
}

resource "aws_sqs_queue_redrive_policy" "invoice_processing" {
  queue_url              = aws_sqs_queue.invoice_processing.id
  dead_letter_target_arn = aws_sqs_queue.invoice_processing_dlq.arn
  max_receive_count      = 3
}

resource "aws_sqs_queue_redrive_policy" "payment_processing" {
  queue_url              = aws_sqs_queue.payment_processing.id
  dead_letter_target_arn = aws_sqs_queue.payment_processing_dlq.arn
  max_receive_count      = 3
}
