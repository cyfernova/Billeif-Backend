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
    aws_secretsmanager_secret.gst_provider.arn
  ]
  worker_runtime_secret_arns = {
    invoice = [
      aws_db_instance.main.master_user_secret[0].secret_arn,
      aws_secretsmanager_secret.credential_encryption.arn
    ]
    payment = []
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
  name               = "${var.project_name}-apigateway-cloudwatch-role"
  assume_role_policy = data.aws_iam_policy_document.apigateway_cloudwatch_assume_role.json
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
  name               = "${var.project_name}-lambda-exec-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_vpc_access" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_iam_role" "lambda_worker_exec" {
  for_each = local.worker_runtime_secret_arns

  name               = "${var.project_name}-lambda-${each.key}-worker-exec-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
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
  name               = "${var.project_name}-lambda-websocket-exec-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "lambda_websocket_basic" {
  role       = aws_iam_role.lambda_websocket_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role" "lambda_voice_exec" {
  name               = "${var.project_name}-lambda-voice-exec-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "lambda_voice_basic" {
  role       = aws_iam_role.lambda_voice_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
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
      aws_sqs_queue.payment_processing.arn,
      aws_sqs_queue.gst_processing.arn,
      aws_sqs_queue.bargaining_negotiation.arn,
      aws_sqs_queue.workflow_runs.arn,
      aws_sqs_queue.invoice_processing_dlq.arn,
      aws_sqs_queue.payment_processing_dlq.arn,
      aws_sqs_queue.gst_processing_dlq.arn,
      aws_sqs_queue.workflow_runs_dlq.arn,
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
      aws_ses_email_identity.main.arn,
      aws_sns_topic.alerts.arn,
      aws_sns_topic.low_stock_alerts.arn,
      aws_sns_topic.payment_notifications.arn,
      aws_sns_topic.workflow_notifications.arn,
      aws_sns_topic.ses_events.arn
    ]
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
      aws_dynamodb_table.voice_sessions.arn,
      "${aws_dynamodb_table.voice_sessions.arn}/index/*",
      aws_dynamodb_table.users_sessions.arn,
      aws_dynamodb_table.refresh_tokens.arn,
      aws_dynamodb_table.password_reset_tokens.arn,
      aws_dynamodb_table.mfa_codes.arn,
      aws_dynamodb_table.phone_auth_cooldowns.arn,
      aws_dynamodb_table.customers_cache.arn,
      aws_dynamodb_table.vendors_cache.arn,
      aws_dynamodb_table.products_cache.arn,
      aws_dynamodb_table.invoices_cache.arn,
      aws_dynamodb_table.invoice_sequences.arn,
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

  statement {
    sid    = "InvokeVoiceSessionWorker"
    effect = "Allow"
    actions = [
      "lambda:InvokeFunction"
    ]
    resources = [
      aws_lambda_function.voice_session.arn
    ]
  }
}

resource "aws_iam_role_policy" "lambda_app" {
  name   = "${var.project_name}-lambda-app-policy"
  role   = aws_iam_role.lambda_exec.id
  policy = data.aws_iam_policy_document.lambda_app.json
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
      aws_sqs_queue.payment_processing.arn,
      aws_sqs_queue.gst_processing.arn,
      aws_sqs_queue.bargaining_negotiation.arn,
      aws_sqs_queue.workflow_runs.arn,
      aws_sqs_queue.invoice_processing_dlq.arn,
      aws_sqs_queue.payment_processing_dlq.arn,
      aws_sqs_queue.gst_processing_dlq.arn,
      aws_sqs_queue.bargaining_negotiation_dlq.arn,
      aws_sqs_queue.workflow_runs_dlq.arn
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

  statement {
    sid       = "WorkerEmail"
    effect    = "Allow"
    actions   = ["ses:SendEmail", "ses:SendRawEmail"]
    resources = [aws_ses_email_identity.main.arn]
  }
}

resource "aws_iam_role_policy" "lambda_worker_app" {
  for_each = local.worker_runtime_secret_arns

  name   = "${var.project_name}-lambda-${each.key}-worker-policy"
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
      "${aws_dynamodb_table.ws_connections.arn}/index/*",
      aws_dynamodb_table.voice_sessions.arn,
      "${aws_dynamodb_table.voice_sessions.arn}/index/*"
    ]
  }

  statement {
    sid       = "WebSocketManageConnections"
    effect    = "Allow"
    actions   = ["execute-api:ManageConnections"]
    resources = ["${aws_apigatewayv2_api.websocket.execution_arn}/${var.environment}/POST/@connections/*"]
  }

  statement {
    sid       = "InvokeVoiceSessionWorker"
    effect    = "Allow"
    actions   = ["lambda:InvokeFunction"]
    resources = [aws_lambda_function.voice_session.arn]
  }
}

resource "aws_iam_role_policy" "lambda_websocket_app" {
  name   = "${var.project_name}-lambda-websocket-policy"
  role   = aws_iam_role.lambda_websocket_exec.id
  policy = data.aws_iam_policy_document.lambda_websocket_app.json
}

data "aws_iam_policy_document" "lambda_voice_app" {
  statement {
    sid    = "VoiceProviderSecrets"
    effect = "Allow"
    actions = [
      "secretsmanager:DescribeSecret",
      "secretsmanager:GetSecretValue"
    ]
    resources = [
      aws_secretsmanager_secret.deepgram.arn,
      aws_secretsmanager_secret.deepseek.arn
    ]

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["true"]
    }
  }

  statement {
    sid    = "VoiceProviderSecretsKMS"
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
      values = [
        aws_secretsmanager_secret.deepgram.arn,
        aws_secretsmanager_secret.deepseek.arn
      ]
    }
  }

  statement {
    sid    = "VoiceSessions"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:DeleteItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query"
    ]
    resources = [
      aws_dynamodb_table.voice_sessions.arn,
      "${aws_dynamodb_table.voice_sessions.arn}/index/*"
    ]
  }

  statement {
    sid       = "VoiceManageConnections"
    effect    = "Allow"
    actions   = ["execute-api:ManageConnections"]
    resources = ["${aws_apigatewayv2_api.websocket.execution_arn}/${var.environment}/POST/@connections/*"]
  }
}

resource "aws_iam_role_policy" "lambda_voice_app" {
  name   = "${var.project_name}-lambda-voice-policy"
  role   = aws_iam_role.lambda_voice_exec.id
  policy = data.aws_iam_policy_document.lambda_voice_app.json
}
