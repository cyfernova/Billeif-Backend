resource "aws_s3_bucket" "business_logos" {
  bucket = "business-logos"
}

resource "aws_s3_bucket_lifecycle_configuration" "business_logos" {
  bucket = aws_s3_bucket.business_logos.id

  rule {
    id     = "delete-old-versions"
    status = "Enabled"

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "business_logos" {
  bucket = aws_s3_bucket.business_logos.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket" "invoices_pdf" {
  bucket = "invoices-pdf"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "invoices_pdf" {
  bucket = aws_s3_bucket.invoices_pdf.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket" "product_images" {
  bucket = "product-images"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "product_images" {
  bucket = aws_s3_bucket.product_images.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket" "email_sink" {
  bucket = "email-sink"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "email_sink" {
  bucket = aws_s3_bucket.email_sink.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket" "integration_data" {
  bucket = "integration-data"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "integration_data" {
  bucket = aws_s3_bucket.integration_data.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}
