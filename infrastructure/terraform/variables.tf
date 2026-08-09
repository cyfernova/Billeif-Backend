variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "ap-south-1"
}

variable "aws_profile" {
  description = "AWS shared configuration profile for local Terraform runs. Ignored when use_ambient_aws_credentials is true."
  type        = string
  nullable    = true
  default     = "default"
}

variable "use_ambient_aws_credentials" {
  description = "Use ambient AWS credentials, such as GitHub Actions OIDC, instead of an AWS shared configuration profile."
  type        = bool
  default     = false
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

variable "allowed_origins" {
  description = "Exact HTTPS origins allowed to call the Billeif API from browsers."
  type        = list(string)
  default     = ["https://billeif.com", "https://www.billeif.com"]

  validation {
    condition = alltrue([
      for origin in var.allowed_origins :
      origin == trimspace(origin) && can(regex("^https://[^/]+$", origin))
    ])
    error_message = "allowed_origins must contain exact HTTPS origins without paths or surrounding whitespace."
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

variable "provision_voice_infrastructure" {
  description = "Provision shared voice infrastructure such as ECR, KVS TURN, networking, and session state without admitting users."
  type        = bool
  default     = false
}

variable "provision_voice_agentcore_runtime" {
  description = "Provision the quota-gated AgentCore runtime, endpoints, reconciler, and runtime-dependent application wiring."
  type        = bool
  default     = true
}

variable "promote_voice_agentcore_prod" {
  description = "Create or update the version-pinned PROD endpoint only after the production evidence gates pass."
  type        = bool
  default     = false
}

variable "enable_voice" {
  description = "Admit users to the explicitly reviewed AgentCore realtime voice rollout. Production deployment keeps this false until a separate cutover change."
  type        = bool
  default     = false
}

variable "voice_rollout_stage" {
  description = "Backend-enforced voice admission cohort: disabled, internal, 5, 25, 50, or 100."
  type        = string
  default     = "disabled"

  validation {
    condition     = contains(["disabled", "internal", "5", "25", "50", "100"], var.voice_rollout_stage)
    error_message = "voice_rollout_stage must be disabled, internal, 5, 25, 50, or 100."
  }
}

variable "voice_rollout_internal_sub_hashes" {
  description = "Canonical authenticated Cognito identity allowlist for the internal cohort, stored only as exact sha256:<64 lowercase hex> hashes. Phone-pool identities are <user-pool-id>:<sub>, matching backend authentication."
  type        = set(string)
  default     = []

  validation {
    condition = alltrue([
      for value in var.voice_rollout_internal_sub_hashes :
      can(regex("^sha256:[0-9a-f]{64}$", value))
    ])
    error_message = "voice_rollout_internal_sub_hashes entries must use sha256:<64 lowercase hex>; raw Cognito subjects are forbidden."
  }
}

variable "voice_agentcore_image_tag" {
  description = "Bounded versioned ECR tag used only as the image publishing input; runtime deployment is pinned by digest."
  type        = string
  default     = ""

  validation {
    condition = var.voice_agentcore_image_tag == "" || (
      var.voice_agentcore_image_tag == trimspace(var.voice_agentcore_image_tag) &&
      can(regex("^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$", var.voice_agentcore_image_tag)) &&
      !contains(["latest", "staging", "prod"], lower(var.voice_agentcore_image_tag))
    )
    error_message = "voice_agentcore_image_tag must be empty or a versioned ECR tag; latest, staging, and prod aliases are not allowed."
  }
}

variable "voice_agentcore_image_digest" {
  description = "Exact immutable ECR manifest digest used by AgentCore as repository@sha256; required when voice infrastructure is provisioned."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_agentcore_image_digest == "" || can(regex("^sha256:[0-9a-f]{64}$", var.voice_agentcore_image_digest))
    error_message = "voice_agentcore_image_digest must be empty or exactly sha256:<64 lowercase hex>."
  }
}

variable "voice_agentcore_release" {
  description = "Reviewed immutable voice rollout identifier used to key the MMDSv2 compatibility update. Required when voice infrastructure is provisioned."
  type        = string
  default     = ""

  validation {
    condition = var.voice_agentcore_release == "" || (
      var.voice_agentcore_release == trimspace(var.voice_agentcore_release) &&
      can(regex("^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$", var.voice_agentcore_release))
    )
    error_message = "voice_agentcore_release must be empty or a 1-128 character immutable rollout identifier."
  }
}

variable "voice_agentcore_prod_version" {
  description = "Explicit immutable numeric AgentCore version for the PROD endpoint. Required only when PROD promotion is requested."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_agentcore_prod_version == "" || can(regex("^[1-9][0-9]*$", var.voice_agentcore_prod_version))
    error_message = "voice_agentcore_prod_version must be empty or a positive base-10 integer without a mutable alias."
  }
}

variable "voice_agentcore_previous_prod_version" {
  description = "Optional previous immutable numeric PROD version retained as the code-rollback target while admission is disabled."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_agentcore_previous_prod_version == "" || can(regex("^[1-9][0-9]*$", var.voice_agentcore_previous_prod_version))
    error_message = "voice_agentcore_previous_prod_version must be empty or a positive base-10 integer."
  }
}

