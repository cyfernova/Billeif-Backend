data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

locals {
  http_runtime_secret_arns = [
    aws_db_instance.main.master_user_secret[0].secret_arn,
    aws_secretsmanager_secret.credential_encryption.arn,
    aws_secretsmanager_secret.razorpay.arn,
    aws_secretsmanager_secret.llm.arn,
    aws_secretsmanager_secret.exa.arn,
    aws_secretsmanager_secret.gst_lookup.arn,
    aws_secretsmanager_secret.gst_provider.arn,
    aws_secretsmanager_secret.deepseek.arn,
    aws_secretsmanager_secret.sarvam.arn
  ]
  worker_runtime_secret_arns = {
    invoice = [
      aws_db_instance.main.master_user_secret[0].secret_arn,
      aws_secretsmanager_secret.credential_encryption.arn
    ]
    gst = [
      aws_db_instance.main.master_user_secret[0].secret_arn,
      aws_secretsmanager_secret.credential_encryption.arn,
      aws_secretsmanager_secret.gst_provider.arn
    ]
    bargaining = [
      aws_db_instance.main.master_user_secret[0].secret_arn,
      aws_secretsmanager_secret.credential_encryption.arn,
      aws_secretsmanager_secret.llm.arn,
      aws_secretsmanager_secret.exa.arn
    ]
  }
}

