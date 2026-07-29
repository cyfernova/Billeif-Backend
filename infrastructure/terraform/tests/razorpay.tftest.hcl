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

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "123456789012"
      arn        = "arn:aws:iam::123456789012:user/terraform-test"
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
      execution_arn    = "arn:aws:execute-api:ap-south-1:123456789012:test-rest-api"
    }
  }

  override_resource {
    target          = aws_kms_key.application_secrets
    override_during = plan
    values = {
      arn    = "arn:aws:kms:ap-south-1:123456789012:key/application-secrets"
      key_id = "application-secrets"
    }
  }

  override_resource {
    target          = aws_db_instance.main
    override_during = plan
    values = {
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:123456789012:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:rds-managed"
      }]
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.credential_encryption
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:credential-encryption"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.razorpay
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:razorpay"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.llm
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:llm"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.exa
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:exa"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.gst_lookup
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:gst-lookup"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.gst_provider
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:gst-provider"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.deepgram
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:deepgram"
    }
  }

  override_resource {
    target          = aws_secretsmanager_secret.deepseek
    override_during = plan
    values = {
      arn = "arn:aws:secretsmanager:ap-south-1:123456789012:secret:deepseek"
    }
  }
}

run "mumbai_defaults_and_oidc_profile" {
  command = plan

  variables {
    project_name        = "invoice-backend-test"
    environment         = "test"
    lambda_artifact_dir = "tests/fixtures/lambda"
    llm_api_url         = "https://llm.example.test/chat/completions"
    llm_model           = "test-model"
    deepseek_base_url   = "https://voice-llm.example.test/v1"
    deepseek_model      = "voice-test-model"

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
}

run "aws_profile_accepts_null_for_oidc" {
  command = plan

  variables {
    project_name        = "invoice-backend-test"
    environment         = "test"
    lambda_artifact_dir = "tests/fixtures/lambda"
    llm_api_url         = "https://llm.example.test/chat/completions"
    llm_model           = "test-model"
    deepseek_base_url   = "https://voice-llm.example.test/v1"
    deepseek_model      = "voice-test-model"

    aws_profile     = null
    db_allowed_cidr = "10.0.0.0/24"
  }

  assert {
    condition     = var.aws_profile == null
    error_message = "The AWS profile must accept null so CI can use OIDC credentials."
  }
}

run "secret_metadata_rds_lambda_iam_and_output" {
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
        aws_secretsmanager_secret.razorpay.name,
        aws_secretsmanager_secret.legacy_jwt.name,
        aws_secretsmanager_secret.google_oauth.name,
        aws_secretsmanager_secret.fcm.name,
        aws_secretsmanager_secret.apns.name,
        aws_secretsmanager_secret.llm.name,
        aws_secretsmanager_secret.exa.name,
        aws_secretsmanager_secret.gst_lookup.name,
        aws_secretsmanager_secret.gst_provider.name,
        aws_secretsmanager_secret.deepgram.name,
        aws_secretsmanager_secret.deepseek.name
      ])) == 12 &&
      alltrue([
        aws_secretsmanager_secret.credential_encryption.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.razorpay.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.legacy_jwt.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.google_oauth.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.fcm.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.apns.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.llm.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.exa.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.gst_lookup.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.gst_provider.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.deepgram.kms_key_id == aws_kms_key.application_secrets.arn,
        aws_secretsmanager_secret.deepseek.kms_key_id == aws_kms_key.application_secrets.arn
      ])
    )
    error_message = "Unrelated application/provider credentials must use separated secret containers."
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
        "LLM_SECRET_ARN",
        "RAZORPAY_SECRET_ARN",
        "DEEPGRAM_SECRET_ARN",
        "DEEPSEEK_SECRET_ARN"
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
        contains(condition.values, "secretsmanager.us-east-1.amazonaws.com")
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
    condition = alltrue([
      toset([
        for key in keys(aws_lambda_function.sqs_invoice.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["CREDENTIAL_ENCRYPTION_SECRET_ARN", "DATABASE_SECRET_ARN"]),
      length([
        for key in keys(aws_lambda_function.sqs_payment.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == 0,
      toset([
        for key in keys(aws_lambda_function.sqs_gst.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["CREDENTIAL_ENCRYPTION_SECRET_ARN", "DATABASE_SECRET_ARN", "GST_PROVIDER_SECRET_ARN"]),
      toset([
        for key in keys(aws_lambda_function.sqs_bargaining.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["CREDENTIAL_ENCRYPTION_SECRET_ARN", "DATABASE_SECRET_ARN", "EXA_SECRET_ARN", "LLM_SECRET_ARN"]),
      toset([
        for key in keys(aws_lambda_function.ws_handler.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["DATABASE_SECRET_ARN"]),
      toset([
        for key in keys(aws_lambda_function.voice_session.environment[0].variables) : key
        if endswith(key, "_SECRET_ARN")
      ]) == toset(["DEEPGRAM_SECRET_ARN", "DEEPSEEK_SECRET_ARN"])
    ])
    error_message = "Every Lambda environment must receive only the secret identifiers used by its entrypoint."
  }

  assert {
    condition = alltrue([
      length([
        for statement in data.aws_iam_policy_document.lambda_worker_app["payment"].statement : statement
        if contains(["WorkerSecrets", "WorkerSecretsKMS", "WorkerParameters"], statement.sid)
      ]) == 0,
      length([
        for statement in data.aws_iam_policy_document.lambda_worker_app["invoice"].statement : statement
        if statement.sid == "WorkerSecrets" && length(statement.resources) == 2
      ]) == 1,
      length([
        for statement in data.aws_iam_policy_document.lambda_worker_app["gst"].statement : statement
        if statement.sid == "WorkerSecrets" &&
        length(statement.resources) == 3 &&
        contains(statement.resources, aws_secretsmanager_secret.gst_provider.arn)
      ]) == 1,
      length([
        for statement in data.aws_iam_policy_document.lambda_worker_app["bargaining"].statement : statement
        if statement.sid == "WorkerSecrets" && length(statement.resources) == 4
      ]) == 1
    ])
    error_message = "Worker roles must have entrypoint-specific secret access."
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
            contains(condition.values, "secretsmanager.us-east-1.amazonaws.com")
          ]) == 1
        ]) == 1,
        length([
          for statement in data.aws_iam_policy_document.lambda_voice_app.statement : statement
          if statement.sid == "VoiceProviderSecretsKMS" &&
          length([
            for condition in statement.condition : condition
            if condition.variable == "kms:ViaService" &&
            condition.test == "StringEquals" &&
            contains(condition.values, "secretsmanager.us-east-1.amazonaws.com")
          ]) == 1
        ]) == 1
      ],
      [
        for worker in ["invoice", "gst", "bargaining"] :
        length([
          for statement in data.aws_iam_policy_document.lambda_worker_app[worker].statement : statement
          if statement.sid == "WorkerSecretsKMS" &&
          length([
            for condition in statement.condition : condition
            if condition.variable == "kms:ViaService" &&
            condition.test == "StringEquals" &&
            contains(condition.values, "secretsmanager.us-east-1.amazonaws.com")
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
          contains(condition.values, "secretsmanager.us-east-1.amazonaws.com")
        ]) == 1
      ]) == 1
    )
    error_message = "KMS administration and runtime cryptographic use must be separated."
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
