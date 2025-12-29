output "user_pool_id" {
  description = "Cognito User Pool ID"
  value       = aws_cognito_user_pool.main.id
}

output "client_id" {
  description = "Cognito App Client ID"
  value       = aws_cognito_user_pool_client.main.id
}

output "s3_bucket_logos" {
  description = "S3 bucket for business logos"
  value       = aws_s3_bucket.business_logos.id
}

output "s3_bucket_invoices" {
  description = "S3 bucket for invoice PDFs"
  value       = aws_s3_bucket.invoices_pdf.id
}

output "s3_bucket_products" {
  description = "S3 bucket for product images"
  value       = aws_s3_bucket.product_images.id
}
