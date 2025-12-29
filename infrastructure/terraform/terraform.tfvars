# AWS Configuration
aws_region = "us-east-1"

# Project Configuration
project_name = "invoice-backend"
environment  = "dev"

# Cognito Configuration
user_pool_name = "invoice-platform-pool"
client_name    = "invoice-platform-client"

# VPC Configuration
vpc_cidr           = "10.0.0.0/16"
availability_zones = ["us-east-1a", "us-east-1b"]

# RDS Configuration
db_instance_class = "db.t3.micro"
db_name           = "invoice_db"
db_username       = "invoice_user"
db_password       = "InvoicePass123"

# ElastiCache Configuration
cache_node_type = "cache.t3.micro"
cache_num_nodes = 1

# EC2 Configuration
ec2_instance_type = "t3.micro"
# ec2_key_name      = "your-key-pair-name" # Untouched since user didn't provide one
ssh_allowed_cidr = "0.0.0.0/0"