variable "voice_sarvam_stt_concurrency_150_acknowledged" {
  description = "Operator acknowledgement that Sarvam STT concurrency of at least 150 was verified for production."
  type        = bool
  default     = false
}

variable "voice_bulbul_concurrency_150_acknowledged" {
  description = "Operator acknowledgement that Bulbul concurrency of at least 150 was verified for production."
  type        = bool
  default     = false
}

variable "voice_sarvam_llm_rpm_300_acknowledged" {
  description = "Operator acknowledgement that Sarvam-105B quota of at least 300 requests per minute was verified for production."
  type        = bool
  default     = false
}

variable "voice_agentcore_kvs_sessions_120_acknowledged" {
  description = "Operator acknowledgement that AgentCore and KVS quotas cover at least 120 tested concurrent sessions."
  type        = bool
  default     = false
}

variable "voice_turn_live_proof_acknowledged" {
  description = "Operator acknowledgement that the Task 7 live AgentCore-to-KVS TURN proof passed through the production-equivalent NAT path."
  type        = bool
  default     = false
}

variable "voice_load_cost_live_evidence_acknowledged" {
  description = "Operator acknowledgement that Task 16 live load and actual cost evidence passed at 120 sessions."
  type        = bool
  default     = false
}

variable "voice_staging_verified" {
  description = "Operator acknowledgement that the exact immutable AgentCore version passed staging verification."
  type        = bool
  default     = false
}

variable "voice_staging_verified_image_digest" {
  description = "Exact immutable image digest covered by the staging evidence. Required to match the selected runtime artifact when voice_staging_verified is true."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_staging_verified_image_digest == "" || can(regex("^sha256:[0-9a-f]{64}$", var.voice_staging_verified_image_digest))
    error_message = "voice_staging_verified_image_digest must be empty or exactly sha256:<64 lowercase hex>."
  }
}

variable "voice_staging_verified_release" {
  description = "Immutable release identifier covered by the staging evidence."
  type        = string
  default     = ""

  validation {
    condition = var.voice_staging_verified_release == "" || (
      var.voice_staging_verified_release == trimspace(var.voice_staging_verified_release) &&
      can(regex("^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$", var.voice_staging_verified_release))
    )
    error_message = "voice_staging_verified_release must be empty or a 1-128 character immutable rollout identifier."
  }
}

variable "voice_staging_verified_version" {
  description = "Exact positive numeric AgentCore runtime version covered by the staging evidence."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_staging_verified_version == "" || can(regex("^[1-9][0-9]*$", var.voice_staging_verified_version))
    error_message = "voice_staging_verified_version must be empty or a positive base-10 integer."
  }
}

variable "voice_generic_websocket_verified" {
  description = "Operator acknowledgement that generic non-voice WebSocket notifications passed staging verification."
  type        = bool
  default     = false
}

variable "enable_voice_turn_udp_egress" {
  description = "Open UDP 443 egress for managed KVS TURN only after the NAT-instance TURN proof gate passes."
  type        = bool
  default     = false
}

variable "migration_lambda_artifact_path" {
  description = "Optional path to the packaged database migration Lambda artifact."
  type        = string
  default     = ""
}

variable "voice_reconciler_lambda_artifact_path" {
  description = "Optional path to the packaged voice lease reconciler Lambda artifact."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_reconciler_lambda_artifact_path == trimspace(var.voice_reconciler_lambda_artifact_path)
    error_message = "voice_reconciler_lambda_artifact_path must not contain leading or trailing whitespace."
  }
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

variable "enable_cognito_custom_domain_provisioning" {
  description = "Validate ACM and create the auth.billeif.com Cognito domain so its CloudFront target can be configured, while runtime consumers remain on the prefix domain."
  type        = bool
  default     = false
}

variable "enable_cognito_custom_domain_cutover" {
  description = "Switch runtime consumers to the already provisioned auth.billeif.com domain only after DNS resolves and Google OAuth URLs are configured. Enabling cutover also implies provisioning."
  type        = bool
  default     = false
}

variable "cognito_additional_callback_urls" {
  description = "Explicit Cognito callback URLs to allow in addition to Swagger OAuth redirect."
  type        = list(string)
  default     = ["billeif://callback"]

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
  default     = ["billeif://logout"]

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

variable "deepseek_base_url" {
  description = "OpenAI-compatible base URL for DeepSeek calls"
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
  description = "OpenAI-compatible DeepSeek model"
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
  description = "Base URL for the deployed MCP server used by API tool calls"
  type        = string
  default     = ""

  validation {
    condition     = trimspace(var.mcp_server_url) == "" || can(regex("^https://", var.mcp_server_url))
    error_message = "mcp_server_url must be empty or an absolute https URL."
  }
}

variable "voice_sessions_table_name" {
  description = "Optional DynamoDB table name override for retained voice session state."
  type        = string
  default     = ""

  validation {
    condition     = var.voice_sessions_table_name == trimspace(var.voice_sessions_table_name) && (var.voice_sessions_table_name == "" || can(regex("^[A-Za-z0-9_.-]{3,255}$", var.voice_sessions_table_name)))
    error_message = "voice_sessions_table_name must be a 3-255 character DynamoDB table name without surrounding whitespace."
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
