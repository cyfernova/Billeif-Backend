locals {
  voice_agentcore_mmdsv2_desired_configuration = local.voice_agentcore_runtime_enabled ? {
    agentRuntimeArtifact = {
      containerConfiguration = {
        containerUri = "${aws_ecr_repository.voice_agentcore[0].repository_url}@${var.voice_agentcore_image_digest}"
      }
    }
    authorizerConfiguration = {
      customJWTAuthorizer = {
        discoveryUrl   = "https://cognito-idp.ap-south-1.amazonaws.com/${aws_cognito_user_pool.phone.id}/.well-known/openid-configuration"
        allowedClients = [aws_cognito_user_pool_client.phone.id]
      }
    }
    description          = "Billeif realtime voice runtime using Sarvam and managed KVS TURN"
    environmentVariables = local.voice_agentcore_runtime_environment
    lifecycleConfiguration = {
      idleRuntimeSessionTimeout = 120
      maxLifetime               = 3600
    }
    metadataConfiguration = {
      requireMMDSV2 = true
    }
    networkConfiguration = {
      networkMode = "VPC"
      networkModeConfig = {
        securityGroups = [aws_security_group.voice_agentcore[0].id]
        subnets        = aws_subnet.private[*].id
      }
    }
    protocolConfiguration = {
      serverProtocol = "HTTP"
    }
    requestHeaderConfiguration = {
      requestHeaderAllowlist = ["Authorization"]
    }
    roleArn = aws_iam_role.voice_agentcore_runtime[0].arn
  } : null

  voice_agentcore_mmdsv2_update_input = local.voice_agentcore_runtime_enabled ? merge(
    local.voice_agentcore_mmdsv2_desired_configuration,
    {
      agentRuntimeId = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_id
      clientToken = "mmdsv2-${sha256(jsonencode({
        configuration   = local.voice_agentcore_mmdsv2_desired_configuration
        release         = var.voice_agentcore_release
        runtime_id      = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_id
        runtime_version = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_version
      }))}"
    },
  ) : null

  voice_agentcore_endpoint_version = local.voice_agentcore_runtime_enabled ? data.external.voice_agentcore_version[0].result.agent_runtime_version : null
}

# The AWS provider does not yet expose RuntimeMetadataConfiguration. Keep this
# compatibility bridge until metadata_configuration.require_mmdsv2 is managed
# natively by aws_bedrockagentcore_agent_runtime.
resource "terraform_data" "voice_agentcore_mmdsv2" {
  count = local.voice_agentcore_runtime_enabled ? 1 : 0

  triggers_replace = {
    desired_configuration_sha256 = sha256(jsonencode(local.voice_agentcore_mmdsv2_desired_configuration))
    release                      = var.voice_agentcore_release
    runtime_id                   = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_id
    runtime_version              = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_version
  }

  provisioner "local-exec" {
    command     = "\"${path.module}/scripts/ensure-agentcore-mmdsv2.sh\" ensure"
    interpreter = ["/usr/bin/env", "bash", "-c"]

    environment = {
      AGENTCORE_BASE_RUNTIME_VERSION = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_version
      AGENTCORE_RUNTIME_ID           = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_id
      AGENTCORE_UPDATE_INPUT         = jsonencode(local.voice_agentcore_mmdsv2_update_input)
      AWS_REGION                     = var.aws_region
    }
  }

  depends_on = [aws_bedrockagentcore_agent_runtime.voice]
}

# This read is intentionally deferred until the bridge has completed. The
# MMDSv2 update creates a new AgentCore version, so endpoints must consume the
# post-update version rather than the version exported by the provider resource.
data "external" "voice_agentcore_version" {
  count = local.voice_agentcore_runtime_enabled ? 1 : 0

  program = [
    "bash",
    "${path.module}/scripts/ensure-agentcore-mmdsv2.sh",
    "read-version",
    aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_id,
    var.aws_region,
    terraform_data.voice_agentcore_mmdsv2[0].id,
    aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_version,
  ]

  depends_on = [terraform_data.voice_agentcore_mmdsv2]
}
