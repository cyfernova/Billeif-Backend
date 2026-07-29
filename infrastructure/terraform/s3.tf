# Locals for unique bucket naming
locals {
  bucket_prefix = "${local.resource_prefix}-${data.aws_caller_identity.current.account_id}"
}

# Business Logos Bucket
resource "aws_s3_bucket" "business_logos" {
  bucket = "${local.bucket_prefix}-business-logos"
}

resource "aws_s3_bucket_versioning" "business_logos" {
  bucket = aws_s3_bucket.business_logos.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "business_logos" {
  bucket = aws_s3_bucket.business_logos.id

  rule {
    id     = "delete-old-versions"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "business_logos" {
  bucket = aws_s3_bucket.business_logos.id

  rule {
    blocked_encryption_types = ["NONE"]
    bucket_key_enabled       = false
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "business_logos" {
  bucket = aws_s3_bucket.business_logos.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Invoices PDF Bucket
resource "aws_s3_bucket" "invoices_pdf" {
  bucket = "${local.bucket_prefix}-invoices-pdf"
}

resource "aws_s3_bucket_versioning" "invoices_pdf" {
  bucket = aws_s3_bucket.invoices_pdf.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "invoices_pdf" {
  bucket = aws_s3_bucket.invoices_pdf.id

  rule {
    blocked_encryption_types = ["NONE"]
    bucket_key_enabled       = false
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "invoices_pdf" {
  bucket = aws_s3_bucket.invoices_pdf.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Product Images Bucket
resource "aws_s3_bucket" "product_images" {
  bucket = "${local.bucket_prefix}-product-images"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "product_images" {
  bucket = aws_s3_bucket.product_images.id

  rule {
    blocked_encryption_types = ["NONE"]
    bucket_key_enabled       = false
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "product_images" {
  bucket = aws_s3_bucket.product_images.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Email Sink Bucket
resource "aws_s3_bucket" "email_sink" {
  bucket = "${local.bucket_prefix}-email-sink"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "email_sink" {
  bucket = aws_s3_bucket.email_sink.id

  rule {
    blocked_encryption_types = ["NONE"]
    bucket_key_enabled       = false
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "email_sink" {
  bucket = aws_s3_bucket.email_sink.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Integration Data Bucket
resource "aws_s3_bucket" "integration_data" {
  bucket = "${local.bucket_prefix}-integration-data"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "integration_data" {
  bucket = aws_s3_bucket.integration_data.id

  rule {
    blocked_encryption_types = ["NONE"]
    bucket_key_enabled       = false
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "integration_data" {
  bucket = aws_s3_bucket.integration_data.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Lambda Artifacts Bucket
resource "aws_s3_bucket" "lambda_artifacts" {
  bucket = "${local.bucket_prefix}-lambda-artifacts"
}

resource "aws_s3_bucket_versioning" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  rule {
    blocked_encryption_types = ["NONE"]
    bucket_key_enabled       = false
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_lifecycle_configuration" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  rule {
    id     = "delete-old-artifacts"
    status = "Enabled"

    filter {
      prefix = "lambda/"
    }

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}

resource "aws_s3_object" "api_http_lambda_artifact" {
  bucket                 = aws_s3_bucket.lambda_artifacts.id
  key                    = "lambda/http-${local.lambda_artifact_hex_hashes.api_http}.zip"
  source                 = local.lambda_artifacts.api_http
  source_hash            = local.lambda_artifact_hashes.api_http
  server_side_encryption = "AES256"

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.api_http)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.api_http}. Run make package-lambda from the repository root before running Terraform."
    }
  }
}

resource "aws_s3_object" "sqs_bargaining_lambda_artifact" {
  bucket                 = aws_s3_bucket.lambda_artifacts.id
  key                    = "lambda/sqs-bargaining-${local.lambda_artifact_hex_hashes.sqs_bargaining}.zip"
  source                 = local.lambda_artifacts.sqs_bargaining
  source_hash            = local.lambda_artifact_hashes.sqs_bargaining
  server_side_encryption = "AES256"

  lifecycle {
    precondition {
      condition     = fileexists(local.lambda_artifacts.sqs_bargaining)
      error_message = "Missing Lambda artifact ${local.lambda_artifacts.sqs_bargaining}. Run make package-lambda from the repository root before running Terraform."
    }
  }
}
