package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProductionDeploymentUsesProtectedOIDCAndBoundedWorkloadRoles(t *testing.T) {
	workflow := readRepositoryFile(t, ".github", "workflows", "deploy.yml")
	for _, required := range []string{
		"environment: production",
		"role-session-name: github-${{ github.run_id }}",
		"role-to-assume: ${{ secrets.AWS_DEPLOY_ROLE_ARN }}",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("production deployment workflow is missing %q", required)
		}
	}

	bootstrap := readRepositoryFile(t, "infrastructure", "bootstrap", "github-actions-deploy-role.yaml")
	var template yaml.Node
	if err := yaml.Unmarshal([]byte(bootstrap), &template); err != nil {
		t.Fatalf("production deployment bootstrap is not valid YAML: %v", err)
	}
	for _, required := range []string{
		"repo:cyfernova/Billeif-Backend:environment:production",
		"token.actions.githubusercontent.com:repository_id",
		"1122461982",
		"token.actions.githubusercontent.com:repository_owner_id",
		"236051948",
		"token.actions.githubusercontent.com:workflow",
		"Terraform Deploy",
		"token.actions.githubusercontent.com:ref",
		"refs/heads/main",
		"workload-boundary",
		"github-actions-production-role",
		"DenyDeploymentRoleSelfMutation",
		"DenyWorkloadIAMMutation",
		"TerraformStateBucketName",
		"TerraformStateLockTableName",
	} {
		if !strings.Contains(bootstrap, required) {
			t.Errorf("bootstrap template is missing %q", required)
		}
	}

	for _, forbidden := range []string{
		"PowerUserAccess",
		"Sid: AllowApplicationServices",
		"Sid: CreateBoundaryConstrainedRoles",
		"Sid: ManageBilleifWorkloadRoles",
		"Sid: ManageBilleifInstanceProfiles",
		"Sid: PassBilleifRolesToApprovedServices",
	} {
		if strings.Contains(bootstrap, forbidden) {
			t.Errorf("production deployment bootstrap must not contain %q", forbidden)
		}
	}

	boundary := cloudFormationStatement(t, bootstrap, "AllowRequiredWorkloadActions")
	if regexp.MustCompile(`(?m)^\s*Action:\s*["']?\*["']?\s*$`).MatchString(boundary) {
		t.Fatal("workload permissions boundary must use a positive action allowlist")
	}

	mutationDeny := cloudFormationStatement(t, bootstrap, "DenyWorkloadIAMMutation")
	for _, action := range []string{
		"iam:AddRoleToInstanceProfile",
		"iam:AttachRolePolicy",
		"iam:CreateInstanceProfile",
		"iam:CreateRole",
		"iam:DeleteRolePermissionsBoundary",
		"iam:PutRolePermissionsBoundary",
		"iam:PutRolePolicy",
		"iam:TagRole",
		"iam:UpdateAssumeRolePolicy",
	} {
		if !strings.Contains(mutationDeny, "- "+action) {
			t.Errorf("workload IAM mutation deny is missing %q", action)
		}
	}

	resourcePolicyDeny := cloudFormationStatement(t, bootstrap, "DenyResourcePolicyDelegation")
	for _, action := range []string{
		"kms:PutKeyPolicy",
		"s3:PutBucketPolicy",
		"secretsmanager:PutResourcePolicy",
		"sns:SetTopicAttributes",
		"sqs:SetQueueAttributes",
	} {
		if !strings.Contains(resourcePolicyDeny, "- "+action) {
			t.Errorf("resource-policy delegation deny is missing %q", action)
		}
	}

	passRoleCases := []struct {
		sid     string
		service string
		roles   []string
	}{
		{
			sid:     "PassLambdaRolesToLambda",
			service: "lambda.amazonaws.com",
			roles: []string{
				"cognito-phone-custom-sms-role",
				"database-migrator-exec-role",
				"email-delivery-exec-role",
				"lambda-bargaining-worker-exec-role",
				"lambda-exec-role",
				"lambda-gst-worker-exec-role",
				"lambda-http-exec-role",
				"lambda-invoice-worker-exec-role",
				"lambda-websocket-exec-role",
				"outbox-dispatcher-exec-role",
				"ses-feedback-exec-role",
				"voice-reconciler-role",
			},
		},
		{sid: "PassEC2RolesToEC2", service: "ec2.amazonaws.com", roles: []string{"nat-instance-role", "rds-tunnel-role"}},
		{sid: "PassAgentCoreRoleToAgentCore", service: "bedrock-agentcore.amazonaws.com", roles: []string{"voice-agentcore-runtime-role"}},
		{sid: "PassSchedulerRolesToScheduler", service: "scheduler.amazonaws.com", roles: []string{"outbox-scheduler-exec-role", "voice-reconciler-scheduler-role"}},
		{sid: "PassAPIGatewayRoleToAPIGateway", service: "apigateway.amazonaws.com", roles: []string{"apigateway-cloudwatch-role"}},
		{sid: "PassCognitoRoleToCognito", service: "cognito-idp.amazonaws.com", roles: []string{"cognito-phone-sms-role"}},
		{sid: "PassRDSRoleToRDS", service: "rds.amazonaws.com", roles: []string{"rds-proxy-role"}},
	}

	seenRoles := make(map[string]string)
	for _, testCase := range passRoleCases {
		statement := cloudFormationStatement(t, bootstrap, testCase.sid)
		if !strings.Contains(statement, "Action: iam:PassRole") {
			t.Errorf("%s must grant iam:PassRole", testCase.sid)
		}
		if !strings.Contains(statement, `"iam:PassedToService": `+testCase.service) {
			t.Errorf("%s must be restricted to %s", testCase.sid, testCase.service)
		}
		if strings.Contains(statement, `role/${ProjectName}-${Environment}-*`) {
			t.Errorf("%s must not use a prefix-wide role resource", testCase.sid)
		}
		for _, role := range testCase.roles {
			if !strings.Contains(statement, "role/${ProjectName}-${Environment}-"+role+`"`) {
				t.Errorf("%s is missing exact role %s", testCase.sid, role)
			}
			if previous, exists := seenRoles[role]; exists {
				t.Errorf("role %s is passable by both %s and %s", role, previous, testCase.sid)
			}
			seenRoles[role] = testCase.sid
		}
	}

	if got, want := len(seenRoles), 20; got != want {
		t.Fatalf("exact PassRole inventory contains %d roles; want %d", got, want)
	}

	for _, sensitive := range []string{
		"secretsmanager:GetSecretValue",
		"secretsmanager:PutResourcePolicy",
		"kms:Decrypt",
		"kms:PutKeyPolicy",
		"dynamodb:Scan",
		"s3:PutBucketPolicy",
		"sns:SetTopicAttributes",
		"sqs:ReceiveMessage",
		"sqs:SetQueueAttributes",
		"ssm:GetParameters",
	} {
		deploymentActions := cloudFormationStatement(t, bootstrap, "ManageCoreInfrastructure") +
			cloudFormationStatement(t, bootstrap, "ManageServiceInfrastructure")
		if strings.Contains(deploymentActions, "- "+sensitive) {
			t.Errorf("deployment identity must not receive application data-plane action %s", sensitive)
		}
	}

	terraformDir := filepath.Join(repositoryRoot(t), "infrastructure", "terraform")
	entries, err := os.ReadDir(terraformDir)
	if err != nil {
		t.Fatalf("read terraform directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tf") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(terraformDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		text := string(contents)
		for offset := 0; ; {
			start := strings.Index(text[offset:], `resource "aws_iam_role" `)
			if start < 0 {
				break
			}
			start += offset
			block, end := terraformBlock(t, text, start)
			offset = end
			if !strings.Contains(block, "permissions_boundary = local.workload_permissions_boundary_arn") {
				t.Errorf("%s contains an IAM workload role without the production permissions boundary: %s", entry.Name(), strings.SplitN(block, "\n", 2)[0])
			}
		}
	}
}

func cloudFormationStatement(t *testing.T, document, sid string) string {
	t.Helper()
	marker := "- Sid: " + sid
	start := strings.Index(document, marker)
	if start < 0 {
		t.Fatalf("CloudFormation statement %s is missing", sid)
	}

	remainder := document[start+len(marker):]
	next := strings.Index(remainder, "\n              - Sid: ")
	if next < 0 {
		return document[start:]
	}
	return document[start : start+len(marker)+next]
}

func terraformBlock(t *testing.T, text string, start int) (string, int) {
	t.Helper()
	open := strings.Index(text[start:], "{")
	if open < 0 {
		t.Fatal("terraform resource has no opening brace")
	}
	open += start
	depth := 0
	for index := open; index < len(text); index++ {
		switch text[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : index+1], index + 1
			}
		}
	}
	t.Fatal("terraform resource has no closing brace")
	return "", len(text)
}
