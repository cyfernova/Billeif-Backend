mock_provider "aws" {}

run "razorpay_ssm_lambda_iam_and_output" {
  command = plan

  variables {
    project_name        = "invoice-backend-test"
    environment         = "test"
    aws_region          = "us-east-1"
    lambda_artifact_dir = "tests/fixtures/lambda"
    llm_api_url         = "https://llm.example.test/chat/completions"
    llm_model           = "test-model"
    deepseek_base_url   = "https://voice-llm.example.test/v1"
    deepseek_model      = "voice-test-model"

    db_allowed_cidr                   = "10.0.0.0/24"
    db_password                       = "unit-test-db-password-123!"
    jwt_secret                        = "unit-test-jwt-secret-32-characters"
    razorpay_key_id                   = "rzp_test_x"
    razorpay_key_secret               = "secret_x"
    razorpay_webhook_secret           = "whsec_x"
    credential_encryption_key         = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
    google_oauth_secret_name          = ""
    india_sms_sender_id               = ""
    india_dlt_entity_id               = ""
    india_signup_template_id          = ""
    india_auth_template_id            = ""
    enable_lambda_reserved_concurrency = false
  }

  assert {
    condition     = aws_ssm_parameter.razorpay_key_id.type == "SecureString"
    error_message = "Razorpay key ID must be stored in SSM as SecureString."
  }

  assert {
    condition     = aws_ssm_parameter.razorpay_key_secret.type == "SecureString"
    error_message = "Razorpay key secret must be stored in SSM as SecureString."
  }

  assert {
    condition     = aws_ssm_parameter.razorpay_webhook_secret.type == "SecureString"
    error_message = "Razorpay webhook secret must be stored in SSM as SecureString."
  }

  assert {
    condition = (
      aws_lambda_function.api_http.environment[0].variables["RAZORPAY_KEY_ID_SSM_PARAM"] == local.razorpay_key_id_ssm_parameter_name &&
      aws_lambda_function.api_http.environment[0].variables["RAZORPAY_KEY_SECRET_SSM_PARAM"] == local.razorpay_key_secret_ssm_parameter_name &&
      aws_lambda_function.api_http.environment[0].variables["RAZORPAY_WEBHOOK_SECRET_SSM_PARAM"] == local.razorpay_webhook_secret_ssm_parameter_name
    )
    error_message = "HTTP Lambda must receive only Razorpay SSM parameter names."
  }

  assert {
    condition = length([
      for statement in jsondecode(data.aws_iam_policy_document.lambda_app.json).Statement : statement
      if statement.Sid == "SSMAccess" &&
      contains(statement.Resource, local.razorpay_key_id_ssm_parameter_arn) &&
      contains(statement.Resource, local.razorpay_key_secret_ssm_parameter_arn) &&
      contains(statement.Resource, local.razorpay_webhook_secret_ssm_parameter_arn)
    ]) == 1
    error_message = "Lambda IAM policy must grant GetParameter access to Razorpay SSM parameters."
  }

  assert {
    condition     = endswith(output.razorpay_webhook_url, "/api/v1/webhooks/razorpay")
    error_message = "Razorpay webhook output must expose the fresh webhook endpoint."
  }
}
