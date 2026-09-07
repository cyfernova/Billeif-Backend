output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
}

output "github_actions_deployment_role_arn" {
  description = "Billeif GitHub Actions OIDC deployment role ARN."
  value       = local.github_actions_deployment_role_arn
}

output "public_subnet_ids" {
  description = "Public subnet IDs"
  value       = aws_subnet.public[*].id
}

output "rds_endpoint" {
  description = "RDS PostgreSQL endpoint"
  value       = aws_db_instance.main.endpoint
}

output "rds_address" {
  description = "RDS PostgreSQL address"
  value       = aws_db_instance.main.address
}

output "rds_proxy_endpoint" {
  description = "Optional Billeif RDS Proxy endpoint, empty while direct RDS is enabled."
  value       = try(aws_db_proxy.main[0].endpoint, "")
}

output "rds_port" {
  description = "RDS PostgreSQL port"
  value       = aws_db_instance.main.port
}

output "rds_database_name" {
  description = "RDS database name"
  value       = aws_db_instance.main.db_name
}

output "rds_tunnel_instance_id" {
  description = "SSM-managed EC2 instance ID for local RDS port forwarding"
  value       = try(aws_instance.rds_tunnel[0].id, "")
}

output "http_api_url" {
  description = "Ordinary HTTP API invoke URL"
  value       = aws_apigatewayv2_stage.http.invoke_url
}

output "rest_api_url" {
  description = "A2A response-streaming REST API invoke URL"
  value       = aws_api_gateway_stage.main.invoke_url
}

output "websocket_api_url" {
  description = "WebSocket API endpoint"
  value       = aws_apigatewayv2_stage.websocket_default.invoke_url
}

output "lambda_api_http_arn" {
  description = "Lambda ARN for HTTP API"
  value       = aws_lambda_function.api_http.arn
}

output "lambda_a2a_stream_arn" {
  description = "Lambda ARN for A2A stream"
  value       = aws_lambda_function.a2a_stream.arn
}

output "lambda_sqs_invoice_arn" {
  description = "Lambda ARN for invoice SQS worker"
  value       = aws_lambda_function.sqs_invoice.arn
}

output "bulk_import_queue_url" {
  description = "Billeif durable bulk import SQS queue URL"
  value       = aws_sqs_queue.bulk_import.url
}

output "lambda_bulk_import_arn" {
  description = "Billeif durable bulk import worker Lambda ARN"
  value       = aws_lambda_function.bulk_import.arn
}

output "lambda_sqs_email_delivery_arn" {
  description = "Billeif email delivery worker Lambda ARN"
  value       = aws_lambda_function.sqs_email_delivery.arn
}

output "email_delivery_queue_url" {
  description = "Billeif email delivery SQS queue URL"
  value       = aws_sqs_queue.email_delivery.url
}

output "lambda_sqs_ses_feedback_arn" {
  description = "Billeif SES feedback worker Lambda ARN"
  value       = aws_lambda_function.sqs_ses_feedback.arn
}

output "ses_feedback_queue_url" {
  description = "Billeif SES feedback SQS queue URL"
  value       = aws_sqs_queue.ses_feedback.url
}

output "lambda_sqs_gst_arn" {
  description = "Lambda ARN for GST SQS worker"
  value       = aws_lambda_function.sqs_gst.arn
}

output "lambda_ws_handler_arn" {
  description = "Lambda ARN for WebSocket routes"
  value       = aws_lambda_function.ws_handler.arn
}

output "invoice_processing_queue_url" {
  description = "Invoice processing SQS queue URL"
  value       = aws_sqs_queue.invoice_processing.url
}

