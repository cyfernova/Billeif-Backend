mock_provider "aws" {
  override_during = plan

  mock_data "aws_ami" {
    defaults = {
      id = "ami-0billeifnat"
    }
  }

  mock_data "aws_iam_policy_document" {
    override_during = plan
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "928282274753"
      arn        = "arn:aws:iam::928282274753:user/terraform-test"
      user_id    = "AIDATEST1234567890"
    }
  }

  override_data {
    target = data.aws_partition.current
    values = {
      partition = "aws"
    }
  }

  override_data {
    target = data.aws_availability_zone.voice_agentcore[0]
    values = {
      name    = "ap-south-1a"
      zone_id = "aps1-az1"
    }
  }

  override_data {
    target = data.aws_availability_zone.voice_agentcore[1]
    values = {
      name    = "ap-south-1b"
      zone_id = "aps1-az2"
    }
  }

  override_resource {
    target = aws_bedrockagentcore_agent_runtime.voice[0]
    values = {
      agent_runtime_arn     = "arn:aws:bedrock-agentcore:ap-south-1:928282274753:runtime/billeif_test_voice"
      agent_runtime_id      = "billeif-test-voice-runtime"
      agent_runtime_version = "1"
    }
  }

  override_resource {
    target = aws_bedrockagentcore_agent_runtime_endpoint.voice_staging[0]
    values = {
      agent_runtime_endpoint_arn = "arn:aws:bedrock-agentcore:ap-south-1:928282274753:runtime/billeif_test_voice/runtime-endpoint/STAGING"
      agent_runtime_version      = "2"
      name                       = "STAGING"
    }
  }

  override_resource {
    target = aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0]
    values = {
      agent_runtime_endpoint_arn = "arn:aws:bedrock-agentcore:ap-south-1:928282274753:runtime/billeif_test_voice/runtime-endpoint/PROD"
      agent_runtime_version      = "2"
      name                       = "PROD"
    }
  }

  override_resource {
    target = aws_apigatewayv2_api.http
    values = {
      id = "billeif-test-http-api"
    }
  }

  override_resource {
    target = aws_secretsmanager_secret.sarvam
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:billeif-test-sarvam"
    }
  }

  override_resource {
    target = aws_security_group.voice_agentcore[0]
    values = {
      id = "sg-voice-agentcore"
    }
  }

  override_resource {
    target = aws_subnet.private[0]
    values = {
      id = "subnet-private-a"
    }
  }

  override_resource {
    target = aws_subnet.private[1]
    values = {
      id = "subnet-private-b"
    }
  }

  override_resource {
    target = aws_kms_key.application_secrets
    values = {
      arn    = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
      key_id = "application-secrets"
    }
  }

  override_resource {
    target = aws_dynamodb_table.voice_sessions
    values = {
      arn  = "arn:aws:dynamodb:ap-south-1:928282274753:table/billeif-test-voice-sessions"
      name = "billeif-test-voice-sessions"
    }
  }

  override_resource {
    target = aws_lambda_function.voice_reconciler[0]
    values = {
      arn           = "arn:aws:lambda:ap-south-1:928282274753:function:billeif-test-voice-reconciler"
      function_name = "billeif-test-voice-reconciler"
    }
  }

  override_resource {
    target = aws_iam_role.voice_reconciler_scheduler[0]
    values = {
      arn  = "arn:aws:iam::928282274753:role/billeif-test-voice-reconciler-scheduler-role"
      id   = "billeif-test-voice-reconciler-scheduler-role"
      name = "billeif-test-voice-reconciler-scheduler-role"
    }
  }

  override_resource {
    target = aws_scheduler_schedule.voice_reconciler[0]
    values = {
      arn = "arn:aws:scheduler:ap-south-1:928282274753:schedule/default/billeif-test-voice-reconciler-minute"
    }
  }

  override_resource {
    target = aws_db_instance.main
    values = {
      address = "database.internal"
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret/rds-managed"
      }]
    }
  }

  override_resource {
    target = aws_instance.nat[0]
    values = {
      id                           = "i-billeifnat"
      primary_network_interface_id = "eni-billeifnat"
    }
  }
}

