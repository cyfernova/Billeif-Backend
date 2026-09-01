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
      result = "{\"status\":\"applied\",\"version\":55,\"latest_version\":55,\"dirty\":false,\"manifest_checksum\":\"372cd4f33e86f10b8f3d5771f22d661b04b111b1a440246291aacca1a10588a0\"}"
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
    target          = aws_iam_openid_connect_provider.github_actions
    override_during = plan
    values = {
      arn = "arn:aws:iam::928282274753:oidc-provider/token.actions.githubusercontent.com"
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

run "github_actions_oidc_provider_retains_only_an_inert_legacy_role" {
  command = plan

  assert {
    condition = (
      aws_iam_openid_connect_provider.github_actions.url == "https://token.actions.githubusercontent.com" &&
      toset(aws_iam_openid_connect_provider.github_actions.client_id_list) == toset(["sts.amazonaws.com"]) &&
      output.github_actions_deployment_role_arn == "arn:aws:iam::928282274753:role/billeif-dev-github-actions-production-role"
    )
    error_message = "The application state must retain the GitHub OIDC provider while exporting the separately bootstrapped production role."
  }

  assert {
    condition = (
      aws_iam_role.github_actions_deployment.name == "billeif-dev-github-actions-deploy-role" &&
      aws_iam_role.github_actions_deployment.permissions_boundary == "arn:aws:iam::928282274753:policy/billeif-dev-workload-boundary" &&
      length([
        for statement in data.aws_iam_policy_document.github_actions_deployment_assume_role.statement : statement
        if statement.sid == "RetiredMainBranchRole" &&
        statement.effect == "Deny" &&
        toset(statement.actions) == toset(["sts:AssumeRoleWithWebIdentity"]) &&
        length([for principal in statement.principals : principal if principal.type == "Federated" && toset(principal.identifiers) == toset(["arn:aws:iam::928282274753:oidc-provider/token.actions.githubusercontent.com"])]) == 1
      ]) == 1
    )
    error_message = "The legacy main-branch role must be inert and capped by the workload permissions boundary."
  }
}

run "github_actions_oidc_rejects_wrong_account" {
  command = plan

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "111111111111"
      arn        = "arn:aws:iam::111111111111:user/terraform-test"
      user_id    = "AIDATEST1111111111"
    }
  }

  expect_failures = [aws_iam_openid_connect_provider.github_actions]
}

run "github_actions_oidc_rejects_wrong_region" {
  command = plan

  variables {
    aws_region = "us-east-1"
  }

  expect_failures = [aws_iam_openid_connect_provider.github_actions]
}
