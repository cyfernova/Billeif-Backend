locals {
  voice_reconciler_artifact_path = var.voice_reconciler_lambda_artifact_path != "" ? var.voice_reconciler_lambda_artifact_path : "${var.lambda_artifact_dir}/voice-reconciler.zip"
  voice_reconciler_artifact_hash = fileexists(local.voice_reconciler_artifact_path) ? filebase64sha256(local.voice_reconciler_artifact_path) : null
}

resource "aws_cloudwatch_log_group" "lambda_voice_reconciler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name              = "/aws/lambda/${local.resource_prefix}-voice-reconciler"
  retention_in_days = local.voice_log_retention_days

  tags = {
    Workload = local.voice_cost_allocation_tag_value
  }
}

resource "aws_iam_role" "voice_reconciler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name                 = "${local.resource_prefix}-voice-reconciler-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

data "aws_iam_policy_document" "voice_reconciler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  statement {
    sid       = "WriteVoiceReconcilerLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.lambda_voice_reconciler[0].arn}:*"]
  }

  statement {
    sid       = "QueryExpiredVoiceLeases"
    effect    = "Allow"
    actions   = ["dynamodb:Query"]
    resources = ["${aws_dynamodb_table.voice_sessions.arn}/index/gsi2"]
  }

  statement {
    sid    = "ReconcileVoiceSessionState"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:TransactWriteItems",
      "dynamodb:UpdateItem",
    ]
    resources = [aws_dynamodb_table.voice_sessions.arn]
  }

  statement {
    sid     = "StopExpiredVoiceRuntime"
    effect  = "Allow"
    actions = ["bedrock-agentcore:StopRuntimeSession"]
    resources = concat(
      [
        aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn,
        aws_bedrockagentcore_agent_runtime_endpoint.voice_staging[0].agent_runtime_endpoint_arn,
      ],
      var.promote_voice_agentcore_prod ? [aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0].agent_runtime_endpoint_arn] : [],
    )
  }
}

resource "aws_iam_role_policy" "voice_reconciler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name   = "${local.resource_prefix}-voice-reconciler-policy"
  role   = aws_iam_role.voice_reconciler[0].id
  policy = data.aws_iam_policy_document.voice_reconciler[0].json
}

resource "aws_lambda_function" "voice_reconciler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  function_name    = "${local.resource_prefix}-voice-reconciler"
  role             = aws_iam_role.voice_reconciler[0].arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.voice_reconciler_artifact_path
  source_code_hash = local.voice_reconciler_artifact_hash
  memory_size      = 128
  timeout          = 15

  reserved_concurrent_executions = 1

  tags = {
    Workload = local.voice_cost_allocation_tag_value
  }

  environment {
    variables = {
      AGENTCORE_RUNTIME_ARN          = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn
      AGENTCORE_RUNTIME_QUALIFIER    = var.promote_voice_agentcore_prod ? "PROD" : "STAGING"
      ENVIRONMENT                    = var.environment
      VOICE_RECONCILER_BATCH_SIZE    = "10"
      VOICE_SESSION_LEASE_INDEX_NAME = "gsi2"
      VOICE_SESSIONS_TABLE_NAME      = aws_dynamodb_table.voice_sessions.name
    }
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.voice_reconciler_artifact_path)
      error_message = "Missing Billeif voice reconciler Lambda artifact ${local.voice_reconciler_artifact_path}. Run make package-lambda-voice-reconciler from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_bedrockagentcore_agent_runtime_endpoint.voice_staging,
    aws_cloudwatch_log_group.lambda_voice_reconciler,
    aws_iam_role_policy.voice_reconciler,
  ]
}

data "aws_iam_policy_document" "voice_reconciler_scheduler_assume_role" {
  count = var.provision_voice_infrastructure ? 1 : 0

  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "voice_reconciler_scheduler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name                 = "${local.resource_prefix}-voice-reconciler-scheduler-role"
  assume_role_policy   = data.aws_iam_policy_document.voice_reconciler_scheduler_assume_role[0].json
  permissions_boundary = local.workload_permissions_boundary_arn
}

data "aws_iam_policy_document" "voice_reconciler_scheduler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  statement {
    sid       = "InvokeVoiceReconciler"
    effect    = "Allow"
    actions   = ["lambda:InvokeFunction"]
    resources = [aws_lambda_function.voice_reconciler[0].arn]
  }
}

resource "aws_iam_role_policy" "voice_reconciler_scheduler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name   = "${local.resource_prefix}-voice-reconciler-scheduler-policy"
  role   = aws_iam_role.voice_reconciler_scheduler[0].id
  policy = data.aws_iam_policy_document.voice_reconciler_scheduler[0].json
}

resource "aws_scheduler_schedule" "voice_reconciler" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name                         = "${local.resource_prefix}-voice-reconciler-minute"
  description                  = "${local.resource_prefix} Billeif voice lease reconciler schedule"
  schedule_expression          = "rate(1 minute)"
  schedule_expression_timezone = "UTC"
  state                        = "ENABLED"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.voice_reconciler[0].arn
    role_arn = aws_iam_role.voice_reconciler_scheduler[0].arn
    input    = "{}"

    retry_policy {
      maximum_event_age_in_seconds = 300
      maximum_retry_attempts       = 3
    }
  }

  depends_on = [aws_iam_role_policy.voice_reconciler_scheduler]
}
