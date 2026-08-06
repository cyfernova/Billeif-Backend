variable "enable_voice_observability" {
  description = "Enable bounded Billeif voice metrics, alarms, dashboard widgets, and NAT custom metrics. Off by default to avoid unreviewed CloudWatch spend."
  type        = bool
  default     = false
}

variable "enable_voice_budgets" {
  description = "Create daily and monthly AWS Budgets for resources carrying the activated Billeif voice workload cost-allocation tag."
  type        = bool
  default     = false
}

variable "voice_cost_allocation_tag_activated" {
  description = "Operator confirmation that the Workload user-defined cost-allocation tag is active in Billing. Budget creation fails closed until confirmed."
  type        = bool
  default     = false
}

variable "voice_daily_budget_amount" {
  description = "Reviewed daily voice budget limit in USD. Zero is a safe disabled default and is rejected when voice budgets are enabled."
  type        = number
  default     = 0

  validation {
    condition     = var.voice_daily_budget_amount >= 0
    error_message = "voice_daily_budget_amount must not be negative."
  }
}

variable "voice_monthly_budget_amount" {
  description = "Reviewed monthly voice budget limit in USD. Zero is a safe disabled default and is rejected when voice budgets are enabled."
  type        = number
  default     = 0

  validation {
    condition     = var.voice_monthly_budget_amount >= 0
    error_message = "voice_monthly_budget_amount must not be negative."
  }
}