mock_provider "aws" {
  alias           = "ap_south_1"
  override_during = plan

  override_resource {
    target = aws_cognito_user_pool.phone
    values = {
      id = "ap-south-1_voicephone"
    }
  }

  override_resource {
    target = aws_cognito_user_pool_client.phone
    values = {
      id = "voice-phone-client"
    }
  }
}

mock_provider "aws" {
  alias           = "us_east_1"
  override_during = plan
}

mock_provider "awscc" {
  override_during = plan

  mock_resource "awscc_kinesisvideo_signaling_channel" {
    defaults = {
      arn  = "arn:aws:kinesisvideo:ap-south-1:928282274753:channel/mock/0"
      type = "SINGLE_MASTER"
    }
  }
}

mock_provider "external" {
  override_during = plan

  override_data {
    target = data.external.voice_agentcore_version[0]
    values = {
      result = {
        agent_runtime_version = "2"
      }
    }
  }
}

variables {
  project_name                          = "billeif-test"
  environment                           = "test"
  lambda_artifact_dir                   = "tests/fixtures/lambda"
  migration_lambda_artifact_path        = "tests/fixtures/lambda/http.zip"
  voice_reconciler_lambda_artifact_path = "tests/fixtures/lambda/http.zip"
  llm_api_url                           = "https://llm.example.test/chat/completions"
  llm_model                             = "test-model"
  deepseek_base_url                     = "https://voice-llm.example.test/v1"
  deepseek_model                        = "voice-test-model"
  ses_verified_identity                 = "billeif.example"
  ses_sender_email                      = "notifications@billeif.example"
  db_allowed_cidr                       = "10.0.0.0/24"
}

run "voice_observability_and_budgets_are_cost_safe_by_default" {
  command = plan

  assert {
    condition = (
      var.enable_voice_observability == false &&
      var.enable_voice_budgets == false &&
      length(aws_cloudwatch_log_group.voice_agentcore) == 0 &&
      length(aws_cloudwatch_metric_alarm.voice_latency) == 0 &&
      length(aws_cloudwatch_metric_alarm.voice_signal) == 0 &&
      length(aws_cloudwatch_metric_alarm.voice_agentcore_error) == 0 &&
      length(aws_cloudwatch_metric_alarm.voice_agentcore_active_sessions) == 0 &&
      length(aws_cloudwatch_metric_alarm.voice_agentcore_latency) == 0 &&
      length(aws_cloudwatch_metric_alarm.nat_conntrack_exhausted) == 0 &&
      length(aws_cloudwatch_metric_alarm.nat_network_drops) == 0 &&
      length(aws_cloudwatch_metric_alarm.nat_cpu_surplus_charged) == 0 &&
      length(aws_iam_role_policy.nat_cloudwatch_metrics) == 0 &&
      length(aws_budgets_budget.voice_daily) == 0 &&
      length(aws_budgets_budget.voice_monthly) == 0 &&
      !strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "EndOfSpeechToFirstAudioMilliseconds") &&
      !strcontains(aws_instance.nat[0].user_data, "amazon-cloudwatch-agent") &&
      !strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "amazon-cloudwatch-agent-ctl") &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "disable --now amazon-cloudwatch-agent")
    )
    error_message = "Voice observability, custom NAT metrics, and voice budgets must be opt-in; disabling observability must also stop an agent left by an earlier enablement."
  }
}

