locals {
  migration_manifest_path     = "${path.module}/../../migrations/manifest.sha256"
  migration_artifact_path     = var.migration_lambda_artifact_path != "" ? var.migration_lambda_artifact_path : "${var.lambda_artifact_dir}/migrator.zip"
  migration_manifest_checksum = filesha256(local.migration_manifest_path)
  migration_artifact_checksum = fileexists(local.migration_artifact_path) ? filesha256(local.migration_artifact_path) : null
  migration_artifact_hash     = fileexists(local.migration_artifact_path) ? filebase64sha256(local.migration_artifact_path) : null
}

resource "aws_cloudwatch_log_group" "database_migrator" {
  name              = "/aws/lambda/${local.resource_prefix}-database-migrator"
  retention_in_days = var.log_retention_days
}

resource "aws_iam_role" "database_migrator" {
  name               = "${local.resource_prefix}-database-migrator-exec-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

data "aws_iam_policy_document" "database_migrator" {
  statement {
    sid       = "MigrationLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.database_migrator.arn}:*"]
  }

  statement {
    sid    = "MigrationVPC"
    effect = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DescribeSubnets",
      "ec2:DeleteNetworkInterface",
      "ec2:AssignPrivateIpAddresses",
      "ec2:UnassignPrivateIpAddresses"
    ]
    resources = ["*"]
  }

  statement {
    sid       = "MigrationParameters"
    effect    = "Allow"
    actions   = ["ssm:GetParameters"]
    resources = [local.db_host_ssm_parameter_arn]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }
  }

  statement {
    sid    = "MigrationSecret"
    effect = "Allow"
    actions = [
      "secretsmanager:DescribeSecret",
      "secretsmanager:GetSecretValue"
    ]
    resources = [aws_db_instance.main.master_user_secret[0].secret_arn]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }
  }

  statement {
    sid    = "MigrationSecretKMS"
    effect = "Allow"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey"
    ]
    resources = [aws_kms_key.application_secrets.arn]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["secretsmanager.${var.aws_region}.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:EncryptionContext:SecretARN"
      values   = [aws_db_instance.main.master_user_secret[0].secret_arn]
    }
  }
}

resource "aws_iam_role_policy" "database_migrator" {
  name   = "${local.resource_prefix}-database-migrator-policy"
  role   = aws_iam_role.database_migrator.id
  policy = data.aws_iam_policy_document.database_migrator.json
}

resource "aws_lambda_function" "database_migrator" {
  function_name    = "${local.resource_prefix}-database-migrator"
  role             = aws_iam_role.database_migrator.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = local.migration_artifact_path
  source_code_hash = local.migration_artifact_hash
  memory_size      = 256
  timeout          = 900

  reserved_concurrent_executions = 1

  environment {
    variables = {
      DATABASE_HOST_SSM_PARAM = local.db_host_ssm_parameter_name
      DATABASE_SECRET_ARN     = aws_db_instance.main.master_user_secret[0].secret_arn
      DATABASE_PORT           = tostring(var.db_port)
      DATABASE_NAME           = var.db_name
      DATABASE_SSL_MODE       = "require"
    }
  }

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.database_migrator.id]
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.migration_artifact_path)
      error_message = "Missing migration Lambda artifact ${local.migration_artifact_path}. Run make package-lambda-migrator from the repository root before running Terraform."
    }
  }

  depends_on = [
    aws_cloudwatch_log_group.database_migrator,
    aws_iam_role_policy.database_migrator,
    aws_ssm_parameter.db_host
  ]
}

resource "aws_lambda_invocation" "database_migrations" {
  function_name   = aws_lambda_function.database_migrator.function_name
  lifecycle_scope = "CREATE_ONLY"

  input = jsonencode({
    manifest_checksum = local.migration_manifest_checksum
  })

  triggers = {
    artifact_checksum = local.migration_artifact_checksum
    manifest_checksum = local.migration_manifest_checksum
  }

  lifecycle {
    postcondition {
      condition = (
        try(jsondecode(self.result).manifest_checksum, "") == local.migration_manifest_checksum &&
        try(jsondecode(self.result).dirty, true) == false
      )
      error_message = "Database migration invocation must return the expected checksum and a clean migration state."
    }
  }

  depends_on = [
    aws_route.private_default_egress,
    aws_ssm_association.nat_bootstrap_ready
  ]
}

locals {
  application_migration_checksum = jsondecode(aws_lambda_invocation.database_migrations.result).manifest_checksum
}
