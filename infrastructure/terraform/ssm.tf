locals {
  db_host_ssm_parameter_name = "/${var.project_name}/${var.environment}/db/host"
  db_host_ssm_parameter_arn  = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.db_host_ssm_parameter_name}"
}

resource "aws_ssm_parameter" "db_host" {
  name        = local.db_host_ssm_parameter_name
  description = "Database host for ${local.resource_prefix}"
  type        = "String"
  value       = var.enable_rds_proxy ? aws_db_proxy.main[0].endpoint : aws_db_instance.main.address
  overwrite   = true
}