run "voice_observability_is_bounded_and_actionable" {
  command = plan

  variables {
    provision_voice_infrastructure      = true
    promote_voice_agentcore_prod        = true
    voice_agentcore_image_tag           = "prod-0123456789abcdef"
    voice_agentcore_image_digest        = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release             = "release-0123456789abcdef"
    voice_agentcore_prod_version        = "2"
    enable_voice_turn_udp_egress        = true
    enable_voice_observability          = true
    enable_voice_budgets                = true
    voice_cost_allocation_tag_activated = true
    voice_daily_budget_amount           = 5
    voice_monthly_budget_amount         = 50
    alert_email                         = "alerts@billeif.example"
  }

  assert {
    condition = (
      length(aws_cloudwatch_log_group.voice_agentcore) == 2 &&
      alltrue([for log_group in values(aws_cloudwatch_log_group.voice_agentcore) : log_group.retention_in_days == 7]) &&
      length(aws_cloudwatch_metric_alarm.voice_latency) == 5 &&
      length(aws_cloudwatch_metric_alarm.voice_signal) == 6 &&
      length(aws_cloudwatch_metric_alarm.voice_agentcore_error) == 2 &&
      length(aws_cloudwatch_metric_alarm.voice_agentcore_active_sessions) == 1 &&
      length(aws_cloudwatch_metric_alarm.voice_agentcore_latency) == 1
    )
    error_message = "Enabled voice observability must retain both endpoint logs for seven non-production days and cover latency, application, and AgentCore signals."
  }

  assert {
    condition = alltrue([for alarm in values(aws_cloudwatch_metric_alarm.voice_latency) : (
      alarm.evaluation_periods == 3 &&
      alarm.datapoints_to_alarm == 2 &&
      alarm.extended_statistic == "p95" &&
      alarm.statistic == null &&
      alarm.treat_missing_data == "notBreaching"
    )])
    error_message = "Voice application latency alarms must use p95, explicit 2-of-3 evaluation, and explicit missing-data behavior."
  }

  assert {
    condition = alltrue([for alarm in values(aws_cloudwatch_metric_alarm.voice_signal) : (
      alarm.evaluation_periods == 3 &&
      alarm.datapoints_to_alarm == 2 &&
      alarm.metric_name == null &&
      alarm.treat_missing_data == "notBreaching"
    )])
    error_message = "Voice signal alarms must use metric math, explicit 2-of-3 evaluation, and explicit missing-data behavior."
  }

  assert {
    condition = alltrue([for alarm in values(aws_cloudwatch_metric_alarm.voice_agentcore_error) : (
      alarm.evaluation_periods == 3 &&
      alarm.datapoints_to_alarm == 2 &&
      alarm.statistic == "Sum" &&
      alarm.treat_missing_data == "notBreaching" &&
      length(alarm.dimensions) == 2 &&
      alarm.dimensions.Service == "AgentCore.Runtime" &&
      alarm.dimensions.Resource == aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn
    )])
    error_message = "AgentCore error alarms must use the exact Service and runtime Resource metric identity with explicit evaluation behavior."
  }

  assert {
    condition = alltrue([for alarm in aws_cloudwatch_metric_alarm.voice_agentcore_latency : (
      alarm.evaluation_periods == 3 &&
      alarm.datapoints_to_alarm == 2 &&
      alarm.extended_statistic == "p99" &&
      alarm.statistic == null &&
      alarm.treat_missing_data == "notBreaching" &&
      length(alarm.dimensions) == 2 &&
      alarm.dimensions.Service == "AgentCore.Runtime" &&
      alarm.dimensions.Resource == aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn
    )])
    error_message = "AgentCore latency alarms must use p99 and the exact Service and runtime Resource metric identity."
  }

  assert {
    condition = alltrue([
      for metric_name, alarm in aws_cloudwatch_metric_alarm.voice_signal : length([
        for query in alarm.metric_query : query if length([
          for metric in query.metric : metric if metric.dimensions.Service == (metric_name == "SessionLeaks" ? "voice-reconciler" : "voice-runtime")
        ]) == 1
      ]) == 3
    ])
    error_message = "Custom signal alarms must select the exact emitting component: SessionLeaks from voice-reconciler and runtime/provider signals from voice-runtime."
  }

  assert {
    condition = alltrue([
      for metric in [
        ["AWS/Bedrock-AgentCore", "SystemErrors", "Service", "AgentCore.Runtime", "Resource", aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, { stat = "Sum" }],
        ["AWS/Bedrock-AgentCore", "Throttles", "Service", "AgentCore.Runtime", "Resource", aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, { stat = "Sum" }],
        ["AWS/Bedrock-AgentCore", "Latency", "Service", "AgentCore.Runtime", "Resource", aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, { stat = "p99" }],
      ] : strcontains(aws_cloudwatch_dashboard.main.dashboard_body, jsonencode(metric))
    ])
    error_message = "AgentCore error and latency dashboard series must include both Service=AgentCore.Runtime and the runtime Resource ARN."
  }

  assert {
    condition = (
      alltrue(flatten([
        for metric_name in sort(keys(local.voice_signal_alarm_thresholds)) : [
          for outcome in ["success", "error", "cancelled"] : strcontains(
            aws_cloudwatch_dashboard.main.dashboard_body,
            jsonencode([local.voice_metric_namespace, metric_name, "Environment", var.environment, "Service", metric_name == "SessionLeaks" ? "voice-reconciler" : "voice-runtime", "Outcome", outcome, { label = "${metric_name} ${outcome}", stat = "Sum" }])
          )
        ]
      ])) &&
      !strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "\"ActiveSessions\"") &&
      strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "AgentCore Active Sessions (account-wide)")
    )
    error_message = "The dashboard must match each custom signal to its emitter, omit the retired custom ActiveSessions series, and label AgentCore ActiveSessionCount as account-wide."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.voice_latency["EndOfSpeechToFirstAudioMilliseconds"].threshold == 2000 &&
      aws_cloudwatch_metric_alarm.voice_latency["EndOfSpeechToFirstAudioMilliseconds"].comparison_operator == "GreaterThanThreshold"
    )
    error_message = "End-of-speech to client-first-audio p95 must alarm above the plan's 2,000 ms launch gate."
  }

  assert {
    condition = (
      length(aws_cloudwatch_metric_alarm.nat_conntrack_exhausted) == 1 &&
      length(aws_cloudwatch_metric_alarm.nat_network_drops) == 2 &&
      length(aws_cloudwatch_metric_alarm.nat_cpu_surplus_charged) == 1
    )
    error_message = "Opt-in instance NAT telemetry must create the bounded conntrack, network-drop, and surplus-credit alarms."
  }

  assert {
    condition = alltrue([for alarm in aws_cloudwatch_metric_alarm.nat_conntrack_exhausted : (
      alarm.evaluation_periods == 3 &&
      alarm.datapoints_to_alarm == 2 &&
      alarm.treat_missing_data == "notBreaching" &&
      alarm.metric_name == null &&
      length([for query in alarm.metric_query : query if(
        query.id == "rate" &&
        query.expression == "RATE(counter)" &&
        query.return_data
      )]) == 1 &&
      length([for query in alarm.metric_query : query if(
        query.id == "counter" &&
        !query.return_data &&
        length([for metric in query.metric : metric if(
          metric.metric_name == "ethtool_conntrack_allowance_exceeded" &&
          metric.namespace == "Billeif/NAT" &&
          metric.period == 60 &&
          metric.stat == "Maximum" &&
          length(metric.dimensions) == 2 &&
          metric.dimensions.InstanceId == aws_instance.nat[0].id &&
          metric.dimensions.interface == "eth0"
        )]) == 1
      )]) == 1
    )])
    error_message = "The conntrack alarm must evaluate RATE(counter) over the exact 60-second cumulative ENA counter so it can recover."
  }

  assert {
    condition = alltrue(concat(
      [for alarm in values(aws_cloudwatch_metric_alarm.nat_network_drops) : alarm.evaluation_periods == 3 && alarm.datapoints_to_alarm == 2 && alarm.treat_missing_data == "notBreaching"],
      [for alarm in aws_cloudwatch_metric_alarm.nat_cpu_surplus_charged : alarm.evaluation_periods == 3 && alarm.datapoints_to_alarm == 2 && alarm.treat_missing_data == "notBreaching"]
    ))
    error_message = "NAT drop and surplus-credit alarms must use explicit 2-of-3 evaluation and explicit missing-data behavior."
  }

  assert {
    condition = (
      length(aws_iam_role_policy.nat_cloudwatch_metrics) == 1 &&
      strcontains(aws_instance.nat[0].user_data, "amazon-cloudwatch-agent") &&
      strcontains(aws_instance.nat[0].user_data, "conntrack_allowance_exceeded") &&
      strcontains(aws_instance.nat[0].user_data, "drop_in") &&
      strcontains(aws_instance.nat[0].user_data, "drop_out") &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "amazon-cloudwatch-agent-ctl") &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "billeif-nat.json")
    )
    error_message = "Opt-in NAT telemetry must install the CloudWatch agent and grant least-privilege custom metric publication."
  }

  assert {
    condition = (
      alltrue([
        for source in [
          { metric_name = "ethtool_conntrack_allowance_exceeded", counter_id = "nat_conntrack_counter" },
          { metric_name = "ethtool_bw_in_allowance_exceeded", counter_id = "nat_bw_in_counter" },
          { metric_name = "ethtool_bw_out_allowance_exceeded", counter_id = "nat_bw_out_counter" },
          { metric_name = "ethtool_linklocal_allowance_exceeded", counter_id = "nat_linklocal_counter" },
          { metric_name = "ethtool_pps_allowance_exceeded", counter_id = "nat_pps_counter" },
          ] : strcontains(
          aws_cloudwatch_dashboard.main.dashboard_body,
          jsonencode(["Billeif/NAT", source.metric_name, "InstanceId", aws_instance.nat[0].id, "interface", "eth0", { id = source.counter_id, period = 60, stat = "Maximum", visible = false }])
        )
      ]) &&
      strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "RATE(nat_conntrack_counter)") &&
      strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "RATE(nat_bw_in_counter)") &&
      strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "RATE(nat_bw_out_counter)") &&
      strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "RATE(nat_linklocal_counter)") &&
      strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "RATE(nat_pps_counter)")
    )
    error_message = "The NAT dashboard must derive recoverable rates from hidden 60-second Maximum series for every cumulative ENA allowance counter."
  }

  assert {
    condition = alltrue([
      for metric_name in [
        "EndOfSpeechToFirstAudioMilliseconds", "STTFinalLatencyMilliseconds",
        "LLMFirstTokenLatencyMilliseconds", "TTSFirstAudioLatencyMilliseconds",
        "InterruptToPlaybackStopMilliseconds", "SessionLeaks",
        "KVSAllocationErrors", "ICERestarts", "Sarvam429", "Sarvam503", "DurabilityFailures",
        "InputTokens", "OutputTokens", "TTSCharacters", "STTSeconds",
        "ActiveSessionCount", "SystemErrors", "Throttles", "Latency",
        "CPUUsed-vCPUHours", "MemoryUsed-GBHours", "CPUUtilization",
        "CPUCreditBalance", "CPUSurplusCreditsCharged",
        "ethtool_conntrack_allowance_exceeded", "net_drop_in", "net_drop_out"
      ] : strcontains(aws_cloudwatch_dashboard.main.dashboard_body, metric_name)
    ])
    error_message = "The existing dashboard must include every required application, usage, AgentCore, and NAT health signal."
  }

  assert {
    condition = (
      aws_ecr_repository.voice_agentcore[0].tags.Workload == local.voice_cost_allocation_tag_value &&
      aws_security_group.voice_agentcore[0].tags.Workload == local.voice_cost_allocation_tag_value &&
      aws_bedrockagentcore_agent_runtime.voice[0].tags.Workload == local.voice_cost_allocation_tag_value &&
      aws_bedrockagentcore_agent_runtime_endpoint.voice_staging[0].tags.Workload == local.voice_cost_allocation_tag_value &&
      aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0].tags.Workload == local.voice_cost_allocation_tag_value &&
      alltrue([for channel in awscc_kinesisvideo_signaling_channel.voice : length([for tag in channel.tags : tag if tag.key == local.voice_cost_allocation_tag_key && tag.value == local.voice_cost_allocation_tag_value]) == 1]) &&
      aws_dynamodb_table.voice_sessions.tags.Workload == local.voice_cost_allocation_tag_value &&
      aws_lambda_function.voice_reconciler[0].tags.Workload == local.voice_cost_allocation_tag_value &&
      alltrue([for log_group in values(aws_cloudwatch_log_group.voice_agentcore) : log_group.tags.Workload == local.voice_cost_allocation_tag_value])
    )
    error_message = "Every budget-scoped voice runtime, endpoint, KVS, state, image, reconciler, and log resource must carry the exact shared workload tag."
  }

  assert {
    condition = (
      length(aws_budgets_budget.voice_daily) == 1 &&
      length(aws_budgets_budget.voice_monthly) == 1 &&
      aws_budgets_budget.voice_daily[0].time_unit == "DAILY" &&
      aws_budgets_budget.voice_monthly[0].time_unit == "MONTHLY" &&
      length([for filter in aws_budgets_budget.voice_daily[0].cost_filter : filter if filter.name == "TagKeyValue" && toset(filter.values) == toset([local.voice_budget_tag_filter])]) == 1 &&
      length([for filter in aws_budgets_budget.voice_monthly[0].cost_filter : filter if filter.name == "TagKeyValue" && toset(filter.values) == toset([local.voice_budget_tag_filter])]) == 1 &&
      local.voice_budget_tag_filter == "user:Workload$voice-agentcore"
    )
    error_message = "Voice budgets must create daily and monthly alerts against the activated exact Workload tag filter."
  }
}

