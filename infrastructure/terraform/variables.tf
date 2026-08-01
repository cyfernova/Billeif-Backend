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
  default     = "Billeif-pool"
}

variable "client_name" {
  description = "Cognito App Client name"
  type        = string
  default     = "Billeif-client"
}

variable "phone_user_pool_name" {
  description = "Cognito User Pool name for India phone auth"
  type        = string
  default     = "Billeif-phone-pool"
}

variable "phone_client_name" {
  description = "Cognito App Client name for India phone auth"
  type        = string
  default     = "Billeif-phone-client"
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

  validation {
    condition     = var.db_allowed_cidr != "0.0.0.0/0" && var.db_allowed_cidr != "::/0"
    error_message = "db_allowed_cidr must be an explicit trusted CIDR, not an internet-wide range."
  }
}

variable "db_publicly_accessible" {
  description = "Whether the RDS instance receives a public endpoint. Public RDS is not allowed."
  type        = bool
  default     = false

  validation {
    condition     = var.db_publicly_accessible == false
    error_message = "db_publicly_accessible must be false."
  }
}

variable "enable_rds_tunnel" {
  description = "Create a private SSM-managed EC2 instance for local RDS port forwarding in non-production environments."
  type        = bool
  default     = true
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

variable "db_port" {
  description = "Database port"
  type        = number
  default     = 5432
}

variable "rds_tunnel_instance_type" {
  description = "EC2 instance type for the SSM RDS tunnel host."
  type        = string
  default     = "t3.micro"
}

variable "db_username" {
  description = "Database master username"
  type        = string
  default     = "invoice_user"
  sensitive   = true
}

variable "db_password" {
  description = "Database master password (fetched from SSM at runtime)"
  type        = string
  sensitive   = true

  validation {
    condition     = can(regex("^[\\x21-\\x7E]+$", var.db_password)) && length(var.db_password) >= 20 && length(regexall("[/@\"]", var.db_password)) == 0 && !contains(["changeme", "password", "password123"], lower(var.db_password))
    error_message = "db_password must use printable ASCII without spaces and cannot contain '/', '@', or '\"'."
  }
}

variable "credential_encryption_key" {
  description = "Base64-encoded 32-byte key used for application credential encryption"
  type        = string
  sensitive   = true

  validation {
    condition     = can(regex("^[A-Za-z0-9+/]{43}=$", var.credential_encryption_key))
    error_message = "credential_encryption_key must be a base64-encoded 32-byte key."
  }
}

variable "razorpay_key_id" {
  description = "Razorpay test/live key ID stored as an SSM SecureString"
  type        = string
  sensitive   = true
  default     = ""
}

variable "razorpay_key_secret" {
  description = "Razorpay test/live key secret stored as an SSM SecureString"
  type        = string
  sensitive   = true
  default     = ""
}

variable "razorpay_webhook_secret" {
  description = "Razorpay webhook signing secret stored as an SSM SecureString"
  type        = string
  sensitive   = true
  default     = ""
}

# Lambda Artifacts
variable "lambda_artifact_dir" {
  description = "Directory containing built lambda zip artifacts"
  type        = string
  default     = "../../.build/lambda"
}

variable "enable_lambda_reserved_concurrency" {
  description = "Whether to apply reserved concurrency limits to Lambda functions. Disable this for low-quota AWS accounts."
  type        = bool
  default     = false
}

variable "worker_queue_visibility_timeout_seconds" {
  description = "Visibility timeout for SQS worker queues. Keep this at least 6x the Lambda timeout plus batching window."
  type        = number
  default     = 365
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

  validation {
    condition     = length(var.jwt_secret) >= 32 && !can(regex("(?i)^(change[-_ ]?me|changeme|secret)$", var.jwt_secret))
    error_message = "jwt_secret must be explicitly provided and at least 32 characters."
  }
}

# Cognito/OIDC
variable "google_oauth_secret_name" {
  description = "AWS Secrets Manager secret name containing Google OAuth credentials as JSON with client_id and client_secret"
  type        = string
  default     = ""
}

variable "google_client_id" {
  description = "Legacy Google OAuth Client ID override. Prefer google_oauth_secret_name backed by AWS Secrets Manager."
  type        = string
  sensitive   = true
  default     = ""
}

variable "google_client_secret" {
  description = "Legacy Google OAuth Client Secret override. Prefer google_oauth_secret_name backed by AWS Secrets Manager."
  type        = string
  sensitive   = true
  default     = ""
}

variable "cognito_domain_prefix" {
  description = "Prefix for the Cognito User Pool Domain"
  type        = string
  default     = "invoice-backend-app"
}

variable "cognito_additional_callback_urls" {
  description = "Explicit Cognito callback URLs to allow in addition to Swagger OAuth redirect."
  type        = list(string)
  default     = []

  validation {
    condition = alltrue([
      for callback_url in var.cognito_additional_callback_urls :
      !can(regex("^(http://localhost|http://127\\.0\\.0\\.1|exp://|https://auth\\.expo\\.io/)", lower(callback_url)))
    ])
    error_message = "Cognito callback URLs cannot include localhost or Expo development redirects."
  }
}

variable "cognito_additional_logout_urls" {
  description = "Explicit Cognito logout URLs to allow."
  type        = list(string)
  default     = []

  validation {
    condition = alltrue([
      for logout_url in var.cognito_additional_logout_urls :
      !can(regex("^(http://localhost|http://127\\.0\\.0\\.1|exp://|https://auth\\.expo\\.io/)", lower(logout_url)))
    ])
    error_message = "Cognito logout URLs cannot include localhost or Expo development redirects."
  }
}

variable "india_sms_sender_id" {
  description = "DLT-approved sender ID for India SMS delivery"
  type        = string
  default     = ""
}

variable "india_dlt_entity_id" {
  description = "DLT entity ID for India SMS delivery"
  type        = string
  default     = ""
}

variable "india_signup_template_id" {
  description = "DLT template ID for signup and verification SMS"
  type        = string
  default     = ""
}

variable "india_auth_template_id" {
  description = "DLT template ID for authentication SMS"
  type        = string
  default     = ""
}

variable "india_signup_message_template" {
  description = "Exact DLT-approved signup or verification SMS template. Use {####} where the OTP should appear."
  type        = string
  default     = "Your Invoice Backend verification code is {####}."
}

variable "india_auth_message_template" {
  description = "Exact DLT-approved authentication SMS template. Use {####} where the OTP should appear."
  type        = string
  default     = "Your Invoice Backend login code is {####}."
}

variable "phone_auth_cooldown_table_name" {
  description = "DynamoDB table used to throttle per-phone OTP requests"
  type        = string
  default     = "phone_auth_cooldowns"
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

# LLM Configuration
variable "llm_api_key" {
  description = "API key for the configured LLM provider"
  type        = string
  sensitive   = true
  default     = ""
}

variable "llm_api_url" {
  description = "Chat completions API URL for the configured LLM provider"
  type        = string

  validation {
    condition     = can(regex("^https://", var.llm_api_url))
    error_message = "llm_api_url must be an absolute https URL."
  }
}

variable "llm_model" {
  description = "Model name for the configured LLM provider"
  type        = string

  validation {
    condition     = length(trimspace(var.llm_model)) > 0
    error_message = "llm_model is required."
  }
}

variable "exa_api_key" {
  description = "Exa API key for LLM web search"
  type        = string
  sensitive   = true
  default     = ""
}

variable "exa_base_url" {
  description = "Exa search API URL"
  type        = string
  default     = "https://api.exa.ai/search"

  validation {
    condition     = can(regex("^https://", var.exa_base_url))
    error_message = "exa_base_url must be an absolute https URL."
  }
}

variable "exa_timeout" {
  description = "Timeout in seconds for Exa search requests"
  type        = number
  default     = 12
}

variable "gst_lookup_api_key" {
  description = "GSTINCheck API key for GSTIN lookup"
  type        = string
  sensitive   = true
  default     = ""
}

variable "gst_lookup_base_url" {
  description = "GSTIN lookup endpoint template. Supports {api_key} and {gstin} placeholders."
  type        = string
  default     = "https://sheet.gstincheck.co.in/check/{api_key}/{gstin}"
}

variable "gst_lookup_timeout" {
  description = "GSTIN lookup HTTP timeout in seconds"
  type        = number
  default     = 15
}

variable "deepgram_api_key" {
  description = "Deepgram API key for realtime voice"
  type        = string
  sensitive   = true
}

variable "deepgram_voice_agent_url" {
  description = "Deepgram Voice Agent websocket URL"
  type        = string
  default     = ""
}

variable "deepgram_voice_listen_model" {
  description = "Deepgram Voice Agent listen model"
  type        = string
  default     = "nova-2"
}

variable "deepgram_voice_speak_model" {
  description = "Deepgram Voice Agent speak model"
  type        = string
  default     = "nova-2"
}

variable "deepgram_voice_input_encoding" {
  description = "Realtime voice input encoding"
  type        = string
  default     = "linear16"
}

variable "deepgram_voice_input_sample_rate" {
  description = "Realtime voice input sample rate"
  type        = number
  default     = 24000
}

variable "deepgram_voice_output_encoding" {
  description = "Realtime voice output encoding"
  type        = string
  default     = "linear16"
}

variable "deepgram_voice_output_sample_rate" {
  description = "Realtime voice output sample rate"
  type        = number
  default     = 24000
}

variable "deepseek_api_key" {
  description = "DeepSeek API key for Deepgram Voice Agent OpenAI-compatible LLM calls"
  type        = string
  sensitive   = true
}

variable "deepseek_base_url" {
  description = "OpenAI-compatible base URL for realtime voice LLM calls"
  type        = string

  validation {
    condition     = can(regex("^https://", var.deepseek_base_url))
    error_message = "deepseek_base_url must be an absolute https URL."
  }
}

variable "deepseek_model" {
  description = "OpenAI-compatible model for realtime voice"
  type        = string

  validation {
    condition     = length(trimspace(var.deepseek_model)) > 0
    error_message = "deepseek_model is required."
  }
}

variable "voice_ws_max_session_seconds" {
  description = "Maximum realtime voice session duration"
  type        = number
  default     = 3600
}

variable "voice_ws_ping_interval_seconds" {
  description = "Realtime voice websocket ping interval"
  type        = number
  default     = 30
}

variable "voice_ws_write_timeout_seconds" {
  description = "Realtime voice websocket write timeout"
  type        = number
  default     = 10
}

variable "voice_ws_max_frame_bytes" {
  description = "Maximum realtime voice websocket binary frame size"
  type        = number
  default     = 16384
}

variable "voice_ws_max_concurrent_sessions_per_user" {
  description = "Maximum concurrent realtime voice sessions per authenticated user"
  type        = number
  default     = 3
}

variable "voice_ws_event_poll_interval_ms" {
  description = "Realtime voice Lambda worker DynamoDB event poll interval"
  type        = number
  default     = 250
}

variable "voice_ws_event_ttl_seconds" {
  description = "Realtime voice queued event TTL"
  type        = number
  default     = 300
}

variable "voice_ws_max_outbound_chunk_bytes" {
  description = "Maximum raw assistant audio bytes per API Gateway WebSocket message before base64 encoding"
  type        = number
  default     = 32768
}

variable "voice_ws_provider_ready_timeout_seconds" {
  description = "Realtime voice provider welcome timeout"
  type        = number
  default     = 30
}

variable "voice_sessions_table_name" {
  description = "DynamoDB table name for realtime voice session state"
  type        = string
  default     = "voice_sessions"
}

variable "voice_session_lambda_function_name" {
  description = "Lambda function name for realtime voice session worker"
  type        = string
  default     = "invoice-backend-voice-session"
}

variable "voice_session_lambda_memory_size" {
  description = "Memory size for realtime voice session worker Lambda"
  type        = number
  default     = 1024
}

variable "voice_session_lambda_timeout_seconds" {
  description = "Timeout for realtime voice session worker Lambda"
  type        = number
  default     = 60
}

variable "voice_session_reserved_concurrency" {
  description = "Reserved concurrency for realtime voice session worker when reserved concurrency is enabled"
  type        = number
  default     = 5
}
