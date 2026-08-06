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
    target = aws_ecr_repository.voice_agentcore[0]
    values = {
      repository_url = "928282274753.dkr.ecr.ap-south-1.amazonaws.com/billeif-test/voice-runtime"
    }
  }

  override_resource {
    target = aws_apigatewayv2_api.http
    values = {
      id = "billeif-test-http-api"
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
  voice_staging_verified_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  voice_staging_verified_release        = "release-0123456789abcdef"
  voice_staging_verified_version        = "2"
}

run "voice_is_cost_safe_when_disabled" {
  command = plan

  assert {
    condition = (
      var.enable_voice == false &&
      length(aws_ecr_repository.voice_agentcore) == 0 &&
      length(aws_bedrockagentcore_agent_runtime.voice) == 0 &&
      length(aws_bedrockagentcore_agent_runtime_endpoint.voice_staging) == 0 &&
      length(aws_bedrockagentcore_agent_runtime_endpoint.voice_prod) == 0 &&
      length(awscc_kinesisvideo_signaling_channel.voice) == 0
    )
    error_message = "The default voice gate must create no ECR, AgentCore runtime, endpoint, or KVS channel resources."
  }
}

run "voice_infrastructure_provisions_while_admission_stays_disabled" {
  command = plan

  variables {
    provision_voice_infrastructure = true
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release        = "release-0123456789abcdef"
  }

  assert {
    condition = (
      var.enable_voice == false &&
      var.voice_rollout_stage == "disabled" &&
      length(aws_bedrockagentcore_agent_runtime.voice) == 1 &&
      length(aws_bedrockagentcore_agent_runtime_endpoint.voice_staging) == 1 &&
      length(aws_bedrockagentcore_agent_runtime_endpoint.voice_prod) == 0 &&
      aws_lambda_function.api_http.environment[0].variables["VOICE_ADMISSION_ENABLED"] == "false" &&
      aws_lambda_function.api_http.environment[0].variables["VOICE_ROLLOUT_STAGE"] == "disabled"
    )
    error_message = "Provisioning must create the runtime and STAGING endpoint without promoting PROD or admitting users."
  }
}

run "voice_image_digest_rejects_tags_and_uppercase_hex" {
  command = plan

  variables {
    provision_voice_infrastructure = true
    voice_agentcore_image_digest   = "prod-latest"
    voice_agentcore_release        = "release-0123456789abcdef"
  }

  expect_failures = [var.voice_agentcore_image_digest]
}

run "production_promotion_fails_until_every_evidence_gate_is_acknowledged" {
  command = plan

  variables {
    environment                    = "prod"
    provision_voice_infrastructure = true
    promote_voice_agentcore_prod   = true
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_prod_version   = "2"
    voice_agentcore_release        = "release-0123456789abcdef"
  }

  expect_failures = [terraform_data.voice_cutover_gates[0]]
}

run "production_promotion_keeps_admission_disabled_after_all_evidence" {
  command = plan

  variables {
    environment                                   = "prod"
    provision_voice_infrastructure                = true
    promote_voice_agentcore_prod                  = true
    enable_voice                                  = false
    voice_agentcore_image_digest                  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_prod_version                  = "2"
    voice_agentcore_release                       = "release-0123456789abcdef"
    enable_voice_turn_udp_egress                  = true
    voice_sarvam_stt_concurrency_150_acknowledged = true
    voice_bulbul_concurrency_150_acknowledged     = true
    voice_sarvam_llm_rpm_300_acknowledged         = true
    voice_agentcore_kvs_sessions_120_acknowledged = true
    voice_turn_live_proof_acknowledged            = true
    voice_load_cost_live_evidence_acknowledged    = true
    voice_staging_verified                        = true
    voice_generic_websocket_verified              = true
  }

  assert {
    condition = (
      length(aws_bedrockagentcore_agent_runtime_endpoint.voice_staging) == 1 &&
      length(aws_bedrockagentcore_agent_runtime_endpoint.voice_prod) == 1 &&
      aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0].agent_runtime_version == "2" &&
      terraform_data.voice_cutover_gates[0].input.evidence_complete == true &&
      terraform_data.voice_cutover_gates[0].input.staging_bound == true &&
      aws_lambda_function.api_http.environment[0].variables["VOICE_ADMISSION_ENABLED"] == "false" &&
      aws_lambda_function.api_http.environment[0].variables["VOICE_ROLLOUT_STAGE"] == "disabled"
    )
    error_message = "A verified PROD promotion must remain independently admission-disabled until the separate cutover change."
  }
}

