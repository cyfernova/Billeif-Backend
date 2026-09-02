mock_provider "aws" {
  override_during = plan

  mock_data "aws_iam_policy_document" {
    override_during = plan
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  mock_resource "aws_lambda_invocation" {
    defaults = {
      result = "{\"status\":\"applied\",\"version\":58,\"latest_version\":58,\"dirty\":false,\"manifest_checksum\":\"eb9f3899fde99bec91964c8188e6e827427d41c43e842b2f0e2a69f11fd1953d\"}"
    }
  }

  mock_resource "aws_acm_certificate" {
    defaults = {
      arn = "arn:aws:acm:ap-south-1:928282274753:certificate/wrong-region"
    }
  }

  mock_resource "aws_cognito_user_pool_domain" {
    defaults = {
      cloudfront_distribution = "d111111abcdef8.cloudfront.net"
    }
  }

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "928282274753"
      arn        = "arn:aws:iam::928282274753:user/terraform-test"
      user_id    = "AIDATEST1234567890"
    }
  }

  override_data {
    target = data.aws_partition.current
    values = {
      partition = "aws"
    }
  }

  override_resource {
    target          = aws_cognito_user_pool.main
    override_during = plan
    values = {
      id = "ap-south-1_TESTPOOL"
    }
  }

  override_resource {
    target          = aws_api_gateway_rest_api.main
    override_during = plan
    values = {
      id               = "test-rest-api"
      root_resource_id = "test-root-resource"
      execution_arn    = "arn:aws:execute-api:ap-south-1:928282274753:test-rest-api"
    }
  }

  override_resource {
    target          = aws_apigatewayv2_api.http
    override_during = plan
    values = {
      id            = "test-http-api"
      api_endpoint  = "https://test-http-api.execute-api.ap-south-1.amazonaws.com"
      execution_arn = "arn:aws:execute-api:ap-south-1:928282274753:test-http-api"
      protocol_type = "HTTP"
    }
  }

  override_resource {
    target          = aws_kms_key.application_secrets
    override_during = plan
    values = {
      arn    = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
      key_id = "application-secrets"
    }
  }

  override_resource {
    target          = aws_db_instance.main
    override_during = plan
    values = {
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:rds-managed"
      }]
    }
  }
}

mock_provider "aws" {
  alias           = "ap_south_1"
  override_during = plan
}

mock_provider "aws" {
  alias           = "us_east_1"
  override_during = plan

  mock_resource "aws_acm_certificate" {
    defaults = {
      arn = "arn:aws:acm:us-east-1:928282274753:certificate/cognito-custom-domain"
      domain_validation_options = [{
        domain_name           = "auth.billeif.com"
        resource_record_name  = "_validation.auth.billeif.com."
        resource_record_type  = "CNAME"
        resource_record_value = "_token.acm-validations.aws."
      }]
    }
  }
}

mock_provider "awscc" {
  override_during = plan
}

mock_provider "dns" {
  override_during = plan

  mock_data "dns_cname_record_set" {
    defaults = {
      cname = "d111111abcdef8.cloudfront.net."
    }
  }
}

variables {
  project_name                   = "billeif"
  environment                    = "dev"
  lambda_artifact_dir            = "tests/fixtures/lambda"
  migration_lambda_artifact_path = "tests/fixtures/lambda/http.zip"
  llm_api_url                    = "https://llm.example.test/chat/completions"
  llm_model                      = "test-model"
  deepseek_base_url              = "https://voice-llm.example.test/v1"
  deepseek_model                 = "voice-test-model"
  ses_verified_identity          = "billeif.example"
  ses_sender_email               = "notifications@billeif.example"
  db_allowed_cidr                = "10.0.0.0/24"
}

