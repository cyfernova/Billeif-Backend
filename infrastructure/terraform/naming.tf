locals {
  resource_name_prefix = "${var.project_name}-${var.environment}"

  cognito_domain_prefix = trimspace(var.cognito_domain_prefix) != "" ? trimspace(var.cognito_domain_prefix) : "${local.resource_name_prefix}-${data.aws_caller_identity.current.account_id}"

  phone_auth_cooldown_table_name     = trimspace(var.phone_auth_cooldown_table_name) != "" ? trimspace(var.phone_auth_cooldown_table_name) : "${local.resource_name_prefix}-phone-auth-cooldowns"
  websocket_connections_table_name   = trimspace(var.websocket_connections_table) != "" ? trimspace(var.websocket_connections_table) : "${local.resource_name_prefix}-ws-connections"
  voice_sessions_table_name          = trimspace(var.voice_sessions_table_name) != "" ? trimspace(var.voice_sessions_table_name) : "${local.resource_name_prefix}-voice-sessions"
  voice_session_lambda_function_name = trimspace(var.voice_session_lambda_function_name) != "" ? trimspace(var.voice_session_lambda_function_name) : "${local.resource_name_prefix}-voice-session"
}
