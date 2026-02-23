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
  supported_identity_providers         = var.google_client_id != "" && var.google_client_secret != "" ? ["COGNITO", "Google"] : ["COGNITO"]
  callback_urls                        = ["myapp://callback", "http://localhost:3000/callback"]
  logout_urls                          = ["myapp://logout", "http://localhost:3000/logout"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code", "implicit"]
  allowed_oauth_scopes                 = ["email", "openid", "profile", "aws.cognito.signin.user.admin"]

  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
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
  domain       = var.cognito_domain_prefix
  user_pool_id = aws_cognito_user_pool.main.id
}

# Google Identity Provider
resource "aws_cognito_identity_provider" "google" {
  count = var.google_client_id != "" && var.google_client_secret != "" ? 1 : 0

  user_pool_id  = aws_cognito_user_pool.main.id
  provider_name = "Google"
  provider_type = "Google"

  provider_details = {
    client_id        = var.google_client_id
    client_secret    = var.google_client_secret
    authorize_scopes = "profile email openid"
  }

  attribute_mapping = {
    email    = "email"
    username = "sub"
    name     = "name"
    picture  = "picture"
  }
}
