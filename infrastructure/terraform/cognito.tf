locals {
  swagger_oauth_redirect_url = "${local.http_api_invoke_url}/swagger/oauth2-redirect.html"
  cognito_callback_urls      = distinct(concat([local.swagger_oauth_redirect_url], var.cognito_additional_callback_urls))
  cognito_logout_urls        = distinct(var.cognito_additional_logout_urls)
  cognito_custom_domain      = "auth.billeif.com"
  cognito_prefix_domain      = "${local.cognito_hosted_ui_domain_prefix}.auth.${var.aws_region}.amazoncognito.com"
  cognito_custom_domain_enabled = (
    var.enable_cognito_custom_domain_provisioning ||
    var.enable_cognito_custom_domain_cutover
  )
  cognito_runtime_domain = var.enable_cognito_custom_domain_cutover ? aws_cognito_user_pool_domain.custom[0].domain : local.cognito_prefix_domain

  google_oauth_client_id_reference     = "{{resolve:secretsmanager:${aws_secretsmanager_secret.google_oauth.arn}:SecretString:client_id}}"
  google_oauth_client_secret_reference = "{{resolve:secretsmanager:${aws_secretsmanager_secret.google_oauth.arn}:SecretString:client_secret}}"
}

resource "aws_cognito_user_pool" "main" {
  name = local.cognito_web_user_pool_name

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
  name         = local.cognito_web_client_name
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
  allowed_oauth_scopes                 = ["email", "openid", "profile", "aws.cognito.signin.user.admin", aws_cognito_resource_server.main.scope_identifiers[0]]

  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }

  depends_on = [aws_cloudformation_stack.google_cognito_identity_provider]
}

resource "aws_cloudformation_stack" "google_cognito_identity_provider" {
  name = "${local.resource_prefix}-google-cognito-identity-provider"

  template_body = jsonencode({
    AWSTemplateFormatVersion = "2010-09-09"
    Description              = "Billeif Google identity provider for the Cognito user pool"
    Resources = {
      GoogleIdentityProvider = {
        Type = "AWS::Cognito::UserPoolIdentityProvider"
        Properties = {
          ProviderName = "Google"
          ProviderType = "Google"
          UserPoolId   = aws_cognito_user_pool.main.id
          ProviderDetails = {
            authorize_scopes = "openid email profile"
            client_id        = local.google_oauth_client_id_reference
            client_secret    = local.google_oauth_client_secret_reference
          }
          AttributeMapping = {
            email          = "email"
            email_verified = "email_verified"
            family_name    = "family_name"
            given_name     = "given_name"
            name           = "name"
            picture        = "picture"
            username       = "sub"
          }
        }
      }
    }
  })

  tags = {
    Name = "${local.resource_prefix}-google-cognito-identity-provider"
  }
}

resource "aws_cognito_resource_server" "main" {
  identifier   = local.cognito_resource_server_id
  name         = local.cognito_resource_server_name
  user_pool_id = aws_cognito_user_pool.main.id

  scope {
    scope_name        = "access"
    scope_description = "Billeif application API access"
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
  domain       = local.cognito_hosted_ui_domain_prefix
  user_pool_id = aws_cognito_user_pool.main.id

  lifecycle {
    precondition {
      condition = (
        length(local.resource_prefix) <= 29 &&
        can(regex("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$", local.cognito_hosted_ui_domain_prefix)) &&
        strcontains(local.cognito_hosted_ui_domain_prefix, "billeif") &&
        !strcontains(local.cognito_hosted_ui_domain_prefix, "aws") &&
        !strcontains(local.cognito_hosted_ui_domain_prefix, "amazon") &&
        !strcontains(local.cognito_hosted_ui_domain_prefix, "cognito")
      )
      error_message = "Generated AWS names must fit their service limits, and the Cognito hosted UI prefix must be a valid Billeif-branded 1-63 character prefix."
    }
  }
}

resource "aws_acm_certificate" "cognito_custom_domain" {
  provider = aws.us_east_1

  domain_name       = local.cognito_custom_domain
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_acm_certificate_validation" "cognito_custom_domain" {
  provider = aws.us_east_1
  count    = local.cognito_custom_domain_enabled ? 1 : 0

  certificate_arn = aws_acm_certificate.cognito_custom_domain.arn
}

resource "aws_cognito_user_pool_domain" "custom" {
  count = local.cognito_custom_domain_enabled ? 1 : 0

  domain          = local.cognito_custom_domain
  certificate_arn = aws_acm_certificate_validation.cognito_custom_domain[0].certificate_arn
  user_pool_id    = aws_cognito_user_pool.main.id
}

moved {
  from = aws_acm_certificate_validation.cognito_custom_domain
  to   = aws_acm_certificate_validation.cognito_custom_domain[0]
}

moved {
  from = aws_cognito_user_pool_domain.custom
  to   = aws_cognito_user_pool_domain.custom[0]
}