data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "apigateway_cloudwatch_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["apigateway.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "apigateway_cloudwatch" {
  name                 = "${local.resource_prefix}-apigateway-cloudwatch-role"
  assume_role_policy   = data.aws_iam_policy_document.apigateway_cloudwatch_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "apigateway_cloudwatch" {
  role       = aws_iam_role.apigateway_cloudwatch.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/service-role/AmazonAPIGatewayPushToCloudWatchLogs"
}

resource "aws_api_gateway_account" "main" {
  cloudwatch_role_arn = aws_iam_role.apigateway_cloudwatch.arn

  depends_on = [aws_iam_role_policy_attachment.apigateway_cloudwatch]
}

resource "aws_iam_role" "lambda_exec" {
  name                 = "${local.resource_prefix}-lambda-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role" "lambda_http_exec" {
  name                 = "${local.resource_prefix}-lambda-http-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_http_basic" {
  role       = aws_iam_role.lambda_http_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_vpc_access" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_http_vpc_access" {
  role       = aws_iam_role.lambda_http_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_iam_role" "lambda_worker_exec" {
  for_each = local.worker_runtime_secret_arns

  name                 = "${local.resource_prefix}-lambda-${each.key}-worker-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "lambda_worker_basic" {
  for_each = local.worker_runtime_secret_arns

  role       = aws_iam_role.lambda_worker_exec[each.key].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_worker_vpc_access" {
  for_each = local.worker_runtime_secret_arns

  role       = aws_iam_role.lambda_worker_exec[each.key].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_iam_role" "lambda_websocket_exec" {
  name                 = "${local.resource_prefix}-lambda-websocket-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "lambda_websocket_basic" {
  role       = aws_iam_role.lambda_websocket_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_websocket_vpc_access" {
  role       = aws_iam_role.lambda_websocket_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_iam_role" "outbox_dispatcher" {
  name                 = "${local.resource_prefix}-outbox-dispatcher-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "outbox_dispatcher_basic" {
  role       = aws_iam_role.outbox_dispatcher.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "outbox_dispatcher_vpc_access" {
  role       = aws_iam_role.outbox_dispatcher.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_iam_role" "email_delivery" {
  name                 = "${local.resource_prefix}-email-delivery-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "email_delivery_basic" {
  role       = aws_iam_role.email_delivery.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "email_delivery_vpc_access" {
  role       = aws_iam_role.email_delivery.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

data "aws_iam_policy_document" "email_delivery" {
  statement {
    sid       = "EmailDeliveryParameters"
    effect    = "Allow"
    actions   = ["ssm:GetParameters"]
    resources = [local.db_host_ssm_parameter_arn]
  }

  statement {
    sid       = "EmailDeliveryDatabaseSecret"
    effect    = "Allow"
    actions   = ["secretsmanager:DescribeSecret", "secretsmanager:GetSecretValue"]
    resources = [aws_db_instance.main.master_user_secret[0].secret_arn]
  }

  statement {
    sid       = "EmailDeliveryDatabaseSecretKMS"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:DescribeKey"]
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

  statement {
    sid    = "EmailDeliveryQueue"
    effect = "Allow"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:ChangeMessageVisibility",
    ]
    resources = [aws_sqs_queue.email_delivery.arn]
  }

  statement {
    sid       = "EmailDeliveryFinalPDF"
    effect    = "Allow"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.invoices_pdf.arn}/invoices/*/*/v*/final.pdf"]
  }

  statement {
    sid     = "EmailDeliverySES"
    effect  = "Allow"
    actions = ["ses:SendRawEmail"]
    resources = [
      local.ses_verified_identity_arn,
      "arn:${data.aws_partition.current.partition}:ses:${var.aws_region}:${data.aws_caller_identity.current.account_id}:configuration-set/${aws_ses_configuration_set.main.name}",
    ]

    condition {
      test     = "StringEquals"
      variable = "ses:FromAddress"
      values   = [var.ses_sender_email]
    }
  }
}

resource "aws_iam_role_policy" "email_delivery" {
  name   = "${local.resource_prefix}-email-delivery-policy"
  role   = aws_iam_role.email_delivery.id
  policy = data.aws_iam_policy_document.email_delivery.json
}

resource "aws_iam_role" "ses_feedback" {
  name                 = "${local.resource_prefix}-ses-feedback-exec-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "ses_feedback_basic" {
  role       = aws_iam_role.ses_feedback.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "ses_feedback_vpc_access" {
  role       = aws_iam_role.ses_feedback.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

data "aws_iam_policy_document" "ses_feedback" {
  statement {
    sid       = "SESFeedbackParameters"
    effect    = "Allow"
    actions   = ["ssm:GetParameters"]
    resources = [local.db_host_ssm_parameter_arn]
  }

  statement {
    sid       = "SESFeedbackDatabaseSecret"
    effect    = "Allow"
    actions   = ["secretsmanager:DescribeSecret", "secretsmanager:GetSecretValue"]
    resources = [aws_db_instance.main.master_user_secret[0].secret_arn]
  }

  statement {
    sid       = "SESFeedbackDatabaseSecretKMS"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:DescribeKey"]
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

  statement {
    sid    = "SESFeedbackQueue"
    effect = "Allow"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:ChangeMessageVisibility",
    ]
    resources = [aws_sqs_queue.ses_feedback.arn]
  }
}

resource "aws_iam_role_policy" "ses_feedback" {
  name   = "${local.resource_prefix}-ses-feedback-policy"
  role   = aws_iam_role.ses_feedback.id
  policy = data.aws_iam_policy_document.ses_feedback.json
}

data "aws_iam_policy_document" "outbox_dispatcher" {
  statement {
    sid       = "OutboxParameters"
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
    sid    = "OutboxSecret"
    effect = "Allow"
    actions = [
      "secretsmanager:DescribeSecret",
      "secretsmanager:GetSecretValue",
    ]
    resources = [aws_db_instance.main.master_user_secret[0].secret_arn]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }
  }

  statement {
    sid    = "OutboxSecretKMS"
    effect = "Allow"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey",
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

  statement {
    sid     = "OutboxQueues"
    effect  = "Allow"
    actions = ["sqs:SendMessage"]
    resources = [
      aws_sqs_queue.invoice_processing.arn,
      aws_sqs_queue.email_delivery.arn,
    ]
  }
}

resource "aws_iam_role_policy" "outbox_dispatcher" {
  name   = "${local.resource_prefix}-outbox-dispatcher-policy"
  role   = aws_iam_role.outbox_dispatcher.id
  policy = data.aws_iam_policy_document.outbox_dispatcher.json
}

data "aws_iam_policy_document" "lambda_app" {
  statement {
    sid    = "S3Access"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:ListBucket"
    ]
    resources = [
      aws_s3_bucket.business_logos.arn,
      "${aws_s3_bucket.business_logos.arn}/*",
      aws_s3_bucket.invoices_pdf.arn,
      "${aws_s3_bucket.invoices_pdf.arn}/*",
      aws_s3_bucket.product_images.arn,
      "${aws_s3_bucket.product_images.arn}/*",
      aws_s3_bucket.email_sink.arn,
      "${aws_s3_bucket.email_sink.arn}/*"
    ]
  }

  statement {
    sid    = "SQSAccess"
    effect = "Allow"
    actions = [
      "sqs:SendMessage",
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:ChangeMessageVisibility"
    ]
    resources = [
      aws_sqs_queue.invoice_processing.arn,
      aws_sqs_queue.gst_processing.arn,
      aws_sqs_queue.bargaining_negotiation.arn,
      aws_sqs_queue.invoice_processing_dlq.arn,
      aws_sqs_queue.gst_processing_dlq.arn,
      aws_sqs_queue.bargaining_negotiation_dlq.arn
    ]
  }

  statement {
    sid    = "SESAndSNS"
    effect = "Allow"
    actions = [
      "ses:SendEmail",
      "ses:SendRawEmail",
      "sns:Publish"
    ]
    resources = [
      local.ses_verified_identity_arn,
      aws_sns_topic.alerts.arn,
      aws_sns_topic.low_stock_alerts.arn
    ]
  }

  statement {
    sid       = "EmailDeliveryQueueSend"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.email_delivery.arn]
  }

  statement {
    sid    = "CognitoAccess"
    effect = "Allow"
    actions = [
      "cognito-idp:SignUp",
      "cognito-idp:InitiateAuth",
      "cognito-idp:GlobalSignOut",
      "cognito-idp:ForgotPassword",
      "cognito-idp:ConfirmForgotPassword",
      "cognito-idp:ConfirmSignUp",
      "cognito-idp:ResendConfirmationCode",
      "cognito-idp:ChangePassword",
      "cognito-idp:AdminCreateUser",
      "cognito-idp:AdminDeleteUser",
      "cognito-idp:AdminConfirmSignUp",
      "cognito-idp:ListUsers"
    ]
    resources = [
      aws_cognito_user_pool.main.arn,
      aws_cognito_user_pool.phone.arn
    ]
  }

  statement {
    sid    = "DynamoDBAccess"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:DeleteItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query",
      "dynamodb:Scan"
    ]
    resources = [
      aws_dynamodb_table.ws_connections.arn,
      "${aws_dynamodb_table.ws_connections.arn}/index/*",
      aws_dynamodb_table.users_sessions.arn,
      aws_dynamodb_table.refresh_tokens.arn,
      aws_dynamodb_table.password_reset_tokens.arn,
      aws_dynamodb_table.mfa_codes.arn,
      aws_dynamodb_table.phone_auth_cooldowns.arn,
      aws_dynamodb_table.customers_cache.arn,
      aws_dynamodb_table.vendors_cache.arn,
      aws_dynamodb_table.products_cache.arn,
      aws_dynamodb_table.invoices_cache.arn,
      aws_dynamodb_table.ledger_cache.arn,
      aws_dynamodb_table.payments_cache.arn
    ]
  }

  statement {
    sid       = "SSMAccess"
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
    sid    = "SecretsManagerAccess"
    effect = "Allow"
    actions = [
      "secretsmanager:DescribeSecret",
      "secretsmanager:GetSecretValue"
    ]
    resources = local.http_runtime_secret_arns

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }
  }

  statement {
    sid    = "ApplicationSecretsKMSDecrypt"
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
      values   = local.http_runtime_secret_arns
    }
  }

  statement {
    sid    = "WebSocketManageConnections"
    effect = "Allow"
    actions = [
      "execute-api:ManageConnections"
    ]
    resources = ["${aws_apigatewayv2_api.websocket.execution_arn}/${var.environment}/POST/@connections/*"]
  }
}

resource "aws_iam_role_policy" "lambda_app" {
  name   = "${local.resource_prefix}-lambda-app-policy"
  role   = aws_iam_role.lambda_exec.id
  policy = data.aws_iam_policy_document.lambda_app.json
}

resource "aws_iam_role_policy" "lambda_http_app" {
  name   = "${local.resource_prefix}-lambda-http-app-policy"
  role   = aws_iam_role.lambda_http_exec.id
  policy = data.aws_iam_policy_document.lambda_app.json
}

data "aws_iam_policy_document" "invoice_cursor_http" {
  statement {
    sid    = "InvoiceCursorSecret"
    effect = "Allow"
    actions = [
      "secretsmanager:DescribeSecret",
      "secretsmanager:GetSecretValue"
    ]
    resources = [aws_secretsmanager_secret.billeif_invoice_cursor_hmac.arn]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }
  }

  statement {
    sid       = "InvoiceCursorKMSDecrypt"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
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
      values   = [aws_secretsmanager_secret.billeif_invoice_cursor_hmac.arn]
    }
  }
}

resource "aws_iam_role_policy" "invoice_cursor_http" {
  name   = "${local.resource_prefix}-invoice-cursor-http-policy"
  role   = aws_iam_role.lambda_http_exec.id
  policy = data.aws_iam_policy_document.invoice_cursor_http.json
}

data "aws_iam_policy_document" "lambda_worker_app" {
  for_each = local.worker_runtime_secret_arns

  statement {
    sid    = "WorkerQueues"
    effect = "Allow"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:ChangeMessageVisibility",
      "sqs:SendMessage"
    ]
    resources = [
      aws_sqs_queue.invoice_processing.arn,
      aws_sqs_queue.gst_processing.arn,
      aws_sqs_queue.bargaining_negotiation.arn,
      aws_sqs_queue.invoice_processing_dlq.arn,
      aws_sqs_queue.gst_processing_dlq.arn,
      aws_sqs_queue.bargaining_negotiation_dlq.arn,
    ]
  }

  statement {
    sid    = "WorkerDocuments"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject"
    ]
    resources = [
      "${aws_s3_bucket.invoices_pdf.arn}/*",
      "${aws_s3_bucket.email_sink.arn}/*"
    ]
  }

  dynamic "statement" {
    for_each = length(each.value) > 0 ? [true] : []
    content {
      sid       = "WorkerParameters"
      effect    = "Allow"
      actions   = ["ssm:GetParameters"]
      resources = [local.db_host_ssm_parameter_arn]

      condition {
        test     = "Bool"
        variable = "aws:SecureTransport"
        values   = ["true"]
      }
    }
  }

  dynamic "statement" {
    for_each = length(each.value) > 0 ? [true] : []
    content {
      sid    = "WorkerSecrets"
      effect = "Allow"
      actions = [
        "secretsmanager:DescribeSecret",
        "secretsmanager:GetSecretValue"
      ]
      resources = each.value

      condition {
        test     = "Bool"
        variable = "aws:SecureTransport"
        values   = ["true"]
      }
    }
  }

  dynamic "statement" {
    for_each = length(each.value) > 0 ? [true] : []
    content {
      sid    = "WorkerSecretsKMS"
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
        values   = each.value
      }
    }
  }

}

