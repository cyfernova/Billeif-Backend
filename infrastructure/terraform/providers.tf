terraform {
  required_version = ">= 1.13.0, < 2.0.0"

  backend "s3" {}

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.25.0, < 7.0.0"
    }
    awscc = {
      source  = "hashicorp/awscc"
      version = "= 1.95.0"
    }
    dns = {
      source  = "hashicorp/dns"
      version = ">= 3.6.1, < 4.0.0"
    }
    external = {
      source  = "hashicorp/external"
      version = "= 2.3.5"
    }
  }
}

provider "aws" {
  region  = var.aws_region
  profile = var.use_ambient_aws_credentials ? null : var.aws_profile

  default_tags {
    tags = {
      Project     = var.project_name
      Environment = var.environment
      ManagedBy   = "Terraform"
    }
  }
}

provider "awscc" {
  region  = "ap-south-1"
  profile = var.use_ambient_aws_credentials ? null : var.aws_profile
}

provider "aws" {
  alias   = "ap_south_1"
  region  = "ap-south-1"
  profile = var.use_ambient_aws_credentials ? null : var.aws_profile

  default_tags {
    tags = {
      Project     = var.project_name
      Environment = var.environment
      ManagedBy   = "Terraform"
    }
  }
}

provider "aws" {
  alias   = "us_east_1"
  region  = "us-east-1"
  profile = var.use_ambient_aws_credentials ? null : var.aws_profile

  default_tags {
    tags = {
      Project     = var.project_name
      Environment = var.environment
      ManagedBy   = "Terraform"
    }
  }
}
