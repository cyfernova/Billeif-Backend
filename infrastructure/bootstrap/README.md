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
- a scoped IAM policy that can manage only `billeif-dev-*` workload roles and
  instance profiles, requires the boundary when roles are created, denies
  boundary removal, and explicitly denies mutation of the deployment role.

Create or update this stack only from an independently authenticated AWS
administrator session. Routine GitHub workflows must never receive permission
to update this bootstrap stack. Review a CloudFormation change set before every
bootstrap update.

After creation, store the `DeploymentRoleArn` output as the
`AWS_DEPLOY_ROLE_ARN` secret in the protected GitHub `production` environment.
The repository-level role secret should then be removed so unprotected jobs
cannot receive it.