resource "aws_iam_role_policy" "lambda_worker_app" {
  for_each = local.worker_runtime_secret_arns

  name   = "${local.resource_prefix}-lambda-${each.key}-worker-policy"
  role   = aws_iam_role.lambda_worker_exec[each.key].id
  policy = data.aws_iam_policy_document.lambda_worker_app[each.key].json
}

data "aws_iam_policy_document" "lambda_websocket_app" {
  statement {
    sid       = "WebSocketParameters"
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
    sid    = "WebSocketSecrets"
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
    sid    = "WebSocketSecretsKMS"
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

  statement {
    sid    = "WebSocketTables"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:DeleteItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query"
    ]
    resources = [
      aws_dynamodb_table.ws_connections.arn,
      "${aws_dynamodb_table.ws_connections.arn}/index/*"
    ]
  }

  statement {
    sid       = "WebSocketManageConnections"
    effect    = "Allow"
    actions   = ["execute-api:ManageConnections"]
    resources = ["${aws_apigatewayv2_api.websocket.execution_arn}/${var.environment}/POST/@connections/*"]
  }
}

resource "aws_iam_role_policy" "lambda_websocket_app" {
  name   = "${local.resource_prefix}-lambda-websocket-policy"
  role   = aws_iam_role.lambda_websocket_exec.id
  policy = data.aws_iam_policy_document.lambda_websocket_app.json
}
