# VPC Outputs
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
}

output "private_subnet_ids" {
  description = "Private subnet IDs"
  value       = aws_subnet.private[*].id
}

output "public_subnet_ids" {
  description = "Public subnet IDs"
  value       = aws_subnet.public[*].id
}

# ALB Outputs
output "alb_dns_name" {
  description = "ALB DNS name"
  value       = aws_lb.main.dns_name
}

output "alb_zone_id" {
  description = "ALB Zone ID (for Route 53)"
  value       = aws_lb.main.zone_id
}

output "alb_arn" {
  description = "ALB ARN"
  value       = aws_lb.main.arn
}

# ECS Outputs
output "ecs_cluster_name" {
  description = "ECS Cluster name"
  value       = aws_ecs_cluster.main.name
}

output "ecs_cluster_arn" {
  description = "ECS Cluster ARN"
  value       = aws_ecs_cluster.main.arn
}

output "ecs_service_name" {
  description = "ECS Service name"
  value       = aws_ecs_service.main.name
}

output "ecr_repository_url" {
  description = "ECR Repository URL"
  value       = aws_ecr_repository.main.repository_url
}

# RDS Outputs
output "rds_endpoint" {
  description = "RDS PostgreSQL endpoint"
  value       = aws_db_instance.main.endpoint
}

output "rds_address" {
  description = "RDS PostgreSQL address (without port)"
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

# ElastiCache Outputs
output "elasticache_endpoint" {
  description = "ElastiCache Redis endpoint"
  value       = aws_elasticache_cluster.main.cache_nodes[0].address
}

output "elasticache_port" {
  description = "ElastiCache Redis port"
  value       = aws_elasticache_cluster.main.cache_nodes[0].port
}

# Cognito Outputs
output "user_pool_id" {
  description = "Cognito User Pool ID"
  value       = aws_cognito_user_pool.main.id
}

output "client_id" {
  description = "Cognito App Client ID"
  value       = aws_cognito_user_pool_client.main.id
}

# S3 Bucket Outputs
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

# SNS/SQS Outputs
output "invoice_processing_queue_url" {
  description = "Invoice processing SQS queue URL"
  value       = aws_sqs_queue.invoice_processing.url
}

output "payment_processing_queue_url" {
  description = "Payment processing SQS queue URL"
  value       = aws_sqs_queue.payment_processing.url
}

# Secrets Manager Outputs
output "db_credentials_secret_arn" {
  description = "ARN of database credentials secret"
  value       = aws_secretsmanager_secret.db_credentials.arn
}

output "app_secrets_arn" {
  description = "ARN of application secrets"
  value       = aws_secretsmanager_secret.app_secrets.arn
}

# Monitoring Outputs
output "cloudwatch_dashboard_url" {
  description = "CloudWatch Dashboard URL"
  value       = "https://${var.aws_region}.console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards:name=${aws_cloudwatch_dashboard.main.dashboard_name}"
}

output "sns_alerts_topic_arn" {
  description = "SNS Topic ARN for alerts"
  value       = aws_sns_topic.alerts.arn
}

# WAF Output
output "waf_web_acl_arn" {
  description = "WAF Web ACL ARN"
  value       = aws_wafv2_web_acl.main.arn
}

# API Gateway Output (for reference)
output "api_gateway_endpoint" {
  description = "API Gateway Endpoint URL"
  value       = aws_apigatewayv2_api.main.api_endpoint
}

# Workflow Queue Output
output "workflow_runs_queue_url" {
  description = "Workflow runs SQS queue URL"
  value       = aws_sqs_queue.workflow_runs.url
}

output "workflow_notifications_topic_arn" {
  description = "SNS Topic ARN for workflow notifications"
  value       = aws_sns_topic.workflow_notifications.arn
}

# Mobile Push Platform Applications
output "fcm_platform_application_arn" {
  description = "FCM (Android) SNS Platform Application ARN"
  value       = aws_sns_platform_application.fcm.arn
}

output "apns_platform_application_arn" {
  description = "APNs (iOS) SNS Platform Application ARN"
  value       = aws_sns_platform_application.apns.arn
}
