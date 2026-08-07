locals {
  voice_production_evidence_acknowledged = alltrue([
    var.voice_sarvam_stt_concurrency_150_acknowledged,
    var.voice_bulbul_concurrency_150_acknowledged,
    var.voice_sarvam_llm_rpm_300_acknowledged,
    var.voice_agentcore_kvs_sessions_120_acknowledged,
    var.voice_turn_live_proof_acknowledged,
    var.voice_load_cost_live_evidence_acknowledged,
    var.voice_staging_verified,
    var.voice_generic_websocket_verified,
  ])
  voice_effective_rollout_stage = var.enable_voice ? var.voice_rollout_stage : "disabled"
  voice_prod_version_is_previous = (
    var.voice_agentcore_previous_prod_version != "" &&
    var.voice_agentcore_prod_version == var.voice_agentcore_previous_prod_version
  )
  voice_selecting_previous_prod_version = !var.enable_voice && local.voice_prod_version_is_previous
  voice_staging_evidence_matches_selection = (
    var.voice_staging_verified_image_digest == var.voice_agentcore_image_digest &&
    var.voice_staging_verified_release == var.voice_agentcore_release &&
    var.voice_staging_verified_version == var.voice_agentcore_prod_version &&
    var.voice_staging_verified_image_digest != "" &&
    var.voice_staging_verified_release != "" &&
    var.voice_staging_verified_version != ""
  )
}

# This resource has no provisioner and performs no remote action. Its lifecycle
# preconditions turn cutover evidence into hard plan/apply failures before any
# PROD endpoint promotion or backend admission can converge.
resource "terraform_data" "voice_cutover_gates" {
  count = var.provision_voice_infrastructure || var.promote_voice_agentcore_prod || var.enable_voice ? 1 : 0

  input = {
    admission_enabled   = var.enable_voice
    evidence_complete   = local.voice_production_evidence_acknowledged
    prod_promoted       = var.promote_voice_agentcore_prod
    prod_version        = var.voice_agentcore_prod_version
    provisioned         = var.provision_voice_infrastructure
    rollout_stage       = local.voice_effective_rollout_stage
    staging_bound       = local.voice_staging_evidence_matches_selection
    turn_udp_proof_gate = var.enable_voice_turn_udp_egress
  }

  lifecycle {
    precondition {
      condition     = !var.promote_voice_agentcore_prod || var.provision_voice_infrastructure
      error_message = "PROD promotion requires provision_voice_infrastructure=true."
    }

    precondition {
      condition     = !var.enable_voice || var.provision_voice_infrastructure
      error_message = "Voice admission requires provision_voice_infrastructure=true."
    }

    precondition {
      condition     = !var.enable_voice || var.environment != "prod" || var.promote_voice_agentcore_prod
      error_message = "Production voice admission requires a version-pinned PROD AgentCore endpoint."
    }

    precondition {
      condition     = !var.promote_voice_agentcore_prod || var.voice_agentcore_prod_version != ""
      error_message = "voice_agentcore_prod_version is required for PROD endpoint promotion."
    }

    precondition {
      condition     = !var.promote_voice_agentcore_prod || var.enable_voice_turn_udp_egress
      error_message = "PROD promotion requires proof-gated KVS TURN UDP egress."
    }

    precondition {
      condition = (
        !var.voice_staging_verified ||
        local.voice_staging_evidence_matches_selection ||
        local.voice_selecting_previous_prod_version
      )
      error_message = "Staging evidence must name the exact selected image digest, release, and numeric AgentCore version; only an admission-disabled rollback may select the recorded previous version."
    }

    precondition {
      condition     = var.environment != "prod" || !var.promote_voice_agentcore_prod || local.voice_production_evidence_acknowledged
      error_message = "Production voice promotion requires every Sarvam quota, AgentCore/KVS capacity, live TURN, live load/cost, staging, and generic WebSocket evidence acknowledgement."
    }

    precondition {
      condition     = !var.enable_voice || var.environment != "prod" || local.voice_production_evidence_acknowledged
      error_message = "Production voice admission requires every Sarvam quota, AgentCore/KVS capacity, live TURN, live load/cost, staging, and generic WebSocket evidence acknowledgement."
    }

    precondition {
      condition     = !var.enable_voice || var.voice_rollout_stage != "disabled"
      error_message = "Voice admission requires a non-disabled voice_rollout_stage."
    }

    precondition {
      condition     = !var.enable_voice || var.voice_rollout_stage != "internal" || length(var.voice_rollout_internal_sub_hashes) > 0
      error_message = "The internal voice rollout requires at least one hashed Cognito subject."
    }

    precondition {
      condition     = !var.enable_voice || !local.voice_prod_version_is_previous
      error_message = "Admission must be disabled before selecting voice_agentcore_previous_prod_version for code rollback."
    }

  }
}