output "gst_processing_queue_url" {
  description = "GST processing SQS queue URL"
  value       = aws_sqs_queue.gst_processing.url
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

output "user_pool_id" {
  description = "Cognito User Pool ID"
  value       = aws_cognito_user_pool.main.id
}

output "client_id" {
  description = "Cognito App Client ID"
  value       = aws_cognito_user_pool_client.main.id
}

output "cognito_region" {
  description = "Cognito region used by the primary user pool"
  value       = var.aws_region
}

output "cognito_domain" {
  description = "Active hosted UI domain used by Billeif runtime consumers."
  value       = local.cognito_runtime_domain

  precondition {
    condition = !var.enable_cognito_custom_domain_cutover || (
      trimsuffix(lower(data.dns_cname_record_set.cognito_custom_domain[0].cname), ".") ==
      trimsuffix(lower(aws_cognito_user_pool_domain.custom[0].cloudfront_distribution), ".")
    )
    error_message = "Cognito custom-domain cutover requires auth.billeif.com to CNAME to the user-pool domain CloudFront target."
  }
}

output "cognito_custom_domain_acm_validation" {
  description = "ACM DNS validation CNAME to create at the authoritative DNS provider for auth.billeif.com before enabling cutover."
  value = {
    name  = one(aws_acm_certificate.cognito_custom_domain.domain_validation_options).resource_record_name
    type  = one(aws_acm_certificate.cognito_custom_domain.domain_validation_options).resource_record_type
    value = one(aws_acm_certificate.cognito_custom_domain.domain_validation_options).resource_record_value
  }
}

output "cognito_custom_domain_cloudfront_target" {
  description = "Cognito CloudFront hostname for the auth.billeif.com CNAME after custom-domain provisioning; null during certificate-only staging."
  value       = try(aws_cognito_user_pool_domain.custom[0].cloudfront_distribution, null)
}

output "cognito_prefix_domain" {
  description = "Preserved AWS Cognito prefix domain for rollback if the custom domain becomes unavailable."
  value       = local.cognito_prefix_domain
}

output "cognito_custom_domain_google_oauth" {
  description = "Google OAuth settings required by the Billeif Cognito custom domain."
  value = {
    authorized_origin = "https://${local.cognito_custom_domain}"
    redirect_uri      = "https://${local.cognito_custom_domain}/oauth2/idpresponse"
  }
}

output "cognito_callback_urls" {
  description = "Allowed callback URLs for the primary Cognito app client"
  value       = aws_cognito_user_pool_client.main.callback_urls
}

output "cognito_logout_urls" {
  description = "Allowed logout URLs for the primary Cognito app client"
  value       = aws_cognito_user_pool_client.main.logout_urls
}

output "google_oauth_secret_name" {
  description = "AWS Secrets Manager secret name for Google OAuth credentials"
  value       = aws_secretsmanager_secret.google_oauth.name
}

output "google_oauth_secret_arn" {
  description = "AWS Secrets Manager secret ARN for Google OAuth credentials"
  value       = aws_secretsmanager_secret.google_oauth.arn
}

output "phone_user_pool_id" {
  description = "India phone-auth Cognito User Pool ID"
  value       = aws_cognito_user_pool.phone.id
}

output "phone_client_id" {
  description = "India phone-auth Cognito App Client ID"
  value       = aws_cognito_user_pool_client.phone.id
}

output "phone_auth_cooldown_table" {
  description = "DynamoDB table name for per-phone OTP cooldowns"
  value       = aws_dynamodb_table.phone_auth_cooldowns.name
}

output "rds_master_user_secret_arn" {
  description = "RDS-managed master user secret ARN"
  value       = aws_db_instance.main.master_user_secret[0].secret_arn
}

output "db_host_ssm_parameter" {
  description = "SSM parameter name for DB host"
  value       = aws_ssm_parameter.db_host.name
}

output "razorpay_secret_arn" {
  description = "Razorpay credential secret container ARN"
  value       = aws_secretsmanager_secret.razorpay.arn
}

output "razorpay_webhook_url" {
  description = "Public Razorpay webhook endpoint URL"
  value       = "${local.http_api_invoke_url}/api/v1/webhooks/razorpay"
}

output "websocket_connections_table" {
  description = "DynamoDB table name for WebSocket connections"
  value       = aws_dynamodb_table.ws_connections.name
}

output "voice_sessions_table" {
  description = "DynamoDB table name for retained voice session state"
  value       = aws_dynamodb_table.voice_sessions.name
}

output "voice_agentcore_ecr_repository_url" {
  description = "Private ECR repository URL for the gated AgentCore voice image."
  value       = try(aws_ecr_repository.voice_agentcore[0].repository_url, null)
}

output "voice_agentcore_runtime_arn" {
  description = "AgentCore voice runtime ARN, or null while voice infrastructure is not provisioned."
  value       = try(aws_bedrockagentcore_agent_runtime.voice[0].agent_runtime_arn, null)
}

output "voice_agentcore_runtime_qualifier" {
  description = "Backend and mobile AgentCore qualifier; DEFAULT is never exposed."
  value       = local.voice_agentcore_runtime_enabled ? (var.promote_voice_agentcore_prod ? "PROD" : "STAGING") : null
}

output "voice_agentcore_prod_endpoint_arn" {
  description = "Version-pinned PROD AgentCore endpoint ARN, or null before explicit promotion."
  value       = try(aws_bedrockagentcore_agent_runtime_endpoint.voice_prod[0].agent_runtime_endpoint_arn, null)
}

output "voice_turn_channel_arns" {
  description = "ARNs for the fixed 12-channel managed KVS TURN credential pool."
  value       = awscc_kinesisvideo_signaling_channel.voice[*].arn
}

output "cloudwatch_dashboard_url" {
  description = "CloudWatch Dashboard URL"
  value       = "https://${var.aws_region}.console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards:name=${aws_cloudwatch_dashboard.main.dashboard_name}"
}

output "sns_alerts_topic_arn" {
  description = "SNS Topic ARN for alerts"
  value       = aws_sns_topic.alerts.arn
}
