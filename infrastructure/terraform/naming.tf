# These Billeif names are prelaunch-only and must be applied before the first
# Terraform apply. No state moves or imports are provided for prior deployments.
locals {
  resource_prefix = "${var.project_name}-${var.environment}"

  cognito_web_user_pool_name      = var.user_pool_name != "" ? var.user_pool_name : "${local.resource_prefix}-web-user-pool"
  cognito_web_client_name         = var.client_name != "" ? var.client_name : "${local.resource_prefix}-web-client"
  cognito_native_user_pool_name   = var.phone_user_pool_name != "" ? var.phone_user_pool_name : "${local.resource_prefix}-native-user-pool"
  cognito_native_client_name      = var.phone_client_name != "" ? var.phone_client_name : "${local.resource_prefix}-native-client"
  cognito_resource_server_id      = "${local.resource_prefix}-api"
  cognito_resource_server_name    = "${local.resource_prefix}-resource-server"
  cognito_hosted_ui_domain_prefix = var.cognito_domain_prefix != "" ? var.cognito_domain_prefix : "billeif-${var.environment}-${data.aws_caller_identity.current.account_id}"

  phone_auth_cooldown_table_name = var.phone_auth_cooldown_table_name != "" ? var.phone_auth_cooldown_table_name : "${local.resource_prefix}-phone-auth-cooldowns"
  websocket_connections_table    = var.websocket_connections_table != "" ? var.websocket_connections_table : "${local.resource_prefix}-ws-connections"
  voice_sessions_table_name      = var.voice_sessions_table_name != "" ? var.voice_sessions_table_name : "${local.resource_prefix}-voice-sessions"
  voice_session_lambda_name      = var.voice_session_lambda_function_name != "" ? var.voice_session_lambda_function_name : "${local.resource_prefix}-voice-session"

  ses_verified_identity_is_email = can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[^@[:space:]]+$", var.ses_verified_identity))
  ses_verified_identity_arn      = "arn:${data.aws_partition.current.partition}:ses:${var.aws_region}:${data.aws_caller_identity.current.account_id}:identity/${var.ses_verified_identity}"
}
