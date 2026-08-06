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
      result = "{\"status\":\"applied\",\"version\":46,\"latest_version\":46,\"dirty\":false,\"manifest_checksum\":\"c8ee4f07d7b006e5fed9890e052d1f567a11a65c1c9460c31387d343ed4a1602\"}"
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

run "github_actions_oidc_deployment_role_is_main_branch_scoped" {
  command = plan

  assert {
    condition = (
      aws_iam_openid_connect_provider.github_actions.url == "https://token.actions.githubusercontent.com" &&
      toset(aws_iam_openid_connect_provider.github_actions.client_id_list) == toset(["sts.amazonaws.com"]) &&
      aws_iam_role.github_actions_deployment.name == "billeif-dev-github-actions-deploy-role" &&
      output.github_actions_deployment_role_arn == "arn:aws:iam::928282274753:role/billeif-dev-github-actions-deploy-role"
    )
    error_message = "GitHub Actions must receive the expected Billeif deployment role through the GitHub OIDC issuer."
  }

  assert {
    condition = (
      length([
        for statement in data.aws_iam_policy_document.github_actions_deployment_assume_role.statement : statement
        if statement.effect == "Allow" &&
        toset(statement.actions) == toset(["sts:AssumeRoleWithWebIdentity"]) &&
        length(statement.principals) == 1 &&
        length([for principal in statement.principals : principal if principal.type == "Federated" && toset(principal.identifiers) == toset(["arn:aws:iam::928282274753:oidc-provider/token.actions.githubusercontent.com"])]) == 1 &&
        length([for condition in statement.condition : condition if condition.test == "StringEquals" && condition.variable == "token.actions.githubusercontent.com:aud" && toset(condition.values) == toset(["sts.amazonaws.com"])]) == 1 &&
        length([for condition in statement.condition : condition if condition.test == "StringEquals" && condition.variable == "token.actions.githubusercontent.com:sub" && toset(condition.values) == toset(["repo:cyfernova/Billeif-Backend:ref:refs/heads/main"])]) == 1
      ]) == 1
    )
    error_message = "Only the exact cyfernova/Billeif-Backend main branch subject may assume the deployment role."
  }

  assert {
    condition = (
      aws_iam_role_policy_attachment.github_actions_deployment_power_user.policy_arn == "arn:aws:iam::aws:policy/PowerUserAccess" &&
      !strcontains(aws_iam_role_policy_attachment.github_actions_deployment_power_user.policy_arn, "AdministratorAccess") &&
      length([
        for statement in data.aws_iam_policy_document.github_actions_deployment_iam.statement : statement
        if statement.sid == "ReadBilleifRoles" &&
        toset(statement.resources) == toset(["arn:aws:iam::928282274753:role/billeif-*"]) &&
        contains(statement.actions, "iam:GetRole") &&
        contains(statement.actions, "iam:GetRolePolicy") &&
        contains(statement.actions, "iam:ListAttachedRolePolicies") &&
        contains(statement.actions, "iam:ListRolePolicies") &&
        contains(statement.actions, "iam:ListRoleTags") &&
        contains(statement.actions, "iam:ListInstanceProfilesForRole")
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.github_actions_deployment_iam.statement : statement
        if statement.sid == "PassBilleifRolesToApprovedServices" &&
        toset(statement.actions) == toset(["iam:PassRole"]) &&
        toset(statement.resources) == toset(["arn:aws:iam::928282274753:role/billeif-*"]) &&
        length([
          for condition in statement.condition : condition
          if condition.test == "StringEquals" &&
          condition.variable == "iam:PassedToService" &&
          contains(condition.values, "bedrock-agentcore.amazonaws.com")
        ]) == 1
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.github_actions_deployment_iam.statement : statement
        if statement.sid == "ReadBilleifInstanceProfiles" &&
        contains(statement.actions, "iam:GetInstanceProfile") &&
        contains(statement.actions, "iam:ListInstanceProfileTags") &&
        toset(statement.resources) == toset(["arn:aws:iam::928282274753:instance-profile/billeif-*"])
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.github_actions_deployment_iam.statement : statement
        if statement.sid == "ReadGitHubOIDCProvider" &&
        contains(statement.actions, "iam:GetOpenIDConnectProvider") &&
        contains(statement.actions, "iam:ListOpenIDConnectProviderTags")
      ]) == 1 &&
      length([
        for statement in data.aws_iam_policy_document.github_actions_deployment_iam.statement : statement
        if statement.sid == "ListOIDCProviders" &&
        toset(statement.actions) == toset(["iam:ListOpenIDConnectProviders"]) &&
        toset(statement.resources) == toset(["*"])
      ]) == 1
    )
    error_message = "The deployment role must use PowerUserAccess with narrowly scoped IAM permissions for Billeif roles and the GitHub OIDC provider."
  }

  assert {
    condition = length(setintersection(
      toset(flatten([for statement in data.aws_iam_policy_document.github_actions_deployment_iam.statement : statement.actions])),
      toset([
        "iam:AddClientIDToOpenIDConnectProvider",
        "iam:AddRoleToInstanceProfile",
        "iam:AttachRolePolicy",
        "iam:CreateInstanceProfile",
        "iam:CreateOpenIDConnectProvider",
        "iam:CreateRole",
        "iam:DeleteInstanceProfile",
        "iam:DeleteOpenIDConnectProvider",
        "iam:DeleteRole",
        "iam:DeleteRolePolicy",
        "iam:DetachRolePolicy",
        "iam:PutRolePolicy",
        "iam:RemoveClientIDFromOpenIDConnectProvider",
        "iam:RemoveRoleFromInstanceProfile",
        "iam:TagInstanceProfile",
        "iam:TagOpenIDConnectProvider",
        "iam:TagRole",
        "iam:UntagInstanceProfile",
        "iam:UntagOpenIDConnectProvider",
        "iam:UntagRole",
        "iam:UpdateAssumeRolePolicy",
        "iam:UpdateOpenIDConnectProviderThumbprint",
        "iam:UpdateRole",
      ])
    )) == 0
    error_message = "Routine GitHub deployments must not receive IAM, OIDC, or instance-profile mutation permissions."
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