run "production_promotion_rejects_staging_evidence_for_another_artifact" {
  command = plan

  variables {
    environment                                   = "prod"
    provision_voice_infrastructure                = true
    promote_voice_agentcore_prod                  = true
    enable_voice                                  = false
    voice_agentcore_image_digest                  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_prod_version                  = "2"
    voice_agentcore_release                       = "release-0123456789abcdef"
    enable_voice_turn_udp_egress                  = true
    voice_sarvam_stt_concurrency_150_acknowledged = true
    voice_bulbul_concurrency_150_acknowledged     = true
    voice_sarvam_llm_rpm_300_acknowledged         = true
    voice_agentcore_kvs_sessions_120_acknowledged = true
    voice_turn_live_proof_acknowledged            = true
    voice_load_cost_live_evidence_acknowledged    = true
    voice_staging_verified                        = true
    voice_staging_verified_image_digest           = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
    voice_generic_websocket_verified              = true
  }

  expect_failures = [terraform_data.voice_cutover_gates[0]]
}

run "production_admission_rejects_a_disabled_rollout" {
  command = plan

  variables {
    environment                                   = "prod"
    provision_voice_infrastructure                = true
    promote_voice_agentcore_prod                  = true
    enable_voice                                  = true
    voice_agentcore_image_digest                  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_prod_version                  = "2"
    voice_agentcore_release                       = "release-0123456789abcdef"
    enable_voice_turn_udp_egress                  = true
    voice_sarvam_stt_concurrency_150_acknowledged = true
    voice_bulbul_concurrency_150_acknowledged     = true
    voice_sarvam_llm_rpm_300_acknowledged         = true
    voice_agentcore_kvs_sessions_120_acknowledged = true
    voice_turn_live_proof_acknowledged            = true
    voice_load_cost_live_evidence_acknowledged    = true
    voice_staging_verified                        = true
    voice_generic_websocket_verified              = true
  }

  expect_failures = [terraform_data.voice_cutover_gates[0]]
}

run "voice_admission_requires_all_evidence_in_every_environment" {
  command = plan

  variables {
    provision_voice_infrastructure = true
    promote_voice_agentcore_prod   = true
    enable_voice                   = true
    voice_rollout_stage            = "100"
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_prod_version   = "2"
    voice_agentcore_release        = "release-0123456789abcdef"
    enable_voice_turn_udp_egress   = true
  }

  expect_failures = [terraform_data.voice_cutover_gates[0]]
}

run "code_rollback_can_select_only_the_recorded_previous_version_with_admission_off" {
  command = plan

  variables {
    environment                                   = "prod"
    provision_voice_infrastructure                = true
    promote_voice_agentcore_prod                  = true
    enable_voice                                  = false
    voice_agentcore_image_digest                  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_prod_version                  = "1"
    voice_agentcore_previous_prod_version         = "1"
    voice_agentcore_release                       = "release-0123456789abcdef"
    enable_voice_turn_udp_egress                  = true
    voice_sarvam_stt_concurrency_150_acknowledged = true
    voice_bulbul_concurrency_150_acknowledged     = true
    voice_sarvam_llm_rpm_300_acknowledged         = true
    voice_agentcore_kvs_sessions_120_acknowledged = true
    voice_turn_live_proof_acknowledged            = true
    voice_load_cost_live_evidence_acknowledged    = true
    voice_staging_verified                        = true
    voice_generic_websocket_verified              = true
  }

  assert {
    condition     = aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0].agent_runtime_version == "1"
    error_message = "Admission-disabled rollback must point PROD to the explicit previous immutable version."
  }
}

run "code_rollback_is_rejected_while_admission_is_enabled" {
  command = plan

  variables {
    environment                                   = "prod"
    provision_voice_infrastructure                = true
    promote_voice_agentcore_prod                  = true
    enable_voice                                  = true
    voice_rollout_stage                           = "100"
    voice_agentcore_image_digest                  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_prod_version                  = "1"
    voice_agentcore_previous_prod_version         = "1"
    voice_agentcore_release                       = "release-0123456789abcdef"
    enable_voice_turn_udp_egress                  = true
    voice_sarvam_stt_concurrency_150_acknowledged = true
    voice_bulbul_concurrency_150_acknowledged     = true
    voice_sarvam_llm_rpm_300_acknowledged         = true
    voice_agentcore_kvs_sessions_120_acknowledged = true
    voice_turn_live_proof_acknowledged            = true
    voice_load_cost_live_evidence_acknowledged    = true
    voice_staging_verified                        = true
    voice_generic_websocket_verified              = true
  }

  expect_failures = [terraform_data.voice_cutover_gates[0]]
}

