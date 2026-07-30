resource "aws_sns_topic" "alerts" {
  name = "${local.resource_prefix}-alerts"

  tags = {
    Name = "${local.resource_prefix}-alerts"
  }
}

resource "aws_sns_topic_subscription" "alerts_email" {
  count     = var.alert_email != "" ? 1 : 0
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

resource "aws_cloudwatch_metric_alarm" "lambda_api_errors" {
  alarm_name          = "${local.resource_prefix}-lambda-api-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  alarm_description   = "API Lambda error count is high"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    FunctionName = aws_lambda_function.api_http.function_name
  }
}

resource "aws_cloudwatch_metric_alarm" "lambda_ws_errors" {
  alarm_name          = "${local.resource_prefix}-lambda-ws-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  alarm_description   = "WebSocket Lambda error count is high"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    FunctionName = aws_lambda_function.ws_handler.function_name
  }
}

resource "aws_cloudwatch_metric_alarm" "lambda_invoice_errors" {
  alarm_name          = "${local.resource_prefix}-lambda-invoice-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 3
  alarm_description   = "Invoice worker Lambda error count is high"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    FunctionName = aws_lambda_function.sqs_invoice.function_name
  }
}

resource "aws_cloudwatch_metric_alarm" "lambda_gst_errors" {
  alarm_name          = "${local.resource_prefix}-lambda-gst-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 3
  alarm_description   = "GST worker Lambda error count is high"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    FunctionName = aws_lambda_function.sqs_gst.function_name
  }
}

resource "aws_cloudwatch_metric_alarm" "lambda_email_delivery_errors" {
  alarm_name          = "${local.resource_prefix}-lambda-email-delivery-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif email delivery Lambda is returning errors"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    FunctionName = aws_lambda_function.sqs_email_delivery.function_name
  }
}

resource "aws_cloudwatch_metric_alarm" "lambda_email_delivery_throttles" {
  alarm_name          = "${local.resource_prefix}-lambda-email-delivery-throttles"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Throttles"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif email delivery Lambda is being throttled"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    FunctionName = aws_lambda_function.sqs_email_delivery.function_name
  }
}

resource "aws_cloudwatch_metric_alarm" "lambda_email_delivery_duration" {
  alarm_name          = "${local.resource_prefix}-lambda-email-delivery-duration"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Duration"
  namespace           = "AWS/Lambda"
  period              = 300
  extended_statistic  = "p95"
  threshold           = 45000
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif email delivery Lambda p95 duration exceeds 45 seconds"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    FunctionName = aws_lambda_function.sqs_email_delivery.function_name
  }
}

resource "aws_cloudwatch_metric_alarm" "email_delivery_queue_age" {
  alarm_name          = "${local.resource_prefix}-email-delivery-queue-age"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "ApproximateAgeOfOldestMessage"
  namespace           = "AWS/SQS"
  period              = 300
  statistic           = "Maximum"
  threshold           = 600
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif email delivery queue oldest message exceeds ten minutes"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    QueueName = aws_sqs_queue.email_delivery.name
  }
}

resource "aws_cloudwatch_metric_alarm" "email_delivery_dlq_messages" {
  alarm_name          = "${local.resource_prefix}-email-delivery-dlq-messages"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 300
  statistic           = "Maximum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif email delivery dead-letter queue has messages"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    QueueName = aws_sqs_queue.email_delivery_dlq.name
  }
}

locals {
  threat_detection_log_groups = {
    api_http   = aws_cloudwatch_log_group.lambda_api_http.name
    a2a_stream = aws_cloudwatch_log_group.lambda_a2a_stream.name
    websocket  = aws_cloudwatch_log_group.lambda_ws_handler.name
  }
}

resource "aws_cloudwatch_log_metric_filter" "threat_detection" {
  for_each       = local.threat_detection_log_groups
  name           = "${local.resource_prefix}-${each.key}-threat-detection"
  log_group_name = each.value
  pattern        = "{ $.security_detection = true }"

  metric_transformation {
    name      = "ThreatDetectionCount"
    namespace = "${local.resource_prefix}/Security"
    value     = "1"
  }
}

resource "aws_cloudwatch_metric_alarm" "threat_detection" {
  alarm_name          = "${local.resource_prefix}-threat-detection"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "ThreatDetectionCount"
  namespace           = "${local.resource_prefix}/Security"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  alarm_description   = "High-signal exploit or scanner probe detected in backend request logs"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  depends_on = [aws_cloudwatch_log_metric_filter.threat_detection]
}

resource "aws_cloudwatch_metric_alarm" "rds_cpu_high" {
  alarm_name          = "${local.resource_prefix}-rds-cpu-high"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "CPUUtilization"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 80
  alarm_description   = "RDS CPU utilization is above 80%"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    DBInstanceIdentifier = aws_db_instance.main.id
  }
}

resource "aws_cloudwatch_metric_alarm" "rds_storage_low" {
  alarm_name          = "${local.resource_prefix}-rds-storage-low"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 1
  metric_name         = "FreeStorageSpace"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 2147483648 # 2 GB in bytes
  alarm_description   = "RDS free storage is below 2GB"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    DBInstanceIdentifier = aws_db_instance.main.id
  }
}

resource "aws_cloudwatch_dashboard" "main" {
  dashboard_name = "${local.resource_prefix}-dashboard"

  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          title  = "Lambda Invocations"
          region = var.aws_region
          metrics = [
            ["AWS/Lambda", "Invocations", "FunctionName", aws_lambda_function.api_http.function_name],
            [".", "Invocations", "FunctionName", aws_lambda_function.sqs_invoice.function_name],
            [".", "Invocations", "FunctionName", aws_lambda_function.sqs_gst.function_name],
            [".", "Invocations", "FunctionName", aws_lambda_function.ws_handler.function_name]
          ]
          stat   = "Sum"
          period = 300
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          title  = "Lambda Errors"
          region = var.aws_region
          metrics = [
            ["AWS/Lambda", "Errors", "FunctionName", aws_lambda_function.api_http.function_name],
            [".", "Errors", "FunctionName", aws_lambda_function.sqs_invoice.function_name],
            [".", "Errors", "FunctionName", aws_lambda_function.sqs_gst.function_name],
            [".", "Errors", "FunctionName", aws_lambda_function.ws_handler.function_name]
          ]
          stat   = "Sum"
          period = 300
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 6
        width  = 24
        height = 6
        properties = {
          title  = "RDS Performance"
          region = var.aws_region
          metrics = [
            ["AWS/RDS", "CPUUtilization", "DBInstanceIdentifier", aws_db_instance.main.id],
            [".", "DatabaseConnections", ".", ".", { yAxis = "right" }]
          ]
          stat   = "Average"
          period = 300
        }
      }
    ]
  })
}
