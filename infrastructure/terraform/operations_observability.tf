locals {
  operations_metric_namespace = "Billeif/Operations"
  operations_alarm_dimensions = {
    recovery       = { Environment = var.environment, Category = "recovery" }
    queue          = { Environment = var.environment, Category = "queue" }
    reconciliation = { Environment = var.environment, Category = "reconciliation" }
    provider       = { Environment = var.environment, Category = "provider" }
    webhook        = { Environment = var.environment, Category = "webhook" }
    schedule       = { Environment = var.environment, Category = "schedule" }
    render         = { Environment = var.environment, Category = "render" }
    delivery       = { Environment = var.environment, Category = "delivery" }
  }
}

resource "aws_cloudwatch_metric_alarm" "operations_repeated_failures" {
  alarm_name          = "${local.resource_prefix}-operations-repeated-failures"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "RepeatedFailures"
  namespace           = local.operations_metric_namespace
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif operational recovery failures are repeating"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = local.operations_alarm_dimensions.recovery
}

resource "aws_cloudwatch_metric_alarm" "operations_dlq_growth" {
  alarm_name          = "${local.resource_prefix}-operations-dlq-growth"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "DLQGrowth"
  namespace           = local.operations_metric_namespace
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif dead-letter queues are receiving new failures"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = local.operations_alarm_dimensions.queue
}

resource "aws_cloudwatch_metric_alarm" "operations_reconciliation_backlog" {
  alarm_name          = "${local.resource_prefix}-operations-reconciliation-backlog"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "ReconciliationBacklog"
  namespace           = local.operations_metric_namespace
  period              = 300
  statistic           = "Maximum"
  threshold           = 10
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif reconciliation backlog exceeds the launch threshold"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = local.operations_alarm_dimensions.reconciliation
}

resource "aws_cloudwatch_metric_alarm" "operations_provider_latency" {
  alarm_name          = "${local.resource_prefix}-operations-provider-latency-p99"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "ProviderLatencyMilliseconds"
  namespace           = local.operations_metric_namespace
  period              = 300
  extended_statistic  = "p99"
  threshold           = 2000
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif external provider p99 latency exceeds two seconds"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = local.operations_alarm_dimensions.provider
}

resource "aws_cloudwatch_metric_alarm" "operations_webhook_failures" {
  alarm_name          = "${local.resource_prefix}-operations-webhook-failures"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "WebhookFailures"
  namespace           = local.operations_metric_namespace
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif verified webhook processing is failing"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = local.operations_alarm_dimensions.webhook
}

resource "aws_cloudwatch_metric_alarm" "operations_recurring_schedule_failures" {
  alarm_name          = "${local.resource_prefix}-operations-recurring-schedule-failures"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "RecurringScheduleFailures"
  namespace           = local.operations_metric_namespace
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif recurring schedules are failing"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = local.operations_alarm_dimensions.schedule
}

resource "aws_cloudwatch_metric_alarm" "operations_render_failure_rate" {
  alarm_name          = "${local.resource_prefix}-operations-render-failure-rate"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "RenderFailureRate"
  namespace           = local.operations_metric_namespace
  period              = 300
  statistic           = "Average"
  threshold           = 10
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif document render failure rate exceeds ten percent"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = local.operations_alarm_dimensions.render
}

resource "aws_cloudwatch_metric_alarm" "operations_delivery_failure_rate" {
  alarm_name          = "${local.resource_prefix}-operations-delivery-failure-rate"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "DeliveryFailureRate"
  namespace           = local.operations_metric_namespace
  period              = 300
  statistic           = "Average"
  threshold           = 10
  treat_missing_data  = "notBreaching"
  alarm_description   = "Billeif message delivery failure rate exceeds ten percent"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
  dimensions          = local.operations_alarm_dimensions.delivery
}
