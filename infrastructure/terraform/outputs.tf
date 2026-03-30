output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
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

output "rds_port" {
  description = "RDS PostgreSQL port"
  value       = aws_db_instance.main.port
}

output "rds_database_name" {
  description = "RDS database name"
  value       = aws_db_instance.main.db_name
}

output "rest_api_url" {
  description = "REST API invoke URL"
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

output "lambda_sqs_payment_arn" {
  description = "Lambda ARN for payment SQS worker"
  value       = aws_lambda_function.sqs_payment.arn
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

output "payment_processing_queue_url" {
  description = "Payment processing SQS queue URL"
  value       = aws_sqs_queue.payment_processing.url
}

output "gst_processing_queue_url" {
  description = "GST processing SQS queue URL"
  value       = aws_sqs_queue.gst_processing.url
}

output "workflow_runs_queue_url" {
  description = "Workflow runs SQS queue URL"
  value       = aws_sqs_queue.workflow_runs.url
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

output "db_username_ssm_parameter" {
  description = "SSM parameter name for DB username"
  value       = aws_ssm_parameter.db_username.name
}

output "db_password_ssm_parameter" {
  description = "SSM parameter name for DB password"
  value       = aws_ssm_parameter.db_password.name
}

output "db_host_ssm_parameter" {
  description = "SSM parameter name for DB host"
  value       = aws_ssm_parameter.db_host.name
}

output "websocket_connections_table" {
  description = "DynamoDB table name for WebSocket connections"
  value       = aws_dynamodb_table.ws_connections.name
}

output "cloudwatch_dashboard_url" {
  description = "CloudWatch Dashboard URL"
  value       = "https://${var.aws_region}.console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards:name=${aws_cloudwatch_dashboard.main.dashboard_name}"
}

output "sns_alerts_topic_arn" {
  description = "SNS Topic ARN for alerts"
  value       = aws_sns_topic.alerts.arn
}
