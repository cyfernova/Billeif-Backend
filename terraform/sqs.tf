resource "aws_sqs_queue" "invoice_processing" {
  name                       = "invoice-processing"
  message_retention_seconds  = 345600
  visibility_timeout_seconds = 30

  tags = {
    Name        = "invoice-processing"
    Environment = "development"
  }
}

resource "aws_sqs_queue" "email_queue" {
  name                       = "email-queue"
  message_retention_seconds  = 1209600
  visibility_timeout_seconds = 60

  tags = {
    Name        = "email-queue"
    Environment = "development"
  }
}

output "invoice_processing_queue_url" {
  value       = aws_sqs_queue.invoice_processing.url
  description = "SQS invoice processing queue URL"
}

output "invoice_processing_queue_arn" {
  value       = aws_sqs_queue.invoice_processing.arn
  description = "SQS invoice processing queue ARN"
}

output "email_queue_url" {
  value       = aws_sqs_queue.email_queue.url
  description = "SQS email queue URL"
}

output "email_queue_arn" {
  value       = aws_sqs_queue.email_queue.arn
  description = "SQS email queue ARN"
}
