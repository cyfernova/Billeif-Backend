variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "aws_access_key_id" {
  description = "AWS access key ID"
  type        = string
  default     = "test"
}

variable "aws_secret_access_key" {
  description = "AWS secret access key"
  type        = string
  default     = "test"
  sensitive   = true
}

variable "aws_endpoint" {
  description = "LocalStack endpoint URL"
  type        = string
  default     = "http://localhost:4566"
}

variable "ses_verified_email" {
  description = "SES verified email address"
  type        = string
  default     = "dev@example.com"
}
