variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "ap-south-1"
}

variable "aws_profile" {
  description = "AWS shared configuration profile for local Terraform runs; set to null for OIDC."
  type        = string
  nullable    = true
  default     = "default"
}

variable "project_name" {
  description = "Lowercase project name used in AWS resource names."
  type        = string
  default     = "billeif"

  validation {
    condition     = var.project_name == trimspace(var.project_name) && can(regex("^[a-z0-9](?:[a-z0-9-]{0,27}[a-z0-9])?$", var.project_name))
    error_message = "project_name must be 1-29 lowercase letters, digits, or hyphens, without surrounding whitespace and beginning and ending with an alphanumeric character."
  }
}

variable "environment" {
  description = "Environment (dev, staging, prod)"
  type        = string
  default     = "dev"

  validation {
    condition     = var.environment == trimspace(var.environment) && can(regex("^[a-z0-9](?:[a-z0-9-]{0,40}[a-z0-9])?$", var.environment))
    error_message = "environment must be 1-42 lowercase letters, digits, or hyphens, without surrounding whitespace and beginning and ending with an alphanumeric character."
  }

  validation {
    condition     = length("${var.project_name}-${var.environment}") <= 29
    error_message = "project_name and environment must form a resource prefix of at most 29 characters so generated IAM role names fit AWS limits."
  }
}

variable "user_pool_name" {
  description = "Optional Billeif web Cognito User Pool name override."
  type        = string
  default     = ""

  validation {
    condition     = var.user_pool_name == trimspace(var.user_pool_name) && (var.user_pool_name == "" || (can(regex("^[\\w\\s+=,.@-]{1,128}$", var.user_pool_name)) && strcontains(lower(var.user_pool_name), "billeif")))
    error_message = "user_pool_name overrides must be 1-128 Cognito-valid characters without surrounding whitespace and contain the billeif brand."
  }
}

variable "client_name" {
  description = "Optional Billeif web Cognito App Client name override."
  type        = string
  default     = ""

  validation {
    condition     = var.client_name == trimspace(var.client_name) && (var.client_name == "" || (can(regex("^[\\w\\s+=,.@-]{1,128}$", var.client_name)) && strcontains(lower(var.client_name), "billeif")))
    error_message = "client_name overrides must be 1-128 Cognito-valid characters without surrounding whitespace and contain the billeif brand."
  }
}

variable "phone_user_pool_name" {
  description = "Optional Billeif native Cognito User Pool name override."
  type        = string
  default     = ""

  validation {
    condition     = var.phone_user_pool_name == trimspace(var.phone_user_pool_name) && (var.phone_user_pool_name == "" || (can(regex("^[\\w\\s+=,.@-]{1,128}$", var.phone_user_pool_name)) && strcontains(lower(var.phone_user_pool_name), "billeif")))
    error_message = "phone_user_pool_name overrides must be 1-128 Cognito-valid characters without surrounding whitespace and contain the billeif brand."
  }
}

variable "phone_client_name" {
  description = "Optional Billeif native Cognito App Client name override."
  type        = string
  default     = ""

  validation {
    condition     = var.phone_client_name == trimspace(var.phone_client_name) && (var.phone_client_name == "" || (can(regex("^[\\w\\s+=,.@-]{1,128}$", var.phone_client_name)) && strcontains(lower(var.phone_client_name), "billeif")))
    error_message = "phone_client_name overrides must be 1-128 Cognito-valid characters without surrounding whitespace and contain the billeif brand."
  }
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
  default     = ["ap-south-1a", "ap-south-1b"]
}

variable "egress_mode" {
  description = "Billeif private-subnet egress mode. NAT instance is the cost-capped default; managed NAT is an explicit opt-in."
  type        = string
  default     = "nat_instance"

  validation {
    condition     = contains(["nat_instance", "managed_nat"], var.egress_mode)
    error_message = "egress_mode must be either nat_instance or managed_nat."
  }
}

variable "db_allowed_cidr" {
  description = "Deprecated compatibility input. RDS ingress is security-group-only."
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
  default     = false
}

variable "db_multi_az" {
  description = "Enable Multi-AZ RDS. The Billeif beta defaults to a documented single-AZ profile."
  type        = bool
  default     = false
}

