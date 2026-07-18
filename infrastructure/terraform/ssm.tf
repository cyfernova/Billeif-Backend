locals {
  # Keep parameter names deterministic so Lambda config/IAM can reference
  # them without waiting on the backing resource to finish creating.
  db_username_ssm_parameter_name = "/${var.project_name}/${var.environment}/db/username"
  db_password_ssm_parameter_name = "/${var.project_name}/${var.environment}/db/password"
  db_host_ssm_parameter_name     = "/${var.project_name}/${var.environment}/db/host"

  razorpay_key_id_ssm_parameter_name           = "/${var.project_name}/${var.environment}/razorpay/key-id"
  razorpay_key_secret_ssm_parameter_name       = "/${var.project_name}/${var.environment}/razorpay/key-secret"
  razorpay_webhook_secret_ssm_parameter_name   = "/${var.project_name}/${var.environment}/razorpay/webhook-secret"
  credential_encryption_key_ssm_parameter_name = "/${var.project_name}/${var.environment}/application/credential-encryption-key"
  llm_api_key_ssm_parameter_name               = "/${var.project_name}/${var.environment}/providers/llm-api-key"
  exa_api_key_ssm_parameter_name               = "/${var.project_name}/${var.environment}/providers/exa-api-key"
  gst_lookup_api_key_ssm_parameter_name        = "/${var.project_name}/${var.environment}/providers/gst-lookup-api-key"
  deepgram_api_key_ssm_parameter_name          = "/${var.project_name}/${var.environment}/providers/deepgram-api-key"
  deepseek_api_key_ssm_parameter_name          = "/${var.project_name}/${var.environment}/providers/deepseek-api-key"

  db_username_ssm_parameter_arn               = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.db_username_ssm_parameter_name}"
  db_password_ssm_parameter_arn               = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.db_password_ssm_parameter_name}"
  db_host_ssm_parameter_arn                   = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.db_host_ssm_parameter_name}"
  razorpay_key_id_ssm_parameter_arn           = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.razorpay_key_id_ssm_parameter_name}"
  razorpay_key_secret_ssm_parameter_arn       = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.razorpay_key_secret_ssm_parameter_name}"
  razorpay_webhook_secret_ssm_parameter_arn   = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.razorpay_webhook_secret_ssm_parameter_name}"
  credential_encryption_key_ssm_parameter_arn = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.credential_encryption_key_ssm_parameter_name}"
  llm_api_key_ssm_parameter_arn               = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.llm_api_key_ssm_parameter_name}"
  exa_api_key_ssm_parameter_arn               = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.exa_api_key_ssm_parameter_name}"
  gst_lookup_api_key_ssm_parameter_arn        = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.gst_lookup_api_key_ssm_parameter_name}"
  deepgram_api_key_ssm_parameter_arn          = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.deepgram_api_key_ssm_parameter_name}"
  deepseek_api_key_ssm_parameter_arn          = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.deepseek_api_key_ssm_parameter_name}"
}

resource "aws_ssm_parameter" "db_username" {
  name        = local.db_username_ssm_parameter_name
  description = "Database username for ${var.project_name}"
  type        = "SecureString"
  value       = var.db_username
  overwrite   = true
}

resource "aws_ssm_parameter" "db_password" {
  name        = local.db_password_ssm_parameter_name
  description = "Database password for ${var.project_name}"
  type        = "SecureString"
  value       = var.db_password
  overwrite   = true
}

resource "aws_ssm_parameter" "db_host" {
  name        = local.db_host_ssm_parameter_name
  description = "Database host for ${var.project_name}"
  type        = "String"
  value       = aws_db_instance.main.address
  overwrite   = true
}

resource "aws_ssm_parameter" "razorpay_key_id" {
  name        = local.razorpay_key_id_ssm_parameter_name
  description = "Razorpay key ID for ${var.project_name}"
  type        = "SecureString"
  value       = var.razorpay_key_id
  overwrite   = true
}

resource "aws_ssm_parameter" "razorpay_key_secret" {
  name        = local.razorpay_key_secret_ssm_parameter_name
  description = "Razorpay key secret for ${var.project_name}"
  type        = "SecureString"
  value       = var.razorpay_key_secret
  overwrite   = true
}

resource "aws_ssm_parameter" "razorpay_webhook_secret" {
  name        = local.razorpay_webhook_secret_ssm_parameter_name
  description = "Razorpay webhook secret for ${var.project_name}"
  type        = "SecureString"
  value       = var.razorpay_webhook_secret
  overwrite   = true
}

resource "aws_ssm_parameter" "credential_encryption_key" {
  name        = local.credential_encryption_key_ssm_parameter_name
  description = "Application credential encryption key for ${var.project_name}"
  type        = "SecureString"
  value       = var.credential_encryption_key
  overwrite   = true
}

resource "aws_ssm_parameter" "llm_api_key" {
  name        = local.llm_api_key_ssm_parameter_name
  description = "LLM provider API key for ${var.project_name}"
  type        = "SecureString"
  value       = var.llm_api_key
  overwrite   = true
}

resource "aws_ssm_parameter" "exa_api_key" {
  name        = local.exa_api_key_ssm_parameter_name
  description = "Exa API key for ${var.project_name}"
  type        = "SecureString"
  value       = var.exa_api_key
  overwrite   = true
}

resource "aws_ssm_parameter" "gst_lookup_api_key" {
  name        = local.gst_lookup_api_key_ssm_parameter_name
  description = "GST lookup API key for ${var.project_name}"
  type        = "SecureString"
  value       = var.gst_lookup_api_key
  overwrite   = true
}

resource "aws_ssm_parameter" "deepgram_api_key" {
  name        = local.deepgram_api_key_ssm_parameter_name
  description = "Deepgram API key for ${var.project_name}"
  type        = "SecureString"
  value       = var.deepgram_api_key
  overwrite   = true
}

resource "aws_ssm_parameter" "deepseek_api_key" {
  name        = local.deepseek_api_key_ssm_parameter_name
  description = "DeepSeek API key for ${var.project_name}"
  type        = "SecureString"
  value       = var.deepseek_api_key
  overwrite   = true
}
