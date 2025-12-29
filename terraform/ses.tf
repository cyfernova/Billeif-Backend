resource "aws_ses_email_identity" "verified" {
  email = var.ses_verified_email
}

output "ses_verified_email" {
  value       = aws_ses_email_identity.verified.email
  description = "SES verified email identity"
}
