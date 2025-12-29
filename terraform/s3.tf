resource "aws_s3_bucket" "invoices" {
  bucket = "invoice-storage-invoices"

  tags = {
    Name        = "invoice-storage-invoices"
    Environment = "development"
  }
}

resource "aws_s3_bucket_versioning" "invoices" {
  bucket = aws_s3_bucket.invoices.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "invoices" {
  bucket = aws_s3_bucket.invoices.id

  rule {
    id     = "archive-to-glacier"
    status = "Enabled"

    transition {
      days          = 90
      storage_class = "GLACIER"
    }
  }
}

output "invoices_bucket_name" {
  value       = aws_s3_bucket.invoices.id
  description = "S3 invoices bucket name"
}

output "invoices_bucket_arn" {
  value       = aws_s3_bucket.invoices.arn
  description = "S3 invoices bucket ARN"
}