run "managed_nat_voice_observability_omits_instance_only_widgets" {
  command = plan

  variables {
    egress_mode                    = "managed_nat"
    provision_voice_infrastructure = true
    voice_agentcore_image_tag      = "prod-0123456789abcdef"
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release        = "release-0123456789abcdef"
    enable_voice_turn_udp_egress   = true
    enable_voice_observability     = true
  }

  assert {
    condition = (
      length(aws_instance.nat) == 0 &&
      length(aws_cloudwatch_metric_alarm.nat_conntrack_exhausted) == 0 &&
      length(aws_cloudwatch_metric_alarm.nat_network_drops) == 0 &&
      length(aws_cloudwatch_metric_alarm.nat_cpu_surplus_charged) == 0 &&
      length(aws_iam_role_policy.nat_cloudwatch_metrics) == 0 &&
      strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "AgentCore Runtime Health") &&
      !strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "NAT Capacity and Drops") &&
      !strcontains(aws_cloudwatch_dashboard.main.dashboard_body, "ethtool_conntrack_allowance_exceeded")
    )
    error_message = "Managed-NAT voice observability must keep the general voice widgets while omitting NAT-instance-only resources, metrics, and alarms."
  }
}

run "production_voice_logs_retain_fourteen_days" {
  command = plan

  variables {
    environment                    = "prod"
    provision_voice_infrastructure = true
    voice_agentcore_image_tag      = "prod-0123456789abcdef"
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release        = "release-0123456789abcdef"
    enable_voice_turn_udp_egress   = true
  }

  assert {
    condition = (
      length(aws_cloudwatch_log_group.voice_agentcore) == 1 &&
      alltrue([for log_group in values(aws_cloudwatch_log_group.voice_agentcore) : log_group.retention_in_days == 14]) &&
      aws_cloudwatch_log_group.lambda_voice_reconciler[0].retention_in_days == 14
    )
    error_message = "Production voice runtime and reconciler logs must retain exactly fourteen days."
  }
}

run "voice_budgets_fail_closed_until_cost_tag_is_activated" {
  command = plan

  variables {
    provision_voice_infrastructure      = true
    voice_agentcore_image_tag           = "prod-0123456789abcdef"
    voice_agentcore_image_digest        = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release             = "release-0123456789abcdef"
    enable_voice_turn_udp_egress        = true
    enable_voice_budgets                = true
    voice_cost_allocation_tag_activated = false
    voice_daily_budget_amount           = 5
    voice_monthly_budget_amount         = 50
    alert_email                         = "alerts@billeif.example"
  }

  expect_failures = [
    aws_budgets_budget.voice_daily,
    aws_budgets_budget.voice_monthly,
  ]
}
