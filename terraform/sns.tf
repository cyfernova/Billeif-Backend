resource "aws_sns_topic" "notifications" {
  name = "invoice-notifications"

  tags = {
    Name        = "invoice-notifications"
    Environment = "development"
  }
}

output "notifications_topic_arn" {
  value       = aws_sns_topic.notifications.arn
  description = "SNS notifications topic ARN"
}