variable "enable_rds_proxy" {
  description = "Route Billeif database connections through the optional RDS Proxy instead of direct RDS."
  type        = bool
  default     = false
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

variable "enable_application" {
  description = "Activate ordinary application Lambda invocation paths after a reviewed successful migration deployment."
  type        = bool
  default     = false
}

variable "enable_background_processing" {
  description = "Activate Billeif background workers, their SQS mappings, and the outbox dispatcher schedule after application enablement."
  type        = bool
  default     = true
}

variable "enable_voice" {
  description = "Activate the explicitly reviewed Billeif realtime voice pilot after ordinary application enablement."
  type        = bool
  default     = false
}

variable "migration_lambda_artifact_path" {
  description = "Optional path to the packaged database migration Lambda artifact."
  type        = string
  default     = ""
}

variable "worker_queue_visibility_timeout_seconds" {
  description = "Visibility timeout for SQS worker queues. Keep this at least 6x the Lambda timeout plus batching window."
  type        = number
  default     = 365
}

# Monitoring Configuration
variable "alert_email" {
  description = "Email address for Billeif CloudWatch and AWS Budget notifications. Required before application enablement."
  type        = string
  default     = ""
}

variable "alert_email_subscription_confirmed" {
  description = "Confirm that the alert_email recipient accepted the SNS subscription before Billeif application enablement."
  type        = bool
  default     = false
}

variable "log_retention_days" {
  description = "Number of days to retain CloudWatch logs"
  type        = number
  default     = 14
}

variable "cognito_domain_prefix" {
  description = "Optional hosted UI prefix override. It must be lowercase, Cognito-valid, and contain billeif."
  type        = string
  default     = ""

  validation {
    condition = var.cognito_domain_prefix == trimspace(var.cognito_domain_prefix) && (var.cognito_domain_prefix == "" || (
      var.cognito_domain_prefix == lower(var.cognito_domain_prefix) &&
      can(regex("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$", var.cognito_domain_prefix)) &&
      strcontains(lower(var.cognito_domain_prefix), "billeif") &&
      !strcontains(lower(var.cognito_domain_prefix), "aws") &&
      !strcontains(lower(var.cognito_domain_prefix), "amazon") &&
      !strcontains(lower(var.cognito_domain_prefix), "cognito")
    ))
    error_message = "cognito_domain_prefix must be a 1-63 character lowercase Cognito prefix containing billeif and no reserved aws, amazon, or cognito text."
  }
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
  default     = "Your Billeif verification code is {####}."
}

variable "india_auth_message_template" {
  description = "Exact DLT-approved authentication SMS template. Use {####} where the OTP should appear."
  type        = string
  default     = "Your Billeif login code is {####}."
}

variable "phone_auth_cooldown_table_name" {
  description = "Optional DynamoDB table name override for per-phone OTP throttling."
  type        = string
  default     = ""

  validation {
    condition     = var.phone_auth_cooldown_table_name == trimspace(var.phone_auth_cooldown_table_name) && (var.phone_auth_cooldown_table_name == "" || can(regex("^[A-Za-z0-9_.-]{3,255}$", var.phone_auth_cooldown_table_name)))
    error_message = "phone_auth_cooldown_table_name must be a 3-255 character DynamoDB table name without surrounding whitespace."
  }
}

# DynamoDB tables
variable "websocket_connections_table" {
  description = "Optional DynamoDB table name override for websocket connections."
  type        = string
  default     = ""

  validation {
    condition     = var.websocket_connections_table == trimspace(var.websocket_connections_table) && (var.websocket_connections_table == "" || can(regex("^[A-Za-z0-9_.-]{3,255}$", var.websocket_connections_table)))
    error_message = "websocket_connections_table must be a 3-255 character DynamoDB table name without surrounding whitespace."
  }
}

# LLM Configuration
variable "llm_api_url" {
  description = "Chat completions API URL for the configured LLM provider"
  type        = string

  validation {
    condition     = can(regex("^https://", var.llm_api_url))
    error_message = "llm_api_url must be an absolute https URL."
  }

  validation {
    condition     = !can(regex("^https://(www\\.)?(test\\.com|example\\.com|placeholder\\.com)([:/]|$)", lower(trimspace(var.llm_api_url))))
    error_message = "llm_api_url cannot use a placeholder host."
  }
}

variable "llm_model" {
  description = "Model name for the configured LLM provider"
  type        = string

  validation {
    condition     = length(trimspace(var.llm_model)) > 0
    error_message = "llm_model is required."
  }

  validation {
    condition     = !contains(["test", "placeholder", "dummy", "changeme", "change-me"], lower(trimspace(var.llm_model))) && !startswith(lower(trimspace(var.llm_model)), "your-")
    error_message = "llm_model cannot be a placeholder value."
  }
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

variable "deepseek_base_url" {
  description = "OpenAI-compatible base URL for realtime voice LLM calls"
  type        = string

  validation {
    condition     = can(regex("^https://", var.deepseek_base_url))
    error_message = "deepseek_base_url must be an absolute https URL."
  }

  validation {
    condition     = !can(regex("^https://(www\\.)?(test\\.com|example\\.com|placeholder\\.com)([:/]|$)", lower(trimspace(var.deepseek_base_url))))
    error_message = "deepseek_base_url cannot use a placeholder host."
  }
}

variable "deepseek_model" {
  description = "OpenAI-compatible model for realtime voice"
  type        = string

  validation {
    condition     = length(trimspace(var.deepseek_model)) > 0
    error_message = "deepseek_model is required."
  }

  validation {
    condition     = !contains(["test", "placeholder", "dummy", "changeme", "change-me"], lower(trimspace(var.deepseek_model))) && !startswith(lower(trimspace(var.deepseek_model)), "your-")
    error_message = "deepseek_model cannot be a placeholder value."
  }
}

variable "mcp_server_url" {
  description = "Base URL for the deployed MCP server used by API and realtime voice tool calls"
  type        = string
  default     = ""

  validation {
    condition     = trimspace(var.mcp_server_url) == "" || can(regex("^https://", var.mcp_server_url))
    error_message = "mcp_server_url must be empty or an absolute https URL."
  }
}

variable "voice_ws_max_session_seconds" {
  description = "Maximum Billeif pilot voice session duration"
  type        = number
  default     = 900

  validation {
    condition     = var.voice_ws_max_session_seconds == 900
    error_message = "voice_ws_max_session_seconds is fixed at the 900-second Billeif pilot cap."
  }
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
  description = "Maximum concurrent Billeif pilot voice sessions per authenticated user"
  type        = number
  default     = 1

  validation {
    condition     = var.voice_ws_max_concurrent_sessions_per_user == 1
    error_message = "voice_ws_max_concurrent_sessions_per_user is fixed at one for the Billeif pilot."
  }
}

variable "voice_ws_event_poll_interval_ms" {
  description = "Billeif pilot voice Lambda worker DynamoDB event poll interval"
  type        = number
  default     = 250

  validation {
    condition     = var.voice_ws_event_poll_interval_ms == 250
    error_message = "voice_ws_event_poll_interval_ms is fixed at 250ms for the Billeif pilot."
  }
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
  description = "Optional DynamoDB table name override for realtime voice session state."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_sessions_table_name == trimspace(var.voice_sessions_table_name) && (var.voice_sessions_table_name == "" || can(regex("^[A-Za-z0-9_.-]{3,255}$", var.voice_sessions_table_name)))
    error_message = "voice_sessions_table_name must be a 3-255 character DynamoDB table name without surrounding whitespace."
  }
}

variable "voice_session_lambda_function_name" {
  description = "Optional Lambda function name override for the realtime voice session worker."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_session_lambda_function_name == trimspace(var.voice_session_lambda_function_name) && (var.voice_session_lambda_function_name == "" || can(regex("^[A-Za-z0-9-_]{1,64}$", var.voice_session_lambda_function_name)))
    error_message = "voice_session_lambda_function_name must be a 1-64 character Lambda name without surrounding whitespace."
  }
}

