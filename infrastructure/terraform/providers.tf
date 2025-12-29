terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region                      = var.aws_region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  s3_use_path_style           = true

  endpoints {
    cognitoidp = var.aws_endpoint
    dynamodb   = var.aws_endpoint
    s3         = var.aws_endpoint
    ses        = var.aws_endpoint
    sqs        = var.aws_endpoint
    sns        = var.aws_endpoint
  }
}
