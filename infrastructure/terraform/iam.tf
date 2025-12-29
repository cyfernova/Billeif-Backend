data "aws_iam_policy_document" "lambda_exec" {
  statement {
    sid    = "LambdaLogging"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["arn:aws:logs:*:*:*"]
  }

  statement {
    sid    = "LambdaDynamoDB"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query",
    ]
    resources = ["arn:aws:dynamodb:*:*:*:*"]
  }

  statement {
    sid    = "LambdaS3"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
    ]
    resources = ["arn:aws:s3:::*:*"]
  }
}

data "aws_iam_policy_document" "sqs_consumer" {
  statement {
    sid    = "SQSReceive"
    effect = "Allow"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role" "lambda_exec" {
  name = "lambda-exec-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })

  inline_policy {
    name   = "lambda-exec-policy"
    policy = data.aws_iam_policy_document.lambda_exec.json
  }
}

resource "aws_iam_role" "sqs_consumer" {
  name = "sqs-consumer-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "sqs.amazonaws.com"
        }
      }
    ]
  })

  inline_policy {
    name   = "sqs-consumer-policy"
    policy = data.aws_iam_policy_document.sqs_consumer.json
  }
}

data "aws_iam_policy_document" "app_s3_access" {
  statement {
    sid    = "S3ReadWrite"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:ListBucket",
    ]
    resources = [
      "arn:aws:s3:::business-logos",
      "arn:aws:s3:::business-logos/*",
      "arn:aws:s3:::invoices-pdf",
      "arn:aws:s3:::invoices-pdf/*",
      "arn:aws:s3:::product-images",
      "arn:aws:s3:::product-images/*",
      "arn:aws:s3:::email-sink",
      "arn:aws:s3:::email-sink/*",
    ]
  }
}

resource "aws_iam_role_policy_attachment" "app_s3_access" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "s3_access" {
  role       = aws_iam_role.sqs_consumer.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonS3FullAccess"
}
