data "aws_iam_policy_document" "cognito_phone_custom_sms_key" {
  statement {
    sid    = "EnableRootPermissions"
    effect = "Allow"
    actions = [
      "kms:*"
    ]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"]
    }
  }

  statement {
    sid    = "AllowCustomSMSLambdaDecrypt"
    effect = "Allow"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey"
    ]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = [aws_iam_role.cognito_phone_custom_sms.arn]
    }
  }
}

locals {
  cognito_phone_sms_external_id = "${var.project_name}-${var.environment}-cognito-phone-sms"
}

resource "aws_kms_key" "cognito_phone_custom_sms" {
  provider                = aws.ap_south_1
  description             = "KMS key used by Cognito custom SMS sender for India phone auth"
  deletion_window_in_days = 7
  enable_key_rotation     = true
  policy                  = data.aws_iam_policy_document.cognito_phone_custom_sms_key.json
}

resource "aws_kms_alias" "cognito_phone_custom_sms" {
  provider      = aws.ap_south_1
  name          = "alias/${var.project_name}-${var.environment}-cognito-phone-custom-sms"
  target_key_id = aws_kms_key.cognito_phone_custom_sms.key_id
}

resource "aws_iam_role" "cognito_phone_custom_sms" {
  name               = "${local.resource_prefix}-cognito-phone-custom-sms-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

data "aws_iam_policy_document" "cognito_phone_sms_assume_role" {
  statement {
    effect = "Allow"
    actions = [
      "sts:AssumeRole",
    ]

    principals {
      type        = "Service"
      identifiers = ["cognito-idp.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }

    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [local.cognito_phone_sms_external_id]
    }

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:aws:cognito-idp:ap-south-1:${data.aws_caller_identity.current.account_id}:userpool/*"]
    }
  }
}

resource "aws_iam_role" "cognito_phone_sms" {
  name               = "${local.resource_prefix}-cognito-phone-sms-role"
  assume_role_policy = data.aws_iam_policy_document.cognito_phone_sms_assume_role.json
}

data "aws_iam_policy_document" "cognito_phone_sms" {
  statement {
    sid    = "SNSPublish"
    effect = "Allow"
    actions = [
      "sns:Publish"
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "cognito_phone_sms" {
  name   = "${local.resource_prefix}-cognito-phone-sms-policy"
  role   = aws_iam_role.cognito_phone_sms.id
  policy = data.aws_iam_policy_document.cognito_phone_sms.json
}

resource "aws_iam_role_policy_attachment" "cognito_phone_custom_sms_basic" {
  role       = aws_iam_role.cognito_phone_custom_sms.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "cognito_phone_custom_sms" {
  statement {
    sid    = "SNSPublish"
    effect = "Allow"
    actions = [
      "sns:Publish"
    ]
    resources = ["*"]
  }

  statement {
    sid    = "KMSDecrypt"
    effect = "Allow"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey"
    ]
    resources = [aws_kms_key.cognito_phone_custom_sms.arn]
  }
}

resource "aws_iam_role_policy" "cognito_phone_custom_sms" {
  name   = "${local.resource_prefix}-cognito-phone-custom-sms-policy"
  role   = aws_iam_role.cognito_phone_custom_sms.id
  policy = data.aws_iam_policy_document.cognito_phone_custom_sms.json
}

resource "aws_cloudwatch_log_group" "cognito_phone_custom_sms" {
  provider          = aws.ap_south_1
  name              = "/aws/lambda/${local.resource_prefix}-cognito-phone-custom-sms"
  retention_in_days = var.log_retention_days
}

resource "aws_lambda_function" "custom_sms_sender" {
  provider         = aws.ap_south_1
  function_name    = "${local.resource_prefix}-cognito-phone-custom-sms"
  role             = aws_iam_role.cognito_phone_custom_sms.arn
  runtime          = "nodejs20.x"
  handler          = "index.handler"
  architectures    = ["arm64"]
  filename         = local.lambda_artifacts.custom_sms_sender
  source_code_hash = local.lambda_artifact_hashes.custom_sms_sender
  memory_size      = 256
  timeout          = 15

  reserved_concurrent_executions = var.enable_application ? (var.enable_lambda_reserved_concurrency ? 2 : null) : 0

  tags = {
    MigrationChecksum = local.application_migration_checksum
  }

  environment {
    variables = {
      KEY_ID                        = aws_kms_key.cognito_phone_custom_sms.key_id
      KEY_ARN                       = aws_kms_key.cognito_phone_custom_sms.arn
      SMS_REGION                    = "ap-south-1"
      INDIA_SENDER_ID               = var.india_sms_sender_id
      INDIA_DLT_ENTITY_ID           = var.india_dlt_entity_id
      INDIA_SIGNUP_TEMPLATE_ID      = var.india_signup_template_id
      INDIA_AUTH_TEMPLATE_ID        = var.india_auth_template_id
      INDIA_SIGNUP_MESSAGE_TEMPLATE = var.india_signup_message_template
      INDIA_AUTH_MESSAGE_TEMPLATE   = var.india_auth_message_template
    }
  }

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.custom_sms_sender)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.custom_sms_sender}. Run make package-lambda from the repository root before running Terraform."
    }

    precondition {
      condition = trimspace(var.india_sms_sender_id) == "" || alltrue([
        trimspace(var.india_sms_sender_id) != "",
        trimspace(var.india_dlt_entity_id) != "",
        trimspace(var.india_signup_template_id) != "",
        trimspace(var.india_auth_template_id) != "",
        can(regex("^[A-Za-z]{3,6}$", trimspace(var.india_sms_sender_id))),
        strcontains(var.india_signup_message_template, "{####}"),
        strcontains(var.india_auth_message_template, "{####}"),
      ])
      error_message = "India phone auth requires non-empty india_sms_sender_id, india_dlt_entity_id, india_signup_template_id, india_auth_template_id, and message templates with {####} placeholder. Set TF_VAR_india_sms_sender_id etc. in deploy.yml or leave india_sms_sender_id empty to skip."
    }
  }

  depends_on = [
    aws_ssm_association.nat_activation_ready,
    aws_nat_gateway.main,
    aws_cloudwatch_log_group.cognito_phone_custom_sms,
  ]
}

resource "aws_cognito_user_pool" "phone" {
  provider                 = aws.ap_south_1
  name                     = local.cognito_native_user_pool_name
  alias_attributes         = ["phone_number"]
  auto_verified_attributes = ["phone_number"]
  mfa_configuration        = "OFF"
  user_pool_tier           = "ESSENTIALS"

  schema {
    attribute_data_type = "String"
    name                = "name"
    required            = true
    mutable             = true
    string_attribute_constraints {
      min_length = 2
      max_length = 255
    }
  }

  sign_in_policy {
    allowed_first_auth_factors = ["PASSWORD", "SMS_OTP"]
  }

  sms_configuration {
    external_id    = local.cognito_phone_sms_external_id
    sns_caller_arn = aws_iam_role.cognito_phone_sms.arn
    sns_region     = "ap-south-1"
  }

  lambda_config {
    kms_key_id = aws_kms_key.cognito_phone_custom_sms.arn

    custom_sms_sender {
      lambda_arn     = aws_lambda_function.custom_sms_sender.arn
      lambda_version = "V1_0"
    }
  }

  depends_on = [
    aws_lambda_permission.cognito_phone_custom_sms,
    aws_iam_role_policy.cognito_phone_sms,
  ]
}

resource "aws_lambda_permission" "cognito_phone_custom_sms" {
  count = var.enable_application ? 1 : 0

  provider       = aws.ap_south_1
  statement_id   = "AllowExecutionFromCognitoPhoneUserPool"
  action         = "lambda:InvokeFunction"
  function_name  = aws_lambda_function.custom_sms_sender.function_name
  principal      = "cognito-idp.amazonaws.com"
  source_account = data.aws_caller_identity.current.account_id
  source_arn     = "arn:aws:cognito-idp:ap-south-1:${data.aws_caller_identity.current.account_id}:userpool/*"
}

resource "aws_cognito_user_pool_client" "phone" {
  provider     = aws.ap_south_1
  name         = local.cognito_native_client_name
  user_pool_id = aws_cognito_user_pool.phone.id

  explicit_auth_flows           = ["ALLOW_USER_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"]
  generate_secret               = false
  prevent_user_existence_errors = "ENABLED"
  enable_token_revocation       = true

  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}
