locals {
  # AgentCore appends a service-generated suffix to the configured runtime
  # name. Keep this pattern aligned with voice_agentcore.tf so the trust policy
  # cannot be reused by an unrelated runtime in this account.
  voice_agentcore_runtime_name       = "${replace(local.resource_prefix, "-", "_")}_voice_runtime"
  voice_agentcore_runtime_arn_prefix = "arn:${data.aws_partition.current.partition}:bedrock-agentcore:${var.aws_region}:${data.aws_caller_identity.current.account_id}:runtime/${local.voice_agentcore_runtime_name}-"
  voice_agentcore_ecr_repository_arn = "arn:${data.aws_partition.current.partition}:ecr:${var.aws_region}:${data.aws_caller_identity.current.account_id}:repository/${local.resource_prefix}/voice-runtime"
  voice_agentcore_log_group_arn      = "arn:${data.aws_partition.current.partition}:logs:${var.aws_region}:${data.aws_caller_identity.current.account_id}:log-group:/aws/bedrock-agentcore/runtimes/${local.voice_agentcore_runtime_name}-*"

  voice_agentcore_table_resources = [
    aws_dynamodb_table.voice_sessions.arn,
    "${aws_dynamodb_table.voice_sessions.arn}/index/gsi1",
    "${aws_dynamodb_table.voice_sessions.arn}/index/gsi2",
  ]

  voice_agentcore_dynamodb_actions = [
    "dynamodb:GetItem",
    "dynamodb:PutItem",
    "dynamodb:Query",
    "dynamodb:TransactWriteItems",
    "dynamodb:UpdateItem",
  ]
}

resource "aws_iam_role" "voice_agentcore_runtime" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name                 = "${local.resource_prefix}-voice-agentcore-runtime-role"
  description          = "Least-privilege execution role for the Billeif AgentCore voice runtime"
  permissions_boundary = local.workload_permissions_boundary_arn
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid    = "AgentCoreRuntimeTrust"
      Effect = "Allow"
      Action = "sts:AssumeRole"
      Principal = {
        Service = "bedrock-agentcore.amazonaws.com"
      }
      Condition = {
        StringEquals = {
          "aws:SourceAccount" = data.aws_caller_identity.current.account_id
        }
        ArnLike = {
          "aws:SourceArn" = "${local.voice_agentcore_runtime_arn_prefix}*"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "voice_agentcore_runtime" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name = "${local.resource_prefix}-voice-agentcore-runtime-policy"
  role = aws_iam_role.voice_agentcore_runtime[0].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ECRAuthorization"
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      },
      {
        Sid    = "VoiceRuntimeImageRead"
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:BatchGetImage",
          "ecr:GetDownloadUrlForLayer",
        ]
        Resource = local.voice_agentcore_ecr_repository_arn
      },
      {
        Sid      = "ECRManagedImageLayerRead"
        Effect   = "Allow"
        Action   = ["s3:GetObject"]
        Resource = "arn:${data.aws_partition.current.partition}:s3:::prod-${var.aws_region}-starport-layer-bucket/*"
      },
      {
        Sid    = "VoiceRuntimeLogGroupAccess"
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:DescribeLogStreams",
        ]
        Resource = local.voice_agentcore_log_group_arn
      },
      {
        Sid      = "VoiceRuntimeLogGroupDiscovery"
        Effect   = "Allow"
        Action   = ["logs:DescribeLogGroups"]
        Resource = "arn:${data.aws_partition.current.partition}:logs:${var.aws_region}:${data.aws_caller_identity.current.account_id}:log-group:*"
      },
      {
        Sid    = "VoiceRuntimeLogStreamWrite"
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ]
        Resource = "${local.voice_agentcore_log_group_arn}:log-stream:*"
      },
      {
        Sid      = "VoiceSessionState"
        Effect   = "Allow"
        Action   = local.voice_agentcore_dynamodb_actions
        Resource = local.voice_agentcore_table_resources
      },
      {
        Sid    = "ManagedTURNCredentials"
        Effect = "Allow"
        Action = [
          "kinesisvideo:DescribeSignalingChannel",
          "kinesisvideo:GetIceServerConfig",
          "kinesisvideo:GetSignalingChannelEndpoint",
        ]
        Resource = [for channel in awscc_kinesisvideo_signaling_channel.voice : channel.arn]
      },
      {
        Sid      = "SarvamCredentialRead"
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = aws_secretsmanager_secret.sarvam.arn
        Condition = {
          Bool = {
            "aws:SecureTransport" = "true"
          }
        }
      },
      {
        Sid      = "SarvamCredentialDecrypt"
        Effect   = "Allow"
        Action   = ["kms:Decrypt"]
        Resource = aws_kms_key.application_secrets.arn
        Condition = {
          Bool = {
            "aws:SecureTransport" = "true"
          }
          StringEquals = {
            "kms:CallerAccount"               = data.aws_caller_identity.current.account_id
            "kms:EncryptionContext:SecretARN" = aws_secretsmanager_secret.sarvam.arn
            "kms:ViaService"                  = "secretsmanager.${var.aws_region}.amazonaws.com"
          }
        }
      },
      {
        Sid      = "DenyUnverifiedUserDelegation"
        Effect   = "Deny"
        Action   = ["bedrock-agentcore:GetWorkloadAccessTokenForUserId"]
        Resource = "*"
      },
    ]
  })
}

resource "aws_iam_role_policy" "voice_session_http" {
  count = var.provision_voice_infrastructure ? 1 : 0

  name = "${local.resource_prefix}-voice-session-http-policy"
  role = aws_iam_role.lambda_http_exec.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "VoiceSessionState"
        Effect   = "Allow"
        Action   = local.voice_agentcore_dynamodb_actions
        Resource = local.voice_agentcore_table_resources
      },
      {
        Sid    = "StopOwnedVoiceRuntimeSession"
        Effect = "Allow"
        Action = ["bedrock-agentcore:StopRuntimeSession"]
        Resource = concat(
          [
            aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn,
            aws_bedrockagentcore_agent_runtime_endpoint.voice_staging[0].agent_runtime_endpoint_arn,
          ],
          var.promote_voice_agentcore_prod ? [aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0].agent_runtime_endpoint_arn] : [],
        )
      },
    ]
  })
}
