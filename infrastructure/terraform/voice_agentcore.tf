locals {
  voice_agentcore_supported_zone_ids = toset(["aps1-az1", "aps1-az2", "aps1-az3"])
  voice_agentcore_dns_resolver_cidr  = "${cidrhost(var.vpc_cidr, 2)}/32"
  voice_agentcore_runtime_environment = {
    AWS_REGION                    = var.aws_region
    RUNTIME_ID                    = local.voice_agentcore_runtime_name
    SARVAM_SECRET_ARN             = aws_secretsmanager_secret.sarvam.arn
    VOICE_GLOBAL_CAPACITY_LIMIT   = "100"
    VOICE_KVS_CHANNEL_ARNS        = join(",", awscc_kinesisvideo_signaling_channel.voice[*].arn)
    VOICE_KVS_CHANNEL_COUNT       = "12"
    VOICE_PER_USER_CAPACITY_LIMIT = "1"
    VOICE_PROTOCOL_VERSION        = "1"
    VOICE_SESSION_LEASE_DURATION  = "2m"
    VOICE_SESSION_MAX_DURATION    = "55m"
    VOICE_SESSION_ROTATE_AFTER    = "52m"
    VOICE_SESSIONS_TABLE_NAME     = aws_dynamodb_table.voice_sessions.name
  }
}

data "aws_availability_zone" "voice_agentcore" {
  count = var.enable_voice ? length(var.availability_zones) : 0

  name = var.availability_zones[count.index]
}

resource "aws_ecr_repository" "voice_agentcore" {
  count = var.enable_voice ? 1 : 0

  name                 = local.voice_agentcore_ecr_name
  image_tag_mutability = var.environment == "prod" ? "IMMUTABLE" : "MUTABLE"
  force_delete         = var.environment != "prod"

  encryption_configuration {
    encryption_type = "AES256"
  }

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = {
    Name = local.voice_agentcore_ecr_name
  }
}

resource "aws_ecr_lifecycle_policy" "voice_agentcore" {
  count = var.enable_voice ? 1 : 0

  repository = aws_ecr_repository.voice_agentcore[0].name
  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Retain no more than five production voice images"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = ["prod-"]
          countType     = "imageCountMoreThan"
          countNumber   = 5
        }
        action = {
          type = "expire"
        }
      },
      {
        rulePriority = 2
        description  = "Expire untagged voice images after one day"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 1
        }
        action = {
          type = "expire"
        }
      },
    ]
  })
}

resource "aws_security_group" "voice_agentcore" {
  count = var.enable_voice ? 1 : 0

  name        = "${local.resource_prefix}-voice-agentcore-sg"
  description = "Billeif AgentCore voice runtime egress with no inbound rules"
  vpc_id      = aws_vpc.main.id

  egress {
    description = "HTTPS for Sarvam, Billeif APIs, KVS, Secrets Manager, and ECR"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "UDP DNS to the VPC resolver"
    from_port   = 53
    to_port     = 53
    protocol    = "udp"
    cidr_blocks = [local.voice_agentcore_dns_resolver_cidr]
  }

  egress {
    description = "TCP DNS fallback to the VPC resolver"
    from_port   = 53
    to_port     = 53
    protocol    = "tcp"
    cidr_blocks = [local.voice_agentcore_dns_resolver_cidr]
  }

  dynamic "egress" {
    for_each = var.enable_voice_turn_udp_egress ? [1] : []

    content {
      description = "KVS managed TURN over UDP after the proof gate passes"
      from_port   = 443
      to_port     = 443
      protocol    = "udp"
      cidr_blocks = ["0.0.0.0/0"]
    }
  }

  tags = {
    Name = "${local.resource_prefix}-voice-agentcore-sg"
  }
}

resource "awscc_kinesisvideo_signaling_channel" "voice" {
  count = var.enable_voice ? 12 : 0

  name                = format("%s-voice-turn-%02d", local.resource_prefix, count.index)
  type                = "SINGLE_MASTER"
  message_ttl_seconds = 60

  tags = [
    {
      key   = "Project"
      value = var.project_name
    },
    {
      key   = "Environment"
      value = var.environment
    },
    {
      key   = "ManagedBy"
      value = "Terraform"
    },
    {
      key   = "Purpose"
      value = "voice-turn-pool"
    },
  ]
}

