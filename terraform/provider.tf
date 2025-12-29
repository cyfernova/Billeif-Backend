terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.27.0"
    }
  }
  required_version = ">= 1.9.0"
}

provider "aws" {
  region     = var.aws_region
  access_key = var.aws_access_key_id
  secret_key = var.aws_secret_access_key

  endpoints {
    dynamodb = var.aws_endpoint
    s3       = var.aws_endpoint
    ses      = var.aws_endpoint
    sns      = var.aws_endpoint
    sqs      = var.aws_endpoint
  }

  skip_metadata_api_check     = true
  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_region_validation      = true
}
