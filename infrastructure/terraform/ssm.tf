resource "aws_ssm_parameter" "db_username" {
  name        = "/${var.project_name}/${var.environment}/db/username"
  description = "Database username for ${var.project_name}"
  type        = "SecureString"
  value       = var.db_username
  overwrite   = true
}

resource "aws_ssm_parameter" "db_password" {
  name        = "/${var.project_name}/${var.environment}/db/password"
  description = "Database password for ${var.project_name}"
  type        = "SecureString"
  value       = var.db_password
  overwrite   = true
}

resource "aws_ssm_parameter" "credential_encryption_key" {
  name        = "/${var.project_name}/${var.environment}/app/credential_encryption_key"
  description = "Credential encryption key for ${var.project_name}"
  type        = "SecureString"
  value       = var.credential_encryption_key
  overwrite   = true
}
