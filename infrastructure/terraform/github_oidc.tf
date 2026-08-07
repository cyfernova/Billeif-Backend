locals {
  github_actions_oidc_url            = "https://token.actions.githubusercontent.com"
  github_actions_deployment_role_arn = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:role/${local.resource_prefix}-github-actions-production-role"
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
    sid     = "RetiredMainBranchRole"
    effect  = "Deny"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github_actions.arn]
    }
  }
}

# Retain the former role as an inert, boundary-constrained state anchor for one
# release. The separately bootstrapped production role is the only role used by
# GitHub Actions and is intentionally outside this Terraform state.
resource "aws_iam_role" "github_actions_deployment" {
  name                 = "${local.resource_prefix}-github-actions-deploy-role"
  assume_role_policy   = data.aws_iam_policy_document.github_actions_deployment_assume_role.json
  permissions_boundary = local.workload_permissions_boundary_arn
}
