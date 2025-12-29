variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "aws_endpoint" {
  description = "LocalStack endpoint"
  type        = string
  default     = "http://localhost:4566"
}

variable "user_pool_name" {
  description = "Cognito User Pool name"
  type        = string
  default     = "invoice-platform-pool"
}

variable "client_name" {
  description = "Cognito App Client name"
  type        = string
  default     = "invoice-platform-client"
}