resource "aws_bedrockagentcore_agent_runtime" "voice" {
  count = var.enable_voice ? 1 : 0

  agent_runtime_name = local.voice_agentcore_runtime_name
  description        = "Billeif realtime voice runtime using Sarvam and managed KVS TURN"
  role_arn           = aws_iam_role.voice_agentcore_runtime[0].arn

  agent_runtime_artifact {
    container_configuration {
      container_uri = "${aws_ecr_repository.voice_agentcore[0].repository_url}:${var.voice_agentcore_image_tag}"
    }
  }

  network_configuration {
    network_mode = "VPC"

    network_mode_config {
      security_groups = [aws_security_group.voice_agentcore[0].id]
      subnets         = aws_subnet.private[*].id
    }
  }

  protocol_configuration {
    server_protocol = "HTTP"
  }

  authorizer_configuration {
    custom_jwt_authorizer {
      discovery_url   = "https://cognito-idp.ap-south-1.amazonaws.com/${aws_cognito_user_pool.phone.id}/.well-known/openid-configuration"
      allowed_clients = [aws_cognito_user_pool_client.phone.id]
    }
  }

  request_header_configuration {
    request_header_allowlist = ["Authorization"]
  }

  lifecycle_configuration {
    idle_runtime_session_timeout = 120
    max_lifetime                 = 3600
  }

  environment_variables = local.voice_agentcore_runtime_environment

  lifecycle {
    precondition {
      condition     = var.aws_region == "ap-south-1"
      error_message = "AgentCore voice is currently supported only in ap-south-1."
    }

    precondition {
      condition     = var.voice_agentcore_image_tag != ""
      error_message = "voice_agentcore_image_tag is required when enable_voice is true."
    }

    precondition {
      condition     = var.voice_agentcore_release != ""
      error_message = "voice_agentcore_release is required when enable_voice is true."
    }

    precondition {
      condition     = var.environment != "prod" || startswith(var.voice_agentcore_image_tag, "prod-")
      error_message = "Production AgentCore voice images must use an immutable prod- prefixed version tag."
    }

    precondition {
      condition = (
        length(toset([
          for zone in data.aws_availability_zone.voice_agentcore : zone.zone_id
        ])) >= 2 &&
        alltrue([
          for zone in data.aws_availability_zone.voice_agentcore :
          contains(local.voice_agentcore_supported_zone_ids, zone.zone_id)
        ])
      )
      error_message = "AgentCore voice requires at least two private subnets in supported Mumbai AZ IDs aps1-az1, aps1-az2, or aps1-az3."
    }

    precondition {
      condition     = var.enable_voice_turn_udp_egress
      error_message = "AgentCore voice enablement requires the reviewed NAT-instance KVS TURN proof gate before UDP egress is opened."
    }
  }

  tags = {
    Name      = local.voice_agentcore_runtime_name
    AuthGate  = "phone-client-and-dynamodb-owner"
    ScopeGate = "voice-use-enforced-by-existing-api"
    TurnProof = "required-before-enable"
  }

  depends_on = [
    aws_ecr_lifecycle_policy.voice_agentcore,
    aws_vpc_endpoint.dynamodb,
    aws_vpc_endpoint.s3,
    aws_route.private_default_egress,
  ]
}

resource "aws_bedrockagentcore_agent_runtime_endpoint" "voice_staging" {
  count = var.enable_voice ? 1 : 0

  name                  = "STAGING"
  description           = "Billeif voice runtime staging qualifier"
  agent_runtime_id      = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_id
  agent_runtime_version = local.voice_agentcore_endpoint_version

  depends_on = [terraform_data.voice_agentcore_mmdsv2]
}

resource "aws_bedrockagentcore_agent_runtime_endpoint" "voice_prod" {
  count = var.enable_voice ? 1 : 0

  name                  = "PROD"
  description           = "Billeif voice runtime production qualifier"
  agent_runtime_id      = aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_id
  agent_runtime_version = local.voice_agentcore_endpoint_version

  depends_on = [
    aws_bedrockagentcore_agent_runtime_endpoint.voice_staging,
    terraform_data.voice_agentcore_mmdsv2,
  ]
}
