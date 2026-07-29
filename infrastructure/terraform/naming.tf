# These Billeif names are prelaunch-only and must be applied before the first
# Terraform apply. No state moves or imports are provided for prior deployments.
locals {
  resource_prefix = "${var.project_name}-${var.environment}"

  cognito_web_user_pool_name      = trimspace(var.user_pool_name) != "" ? trimspace(var.user_pool_name) : "${local.resource_prefix}-web-user-pool"
  cognito_web_client_name         = trimspace(var.client_name) != "" ? trimspace(var.client_name) : "${local.resource_prefix}-web-client"
  cognito_native_user_pool_name   = trimspace(var.phone_user_pool_name) != "" ? trimspace(var.phone_user_pool_name) : "${local.resource_prefix}-native-user-pool"
  cognito_native_client_name      = trimspace(var.phone_client_name) != "" ? trimspace(var.phone_client_name) : "${local.resource_prefix}-native-client"
  cognito_resource_server_id      = "${local.resource_prefix}-api"
  cognito_resource_server_name    = "${local.resource_prefix}-resource-server"
  cognito_hosted_ui_domain_prefix = trimspace(var.cognito_domain_prefix) != "" ? trimspace(var.cognito_domain_prefix) : "billeif-${var.environment}-${data.aws_caller_identity.current.account_id}"

  phone_auth_cooldown_table_name = trimspace(var.phone_auth_cooldown_table_name) != "" ? trimspace(var.phone_auth_cooldown_table_name) : "${local.resource_prefix}-phone-auth-cooldowns"
  websocket_connections_table    = trimspace(var.websocket_connections_table) != "" ? trimspace(var.websocket_connections_table) : "${local.resource_prefix}-ws-connections"
  voice_sessions_table_name      = trimspace(var.voice_sessions_table_name) != "" ? trimspace(var.voice_sessions_table_name) : "${local.resource_prefix}-voice-sessions"
  voice_session_lambda_name      = trimspace(var.voice_session_lambda_function_name) != "" ? trimspace(var.voice_session_lambda_function_name) : "${local.resource_prefix}-voice-session"
}