locals {
  voice_metric_namespace               = "Billeif/Voice"
  voice_service_dimension              = "voice-runtime"
  voice_observability_enabled          = var.provision_voice_infrastructure && var.enable_voice_observability
  voice_log_retention_days             = var.environment == "prod" ? 14 : 7
  voice_cost_allocation_tag_key        = "Workload"
  voice_cost_allocation_tag_value      = "voice-agentcore"
  voice_budget_tag_filter              = format("user:%s$%s", local.voice_cost_allocation_tag_key, local.voice_cost_allocation_tag_value)
  voice_agentcore_endpoint_metric_name = "${local.voice_agentcore_runtime_name}::${var.promote_voice_agentcore_prod ? "PROD" : "STAGING"}"
  voice_outcome_metric_ids = {
    success   = "s"
    error     = "e"
    cancelled = "c"
  }

  voice_latency_alarm_thresholds = {
    EndOfSpeechToFirstAudioMilliseconds = 2000
    STTFinalLatencyMilliseconds         = 1000
    LLMFirstTokenLatencyMilliseconds    = 1500
    TTSFirstAudioLatencyMilliseconds    = 1000
    InterruptToPlaybackStopMilliseconds = 150
  }

  voice_signal_alarm_thresholds = {
    SessionLeaks        = 0
    KVSAllocationErrors = 0
    ICERestarts         = 5
    Sarvam429           = 2
    Sarvam503           = 0
    DurabilityFailures  = 0
  }

  voice_signal_service_dimensions = {
    SessionLeaks        = "voice-reconciler"
    KVSAllocationErrors = local.voice_service_dimension
    ICERestarts         = local.voice_service_dimension
    Sarvam429           = local.voice_service_dimension
    Sarvam503           = local.voice_service_dimension
    DurabilityFailures  = local.voice_service_dimension
  }

  voice_agentcore_error_metrics = toset([
    "SystemErrors",
    "Throttles",
  ])

  voice_usage_metrics = toset([
    "InputTokens",
    "OutputTokens",
    "TTSCharacters",
    "STTSeconds",
  ])

  nat_cloudwatch_agent_config = jsonencode({
    agent = {
      metrics_collection_interval = 60
    }
    metrics = {
      namespace = "Billeif/NAT"
      append_dimensions = {
        InstanceId = "$${aws:InstanceId}"
      }
      metrics_collected = {
        net = {
          measurement = [
            "bytes_recv",
            "bytes_sent",
            "drop_in",
            "drop_out",
          ]
          metrics_collection_interval = 60
          resources                   = ["eth0"]
        }
        ethtool = {
          interface_include = ["eth0"]
          metrics_include = [
            "bw_in_allowance_exceeded",
            "bw_out_allowance_exceeded",
            "conntrack_allowance_exceeded",
            "linklocal_allowance_exceeded",
            "pps_allowance_exceeded",
          ]
        }
      }
    }
  })

  voice_dashboard_widgets = jsondecode(local.voice_observability_enabled ? jsonencode(concat([
    {
      type   = "metric"
      x      = 0
      y      = 12
      width  = 24
      height = 6
      properties = {
        title  = "Voice Turn Latency p95 and p99"
        region = var.aws_region
        period = 60
        metrics = concat(
          [for metric_name in sort(keys(local.voice_latency_alarm_thresholds)) : [
            local.voice_metric_namespace,
            metric_name,
            "Environment",
            var.environment,
            "Service",
            local.voice_service_dimension,
            "Outcome",
            "success",
            { label = "${metric_name} p95", stat = "p95" },
          ]],
          [for metric_name in sort(keys(local.voice_latency_alarm_thresholds)) : [
            local.voice_metric_namespace,
            metric_name,
            "Environment",
            var.environment,
            "Service",
            local.voice_service_dimension,
            "Outcome",
            "success",
            { label = "${metric_name} p99", stat = "p99" },
          ]],
        )
      }
    },
    {
      type   = "metric"
      x      = 0
      y      = 18
      width  = 12
      height = 6
      properties = {
        title  = "Voice Session and Provider Health"
        region = var.aws_region
        period = 60
        metrics = concat(
          [for metric_name in sort(keys(local.voice_signal_alarm_thresholds)) : [local.voice_metric_namespace, metric_name, "Environment", var.environment, "Service", local.voice_signal_service_dimensions[metric_name], "Outcome", "success", { label = "${metric_name} success", stat = "Sum" }]],
          [for metric_name in sort(keys(local.voice_signal_alarm_thresholds)) : [local.voice_metric_namespace, metric_name, "Environment", var.environment, "Service", local.voice_signal_service_dimensions[metric_name], "Outcome", "error", { label = "${metric_name} error", stat = "Sum" }]],
          [for metric_name in sort(keys(local.voice_signal_alarm_thresholds)) : [local.voice_metric_namespace, metric_name, "Environment", var.environment, "Service", local.voice_signal_service_dimensions[metric_name], "Outcome", "cancelled", { label = "${metric_name} cancelled", stat = "Sum" }]],
        )
      }
    },
    {
      type   = "metric"
      x      = 12
      y      = 18
      width  = 12
      height = 6
      properties = {
        title  = "Voice Metered Units per Turn"
        region = var.aws_region
        period = 300
        metrics = concat(
          [for metric_name in sort(tolist(local.voice_usage_metrics)) : [local.voice_metric_namespace, metric_name, "Environment", var.environment, "Service", local.voice_service_dimension, "Outcome", "success", { label = "${metric_name} success", stat = "Sum" }]],
          [for metric_name in sort(tolist(local.voice_usage_metrics)) : [local.voice_metric_namespace, metric_name, "Environment", var.environment, "Service", local.voice_service_dimension, "Outcome", "error", { label = "${metric_name} error", stat = "Sum" }]],
          [for metric_name in sort(tolist(local.voice_usage_metrics)) : [local.voice_metric_namespace, metric_name, "Environment", var.environment, "Service", local.voice_service_dimension, "Outcome", "cancelled", { label = "${metric_name} cancelled", stat = "Sum" }]],
        )
      }
    },
    {
      type   = "metric"
      x      = 0
      y      = 24
      width  = 12
      height = 6
      properties = {
        title  = "AgentCore Runtime Health"
        region = var.aws_region
        period = 60
        metrics = [
          ["AWS/Bedrock-AgentCore", "ActiveSessionCount", "Service", "AgentCore.Runtime", { label = "AgentCore Active Sessions (account-wide)", stat = "Maximum" }],
          ["AWS/Bedrock-AgentCore", "SystemErrors", "Service", "AgentCore.Runtime", "Resource", aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, { stat = "Sum" }],
          ["AWS/Bedrock-AgentCore", "Throttles", "Service", "AgentCore.Runtime", "Resource", aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, { stat = "Sum" }],
          ["AWS/Bedrock-AgentCore", "Latency", "Service", "AgentCore.Runtime", "Resource", aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, { stat = "p99" }],
          ["AWS/Bedrock-AgentCore", "CPUUsed-vCPUHours", "Service", "AgentCore.Runtime", "Resource", aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, "Name", local.voice_agentcore_endpoint_metric_name, { stat = "Sum" }],
          ["AWS/Bedrock-AgentCore", "MemoryUsed-GBHours", "Service", "AgentCore.Runtime", "Resource", aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, "Name", local.voice_agentcore_endpoint_metric_name, { stat = "Sum" }],
        ]
      }
    },
    ], local.nat_instance_enabled ? [
    {
      type   = "metric"
      x      = 12
      y      = 24
      width  = 12
      height = 6
      properties = {
        title  = "NAT Capacity and Drops"
        region = var.aws_region
        period = 300
        metrics = [
          ["AWS/EC2", "CPUUtilization", "InstanceId", aws_instance.nat[0].id, { stat = "Maximum" }],
          ["AWS/EC2", "CPUCreditBalance", "InstanceId", aws_instance.nat[0].id, { stat = "Minimum" }],
          ["AWS/EC2", "CPUSurplusCreditBalance", "InstanceId", aws_instance.nat[0].id, { stat = "Maximum" }],
          ["AWS/EC2", "CPUSurplusCreditsCharged", "InstanceId", aws_instance.nat[0].id, { stat = "Sum" }],
          ["Billeif/NAT", "ethtool_conntrack_allowance_exceeded", "InstanceId", aws_instance.nat[0].id, "interface", "eth0", { id = "nat_conntrack_counter", period = 60, stat = "Maximum", visible = false }],
          [{ expression = "RATE(nat_conntrack_counter)", id = "nat_conntrack_rate", label = "Conntrack allowance exceeded per second" }],
          ["Billeif/NAT", "ethtool_bw_in_allowance_exceeded", "InstanceId", aws_instance.nat[0].id, "interface", "eth0", { id = "nat_bw_in_counter", period = 60, stat = "Maximum", visible = false }],
          [{ expression = "RATE(nat_bw_in_counter)", id = "nat_bw_in_rate", label = "Inbound bandwidth allowance exceeded per second" }],
          ["Billeif/NAT", "ethtool_bw_out_allowance_exceeded", "InstanceId", aws_instance.nat[0].id, "interface", "eth0", { id = "nat_bw_out_counter", period = 60, stat = "Maximum", visible = false }],
          [{ expression = "RATE(nat_bw_out_counter)", id = "nat_bw_out_rate", label = "Outbound bandwidth allowance exceeded per second" }],
          ["Billeif/NAT", "ethtool_linklocal_allowance_exceeded", "InstanceId", aws_instance.nat[0].id, "interface", "eth0", { id = "nat_linklocal_counter", period = 60, stat = "Maximum", visible = false }],
          [{ expression = "RATE(nat_linklocal_counter)", id = "nat_linklocal_rate", label = "Link-local allowance exceeded per second" }],
          ["Billeif/NAT", "ethtool_pps_allowance_exceeded", "InstanceId", aws_instance.nat[0].id, "interface", "eth0", { id = "nat_pps_counter", period = 60, stat = "Maximum", visible = false }],
          [{ expression = "RATE(nat_pps_counter)", id = "nat_pps_rate", label = "Packet-rate allowance exceeded per second" }],
          ["Billeif/NAT", "net_drop_in", "InstanceId", aws_instance.nat[0].id, "interface", "eth0", { stat = "Sum" }],
          ["Billeif/NAT", "net_drop_out", "InstanceId", aws_instance.nat[0].id, "interface", "eth0", { stat = "Sum" }],
        ]
      }
    },
  ] : [])) : "[]")
}

