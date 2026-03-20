locals {
  # Keep parameter names deterministic so Lambda config/IAM can reference
  # them without waiting on the backing resource to finish creating.
  db_username_ssm_parameter_name = "/${var.project_name}/${var.environment}/db/username"
  db_password_ssm_parameter_name = "/${var.project_name}/${var.environment}/db/password"
  db_host_ssm_parameter_name     = "/${var.project_name}/${var.environment}/db/host"

  db_username_ssm_parameter_arn = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.db_username_ssm_parameter_name}"
  db_password_ssm_parameter_arn = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.db_password_ssm_parameter_name}"
  db_host_ssm_parameter_arn     = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.db_host_ssm_parameter_name}"
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