variable "ses_verified_identity" {
  description = "Already-verified SES identity email address or domain. Supply this before the first application apply; Terraform only references its ARN."
  type        = string

  validation {
    condition = (
      can(regex("^[A-Za-z0-9!#$%&'+_-]+(?:\\.[A-Za-z0-9!#$%&'+_-]+)*@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$", var.ses_verified_identity)) ||
      can(regex("^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$", var.ses_verified_identity))
    ) && var.ses_verified_identity == trimspace(var.ses_verified_identity) && !endswith(lower(var.ses_verified_identity), ".local")
    error_message = "ses_verified_identity must be a non-.local email address or domain with strict DNS labels and no ARN wildcard or path characters."
  }
}

variable "ses_sender_email" {
  description = "Concrete email address used as SES From address. It must be the verified identity or belong to the verified identity domain."
  type        = string

  validation {
    condition     = var.ses_sender_email == trimspace(var.ses_sender_email) && can(regex("^[A-Za-z0-9!#$%&'+_-]+(?:\\.[A-Za-z0-9!#$%&'+_-]+)*@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$", var.ses_sender_email)) && !endswith(lower(var.ses_sender_email), ".local")
    error_message = "ses_sender_email must be a non-.local email address with strict DNS labels and no ARN wildcard or path characters."
  }
}

variable "voice_session_lambda_memory_size" {
  description = "Memory size for realtime voice session worker Lambda"
  type        = number
  default     = 1024
}

variable "voice_session_lambda_timeout_seconds" {
  description = "Timeout for the Billeif pilot realtime voice session worker Lambda"
  type        = number
  default     = 900

  validation {
    condition     = var.voice_session_lambda_timeout_seconds == 900
    error_message = "voice_session_lambda_timeout_seconds is fixed at the 900-second Billeif pilot cap."
  }
}

variable "voice_session_reserved_concurrency" {
  description = "Hard reserved-concurrency cap for the explicitly enabled Billeif realtime voice pilot"
  type        = number
  default     = 5

  validation {
    condition     = var.voice_session_reserved_concurrency == 5
    error_message = "voice_session_reserved_concurrency is fixed at five for the Billeif pilot."
  }
}