run "custom_domain_uses_us_east_1_certificate_and_preserves_prefix_domain" {
  command = plan

  variables {
    enable_cognito_custom_domain_cutover = true
  }

  assert {
    condition = (
      aws_acm_certificate.cognito_custom_domain.domain_name == "auth.billeif.com" &&
      aws_acm_certificate.cognito_custom_domain.validation_method == "DNS" &&
      startswith(aws_acm_certificate.cognito_custom_domain.arn, "arn:aws:acm:us-east-1:")
    )
    error_message = "The Cognito custom domain certificate must use DNS validation in us-east-1."
  }

  assert {
    condition = (
      aws_cognito_user_pool_domain.custom[0].domain == "auth.billeif.com" &&
      aws_cognito_user_pool_domain.custom[0].user_pool_id == aws_cognito_user_pool.main.id &&
      aws_cognito_user_pool_domain.custom[0].certificate_arn == aws_acm_certificate_validation.cognito_custom_domain[0].certificate_arn
    )
    error_message = "The custom domain must wait for ACM validation before attaching auth.billeif.com to the existing primary user pool."
  }

  assert {
    condition = (
      aws_cognito_user_pool_domain.main.domain == "billeif-dev-928282274753" &&
      aws_cognito_user_pool_domain.main.user_pool_id == aws_cognito_user_pool.main.id &&
      aws_cognito_user_pool_domain.main.certificate_arn == null
    )
    error_message = "The existing Cognito prefix domain must remain attached during the custom-domain rollout."
  }

  assert {
    condition = (
      output.cognito_domain == "auth.billeif.com" &&
      aws_lambda_function.api_http.environment[0].variables["COGNITO_DOMAIN"] == "auth.billeif.com"
    )
    error_message = "Runtime consumers must use auth.billeif.com while the prefix domain remains available for rollback."
  }

  assert {
    condition = output.cognito_custom_domain_acm_validation == {
      name  = "_validation.auth.billeif.com."
      type  = "CNAME"
      value = "_token.acm-validations.aws."
    }
    error_message = "Terraform must output the exact ACM validation CNAME for the manual GoDaddy update."
  }

  assert {
    condition     = output.cognito_custom_domain_cloudfront_target == "d111111abcdef8.cloudfront.net"
    error_message = "Terraform must output Cognito's CloudFront target for the manual auth.billeif.com CNAME."
  }

  assert {
    condition     = output.cognito_prefix_domain == "billeif-dev-928282274753.auth.ap-south-1.amazoncognito.com"
    error_message = "Terraform must expose the preserved prefix domain for configuration-driven rollback."
  }

  assert {
    condition = output.cognito_custom_domain_google_oauth == {
      authorized_origin = "https://auth.billeif.com"
      redirect_uri      = "https://auth.billeif.com/oauth2/idpresponse"
    }
    error_message = "Terraform must output the exact Google OAuth origin and redirect URI required by the Cognito custom domain."
  }
}

run "custom_domain_cutover_rejects_incorrect_dns_target" {
  command = plan

  variables {
    enable_cognito_custom_domain_cutover = true
  }

  override_data {
    target = data.dns_cname_record_set.cognito_custom_domain[0]
    values = {
      cname = "wrong-target.example.com."
    }
  }

  expect_failures = [output.cognito_domain]
}

run "default_rollout_keeps_consumers_on_the_prefix_domain" {
  command = plan

  assert {
    condition = (
      output.cognito_domain == "billeif-dev-928282274753.auth.ap-south-1.amazoncognito.com" &&
      aws_lambda_function.api_http.environment[0].variables["COGNITO_DOMAIN"] == "billeif-dev-928282274753.auth.ap-south-1.amazoncognito.com"
    )
    error_message = "Runtime consumers must stay on the working prefix domain until the custom-domain DNS cutover is explicitly confirmed."
  }

  assert {
    condition = (
      length(aws_acm_certificate_validation.cognito_custom_domain) == 0 &&
      length(aws_cognito_user_pool_domain.custom) == 0
    )
    error_message = "The default certificate-only stage must not wait for manual DNS validation or create the Cognito custom domain."
  }

  assert {
    condition     = aws_acm_certificate.cognito_custom_domain.domain_name == "auth.billeif.com"
    error_message = "The certificate-only stage must expose the exact Billeif hostname regardless of project resource naming."
  }

  assert {
    condition     = output.cognito_custom_domain_cloudfront_target == null
    error_message = "The certificate-only stage must not advertise a CloudFront target before Cognito accepts the custom domain."
  }
}

run "provisioning_exposes_cloudfront_target_without_runtime_cutover" {
  command = plan

  variables {
    enable_cognito_custom_domain_provisioning = true
  }

  assert {
    condition = (
      length(aws_acm_certificate_validation.cognito_custom_domain) == 1 &&
      length(aws_cognito_user_pool_domain.custom) == 1 &&
      output.cognito_custom_domain_cloudfront_target == "d111111abcdef8.cloudfront.net"
    )
    error_message = "Provisioning must create the Cognito domain and expose its CloudFront target."
  }

  assert {
    condition = (
      output.cognito_domain == "billeif-dev-928282274753.auth.ap-south-1.amazoncognito.com" &&
      aws_lambda_function.api_http.environment[0].variables["COGNITO_DOMAIN"] == "billeif-dev-928282274753.auth.ap-south-1.amazoncognito.com"
    )
    error_message = "Provisioning the custom domain must not promote runtime consumers before DNS and Google OAuth are ready."
  }
}