resource "aws_cloudwatch_log_group" "voice_agentcore" {
  for_each = var.provision_voice_infrastructure ? setunion(toset(["STAGING"]), var.promote_voice_agentcore_prod ? toset(["PROD"]) : toset([])) : toset([])

  name              = "/aws/bedrock-agentcore/runtimes/${aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_id}-${each.value}"
  retention_in_days = local.voice_log_retention_days

  tags = {
    Name     = "${local.resource_prefix}-voice-${lower(each.value)}-logs"
    Workload = local.voice_cost_allocation_tag_value
  }

  depends_on = [aws_bedrockagentcore_agent_runtime.voice]
}

resource "aws_cloudwatch_metric_alarm" "voice_latency" {
  for_each = local.voice_observability_enabled ? local.voice_latency_alarm_thresholds : {}

  alarm_name          = "${local.resource_prefix}-voice-${lower(replace(each.key, "Milliseconds", ""))}-p95"
  alarm_description   = "Billeif voice ${each.key} p95 breached its initial launch guardrail; validate the threshold after load testing"
  namespace           = local.voice_metric_namespace
  metric_name         = each.key
  extended_statistic  = "p95"
  comparison_operator = "GreaterThanThreshold"
  threshold           = each.value
  period              = 60
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  unit                = "Milliseconds"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    Environment = var.environment
    Service     = local.voice_service_dimension
    Outcome     = "success"
  }

  tags = {
    Workload = local.voice_cost_allocation_tag_value
  }
}