run "voice_core_uses_private_mumbai_runtime_and_bounded_resources" {
  command = plan

  variables {
    provision_voice_infrastructure                = true
    promote_voice_agentcore_prod                  = true
    enable_voice                                  = true
    voice_rollout_stage                           = "100"
    voice_agentcore_image_tag                     = "prod-0123456789abcdef"
    voice_agentcore_image_digest                  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_prod_version                  = "2"
    voice_agentcore_release                       = "release-0123456789abcdef"
    enable_voice_turn_udp_egress                  = true
    voice_sarvam_stt_concurrency_150_acknowledged = true
    voice_bulbul_concurrency_150_acknowledged     = true
    voice_sarvam_llm_rpm_300_acknowledged         = true
    voice_agentcore_kvs_sessions_120_acknowledged = true
    voice_turn_live_proof_acknowledged            = true
    voice_load_cost_live_evidence_acknowledged    = true
    voice_staging_verified                        = true
    voice_generic_websocket_verified              = true
  }

  assert {
    condition = (
      length(aws_ecr_repository.voice_agentcore) == 1 &&
      aws_ecr_repository.voice_agentcore[0].image_scanning_configuration[0].scan_on_push == true &&
      aws_ecr_repository.voice_agentcore[0].encryption_configuration[0].encryption_type == "AES256" &&
      length(aws_ecr_lifecycle_policy.voice_agentcore) == 1 &&
      jsondecode(aws_ecr_lifecycle_policy.voice_agentcore[0].policy).rules[0].selection.countNumber == 5 &&
      jsondecode(aws_ecr_lifecycle_policy.voice_agentcore[0].policy).rules[0].selection.tagPrefixList == ["prod-"]
    )
    error_message = "Voice enablement must create one encrypted, scan-on-push ECR repository with lifecycle retention."
  }

  assert {
    condition = (
      length(aws_security_group.voice_agentcore) == 1 &&
      var.enable_voice_turn_udp_egress == true &&
      local.voice_agentcore_dns_resolver_cidr == "10.0.0.2/32"
    )
    error_message = "The AgentCore security group must have no ingress and only HTTPS, VPC DNS, and proof-gated KVS TURN egress."
  }

  assert {
    condition = (
      length(aws_bedrockagentcore_agent_runtime.voice) == 1 &&
      aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_artifact[0].container_configuration[0].container_uri == "${aws_ecr_repository.voice_agentcore[0].repository_url}@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" &&
      aws_bedrockagentcore_agent_runtime.voice[0].network_configuration[0].network_mode == "VPC" &&
      toset(aws_bedrockagentcore_agent_runtime.voice[0].network_configuration[0].network_mode_config[0].subnets) == toset(aws_subnet.private[*].id) &&
      length(aws_bedrockagentcore_agent_runtime.voice[0].network_configuration[0].network_mode_config[0].subnets) >= 2 &&
      aws_bedrockagentcore_agent_runtime.voice[0].protocol_configuration[0].server_protocol == "HTTP"
    )
    error_message = "AgentCore voice must use HTTP in the existing private VPC subnets."
  }

  assert {
    condition = (
      toset([for zone in data.aws_availability_zone.voice_agentcore : zone.zone_id]) == toset(["aps1-az1", "aps1-az2"]) &&
      alltrue([for zone in data.aws_availability_zone.voice_agentcore : contains(["aps1-az1", "aps1-az2", "aps1-az3"], zone.zone_id)]) &&
      aws_route.private_default_egress.network_interface_id == aws_instance.nat[0].primary_network_interface_id &&
      aws_route.private_default_egress.nat_gateway_id == null
    )
    error_message = "Voice subnets must be in supported Mumbai AZ IDs and retain the default NAT-instance route."
  }

  assert {
    condition = (
      aws_bedrockagentcore_agent_runtime.voice[0].lifecycle_configuration[0].idle_runtime_session_timeout == 120 &&
      aws_bedrockagentcore_agent_runtime.voice[0].lifecycle_configuration[0].max_lifetime == 3600 &&
      toset(aws_bedrockagentcore_agent_runtime.voice[0].request_header_configuration[0].request_header_allowlist) == toset(["Authorization"]) &&
      toset(aws_bedrockagentcore_agent_runtime.voice[0].authorizer_configuration[0].custom_jwt_authorizer[0].allowed_clients) == toset(["voice-phone-client"]) &&
      aws_bedrockagentcore_agent_runtime.voice[0].authorizer_configuration[0].custom_jwt_authorizer[0].allowed_audience == null &&
      aws_bedrockagentcore_agent_runtime.voice[0].authorizer_configuration[0].custom_jwt_authorizer[0].allowed_scopes == null &&
      aws_bedrockagentcore_agent_runtime.voice[0].authorizer_configuration[0].custom_jwt_authorizer[0].discovery_url == "https://cognito-idp.ap-south-1.amazonaws.com/ap-south-1_voicephone/.well-known/openid-configuration"
    )
    error_message = "AgentCore must use bounded lifecycle settings, the phone Cognito client, and only Authorization forwarding without an access-token audience constraint."
  }

  assert {
    condition = (
      toset(keys(aws_bedrockagentcore_agent_runtime.voice[0].environment_variables)) == toset([
        "AWS_REGION",
        "BILLEIF_API_ORIGIN",
        "COGNITO_PHONE_CLIENT_ID",
        "COGNITO_PHONE_REGION",
        "COGNITO_PHONE_USER_POOL_ID",
        "ENVIRONMENT",
        "RUNTIME_ID",
        "SARVAM_SECRET_ARN",
        "VOICE_GLOBAL_CAPACITY_LIMIT",
        "VOICE_KVS_CHANNEL_ARNS",
        "VOICE_KVS_CHANNEL_COUNT",
        "VOICE_PER_USER_CAPACITY_LIMIT",
        "VOICE_PROTOCOL_VERSION",
        "VOICE_SESSION_LEASE_DURATION",
        "VOICE_SESSION_MAX_DURATION",
        "VOICE_SESSION_ROTATE_AFTER",
        "VOICE_SESSIONS_TABLE_NAME",
      ]) &&
      aws_bedrockagentcore_agent_runtime.voice[0].environment_variables["BILLEIF_API_ORIGIN"] == local.http_api_invoke_url &&
      aws_bedrockagentcore_agent_runtime.voice[0].environment_variables["COGNITO_PHONE_USER_POOL_ID"] == "ap-south-1_voicephone" &&
      aws_bedrockagentcore_agent_runtime.voice[0].environment_variables["COGNITO_PHONE_CLIENT_ID"] == "voice-phone-client" &&
      aws_bedrockagentcore_agent_runtime.voice[0].environment_variables["COGNITO_PHONE_REGION"] == "ap-south-1" &&
      aws_bedrockagentcore_agent_runtime.voice[0].environment_variables["ENVIRONMENT"] == "test" &&
      aws_bedrockagentcore_agent_runtime.voice[0].environment_variables["SARVAM_SECRET_ARN"] == "arn:aws:secretsmanager:ap-south-1:928282274753:secret:billeif-test-sarvam" &&
      !contains(keys(aws_bedrockagentcore_agent_runtime.voice[0].environment_variables), "SARVAM_API_KEY")
    )
    error_message = "AgentCore environment variables must be the bounded non-secret runtime identifiers only."
  }

  assert {
    condition = (
      length(awscc_kinesisvideo_signaling_channel.voice) == 12 &&
      length(aws_bedrockagentcore_agent_runtime_endpoint.voice_staging) == 1 &&
      length(aws_bedrockagentcore_agent_runtime_endpoint.voice_prod) == 1 &&
      aws_bedrockagentcore_agent_runtime_endpoint.voice_staging[0].name == "STAGING" &&
      aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0].name == "PROD"
    )
    error_message = "Voice must have exactly 12 pooled TURN channels and version-pinned STAGING and PROD endpoints."
  }

  assert {
    condition = (
      aws_dynamodb_table.voice_sessions.billing_mode == "PAY_PER_REQUEST" &&
      aws_dynamodb_table.voice_sessions.ttl[0].enabled == true &&
      toset([for index in aws_dynamodb_table.voice_sessions.global_secondary_index : index.name]) == toset(["gsi1", "gsi2"]) &&
      alltrue([for index in aws_dynamodb_table.voice_sessions.global_secondary_index : index.projection_type == "ALL"]) &&
      aws_vpc_endpoint.s3.vpc_endpoint_type == "Gateway" &&
      aws_vpc_endpoint.dynamodb.vpc_endpoint_type == "Gateway" &&
      length(aws_nat_gateway.main) == 0
    )
    error_message = "Voice must extend the retained on-demand table in place, retain free gateway endpoints, and add no managed NAT gateway in the default mode."
  }

  assert {
    condition = (
      aws_lambda_function.api_http.environment[0].variables["AGENTCORE_RUNTIME_ARN"] == aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn &&
      aws_lambda_function.api_http.environment[0].variables["AGENTCORE_RUNTIME_QUALIFIER"] == "PROD" &&
      aws_lambda_function.api_http.environment[0].variables["VOICE_ADMISSION_ENABLED"] == "true" &&
      aws_lambda_function.api_http.environment[0].variables["VOICE_ROLLOUT_STAGE"] == "100" &&
      aws_lambda_function.api_http.environment[0].variables["VOICE_ROLLOUT_INTERNAL_SUB_HASHES"] == "" &&
      aws_lambda_function.api_http.environment[0].variables["VOICE_SESSIONS_TABLE_NAME"] == aws_dynamodb_table.voice_sessions.name &&
      alltrue([
        for environment in [
          aws_lambda_function.a2a_stream.environment[0].variables,
          aws_lambda_function.sqs_invoice.environment[0].variables,
          aws_lambda_function.sqs_gst.environment[0].variables,
          aws_lambda_function.sqs_bargaining.environment[0].variables,
          aws_lambda_function.ws_handler.environment[0].variables,
        ] :
        !contains(keys(environment), "AGENTCORE_RUNTIME_ARN") &&
        !contains(keys(environment), "VOICE_SESSIONS_TABLE_NAME")
      ])
    )
    error_message = "The existing HTTP Lambda must receive only the non-secret PROD runtime identity and retained voice table settings."
  }

  assert {
    condition = (
      length([
        for statement in jsondecode(aws_iam_role.voice_agentcore_runtime[0].assume_role_policy).Statement : statement
        if statement.Effect == "Allow" &&
        statement.Action == "sts:AssumeRole" &&
        statement.Principal.Service == "bedrock-agentcore.amazonaws.com" &&
        statement.Condition.StringEquals["aws:SourceAccount"] == "928282274753" &&
        startswith(statement.Condition.ArnLike["aws:SourceArn"], "arn:aws:bedrock-agentcore:ap-south-1:928282274753:runtime/")
      ]) == 1
    )
    error_message = "The AgentCore execution-role trust must be service-, account-, and runtime-ARN-scoped."
  }

  assert {
    condition = (
      length([
        for statement in jsondecode(aws_iam_role_policy.voice_agentcore_runtime[0].policy).Statement : statement
        if statement.Sid == "VoiceSessionState" &&
        toset(statement.Action) == toset([
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:Query",
          "dynamodb:TransactWriteItems",
          "dynamodb:UpdateItem",
        ]) &&
        toset(statement.Resource) == toset([
          aws_dynamodb_table.voice_sessions.arn,
          "${aws_dynamodb_table.voice_sessions.arn}/index/gsi1",
          "${aws_dynamodb_table.voice_sessions.arn}/index/gsi2",
        ])
      ]) == 1 &&
      length([
        for statement in jsondecode(aws_iam_role_policy.voice_agentcore_runtime[0].policy).Statement : statement
        if statement.Sid == "SarvamCredentialRead" &&
        toset(statement.Action) == toset(["secretsmanager:GetSecretValue"]) &&
        statement.Resource == "arn:aws:secretsmanager:ap-south-1:928282274753:secret:billeif-test-sarvam"
      ]) == 1 &&
      length([
        for statement in jsondecode(aws_iam_role_policy.voice_agentcore_runtime[0].policy).Statement : statement
        if statement.Sid == "SarvamCredentialDecrypt" &&
        toset(statement.Action) == toset(["kms:Decrypt"]) &&
        statement.Resource == "arn:aws:kms:ap-south-1:928282274753:key/application-secrets" &&
        statement.Condition.StringEquals["kms:ViaService"] == "secretsmanager.ap-south-1.amazonaws.com" &&
        statement.Condition.StringEquals["kms:CallerAccount"] == "928282274753"
      ]) == 1 &&
      length([
        for statement in jsondecode(aws_iam_role_policy.voice_agentcore_runtime[0].policy).Statement : statement
        if statement.Sid == "DenyUnverifiedUserDelegation" &&
        statement.Effect == "Deny" &&
        toset(statement.Action) == toset(["bedrock-agentcore:GetWorkloadAccessTokenForUserId"])
      ]) == 1 &&
      length([
        for statement in jsondecode(aws_iam_role_policy.voice_agentcore_runtime[0].policy).Statement : statement
        if statement.Sid == "VoiceRuntimeLogGroupAccess" &&
        toset(statement.Action) == toset(["logs:CreateLogGroup", "logs:DescribeLogStreams"]) &&
        statement.Resource == "arn:aws:logs:ap-south-1:928282274753:log-group:/aws/bedrock-agentcore/runtimes/billeif_test_test_voice_runtime-*"
      ]) == 1 &&
      length([
        for statement in jsondecode(aws_iam_role_policy.voice_agentcore_runtime[0].policy).Statement : statement
        if statement.Sid == "VoiceRuntimeLogGroupDiscovery" &&
        toset(statement.Action) == toset(["logs:DescribeLogGroups"]) &&
        statement.Resource == "arn:aws:logs:ap-south-1:928282274753:log-group:*"
      ]) == 1 &&
      length([
        for statement in jsondecode(aws_iam_role_policy.voice_agentcore_runtime[0].policy).Statement : statement
        if statement.Sid == "VoiceRuntimeLogStreamWrite" &&
        toset(statement.Action) == toset(["logs:CreateLogStream", "logs:PutLogEvents"]) &&
        statement.Resource == "arn:aws:logs:ap-south-1:928282274753:log-group:/aws/bedrock-agentcore/runtimes/billeif_test_test_voice_runtime-*:log-stream:*"
      ]) == 1 &&
      alltrue([
        for action in flatten([
          for statement in jsondecode(aws_iam_role_policy.voice_agentcore_runtime[0].policy).Statement : statement.Action
        ]) : !endswith(action, "*")
      ])
    )
    error_message = "The AgentCore runtime policy must remain table/index-, Sarvam-secret-, KMS-, and action-scoped with unverified user delegation denied."
  }

  assert {
    condition = (
      length(jsondecode(aws_iam_role_policy.voice_session_http[0].policy).Statement) == 2 &&
      toset(flatten([
        for statement in jsondecode(aws_iam_role_policy.voice_session_http[0].policy).Statement : statement.Action
        ])) == toset([
        "bedrock-agentcore:StopRuntimeSession",
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:Query",
        "dynamodb:TransactWriteItems",
        "dynamodb:UpdateItem",
      ]) &&
      toset([
        for statement in jsondecode(aws_iam_role_policy.voice_session_http[0].policy).Statement : statement.Resource
        if statement.Sid == "StopOwnedVoiceRuntimeSession"
        ][0]) == toset([
        aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn,
        aws_bedrockagentcore_agent_runtime_endpoint.voice_staging[0].agent_runtime_endpoint_arn,
        aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0].agent_runtime_endpoint_arn,
      ])
    )
    error_message = "The HTTP Lambda voice policy must contain only table transactions and StopRuntimeSession."
  }
}

run "production_voice_registry_is_immutable_and_recoverable" {
  command = plan

  variables {
    environment                    = "prod"
    provision_voice_infrastructure = true
    voice_agentcore_image_tag      = "prod-0123456789abcdef"
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release        = "release-0123456789abcdef"
  }

  assert {
    condition = (
      aws_ecr_repository.voice_agentcore[0].image_tag_mutability == "IMMUTABLE" &&
      aws_ecr_repository.voice_agentcore[0].force_delete == false &&
      aws_dynamodb_table.voice_sessions.point_in_time_recovery[0].enabled == true
    )
    error_message = "Production voice ECR tags must be immutable and non-force-destroyable, with voice-session PITR enabled."
  }
}

run "voice_rejects_duplicate_physical_availability_zones" {
  command = plan

  variables {
    provision_voice_infrastructure = true
    voice_agentcore_image_digest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    voice_agentcore_release        = "release-0123456789abcdef"
  }

  override_data {
    target = data.aws_availability_zone.voice_agentcore[1]
    values = {
      name    = "ap-south-1b"
      zone_id = "aps1-az1"
    }
  }

  expect_failures = [aws_bedrockagentcore_agent_runtime.voice[0]]
}
