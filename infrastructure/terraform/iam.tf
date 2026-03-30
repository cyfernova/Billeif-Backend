data "aws_caller_identity" "current" {}

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

resource "aws_iam_role" "lambda_exec" {
  name               = "${var.project_name}-lambda-exec-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda_exec.name
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
      aws_sqs_queue.workflow_runs.arn,
      aws_sqs_queue.invoice_processing_dlq.arn,
      aws_sqs_queue.payment_processing_dlq.arn,
      aws_sqs_queue.gst_processing_dlq.arn,
      aws_sqs_queue.workflow_runs_dlq.arn
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
    resources = ["*"]
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
      aws_dynamodb_table.invoice_sequences.arn,
      aws_dynamodb_table.ledger_cache.arn,
      aws_dynamodb_table.payments_cache.arn
    ]
  }

  statement {
    sid    = "SSMAccess"
    effect = "Allow"
    actions = [
      "ssm:GetParameter",
      "ssm:GetParameters"
    ]
    resources = [
      local.db_username_ssm_parameter_arn,
      local.db_password_ssm_parameter_arn,
      local.db_host_ssm_parameter_arn
    ]
  }

  statement {
    sid    = "WebSocketManageConnections"
    effect = "Allow"
    actions = [
      "execute-api:ManageConnections"
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "lambda_app" {
  name   = "${var.project_name}-lambda-app-policy"
  role   = aws_iam_role.lambda_exec.id
  policy = data.aws_iam_policy_document.lambda_app.json
}
