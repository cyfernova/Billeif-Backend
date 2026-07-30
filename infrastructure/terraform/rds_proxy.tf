data "aws_iam_policy_document" "rds_proxy_assume_role" {
  count = var.enable_rds_proxy ? 1 : 0

  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["rds.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "rds_proxy" {
  count              = var.enable_rds_proxy ? 1 : 0
  name               = "${local.resource_prefix}-rds-proxy-role"
  assume_role_policy = data.aws_iam_policy_document.rds_proxy_assume_role[0].json
}

data "aws_iam_policy_document" "rds_proxy" {
  count = var.enable_rds_proxy ? 1 : 0

  statement {
    sid       = "ReadBilleifRDSSecret"
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_db_instance.main.master_user_secret[0].secret_arn]
  }

  statement {
    sid       = "DecryptBilleifRDSSecret"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.application_secrets.arn]

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

resource "aws_iam_role_policy" "rds_proxy" {
  count  = var.enable_rds_proxy ? 1 : 0
  name   = "${local.resource_prefix}-rds-proxy-policy"
  role   = aws_iam_role.rds_proxy[0].id
  policy = data.aws_iam_policy_document.rds_proxy[0].json
}

resource "aws_db_proxy" "main" {
  count = var.enable_rds_proxy ? 1 : 0

  name                   = "${local.resource_prefix}-rds-proxy"
  debug_logging          = false
  engine_family          = "POSTGRESQL"
  idle_client_timeout    = 1800
  require_tls            = true
  role_arn               = aws_iam_role.rds_proxy[0].arn
  vpc_security_group_ids = [aws_security_group.lambda.id]
  vpc_subnet_ids         = aws_subnet.private[*].id

  auth {
    auth_scheme = "SECRETS"
    description = "Billeif RDS managed master credential"
    iam_auth    = "DISABLED"
    secret_arn  = aws_db_instance.main.master_user_secret[0].secret_arn
  }

  tags = {
    Name = "${local.resource_prefix}-rds-proxy"
  }

  depends_on = [aws_iam_role_policy.rds_proxy]
}

resource "aws_db_proxy_default_target_group" "main" {
  count = var.enable_rds_proxy ? 1 : 0

  db_proxy_name = aws_db_proxy.main[0].name

  connection_pool_config {
    max_connections_percent      = 90
    max_idle_connections_percent = 50
    connection_borrow_timeout    = 30
  }
}

resource "aws_db_proxy_target" "main" {
  count = var.enable_rds_proxy ? 1 : 0

  db_instance_identifier = aws_db_instance.main.identifier
  db_proxy_name          = aws_db_proxy.main[0].name
  target_group_name      = aws_db_proxy_default_target_group.main[0].name
}
