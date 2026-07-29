package tests

import (
	"os"
	"strings"
	"testing"
)

func TestRuntimeEntrypointsUseScopedConfigurationProfiles(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"../cmd/lambda/http/main.go":           "config.ProfileHTTP",
		"../cmd/lambda/a2a-stream/main.go":     "config.ProfileA2A",
		"../cmd/lambda/sqs-invoice/main.go":    "config.ProfileInvoice",
		"../cmd/lambda/sqs-gst/main.go":        "config.ProfileGST",
		"../cmd/lambda/sqs-bargaining/main.go": "config.ProfileBargaining",
		"../cmd/server/main.go":                "config.ProfileHTTP",
	}
	for path, profile := range cases {
		path, profile := path, profile
		t.Run(path, func(t *testing.T) {
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read entrypoint: %v", err)
			}
			source := string(body)
			if !strings.Contains(source, "Profile:") || !strings.Contains(source, profile) {
				t.Fatalf("entrypoint must bootstrap with %s", profile)
			}
			if strings.Contains(source, "RefreshCredentials(") {
				t.Fatal("entrypoint must retain its initialized service graph across invocations")
			}
		})
	}
}

func TestWebSocketEntrypointUsesWebSocketProfile(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../cmd/lambda/ws/main.go")
	if err != nil {
		t.Fatalf("read WebSocket entrypoint: %v", err)
	}
	if !strings.Contains(string(body), "config.LoadForProfile(config.ProfileWebSocket)") {
		t.Fatal("WebSocket entrypoint must validate only its scoped runtime configuration")
	}
}
