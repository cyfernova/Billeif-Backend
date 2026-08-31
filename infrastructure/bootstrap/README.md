# Production deployment bootstrap

`github-actions-deploy-role.yaml` is the one-time security boundary for the
GitHub-to-AWS deployment path. It is intentionally separate from the application
Terraform state: the production deployment role must never be able to edit its
own trust policy, inline policy, or workload permissions boundary.

The stack creates:

- `billeif-dev-github-actions-production-role`, assumed only by the repository's
  protected `production` GitHub environment on `main`;
- `billeif-dev-workload-boundary`, which prevents application workload roles
  from administering IAM, AWS Organizations, IAM Identity Center, or assuming
  other roles;
- an application deployment policy that excludes direct application-data access
  and all workload-role, trust-policy, permissions-boundary, and instance-profile
  mutation;
- exact role-to-service `iam:PassRole` grants for the pre-provisioned workload
  roles used by Lambda, EC2, AgentCore, Scheduler, API Gateway, Cognito, and RDS.

Create or update this stack only from an independently authenticated AWS
administrator session. Routine GitHub workflows must never receive permission
to update this bootstrap stack. Review a CloudFormation change set before every
bootstrap update.

After creation, store the `DeploymentRoleArn` output as the
`AWS_DEPLOY_ROLE_ARN` secret in the protected GitHub `production` environment.
The repository-level role secret should then be removed so unprotected jobs
cannot receive it.

Supply the existing Terraform state bucket and lock-table names through the
`TerraformStateBucketName` and `TerraformStateLockTableName` stack parameters.
The deployment role can access only that state, the versioned Lambda artifacts
under the application artifact bucket, and the database migrator invocation.

Workload IAM and resource-based access policies remain declared in Terraform,
but GitHub Actions cannot reconcile IAM, S3 bucket, KMS key, Secrets Manager,
SNS topic, or SQS queue policy changes. For a deployment containing an
intentional identity or resource-policy change, an independently authenticated
administrator must first review the complete plan and apply that change. Run the
normal production workflow only after the administrator-owned policy state is
current. Unexpected policy drift or a malicious policy change therefore fails
closed in the GitHub workflow.
