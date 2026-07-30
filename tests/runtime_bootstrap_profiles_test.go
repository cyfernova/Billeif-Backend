package tests

import (
	"os"
	"strings"
	"testing"

	"invoice-backend/internal/config"
)

func TestRuntimeEntrypointsUseScopedConfigurationProfiles(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"../cmd/lambda/http/main.go":               "config.ProfileHTTP",
		"../cmd/lambda/a2a-stream/main.go":         "config.ProfileA2A",
		"../cmd/lambda/sqs-invoice/main.go":        "config.ProfileInvoice",
		"../cmd/lambda/sqs-gst/main.go":            "config.ProfileGST",
		"../cmd/lambda/sqs-bargaining/main.go":     "config.ProfileBargaining",
		"../cmd/lambda/sqs-email-delivery/main.go": "config.ProfileEmailDelivery",
		"../cmd/server/main.go":                    "config.ProfileHTTP",
	}
	for path, profile := range cases {
		path, profile := path, profile
		t.Run(path, func(t *testing.T) {
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read entrypoint: %v", err)
			}
			source := string(body)
			usesInitializeProfile := strings.Contains(source, "Profile:") && strings.Contains(source, profile)
			usesScopedLoad := strings.Contains(source, "config.LoadForProfile("+profile+")")
			if !usesInitializeProfile && !usesScopedLoad {
				t.Fatalf("entrypoint must bootstrap with %s", profile)
			}
			if strings.Contains(source, "RefreshCredentials(") {
				t.Fatal("entrypoint must retain its initialized service graph across invocations")
			}
		})
	}
}

func TestWorkerBootstrapProfilesFailClosedWithoutTheirConcreteDependencies(t *testing.T) {
	tests := []struct {
		name    string
		profile config.Profile
		mutate  func(*config.Config)
		want    string
	}{
		{"invoice", config.ProfileInvoice, func(cfg *config.Config) { cfg.S3.BucketInvoices = "" }, "S3_BUCKET_INVOICES"},
		{"GST", config.ProfileGST, func(cfg *config.Config) { cfg.S3.BucketInvoices = "" }, "S3_BUCKET_INVOICES"},
		{"bargaining", config.ProfileBargaining, func(cfg *config.Config) { cfg.SQS.BargainingQueue = "" }, "SQS_BARGAINING_QUEUE"},
		{"outbox", config.ProfileOutbox, func(cfg *config.Config) { cfg.SQS.InvoiceQueue = "" }, "SQS_INVOICE_QUEUE"},
		{"email delivery", config.ProfileEmailDelivery, func(cfg *config.Config) { cfg.SES.SenderEmail = "" }, "SES_SENDER_EMAIL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := completeWorkerBootstrapConfig()
			tc.mutate(cfg)
			err := config.ValidateForProfile(cfg, tc.profile)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s error, got %v", tc.want, err)
			}
		})
	}
}

func TestWorkerBootstrapProfilesAcceptCompleteConcreteDependencies(t *testing.T) {
	for _, profile := range []config.Profile{config.ProfileInvoice, config.ProfileGST, config.ProfileBargaining, config.ProfileOutbox, config.ProfileEmailDelivery} {
		if err := config.ValidateForProfile(completeWorkerBootstrapConfig(), profile); err != nil {
			t.Fatalf("%s rejected complete bootstrap config: %v", profile, err)
		}
	}
}

func completeWorkerBootstrapConfig() *config.Config {
	return &config.Config{
		Environment: "production",
		Logging:     config.LoggingConfig{Level: "info", Format: "json"},
		AWS:         config.AWSConfig{Region: "ap-south-1"},
		Database:    config.DatabaseConfig{Port: 5432, Name: "invoice", SSLMode: "require"},
		Secrets: config.SecretIdentifiers{
			Database:             "database-secret",
			CredentialEncryption: "credential-secret",
			GSTProvider:          "gst-provider-secret",
			LLM:                  "llm-secret",
			Exa:                  "exa-secret",
		},
		SSM: config.SSMConfig{DatabaseHostParam: "/app/database/host"},
		S3:  config.S3Config{BucketInvoices: "invoice-pdfs"},
		SQS: config.SQSConfig{
			BargainingQueue:    "https://sqs.ap-south-1.amazonaws.com/123/bargaining",
			InvoiceQueue:       "https://sqs.ap-south-1.amazonaws.com/123/billeif-invoice",
			EmailDeliveryQueue: "https://sqs.ap-south-1.amazonaws.com/123/billeif-email-delivery",
		},
		SES: config.SESConfig{SenderEmail: "billing@example.com", ConfigurationSet: "Billeif-production-ses-events"},
		LLM: config.LLMConfig{APIURL: "https://llm.example.test/chat", Model: "production-model"},
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

func TestOutboxEntrypointBootstrapsOnlyItsScopedRuntime(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../cmd/lambda/outbox/main.go")
	if err != nil {
		t.Fatalf("read outbox entrypoint: %v", err)
	}
	source := string(body)
	for _, required := range []string{
		"config.LoadForProfile(config.ProfileOutbox)",
		"app.OpenDatabase(",
		"postgresrepo.NewOutboxRepository(",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("outbox entrypoint must contain %s", required)
		}
	}
	for _, forbidden := range []string{"app.Initialize(", "awsclients.New("} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("outbox entrypoint must not contain %s", forbidden)
		}
	}
}
