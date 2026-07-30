data "aws_iam_policy_document" "application_secrets_kms" {
  statement {
    sid    = "AccountAdministration"
    effect = "Allow"
    actions = [
      "kms:CancelKeyDeletion",
      "kms:CreateAlias",
      "kms:CreateKey",
      "kms:DeleteAlias",
      "kms:DescribeKey",
      "kms:DisableKey",
      "kms:EnableKey",
      "kms:EnableKeyRotation",
      "kms:GetKeyPolicy",
      "kms:GetKeyRotationStatus",
      "kms:ListAliases",
      "kms:ListKeyPolicies",
      "kms:ListResourceTags",
      "kms:PutKeyPolicy",
      "kms:ScheduleKeyDeletion",
      "kms:TagResource",
      "kms:UntagResource",
      "kms:UpdateAlias",
      "kms:UpdateKeyDescription"
    ]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:root"]
    }
  }

  statement {
    sid    = "SecretsManagerServiceUse"
    effect = "Allow"
    actions = [
      "kms:CreateGrant",
      "kms:Decrypt",
      "kms:DescribeKey",
      "kms:Encrypt",
      "kms:GenerateDataKey*",
      "kms:ReEncrypt*"
    ]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:root"]
    }

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
  }
}

resource "aws_kms_key" "application_secrets" {
  description             = "${var.project_name}-${var.environment} application Secrets Manager encryption"
  enable_key_rotation     = true
  deletion_window_in_days = 30
  policy                  = data.aws_iam_policy_document.application_secrets_kms.json

  tags = {
    Name = "${var.project_name}-${var.environment}-application-secrets"
  }
}

resource "aws_kms_alias" "application_secrets" {
  name          = "alias/${var.project_name}-${var.environment}-application-secrets"
  target_key_id = aws_kms_key.application_secrets.key_id
}

resource "aws_secretsmanager_secret" "credential_encryption" {
  name                    = "/${var.project_name}/${var.environment}/application/credential-encryption"
  description             = "Application credential encryption key metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "billeif_invoice_cursor_hmac" {
  name                    = "/${var.project_name}/${var.environment}/application/billeif-invoice-cursor-hmac"
  description             = "Billeif invoice cursor HMAC key metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "razorpay" {
  name                    = "/${var.project_name}/${var.environment}/providers/razorpay"
  description             = "Razorpay credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "legacy_jwt" {
  name                    = "/${var.project_name}/${var.environment}/application/legacy-jwt"
  description             = "Legacy JWT signing credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "google_oauth" {
  name                    = "/${var.project_name}/${var.environment}/providers/google-oauth"
  description             = "Google OAuth credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "fcm" {
  name                    = "/${var.project_name}/${var.environment}/notifications/fcm"
  description             = "Firebase Cloud Messaging credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "apns" {
  name                    = "/${var.project_name}/${var.environment}/notifications/apns"
  description             = "Apple Push Notification service credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "llm" {
  name                    = "/${var.project_name}/${var.environment}/providers/llm"
  description             = "Primary LLM provider credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "exa" {
  name                    = "/${var.project_name}/${var.environment}/providers/exa"
  description             = "Exa provider credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "gst_lookup" {
  name                    = "/${var.project_name}/${var.environment}/providers/gst-lookup"
  description             = "GST lookup provider credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "gst_provider" {
  name                    = "/${var.project_name}/${var.environment}/providers/gst"
  description             = "GST filing provider credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "deepgram" {
  name                    = "/${var.project_name}/${var.environment}/providers/deepgram"
  description             = "Deepgram voice provider credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret" "deepseek" {
  name                    = "/${var.project_name}/${var.environment}/providers/deepseek"
  description             = "DeepSeek voice LLM credential metadata"
  kms_key_id              = aws_kms_key.application_secrets.arn
  recovery_window_in_days = 7
}
