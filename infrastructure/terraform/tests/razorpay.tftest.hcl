mock_provider "aws" {
  override_during = plan

  mock_data "aws_iam_policy_document" {
    override_during = plan
    defaults = {
      json = <<-JSON
        {"Version":"2012-10-17","Statement":[{"Sid":"AssumeRole","Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"lambda.amazonaws.com"}}]}
      JSON
    }
  }

  mock_resource "aws_lambda_invocation" {
    defaults = {
      result = "{\"status\":\"applied\",\"version\":49,\"latest_version\":49,\"dirty\":false,\"manifest_checksum\":\"31b347c1fb7c8ef8af2056af21c5cea15698855f7de547ed2a9caacba7f3e17c\"}"
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
    target          = aws_api_gateway_rest_api.main
    override_during = plan
    values = {
      id               = "test-rest-api"
      root_resource_id = "test-root-resource"
      execution_arn    = "arn:aws:execute-api:ap-south-1:928282274753:test-rest-api"
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
    target          = aws_iam_role.lambda_exec
    override_during = plan
    values = {
      arn = "arn:aws:iam::928282274753:role/billeif-lambda-exec"
    }
  }

  override_resource {
    target          = aws_iam_role.lambda_http_exec
    override_during = plan
    values = {
      arn = "arn:aws:iam::928282274753:role/billeif-lambda-http-exec"
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

  override_resource {
    target          = aws_sqs_queue.invoice_processing
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-invoice-processing-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.gst_processing
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-gst-processing-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.bargaining_negotiation
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-bargaining-negotiation-queue"
    }
  }

  override_resource {
    target          = aws_sqs_queue.invoice_processing_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-invoice-processing-dlq"
    }
  }

  override_resource {
    target          = aws_sqs_queue.gst_processing_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-gst-processing-dlq"
    }
  }

  override_resource {
    target          = aws_sqs_queue.bargaining_negotiation_dlq
    override_during = plan
    values = {
      arn = "arn:aws:sqs:ap-south-1:928282274753:billeif-test-test-bargaining-negotiation-dlq"
    }
  }

  override_resource {
    target          = aws_s3_bucket.invoices_pdf
    override_during = plan
    values = {
      arn = "arn:aws:s3:::billeif-test-test-invoices-pdf"
      id  = "billeif-test-test-invoices-pdf"
    }
  }

  override_resource {
    target          = aws_s3_bucket.email_sink
    override_during = plan
    values = {
      arn = "arn:aws:s3:::billeif-test-test-email-sink"
      id  = "billeif-test-test-email-sink"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.credential_encryption
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:credential-encryption"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.razorpay
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:razorpay"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.google_oauth
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:/billeif-test/test/providers/google-oauth-test"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.llm
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:llm"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.exa
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:exa"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.gst_lookup
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:gst-lookup"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.gst_provider
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:gst-provider"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.deepseek
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:deepseek"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.sarvam
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:sarvam"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.billeif_invoice_cursor_hmac
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret:billeif-invoice-cursor-hmac"
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
}

mock_provider "awscc" {
  override_during = plan
}

variables {
  migration_lambda_artifact_path = "tests/fixtures/lambda/http.zip"
}

run "mumbai_defaults_and_oidc_profile" {
  command = plan

  variables {
    project_name          = "billeif-test"
    environment           = "test"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"

    db_allowed_cidr = "10.0.0.0/24"
  }

  assert {
    condition     = output.cognito_region == "ap-south-1"
    error_message = "The primary Terraform region must default to ap-south-1."
  }

  assert {
    condition = (
      aws_subnet.public[0].availability_zone == "ap-south-1a" &&
      aws_subnet.public[1].availability_zone == "ap-south-1b" &&
      aws_subnet.private[0].availability_zone == "ap-south-1a" &&
      aws_subnet.private[1].availability_zone == "ap-south-1b"
    )
    error_message = "Public and private subnets must use Mumbai availability-zone defaults."
  }

  assert {
    condition     = var.aws_profile == "default"
    error_message = "The AWS provider profile must default to the local default profile."
  }

  assert {
    condition     = var.use_ambient_aws_credentials == false
    error_message = "Local Terraform runs must use the configured AWS profile by default."
  }

  assert {
    condition     = toset(aws_cognito_user_pool_client.main.supported_identity_providers) == toset(["COGNITO", "Google"])
    error_message = "The Cognito web client must offer exactly its native and Google identity providers."
  }

  assert {
    condition = (
      jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Resources.GoogleIdentityProvider.Type == "AWS::Cognito::UserPoolIdentityProvider" &&
      jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Resources.GoogleIdentityProvider.Properties.ProviderName == "Google" &&
      jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Resources.GoogleIdentityProvider.Properties.ProviderType == "Google" &&
      jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Resources.GoogleIdentityProvider.Properties.UserPoolId == "ap-south-1_TESTPOOL" &&
      jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Resources.GoogleIdentityProvider.Properties.ProviderDetails.authorize_scopes == "openid email profile" &&
      try(length(keys(jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Parameters)), 0) == 0 &&
      jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Resources.GoogleIdentityProvider.Properties.ProviderDetails.client_id == "{{resolve:secretsmanager:arn:aws:secretsmanager:ap-south-1:928282274753:secret:/billeif-test/test/providers/google-oauth-test:SecretString:client_id}}" &&
      jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Resources.GoogleIdentityProvider.Properties.ProviderDetails.client_secret == "{{resolve:secretsmanager:arn:aws:secretsmanager:ap-south-1:928282274753:secret:/billeif-test/test/providers/google-oauth-test:SecretString:client_secret}}" &&
      jsondecode(aws_cloudformation_stack.google_cognito_identity_provider.template_body).Resources.GoogleIdentityProvider.Properties.AttributeMapping == {
        email          = "email"
        email_verified = "email_verified"
        family_name    = "family_name"
        given_name     = "given_name"
        name           = "name"
        picture        = "picture"
        username       = "sub"
      }
    )
    error_message = "The Google identity provider must resolve its credentials from the KMS-encrypted Secrets Manager JSON keys at deployment time."
  }
}

run "ambient_aws_credentials_are_explicit_for_oidc" {
  command = plan

  variables {
    project_name          = "billeif-test"
    environment           = "test"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"

    use_ambient_aws_credentials = true
    db_allowed_cidr             = "10.0.0.0/24"
  }

  assert {
    condition     = var.use_ambient_aws_credentials == true
    error_message = "OIDC must explicitly select ambient AWS credentials instead of a named profile."
  }
}

run "secret_metadata_rds_lambda_iam_and_output" {
  command = plan

  variables {
    project_name          = "billeif-test"
    environment           = "test"
    aws_region            = "ap-south-1"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"

    db_allowed_cidr                    = "10.0.0.0/24"
    enable_lambda_reserved_concurrency = false
  }

  assert {
    condition     = aws_kms_key.application_secrets.enable_key_rotation
    error_message = "The application Secrets Manager KMS key must enable rotation."
  }

  assert {
    condition = (
      aws_db_instance.main.manage_master_user_password &&
      aws_db_instance.main.master_user_secret_kms_key_id == aws_kms_key.application_secrets.arn &&
      aws_db_instance.main.password == null
    )
    error_message = "RDS must manage its master password with the dedicated KMS key."
  }

  assert {
    condition = (
      length(distinct([
        aws_secretsmanager_secret.credential_encryption.name,
        aws_secretsmanager_secret.billeif_invoice_cursor_hmac.name,
        aws_secretsmanager_secret.razorpay.name,
        aws_secretsmanager_secret.legacy_jwt.name,
        aws_secretsmanager_secret.google_oauth.name,
        aws_secretsmanager_secret.fcm.name,
        aws_secretsmanager_secret.apns.name,
        aws_secretsmanager_secret.llm.name,
        aws_secretsmanager_secret.exa.name,
        aws_secretsmanager_secret.gst_lookup.name,
        aws_secretsmanager_secret.gst_provider.name,
        aws_secretsmanager_secret.deepseek.name,
        aws_secretsmanager_secret.sarvam.name
      ])) == 13 &&
      alltrue([
        aws_secretsmanager_secret.credential_encryption.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.billeif_invoice_cursor_hmac.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.razorpay.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.legacy_jwt.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.google_oauth.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.fcm.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.apns.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.llm.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.exa.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.gst_lookup.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.gst_provider.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.deepseek.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.sarvam.kms_key_id == aws_kms_key.application_secrets.arn
      ])
    )
    error_message = "Unrelated application/provider credentials must use separated secret containers."
  }

  assert {
    condition     = aws_secretsmanager_secret.google_oauth.name == "/billeif-test/test/providers/google-oauth"
    error_message = "The Google OAuth metadata must use the Billeif project/environment secret path."
  }

  assert {
    condition = (
      aws_lambda_function.api_http.environment[0].variables["DATABASE_SECRET_ARN"] == aws_db_instance.main.master_user_secret[0].secret_arn &&
      aws_lambda_function.api_http.environment[0].variables["RAZORPAY_SECRET_ARN"] == aws_secretsmanager_secret.razorpay.arn &&
      aws_lambda_function.api_http.environment[0].variables["LLM_SECRET_ARN"] == aws_secretsmanager_secret.llm.arn &&
      sort([
        for key in keys(aws_lambda_function.api_http.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
        ]) == sort([
        "CREDENTIAL_ENCRYPTION_SECRET_ARN",
        "DATABASE_SECRET_ARN",
        "EXA_SECRET_ARN",
        "GST_LOOKUP_SECRET_ARN",
        "GST_PROVIDER_SECRET_ARN",
        "INVOICE_CURSOR_HMAC_SECRET_ARN",
        "LLM_SECRET_ARN",
        "RAZORPAY_SECRET_ARN",
        "DEEPSEEK_SECRET_ARN",
        "SARVAM_SECRET_ARN"
      ]) &&
      !contains(keys(aws_lambda_function.api_http.environment[0].variables), "DATABASE_PASSWORD") &&
      !contains(keys(aws_lambda_function.api_http.environment[0].variables), "RAZORPAY_KEY_SECRET")
    )
    error_message = "HTTP Lambda environments must receive secret identifiers only."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.lambda_app.statement : statement
      if statement.sid == "SecretsManagerAccess" &&
      contains(statement.actions, "secretsmanager:GetSecretValue") &&
      contains(statement.actions, "secretsmanager:DescribeSecret") &&
      contains(statement.resources, aws_secretsmanager_secret.razorpay.arn) &&
      contains(statement.resources, aws_secretsmanager_secret.gst_provider.arn) &&
      contains(statement.resources, aws_secretsmanager_secret.sarvam.arn) &&
      length(statement.resources) == 9 &&
      !contains(statement.resources, "*")
    ]) == 1
    error_message = "Lambda IAM must scope secret reads to exact managed ARNs."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.lambda_app.statement : statement
      if statement.sid == "ApplicationSecretsKMSDecrypt" &&
      length(statement.resources) == 1 &&
      contains(statement.resources, aws_kms_key.application_secrets.arn) &&
      length([
        for condition in statement.condition : condition
        if condition.variable == "kms:ViaService" &&
        condition.test == "StringEquals" &&
        length(condition.values) == 1 &&
        contains(condition.values, "secretsmanager.ap-south-1.amazonaws.com")
      ]) == 1 &&
      length([
        for condition in statement.condition : condition
        if condition.variable == "kms:EncryptionContext:SecretARN" &&
        condition.test == "StringEquals" &&
        length(condition.values) == 9
      ]) == 1
    ]) == 1
    error_message = "Lambda IAM must scope KMS decrypt to the dedicated key ARN."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
      if statement.sid == "HTTPRuntimeSecrets" &&
      toset(statement.actions) == toset(["secretsmanager:GetSecretValue"]) &&
      toset(statement.resources) == toset([
        aws_db_instance.main.master_user_secret[0].secret_arn,
        aws_secretsmanager_secret.credential_encryption.arn,
        aws_secretsmanager_secret.razorpay.arn,
        aws_secretsmanager_secret.llm.arn,
        aws_secretsmanager_secret.exa.arn,
        aws_secretsmanager_secret.gst_lookup.arn,
        aws_secretsmanager_secret.gst_provider.arn,
        aws_secretsmanager_secret.sarvam.arn,
      ]) &&
      !contains(statement.resources, aws_secretsmanager_secret.deepseek.arn)
    ]) == 1
    error_message = "The HTTP role must read only request-reachable runtime secrets and must not describe or read the unused DeepSeek secret."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.lambda_http_app.statement : statement
      if statement.sid == "HTTPRuntimeSecretDecrypt" &&
      toset(statement.actions) == toset(["kms:Decrypt"]) &&
      toset(statement.resources) == toset([aws_kms_key.application_secrets.arn]) &&
      length([
        for condition in statement.condition : condition
        if condition.test == "StringEquals" &&
        condition.variable == "kms:CallerAccount" &&
        toset(condition.values) == toset([data.aws_caller_identity.current.account_id])
      ]) == 1 &&
      length([
        for condition in statement.condition : condition
        if condition.test == "StringEquals" &&
        condition.variable == "kms:ViaService" &&
        toset(condition.values) == toset(["secretsmanager.ap-south-1.amazonaws.com"])
      ]) == 1 &&
      length([
        for condition in statement.condition : condition
        if condition.test == "StringEquals" &&
        condition.variable == "kms:EncryptionContext:SecretARN" &&
        toset(condition.values) == toset(local.http_runtime_secret_arns)
      ]) == 1
    ]) == 1
    error_message = "HTTP KMS decrypt must be restricted to same-account Secrets Manager use and the exact runtime secret encryption contexts."
  }

  assert {
    condition = alltrue([
      toset([
        for key in keys(aws_lambda_function.sqs_invoice.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["CREDENTIAL_ENCRYPTION_SECRET_ARN", "DATABASE_SECRET_ARN"]),
      toset([
        for key in keys(aws_lambda_function.sqs_gst.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["CREDENTIAL_ENCRYPTION_SECRET_ARN", "DATABASE_SECRET_ARN", "GST_PROVIDER_SECRET_ARN"]),
      toset([
        for key in keys(aws_lambda_function.sqs_bargaining.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["DATABASE_SECRET_ARN", "EXA_SECRET_ARN", "LLM_SECRET_ARN"]),
      toset([
        for key in keys(aws_lambda_function.ws_handler.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["DATABASE_SECRET_ARN"])
    ])
    error_message = "Every Lambda environment must receive only the secret identifiers used by its entrypoint."
  }

  assert {
    condition = alltrue([
      for worker, queue_arn in {
        invoice    = aws_sqs_queue.invoice_processing.arn
        gst        = aws_sqs_queue.gst_processing.arn
        bargaining = aws_sqs_queue.bargaining_negotiation.arn
        } : length([
          for statement in data.aws_iam_policy_document.lambda_worker_app[worker].statement : statement
          if statement.sid == "WorkerQueueConsume" &&
          toset(statement.actions) == toset([
            "sqs:DeleteMessage",
            "sqs:GetQueueAttributes",
            "sqs:ReceiveMessage",
          ]) &&
          toset(statement.resources) == toset([queue_arn])
      ]) == 1
    ])
    error_message = "Each worker role must consume exactly its own queue with only the Lambda SQS poller actions."
  }

  assert {
    condition = (
      length([
        for statement in data.aws_iam_policy_document.lambda_worker_app["bargaining"].statement : statement
        if statement.sid == "WorkerQueueSelfSend" &&
        toset(statement.actions) == toset(["sqs:SendMessage"]) &&
        toset(statement.resources) == toset([aws_sqs_queue.bargaining_negotiation.arn])
      ]) == 1 &&
      alltrue([
        for worker in ["invoice", "gst"] : alltrue([
          for statement in data.aws_iam_policy_document.lambda_worker_app[worker].statement :
          !contains(statement.actions, "sqs:SendMessage")
        ])
      ])
    )
    error_message = "Only the bargaining worker may self-send, and only to the bargaining queue."
  }

  assert {
    condition = (
      length([
        for statement in data.aws_iam_policy_document.lambda_worker_app["invoice"].statement : statement
        if statement.sid == "WorkerStorageRead" &&
        toset(statement.actions) == toset(["s3:GetObject"]) &&
        toset(statement.resources) == toset(["${aws_s3_bucket.invoices_pdf.arn}/invoices/*"])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_worker_app["invoice"].statement : statement
        if statement.sid == "WorkerStorageWrite" &&
        toset(statement.actions) == toset(["s3:PutObject"]) &&
        toset(statement.resources) == toset([
          "${aws_s3_bucket.invoices_pdf.arn}/documents/*",
          "${aws_s3_bucket.invoices_pdf.arn}/invoices/*",
        ])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.lambda_worker_app["gst"].statement : statement
        if statement.sid == "WorkerStorageWrite" &&
        toset(statement.actions) == toset(["s3:PutObject"]) &&
        toset(statement.resources) == toset(["${aws_s3_bucket.invoices_pdf.arn}/gst/*"])
      ]) == 1 &&
      alltrue([
        for statement in data.aws_iam_policy_document.lambda_worker_app["gst"].statement :
        !contains(statement.actions, "s3:GetObject") &&
        !contains(statement.actions, "s3:DeleteObject")
      ]) &&
      alltrue([
        for statement in data.aws_iam_policy_document.lambda_worker_app["bargaining"].statement :
        alltrue([for action in statement.actions : !startswith(action, "s3:")])
      ])
    )
    error_message = "Worker storage must be limited to the exact invoice, document, or GST prefixes used by that worker, with no bargaining S3 access."
  }

  assert {
    condition = alltrue([
      for worker, allowed in {
        invoice = {
          queues = [aws_sqs_queue.invoice_processing.arn]
          storage = [
            "${aws_s3_bucket.invoices_pdf.arn}/documents/*",
            "${aws_s3_bucket.invoices_pdf.arn}/invoices/*",
          ]
          secrets = [
            aws_db_instance.main.master_user_secret[0].secret_arn,
            aws_secretsmanager_secret.credential_encryption.arn,
          ]
        }
        gst = {
          queues  = [aws_sqs_queue.gst_processing.arn]
          storage = ["${aws_s3_bucket.invoices_pdf.arn}/gst/*"]
          secrets = [
            aws_db_instance.main.master_user_secret[0].secret_arn,
            aws_secretsmanager_secret.credential_encryption.arn,
            aws_secretsmanager_secret.gst_provider.arn,
          ]
        }
        bargaining = {
          queues  = [aws_sqs_queue.bargaining_negotiation.arn]
          storage = []
          secrets = [
            aws_db_instance.main.master_user_secret[0].secret_arn,
            aws_secretsmanager_secret.exa.arn,
            aws_secretsmanager_secret.llm.arn,
          ]
        }
        } : alltrue([
          for statement in data.aws_iam_policy_document.lambda_worker_app[worker].statement : (
            (
              alltrue([for action in statement.actions : !startswith(action, "sqs:")]) ||
              alltrue([for resource in statement.resources : contains(allowed.queues, resource)])
            ) &&
            (
              alltrue([for action in statement.actions : !startswith(action, "s3:")]) ||
              alltrue([for resource in statement.resources : contains(allowed.storage, resource)])
            ) &&
            (
              alltrue([for action in statement.actions : !startswith(action, "secretsmanager:")]) ||
              alltrue([for resource in statement.resources : contains(allowed.secrets, resource)])
            )
          )
      ])
    ])
    error_message = "Worker policies must not include sibling queues, any DLQ, out-of-profile storage, or out-of-profile secrets."
  }

  assert {
    condition = alltrue(flatten([
      for worker in ["invoice", "gst", "bargaining"] : [
        for statement in data.aws_iam_policy_document.lambda_worker_app[worker].statement : alltrue([
          for action in statement.actions : !contains([
            "kms:DescribeKey",
            "s3:DeleteObject",
            "secretsmanager:DescribeSecret",
            "sqs:ChangeMessageVisibility",
          ], action)
        ])
      ]
    ]))
    error_message = "Worker policies must omit unused queue, storage, secret metadata, and KMS metadata actions."
  }

  assert {
    condition = alltrue([
      for worker, secret_arns in {
        invoice = [
          aws_db_instance.main.master_user_secret[0].secret_arn,
          aws_secretsmanager_secret.credential_encryption.arn,
        ]
        gst = [
          aws_db_instance.main.master_user_secret[0].secret_arn,
          aws_secretsmanager_secret.credential_encryption.arn,
          aws_secretsmanager_secret.gst_provider.arn,
        ]
        bargaining = [
          aws_db_instance.main.master_user_secret[0].secret_arn,
          aws_secretsmanager_secret.exa.arn,
          aws_secretsmanager_secret.llm.arn,
        ]
        } : length([
          for statement in data.aws_iam_policy_document.lambda_worker_app[worker].statement : statement
          if statement.sid == "WorkerSecrets" &&
          toset(statement.actions) == toset(["secretsmanager:GetSecretValue"]) &&
          toset(statement.resources) == toset(secret_arns) &&
          length([
            for condition in statement.condition : condition
            if condition.variable == "aws:SecureTransport" &&
            condition.test == "Bool" &&
            toset(condition.values) == toset(["true"])
          ]) == 1
      ]) == 1
    ])
    error_message = "Worker roles must read only their exact database and provider secret sets."
  }

  assert {
    condition = alltrue(concat(
      [
        length([
          for statement in data.aws_iam_policy_document.lambda_websocket_app.statement : statement
          if statement.sid == "WebSocketSecretsKMS" &&
          length([
            for condition in statement.condition : condition
            if condition.variable == "kms:ViaService" &&
            condition.test == "StringEquals" &&
            contains(condition.values, "secretsmanager.ap-south-1.amazonaws.com")
          ]) == 1
        ]) == 1
      ],
      [
        for worker, secret_arns in {
          invoice = [
            aws_db_instance.main.master_user_secret[0].secret_arn,
            aws_secretsmanager_secret.credential_encryption.arn,
          ]
          gst = [
            aws_db_instance.main.master_user_secret[0].secret_arn,
            aws_secretsmanager_secret.credential_encryption.arn,
            aws_secretsmanager_secret.gst_provider.arn,
          ]
          bargaining = [
            aws_db_instance.main.master_user_secret[0].secret_arn,
            aws_secretsmanager_secret.exa.arn,
            aws_secretsmanager_secret.llm.arn,
          ]
        } :
        length([
          for statement in data.aws_iam_policy_document.lambda_worker_app[worker].statement : statement
          if statement.sid == "WorkerSecretsKMS" &&
          toset(statement.actions) == toset(["kms:Decrypt"]) &&
          toset(statement.resources) == toset([aws_kms_key.application_secrets.arn]) &&
          length([
            for condition in statement.condition : condition
            if condition.variable == "kms:ViaService" &&
            condition.test == "StringEquals" &&
            toset(condition.values) == toset(["secretsmanager.ap-south-1.amazonaws.com"])
          ]) == 1 &&
          length([
            for condition in statement.condition : condition
            if condition.variable == "kms:EncryptionContext:SecretARN" &&
            condition.test == "StringEquals" &&
            toset(condition.values) == toset(secret_arns)
          ]) == 1
        ]) == 1
      ]
    ))
    error_message = "Every runtime KMS decrypt statement must be constrained through regional Secrets Manager."
  }

  assert {
    condition = (
      length([
        for statement in data.aws_iam_policy_document.application_secrets_kms.statement : statement
        if statement.sid == "AccountAdministration" &&
        !contains(statement.actions, "kms:*") &&
        !contains(statement.actions, "kms:Decrypt") &&
        !contains(statement.actions, "kms:Encrypt")
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.application_secrets_kms.statement : statement
        if statement.sid == "SecretsManagerServiceUse" &&
        contains(statement.actions, "kms:Decrypt") &&
        length([
          for condition in statement.condition : condition
          if condition.variable == "kms:ViaService" &&
          contains(condition.values, "secretsmanager.ap-south-1.amazonaws.com")
        ]) == 1
      ]) == 1
    )
    error_message = "KMS administration and runtime cryptographic use must be separated."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.application_secrets_kms.statement : statement
      if statement.sid == "RDSManagedMasterSecretUse" &&
      toset(statement.actions) == toset([
        "kms:CreateGrant",
        "kms:Decrypt",
        "kms:DescribeKey",
        "kms:GenerateDataKey"
      ]) &&
      length(statement.principals) == 1 &&
      length([
        for principal in statement.principals : principal
        if principal.type == "AWS" &&
        toset(principal.identifiers) == toset([
          "arn:aws:iam::928282274753:root"
        ])
      ]) == 1 &&
      length([
        for condition in statement.condition : condition
        if condition.test == "StringEquals" &&
        condition.variable == "kms:CallerAccount" &&
        toset(condition.values) == toset(["928282274753"])
      ]) == 1 &&
      length([
        for condition in statement.condition : condition
        if condition.test == "StringEquals" &&
        condition.variable == "kms:ViaService" &&
        toset(condition.values) == toset(["rds.ap-south-1.amazonaws.com"])
      ]) == 1 &&
      length([
        for condition in statement.condition : condition
        if condition.test == "Bool" &&
        condition.variable == "aws:SecureTransport" &&
        toset(condition.values) == toset(["true"])
      ]) == 1
    ]) == 1
    error_message = "The account caller must have only the exact KMS permissions needed to create an RDS-managed master secret, restricted to same-account RDS service use."
  }

  assert {
    condition = length([
      for statement in data.aws_iam_policy_document.lambda_app.statement : statement
      if statement.sid == "SSMAccess" &&
      length(statement.actions) == 1 &&
      contains(statement.actions, "ssm:GetParameters") &&
      length(statement.resources) == 1 &&
      contains(statement.resources, local.db_host_ssm_parameter_arn)
    ]) == 1
    error_message = "Lambda IAM must batch-read only exact non-secret SSM parameter ARNs."
  }

  assert {
    condition     = endswith(output.razorpay_webhook_url, "/api/v1/webhooks/razorpay")
    error_message = "Razorpay webhook output must expose the fresh webhook endpoint."
  }
}

run "billeif_branding_defaults_and_public_url_inputs" {
  command = plan

  variables {
    environment                      = "preview"
    lambda_artifact_dir              = "tests/fixtures/lambda"
    llm_api_url                      = "https://llm.example.test/chat/completions"
    llm_model                        = "test-model"
    deepseek_base_url                = "https://voice-llm.example.test/v1"
    deepseek_model                   = "voice-test-model"
    db_allowed_cidr                  = "10.0.0.0/24"
    ses_verified_identity            = "billeif.example"
    ses_sender_email                 = "notifications@billeif.example"
    cognito_additional_callback_urls = ["https://customer.example/callback"]
    cognito_additional_logout_urls   = ["https://customer.example/logout"]
  }

  assert {
    condition     = var.project_name == "billeif"
    error_message = "Billeif must be the lowercase Terraform project default."
  }

  assert {
    condition = (
      local.cognito_hosted_ui_domain_prefix == "billeif-preview-928282274753" &&
      can(regex("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$", local.cognito_hosted_ui_domain_prefix)) &&
      length(local.cognito_hosted_ui_domain_prefix) <= 63
    )
    error_message = "The Cognito hosted UI prefix must be a collision-resistant Billeif-branded value within Cognito limits."
  }

  assert {
    condition = (
      aws_cognito_user_pool.main.name == "billeif-preview-web-user-pool" &&
      aws_cognito_user_pool_client.main.name == "billeif-preview-web-client" &&
      aws_cognito_user_pool.phone.name == "billeif-preview-native-user-pool" &&
      aws_cognito_user_pool_client.phone.name == "billeif-preview-native-client" &&
      aws_cognito_resource_server.main.identifier == "billeif-preview-api" &&
      aws_cognito_resource_server.main.name == "billeif-preview-resource-server"
    )
    error_message = "Cognito web, native, and resource-server names must use Billeif branding."
  }

  assert {
    condition = (
      aws_dynamodb_table.users_sessions.name == "billeif-preview-users-sessions" &&
      aws_sns_topic.low_stock_alerts.name == "billeif-preview-low-stock-alerts" &&
      aws_sqs_queue.invoice_processing.name == "billeif-preview-invoice-processing-queue" &&
      aws_iam_role.lambda_exec.name == "billeif-preview-lambda-exec-role" &&
      aws_lambda_function.api_http.function_name == "billeif-preview-api-http" &&
      aws_cloudwatch_log_group.lambda_api_http.name == "/aws/lambda/billeif-preview-api-http" &&
      aws_db_instance.main.identifier == "billeif-preview-postgres" &&
      aws_s3_bucket.business_logos.bucket == "billeif-preview-928282274753-business-logos" &&
      aws_apigatewayv2_api.websocket.name == "billeif-preview-websocket" &&
      aws_dynamodb_table.voice_sessions.name == "billeif-preview-voice-sessions" &&
      aws_ses_configuration_set.main.name == "billeif-preview-ses-config" &&
      aws_iam_role.nat_instance[0].name == "billeif-preview-nat-instance-role" &&
      aws_iam_instance_profile.nat_instance[0].name == "billeif-preview-nat-instance-profile" &&
      aws_security_group.nat_instance[0].name == "billeif-preview-nat-instance-sg" &&
      aws_instance.nat[0].tags.Name == "billeif-preview-nat-instance" &&
      aws_vpc_endpoint.s3.tags.Name == "billeif-preview-s3-gateway-endpoint" &&
      aws_vpc_endpoint.dynamodb.tags.Name == "billeif-preview-dynamodb-gateway-endpoint" &&
      aws_cloudwatch_metric_alarm.nat_system_status[0].alarm_name == "billeif-preview-nat-system-status" &&
      local.ses_verified_identity_arn == "arn:aws:ses:ap-south-1:928282274753:identity/billeif.example"
    )
    error_message = "Representative AWS resources and the SES contract must use Billeif project/environment naming."
  }

  assert {
    condition = (
      aws_secretsmanager_secret.billeif_invoice_cursor_hmac.name == "/billeif/preview/application/billeif-invoice-cursor-hmac" &&
      aws_secretsmanager_secret.billeif_invoice_cursor_hmac.kms_key_id == aws_kms_key.application_secrets.arn &&
      aws_secretsmanager_secret.billeif_invoice_cursor_hmac.recovery_window_in_days == 7
    )
    error_message = "The Billeif invoice cursor secret must be metadata-only under the application KMS key and recovery convention."
  }

  assert {
    condition = (
      aws_lambda_function.api_http.role == aws_iam_role.lambda_http_exec.arn &&
      aws_lambda_function.a2a_stream.role == aws_iam_role.lambda_exec.arn &&
      aws_lambda_function.api_http.environment[0].variables["INVOICE_CURSOR_HMAC_SECRET_ARN"] == aws_secretsmanager_secret.billeif_invoice_cursor_hmac.arn &&
      !contains(keys(aws_lambda_function.a2a_stream.environment[0].variables), "INVOICE_CURSOR_HMAC_SECRET_ARN") &&
      !contains(keys(aws_lambda_function.sqs_invoice.environment[0].variables), "INVOICE_CURSOR_HMAC_SECRET_ARN") &&
      !contains(keys(aws_lambda_function.sqs_gst.environment[0].variables), "INVOICE_CURSOR_HMAC_SECRET_ARN") &&
      !contains(keys(aws_lambda_function.sqs_bargaining.environment[0].variables), "INVOICE_CURSOR_HMAC_SECRET_ARN") &&
      !contains(keys(aws_lambda_function.sqs_email_delivery.environment[0].variables), "INVOICE_CURSOR_HMAC_SECRET_ARN") &&
      !contains(keys(aws_lambda_function.sqs_ses_feedback.environment[0].variables), "INVOICE_CURSOR_HMAC_SECRET_ARN") &&
      !contains(keys(aws_lambda_function.outbox_dispatcher.environment[0].variables), "INVOICE_CURSOR_HMAC_SECRET_ARN") &&
      !contains(keys(aws_lambda_function.ws_handler.environment[0].variables), "INVOICE_CURSOR_HMAC_SECRET_ARN")
    )
    error_message = "Only the dedicated Billeif HTTP runtime may receive the invoice cursor secret ARN."
  }

  assert {
    condition = (
      length([
        for statement in data.aws_iam_policy_document.invoice_cursor_http.statement : statement
        if statement.sid == "InvoiceCursorSecret" &&
        length(statement.actions) == 1 &&
        contains(statement.actions, "secretsmanager:GetSecretValue") &&
        length(statement.resources) == 1 &&
        contains(statement.resources, aws_secretsmanager_secret.billeif_invoice_cursor_hmac.arn)
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.invoice_cursor_http.statement : statement
        if statement.sid == "InvoiceCursorKMSDecrypt" &&
        length(statement.actions) == 1 &&
        contains(statement.actions, "kms:Decrypt") &&
        length(statement.resources) == 1 &&
        contains(statement.resources, aws_kms_key.application_secrets.arn) &&
        length([
          for condition in statement.condition : condition
          if condition.variable == "kms:ViaService" &&
          contains(condition.values, "secretsmanager.ap-south-1.amazonaws.com")
        ]) == 1 &&
        length([
          for condition in statement.condition : condition
          if condition.variable == "kms:CallerAccount" &&
          contains(condition.values, data.aws_caller_identity.current.account_id)
        ]) == 1 &&
        length([
          for condition in statement.condition : condition
          if condition.variable == "kms:EncryptionContext:SecretARN" &&
          length(condition.values) == 1 &&
          contains(condition.values, aws_secretsmanager_secret.billeif_invoice_cursor_hmac.arn)
        ]) == 1
      ]) == 1
    )
    error_message = "The dedicated HTTP cursor policy must scope Secrets Manager and KMS access to the Billeif cursor secret."
  }

  assert {
    condition = (
      contains(aws_cognito_user_pool_client.main.callback_urls, "https://customer.example/callback") &&
      contains(aws_cognito_user_pool_client.main.logout_urls, "https://customer.example/logout")
    )
    error_message = "User-owned Cognito callback and logout URLs must remain unchanged."
  }
}

run "billeif_mobile_redirects_are_enabled_by_default" {
  command = plan

  variables {
    environment           = "preview"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"
  }

  assert {
    condition = (
      contains(aws_cognito_user_pool_client.main.callback_urls, "billeif://callback") &&
      contains(aws_cognito_user_pool_client.main.logout_urls, "billeif://logout")
    )
    error_message = "The Cognito app client must allow the Billeif native callback and logout URLs by default."
  }
}

run "billeif_cognito_domain_override_is_constrained" {
  command = plan

  variables {
    environment           = "preview"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"
    cognito_domain_prefix = "billeif-preview-ui"
  }

  assert {
    condition     = aws_cognito_user_pool_domain.main.domain == "billeif-preview-ui"
    error_message = "A valid Billeif-branded Cognito hosted UI prefix must remain overridable."
  }
}

run "cognito_domain_rejects_reserved_or_unbranded_prefixes" {
  command = plan

  variables {
    environment           = "preview"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"
    cognito_domain_prefix = "aws-preview-ui"
  }

  expect_failures = [var.cognito_domain_prefix]
}

run "billeif_resource_prefix_accepts_iam_role_boundary" {
  command = plan

  variables {
    project_name          = "billeif-project"
    environment           = "integration-x"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"
  }

  assert {
    condition = (
      length(local.resource_prefix) == 29 &&
      length(aws_iam_role.lambda_worker_exec["bargaining"].name) == 64 &&
      length(aws_lambda_function.custom_sms_sender.function_name) <= 64 &&
      length(aws_s3_bucket.lambda_artifacts.bucket) <= 63
    )
    error_message = "The shared Billeif prefix must fit IAM, Lambda, and S3 generated-name limits at the boundary."
  }
}

run "resource_prefix_rejects_iam_role_overflow" {
  command = plan

  variables {
    project_name          = "billeif-project"
    environment           = "integration-xx"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"
  }

  expect_failures = [var.environment]
}

run "project_name_rejects_surrounding_whitespace" {
  command = plan

  variables {
    project_name          = "billeif "
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"
  }

  expect_failures = [var.project_name]
}

run "cognito_name_override_accepts_128_characters" {
  command = plan

  variables {
    client_name           = format("billeif-%s", join("", [for index in range(120) : "a"]))
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"
  }

  assert {
    condition     = length(aws_cognito_user_pool_client.main.name) == 128
    error_message = "A valid 128-character Billeif Cognito client override must be accepted."
  }
}

run "cognito_name_override_rejects_invalid_characters" {
  command = plan

  variables {
    client_name           = "billeif/invalid"
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@billeif.example"
  }

  expect_failures = [var.client_name]
}

run "ses_email_identity_uses_caller_supplied_arn" {
  command = plan

  variables {
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billing@billeif.example"
    ses_sender_email      = "billing@billeif.example"
  }

  assert {
    condition     = local.ses_verified_identity_arn == "arn:aws:ses:ap-south-1:928282274753:identity/billing@billeif.example"
    error_message = "An already-verified SES email identity must be referenced directly in IAM."
  }
}

run "ses_sender_must_belong_to_verified_domain_identity" {
  command = plan

  variables {
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@other.example"
  }

  expect_failures = [aws_ses_configuration_set.main]
}

run "ses_email_identity_allows_case_insensitive_domain" {
  command = plan

  variables {
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "Billing@Billeif.example"
    ses_sender_email      = "Billing@billeif.example"
  }

  assert {
    condition     = local.ses_verified_identity_arn == "arn:aws:ses:ap-south-1:928282274753:identity/Billing@Billeif.example"
    error_message = "An SES email identity ARN must retain the validated caller-supplied identity exactly."
  }
}

run "ses_email_identity_rejects_different_local_part_case" {
  command = plan

  variables {
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "Billing@billeif.example"
    ses_sender_email      = "billing@billeif.example"
  }

  expect_failures = [aws_ses_configuration_set.main]
}

run "ses_domain_identity_accepts_true_subdomain_sender" {
  command = plan

  variables {
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@mail.billeif.example"
  }
}

run "ses_domain_identity_rejects_suffix_spoof" {
  command = plan

  variables {
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif.example"
    ses_sender_email      = "notifications@notbilleif.example"
  }

  expect_failures = [aws_ses_configuration_set.main]
}

run "ses_identity_rejects_iam_wildcard" {
  command = plan

  variables {
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "*.billeif.example"
    ses_sender_email      = "notifications@billeif.example"
  }

  expect_failures = [var.ses_verified_identity]
}

run "ses_identity_rejects_malformed_domain" {
  command = plan

  variables {
    lambda_artifact_dir   = "tests/fixtures/lambda"
    llm_api_url           = "https://llm.example.test/chat/completions"
    llm_model             = "test-model"
    deepseek_base_url     = "https://voice-llm.example.test/v1"
    deepseek_model        = "voice-test-model"
    db_allowed_cidr       = "10.0.0.0/24"
    ses_verified_identity = "billeif..example"
    ses_sender_email      = "notifications@billeif.example"
  }

  expect_failures = [var.ses_verified_identity]
}
