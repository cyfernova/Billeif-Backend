locals {
  github_actions_oidc_url            = "https://token.actions.githubusercontent.com"
  github_actions_main_branch_sub     = "repo:cyfernova/Billeif-Backend:ref:refs/heads/main"
  billeif_role_arn_pattern           = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:role/billeif-*"
  github_actions_deployment_role_arn = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:role/${local.resource_prefix}-github-actions-deploy-role"
}

resource "aws_iam_openid_connect_provider" "github_actions" {
  url            = local.github_actions_oidc_url
  client_id_list = ["sts.amazonaws.com"]

  lifecycle {
    precondition {
      condition     = data.aws_caller_identity.current.account_id == "928282274753"
      error_message = "GitHub Actions OIDC must be applied only in AWS account 928282274753."
    }

    precondition {
      condition     = var.aws_region == "ap-south-1"
      error_message = "GitHub Actions OIDC must be applied only with aws_region set to ap-south-1."
    }
  }
}

data "aws_iam_policy_document" "github_actions_deployment_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github_actions.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = [local.github_actions_main_branch_sub]
    }
  }
}

resource "aws_iam_role" "github_actions_deployment" {
  name               = "${local.resource_prefix}-github-actions-deploy-role"
  assume_role_policy = data.aws_iam_policy_document.github_actions_deployment_assume_role.json
}

resource "aws_iam_role_policy_attachment" "github_actions_deployment_power_user" {
  role       = aws_iam_role.github_actions_deployment.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/PowerUserAccess"
}

# IAM and OIDC bootstrap changes require a local default-profile apply; GitHub
# Actions uses this role only for routine application and infrastructure deployment.
data "aws_iam_policy_document" "github_actions_deployment_iam" {
  statement {
    sid    = "ReadBilleifRoles"
    effect = "Allow"
    actions = [
      "iam:GetRole",
      "iam:GetRolePolicy",
      "iam:ListInstanceProfilesForRole",
      "iam:ListRolePolicies",
      "iam:ListAttachedRolePolicies",
      "iam:ListRoleTags",
    ]
    resources = [local.billeif_role_arn_pattern]
  }

  statement {
    sid       = "PassBilleifRolesToApprovedServices"
    effect    = "Allow"
    actions   = ["iam:PassRole"]
    resources = [local.billeif_role_arn_pattern]

    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values = [
        "apigateway.amazonaws.com",
        "cognito-idp.amazonaws.com",
        "ec2.amazonaws.com",
        "lambda.amazonaws.com",
        "rds.amazonaws.com",
        "scheduler.amazonaws.com",
      ]
    }
  }

  statement {
    sid    = "ReadBilleifInstanceProfiles"
    effect = "Allow"
    actions = [
      "iam:GetInstanceProfile",
      "iam:ListInstanceProfileTags",
    ]
    resources = ["arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:instance-profile/billeif-*"]
  }

  statement {
    sid    = "ReadGitHubOIDCProvider"
    effect = "Allow"
    actions = [
      "iam:GetOpenIDConnectProvider",
      "iam:ListOpenIDConnectProviderTags",
    ]
    resources = [aws_iam_openid_connect_provider.github_actions.arn]
  }

  statement {
    sid       = "ListOIDCProviders"
    effect    = "Allow"
    actions   = ["iam:ListOpenIDConnectProviders"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "github_actions_deployment_iam" {
  name   = "${local.resource_prefix}-github-actions-deploy-iam-policy"
  role   = aws_iam_role.github_actions_deployment.id
  policy = data.aws_iam_policy_document.github_actions_deployment_iam.json
}