resource "aws_cloudwatch_metric_alarm" "voice_signal" {
  for_each = local.voice_observability_enabled ? local.voice_signal_alarm_thresholds : {}

  alarm_name          = "${local.resource_prefix}-voice-${lower(each.key)}"
  alarm_description   = "Billeif voice ${each.key} aggregate breached its launch guardrail across all bounded outcomes"
  comparison_operator = "GreaterThanThreshold"
  threshold           = each.value
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dynamic "metric_query" {
    for_each = local.voice_outcome_metric_ids

    content {
      id          = metric_query.value
      return_data = false

      metric {
        metric_name = each.key
        namespace   = local.voice_metric_namespace
        period      = 60
        stat        = "Sum"
        unit        = "Count"
        dimensions = {
          Environment = var.environment
          Service     = local.voice_signal_service_dimensions[each.key]
          Outcome     = metric_query.key
        }
      }
    }
  }

  metric_query {
    id          = "total"
    expression  = "SUM([s,e,c])"
    label       = "${each.key} all outcomes"
    return_data = true
  }

  tags = {
    Workload = local.voice_cost_allocation_tag_value
  }
}

resource "aws_cloudwatch_metric_alarm" "voice_agentcore_error" {
  for_each = local.voice_observability_enabled ? local.voice_agentcore_error_metrics : toset([])

  alarm_name          = "${local.resource_prefix}-agentcore-${lower(each.value)}"
  alarm_description   = "Billeif AgentCore runtime reported ${each.value}"
  namespace           = "AWS/Bedrock-AgentCore"
  metric_name         = each.value
  statistic           = "Sum"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0
  period              = 60
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    Service  = "AgentCore.Runtime"
    Resource = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn
  }

  tags = {
    Workload = local.voice_cost_allocation_tag_value
  }
}

resource "aws_cloudwatch_metric_alarm" "voice_agentcore_active_sessions" {
  count = local.voice_observability_enabled ? 1 : 0

  alarm_name          = "${local.resource_prefix}-agentcore-active-sessions"
  alarm_description   = "Account-wide AgentCore.Runtime active sessions reached the initial 80-session operational guardrail"
  namespace           = "AWS/Bedrock-AgentCore"
  metric_name         = "ActiveSessionCount"
  statistic           = "Maximum"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = 80
  period              = 60
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "breaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    Service = "AgentCore.Runtime"
  }

  tags = {
    Workload = local.voice_cost_allocation_tag_value
  }
}

resource "aws_cloudwatch_metric_alarm" "voice_agentcore_latency" {
  count = local.voice_observability_enabled ? 1 : 0

  alarm_name          = "${local.resource_prefix}-agentcore-latency-p99"
  alarm_description   = "Billeif AgentCore runtime p99 request latency exceeded the initial five-second guardrail"
  namespace           = "AWS/Bedrock-AgentCore"
  metric_name         = "Latency"
  extended_statistic  = "p99"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 5000
  period              = 60
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    Service  = "AgentCore.Runtime"
    Resource = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn
  }

  tags = {
    Workload = local.voice_cost_allocation_tag_value
  }
}

data "aws_iam_policy_document" "nat_cloudwatch_metrics" {
  count = local.voice_observability_enabled && local.nat_instance_enabled ? 1 : 0

  statement {
    sid       = "PublishBoundedNATMetrics"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["Billeif/NAT"]
    }
  }
}

resource "aws_iam_role_policy" "nat_cloudwatch_metrics" {
  count = local.voice_observability_enabled && local.nat_instance_enabled ? 1 : 0

  name   = "${local.resource_prefix}-nat-cloudwatch-metrics"
  role   = aws_iam_role.nat_instance[0].id
  policy = data.aws_iam_policy_document.nat_cloudwatch_metrics[0].json
}

resource "aws_cloudwatch_metric_alarm" "nat_conntrack_exhausted" {
  count = local.voice_observability_enabled && local.nat_instance_enabled ? 1 : 0

  alarm_name          = "${local.resource_prefix}-nat-conntrack-exhausted"
  alarm_description   = "Billeif NAT dropped packets after exhausting its ENA connection tracking allowance"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  metric_query {
    id          = "counter"
    return_data = false

    metric {
      metric_name = "ethtool_conntrack_allowance_exceeded"
      namespace   = "Billeif/NAT"
      period      = 60
      stat        = "Maximum"

      dimensions = {
        InstanceId = aws_instance.nat[0].id
        interface  = "eth0"
      }
    }
  }

  metric_query {
    id          = "rate"
    expression  = "RATE(counter)"
    label       = "Conntrack allowance exceeded per second"
    return_data = true
  }
}

