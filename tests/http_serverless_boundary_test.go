package tests

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRESTAPIContainsOnlyTheTwoStreamingRouteIntegrations(t *testing.T) {
	t.Parallel()

	resources := terraformResourceLabels(t)
	wantMethods := []string{
		"aws_api_gateway_method.api_v1_a2a_message_stream_post",
		"aws_api_gateway_method.api_v1_a2a_task_subscribe_get",
	}
	wantIntegrations := []string{
		"aws_api_gateway_integration.api_v1_a2a_message_stream_post",
		"aws_api_gateway_integration.api_v1_a2a_task_subscribe_get",
	}

	var methods []string
	var integrations []string
	for _, resource := range resources {
		switch {
		case strings.HasPrefix(resource, "aws_api_gateway_method."):
			methods = append(methods, resource)
		case strings.HasPrefix(resource, "aws_api_gateway_integration."):
			integrations = append(integrations, resource)
		}
	}
	assertExactManifest(t, "REST API methods", methods, wantMethods)
	assertExactManifest(t, "REST API integrations", integrations, wantIntegrations)

	for _, required := range []string{
		"aws_apigatewayv2_api.http",
		"aws_apigatewayv2_integration.http_lambda",
		"aws_apigatewayv2_route.http_default",
		"aws_apigatewayv2_stage.http",
		"aws_lambda_permission.allow_http_api_http",
	} {
		if !containsString(resources, required) {
			t.Errorf("ordinary HTTP API boundary is missing %s", required)
		}
	}
	if containsString(resources, "aws_lambda_permission.allow_rest_api_http") {
		t.Error("ordinary HTTP Lambda must not retain REST API invocation permission")
	}
}

func TestHTTPAndStreamingBuildCommandsAreStrippedARM64(t *testing.T) {
	t.Parallel()

	command := exec.Command("make", "-n", "build-lambda-http", "build-lambda-a2a-stream")
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("render Lambda build commands: %v: %s", err, output)
	}
	rendered := string(output)
	for _, entrypoint := range []string{"./cmd/lambda/http", "./cmd/lambda/a2a-stream"} {
		line := ""
		for _, candidate := range strings.Split(rendered, "\n") {
			if strings.Contains(candidate, entrypoint) {
				line = candidate
				break
			}
		}
		if line == "" {
			t.Errorf("no rendered build command for %s", entrypoint)
			continue
		}
		for _, required := range []string{
			"GOOS=linux",
			"GOARCH=arm64",
			"CGO_ENABLED=0",
			"go build -trimpath",
			`-ldflags="-s -w -buildid="`,
		} {
			if !strings.Contains(line, required) {
				t.Errorf("%s build command is missing %q: %s", entrypoint, required, line)
			}
		}
	}
}
