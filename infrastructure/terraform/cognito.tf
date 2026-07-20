locals {
  google_oauth_secret_name   = var.google_oauth_secret_name != "" ? var.google_oauth_secret_name : "/${var.project_name}/${var.environment}/cognito/google-auth"
  swagger_oauth_redirect_url = "${local.rest_api_invoke_url}/swagger/oauth2-redirect.html"
  cognito_callback_urls      = distinct(concat([local.swagger_oauth_redirect_url], var.cognito_additional_callback_urls))
  cognito_logout_urls        = distinct(var.cognito_additional_logout_urls)
}

data "aws_secretsmanager_secret" "google_oauth" {
  count = var.google_client_id == "" && var.google_client_secret == "" ? 1 : 0
  name  = local.google_oauth_secret_name
}

data "aws_secretsmanager_secret_version" "google_oauth" {
  count     = length(data.aws_secretsmanager_secret.google_oauth) > 0 ? 1 : 0
  secret_id = data.aws_secretsmanager_secret.google_oauth[0].id
}

locals {
  google_oauth_secret_sensitive  = length(data.aws_secretsmanager_secret_version.google_oauth) > 0 ? jsondecode(data.aws_secretsmanager_secret_version.google_oauth[0].secret_string) : {}
  google_oauth_secret            = length(data.aws_secretsmanager_secret_version.google_oauth) > 0 ? jsondecode(nonsensitive(data.aws_secretsmanager_secret_version.google_oauth[0].secret_string)) : {}
  google_client_id_resolved      = trimspace(var.google_client_id != "" ? var.google_client_id : try(local.google_oauth_secret.client_id, ""))
  google_client_secret_resolved  = trimspace(var.google_client_secret != "" ? var.google_client_secret : try(local.google_oauth_secret.client_secret, ""))
  google_client_id_sensitive     = var.google_client_id != "" ? sensitive(var.google_client_id) : try(local.google_oauth_secret_sensitive.client_id, sensitive(""))
  google_client_secret_sensitive = var.google_client_secret != "" ? sensitive(var.google_client_secret) : try(local.google_oauth_secret_sensitive.client_secret, sensitive(""))
}

resource "aws_cognito_user_pool" "main" {
  name = var.user_pool_name

  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  password_policy {
    minimum_length                   = 8
    require_uppercase                = true
    require_lowercase                = true
    require_numbers                  = true
    require_symbols                  = true
    temporary_password_validity_days = 7
  }

  schema {
    attribute_data_type = "String"
    name                = "businessId"
    required            = false
    mutable             = true
    string_attribute_constraints {
      min_length = 1
      max_length = 256
    }
  }

  schema {
    attribute_data_type = "String"
    name                = "role"
    required            = false
    mutable             = true
    string_attribute_constraints {
      min_length = 1
      max_length = 50
    }
  }

  verification_message_template {
    default_email_option = "CONFIRM_WITH_CODE"
    email_message        = "Your verification code is {####}"
    email_subject        = "Verify your email"
  }

  email_configuration {
    email_sending_account = "COGNITO_DEFAULT"
  }
}

resource "aws_cognito_user_pool_client" "main" {
  name         = var.client_name
  user_pool_id = aws_cognito_user_pool.main.id

  explicit_auth_flows           = ["ALLOW_USER_PASSWORD_AUTH", "ALLOW_REFRESH_TOKEN_AUTH", "ALLOW_USER_SRP_AUTH", "ALLOW_ADMIN_USER_PASSWORD_AUTH"]
  generate_secret               = false
  prevent_user_existence_errors = "ENABLED"
  enable_token_revocation       = true

  # OAuth configuration for Google Sign-In
  supported_identity_providers         = ["COGNITO", "Google"]
  callback_urls                        = local.cognito_callback_urls
  logout_urls                          = local.cognito_logout_urls
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["email", "openid", "profile", "aws.cognito.signin.user.admin"]

  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }

  lifecycle {
    precondition {
      condition     = local.google_client_id_resolved != "" && local.google_client_secret_resolved != ""
      error_message = "Google OAuth credentials are required. Store JSON with client_id and client_secret in AWS Secrets Manager secret ${local.google_oauth_secret_name} or provide the legacy google_client_id/google_client_secret variables."
    }
  }
}

resource "aws_cognito_user_group" "admin" {
  name         = "admin"
  user_pool_id = aws_cognito_user_pool.main.id
  description  = "Administrators with full access"
  precedence   = 0
}

resource "aws_cognito_user_group" "accountant" {
  name         = "accountant"
  user_pool_id = aws_cognito_user_pool.main.id
  description  = "Accountants with financial access"
  precedence   = 1
}

resource "aws_cognito_user_group" "viewer" {
  name         = "viewer"
  user_pool_id = aws_cognito_user_pool.main.id
  description  = "Viewers with read-only access"
  precedence   = 2
}

# Cognito Domain for Hosted UI
resource "aws_cognito_user_pool_domain" "main" {
  domain       = local.cognito_domain_prefix
  user_pool_id = aws_cognito_user_pool.main.id
}

# Google Identity Provider
resource "aws_cognito_identity_provider" "google" {
  count = 1

  user_pool_id  = aws_cognito_user_pool.main.id
  provider_name = "Google"
  provider_type = "Google"

  provider_details = {
    attributes_url                = "https://people.googleapis.com/v1/people/me?personFields="
    attributes_url_add_attributes = "true"
    authorize_url                 = "https://accounts.google.com/o/oauth2/v2/auth"
    oidc_issuer                   = "https://accounts.google.com"
    token_request_method          = "POST"
    token_url                     = "https://www.googleapis.com/oauth2/v4/token"
    client_id                     = local.google_client_id_sensitive
    client_secret                 = local.google_client_secret_sensitive
    authorize_scopes              = "profile email openid"
  }

  attribute_mapping = {
    email    = "email"
    username = "sub"
    name     = "name"
    picture  = "picture"
  }

  lifecycle {
    precondition {
      condition     = local.google_client_id_resolved != "" && local.google_client_secret_resolved != ""
      error_message = "Google OAuth credentials are required. Store JSON with client_id and client_secret in AWS Secrets Manager secret ${local.google_oauth_secret_name} or provide the legacy google_client_id/google_client_secret variables."
    }
  }
}
