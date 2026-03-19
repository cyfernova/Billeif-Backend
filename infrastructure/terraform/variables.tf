variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name for resource naming"
  type        = string
  default     = "invoice-backend"
}

variable "environment" {
  description = "Environment (dev, staging, prod)"
  type        = string
  default     = "dev"
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

# VPC Configuration
variable "vpc_cidr" {
  description = "CIDR block for VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "availability_zones" {
  description = "Availability zones for public subnets"
  type        = list(string)
  default     = ["us-east-1a", "us-east-1b"]
}

variable "db_allowed_cidr" {
  description = "CIDR block allowed to access public RDS instance"
  type        = string
  default     = "0.0.0.0/0"
}

# RDS Configuration
variable "db_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t4g.micro"
}

variable "db_name" {
  description = "Database name"
  type        = string
  default     = "invoice_db"
}

variable "db_username" {
  description = "Database master username"
  type        = string
  default     = "invoice_user"
  sensitive   = true
}

variable "db_password" {
  description = "Database master password"
  type        = string
  sensitive   = true
}

# Lambda Artifacts
variable "lambda_artifact_dir" {
  description = "Directory containing built lambda zip artifacts"
  type        = string
  default     = "../../.build/lambda"
}

# Monitoring Configuration
variable "alert_email" {
  description = "Email address for CloudWatch alarm notifications (optional)"
  type        = string
  default     = ""
}

variable "log_retention_days" {
  description = "Number of days to retain CloudWatch logs"
  type        = number
  default     = 14
}

# Application Secrets
variable "jwt_secret" {
  description = "JWT secret for compatibility with legacy integrations"
  type        = string
  sensitive   = true
  default     = "change-me-in-production-with-secure-secret"
}

# Cognito/OIDC
variable "google_client_id" {
  description = "Google OAuth Client ID"
  type        = string
  sensitive   = true
  default     = ""
}

variable "google_client_secret" {
  description = "Google OAuth Client Secret"
  type        = string
  sensitive   = true
  default     = ""
}

variable "cognito_domain_prefix" {
  description = "Prefix for the Cognito User Pool Domain"
  type        = string
  default     = "invoice-backend-app"
}

# DynamoDB tables
variable "websocket_connections_table" {
  description = "DynamoDB table name for websocket connections"
  type        = string
  default     = "invoice-backend-ws-connections"
}

# Mobile Push Notification Configuration
variable "fcm_api_key" {
  description = "Firebase Cloud Messaging (FCM) API Key for Android push notifications"
  type        = string
  sensitive   = true
  default     = ""
}

variable "apns_sandbox" {
  description = "Whether to use APNs sandbox"
  type        = bool
  default     = true
}

variable "apns_private_key" {
  description = "Apple Push Notification service (APNs) private key content"
  type        = string
  sensitive   = true
  default     = ""
}

variable "apns_certificate" {
  description = "Apple Push Notification service (APNs) certificate content"
  type        = string
  sensitive   = true
  default     = ""
}