resource "aws_cloudwatch_metric_alarm" "nat_network_drops" {
  for_each = local.voice_observability_enabled && local.nat_instance_enabled ? toset(["net_drop_in", "net_drop_out"]) : toset([])

  alarm_name          = "${local.resource_prefix}-nat-${replace(each.value, "_", "-")}"
  alarm_description   = "Billeif NAT network interface reported ${each.value}"
  namespace           = "Billeif/NAT"
  metric_name         = each.value
  statistic           = "Sum"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0
  period              = 60
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    InstanceId = aws_instance.nat[0].id
    interface  = "eth0"
  }
}

resource "aws_cloudwatch_metric_alarm" "nat_cpu_surplus_charged" {
  count = local.voice_observability_enabled && local.nat_instance_enabled ? 1 : 0

  alarm_name          = "${local.resource_prefix}-nat-cpu-surplus-charged"
  alarm_description   = "Billeif NAT reported surplus CPU credits charged; verify its credit mode and workload"
  namespace           = "AWS/EC2"
  metric_name         = "CPUSurplusCreditsCharged"
  statistic           = "Sum"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0
  period              = 300
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    InstanceId = aws_instance.nat[0].id
  }
}

locals {
  voice_daily_budget_notifications = {
    actual_80  = 80
    actual_100 = 100
  }
  voice_monthly_budget_notifications = {
    actual_80 = {
      notification_type = "ACTUAL"
      threshold         = 80
    }
    actual_100 = {
      notification_type = "ACTUAL"
      threshold         = 100
    }
    forecasted_100 = {
      notification_type = "FORECASTED"
      threshold         = 100
    }
  }
}

resource "aws_budgets_budget" "voice_daily" {
  provider = aws.us_east_1
  count    = var.provision_voice_infrastructure && var.enable_voice_budgets ? 1 : 0

  name         = "${local.resource_prefix}-voice-daily"
  budget_type  = "COST"
  limit_amount = tostring(var.voice_daily_budget_amount)
  limit_unit   = "USD"
  time_unit    = "DAILY"

  cost_filter {
    name   = "TagKeyValue"
    values = [local.voice_budget_tag_filter]
  }

  dynamic "notification" {
    for_each = local.voice_daily_budget_notifications

    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value
      threshold_type             = "PERCENTAGE"
      notification_type          = "ACTUAL"
      subscriber_email_addresses = [var.alert_email]
    }
  }

  lifecycle {
    precondition {
      condition     = var.voice_cost_allocation_tag_activated
      error_message = "Activate the Workload user-defined cost-allocation tag in AWS Billing and set voice_cost_allocation_tag_activated=true before creating voice budgets."
    }

    precondition {
      condition     = var.voice_daily_budget_amount > 0
      error_message = "voice_daily_budget_amount must be a reviewed positive USD limit before enabling voice budgets."
    }

    precondition {
      condition     = trimspace(var.alert_email) != ""
      error_message = "alert_email is required before enabling voice budgets."
    }
  }
}

resource "aws_budgets_budget" "voice_monthly" {
  provider = aws.us_east_1
  count    = var.provision_voice_infrastructure && var.enable_voice_budgets ? 1 : 0

  name         = "${local.resource_prefix}-voice-monthly"
  budget_type  = "COST"
  limit_amount = tostring(var.voice_monthly_budget_amount)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  cost_filter {
    name   = "TagKeyValue"
    values = [local.voice_budget_tag_filter]
  }

  dynamic "notification" {
    for_each = local.voice_monthly_budget_notifications

    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value.threshold
      threshold_type             = "PERCENTAGE"
      notification_type          = notification.value.notification_type
      subscriber_email_addresses = [var.alert_email]
    }
  }

  lifecycle {
    precondition {
      condition     = var.voice_cost_allocation_tag_activated
      error_message = "Activate the Workload user-defined cost-allocation tag in AWS Billing and set voice_cost_allocation_tag_activated=true before creating voice budgets."
    }

    precondition {
      condition     = var.voice_monthly_budget_amount > 0
      error_message = "voice_monthly_budget_amount must be a reviewed positive USD limit before enabling voice budgets."
    }

    precondition {
      condition     = trimspace(var.alert_email) != ""
      error_message = "alert_email is required before enabling voice budgets."
    }
  }
}
