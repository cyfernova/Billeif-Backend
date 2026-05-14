package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestSetDefaultsDoesNotInstallDevelopmentAllowedOrigins(t *testing.T) {
	cfg := &Config{Environment: "dev"}

	setDefaults(cfg)

	if len(cfg.AllowedOrigins) != 0 {
		t.Fatalf("expected development defaults to require explicit origins, got %#v", cfg.AllowedOrigins)
	}
}

func TestSetDefaultsProductionDoesNotInstallWildcardOrigin(t *testing.T) {
	cfg := &Config{Environment: "production"}

	setDefaults(cfg)

	if len(cfg.AllowedOrigins) != 0 {
		t.Fatalf("expected production defaults to require explicit origins, got %#v", cfg.AllowedOrigins)
	}
}

func TestSetDefaultsProductionPreservesExplicitWAFEnabled(t *testing.T) {
	cfg := &Config{Environment: "production"}
	cfg.AWS.WAF.Enabled = true

	setDefaults(cfg)

	if !cfg.AWS.WAF.Enabled {
		t.Fatal("expected production WAF setting to preserve explicit enabled value")
	}
}

func TestValidateRequiresAllowedOrigins(t *testing.T) {
	cfg := validConfigForTest()
	cfg.AllowedOrigins = nil

	if err := validate(cfg); err == nil {
		t.Fatal("expected config without ALLOWED_ORIGINS to fail validation")
	}
}

func TestValidateRequiresExplicitLLMConfig(t *testing.T) {
	cfg := validConfigForTest()
	cfg.LLM.APIURL = ""

	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "LLM_API_URL") {
		t.Fatalf("expected missing LLM_API_URL to fail validation, got %v", err)
	}

	cfg = validConfigForTest()
	cfg.LLM.APIURL = "http://llm.example.test/chat/completions"
	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "LLM_API_URL must be an absolute https URL") {
		t.Fatalf("expected insecure LLM_API_URL to fail validation, got %v", err)
	}
}

func TestValidateRejectsInsecureProductionMCP(t *testing.T) {
	cfg := validConfigForTest()
	cfg.Environment = "production"
	cfg.MCP.ServerURL = "http://mcp.example.test"

	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "MCP_SERVER_URL must use https") {
		t.Fatalf("expected production MCP http URL to fail validation, got %v", err)
	}

	cfg.MCP.ServerURL = "https://mcp.example.test"
	cfg.MCP.InsecureSkipVerify = true
	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "MCP_INSECURE_SKIP_VERIFY") {
		t.Fatalf("expected production MCP skip verify to fail validation, got %v", err)
	}
}

func TestTerraformDoesNotShipRuntimeSecretDefaults(t *testing.T) {
	variables, err := os.ReadFile("../../infrastructure/terraform/variables.tf")
	if err != nil {
		t.Fatalf("read terraform variables: %v", err)
	}
	cognito, err := os.ReadFile("../../infrastructure/terraform/cognito.tf")
	if err != nil {
		t.Fatalf("read terraform cognito config: %v", err)
	}
	configGo, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	envExample, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}

	combined := string(variables) + "\n" + string(cognito) + "\n" + string(configGo) + "\n" + string(envExample)
	for _, forbidden := range []string{
		`default     = "changeme"`,
		`change-me-in-production-with-secure-secret`,
		`TPRzhZvL3pvBBvNXN26Sa+yfrLZogwLpyD5rCDmB140=`,
		`http://localhost:3000/callback`,
		`https://auth.expo.io/`,
		`"implicit"`,
		`https://api.deepseek.com/chat/completions`,
		`deepseek-v4-flash`,
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("terraform security config still contains forbidden default %q", forbidden)
		}
	}
}

func TestParseAllowedOriginsTrimsCommaSeparatedValues(t *testing.T) {
	got := parseAllowedOrigins(" http://localhost:3000, http://localhost:8081 ,,http://127.0.0.1:8081 ")
	want := []string{"http://localhost:3000", "http://localhost:8081", "http://127.0.0.1:8081"}

	if len(got) != len(want) {
		t.Fatalf("expected %d origins, got %d: %#v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected origin %d to be %q, got %q", i, want[i], got[i])
		}
	}
}

func TestApplyFlatEnvFileFallbacksCopiesFlatKeys(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("DATABASE_HOST_SSM_PARAM", "/invoice-backend/dev/db/host")
	viper.Set("AWS_REGION", "us-east-1")
	viper.Set("JWT_ACCESS_TOKEN_EXPIRY", "1h")
	viper.Set("S3_BUCKET_LOGOS", "logos")

	cfg := &Config{}
	applyFlatEnvFileFallbacks(cfg)

	if cfg.SSM.DatabaseHostParam != "/invoice-backend/dev/db/host" {
		t.Fatalf("expected flat DATABASE_HOST_SSM_PARAM to populate SSM config, got %q", cfg.SSM.DatabaseHostParam)
	}
	if cfg.AWS.Region != "us-east-1" {
		t.Fatalf("expected flat AWS_REGION to populate AWS config, got %q", cfg.AWS.Region)
	}
	if cfg.JWT.AccessTokenExpiry != time.Hour {
		t.Fatalf("expected flat JWT_ACCESS_TOKEN_EXPIRY to populate JWT config, got %s", cfg.JWT.AccessTokenExpiry)
	}
	if cfg.S3.BucketLogos != "logos" {
		t.Fatalf("expected flat S3_BUCKET_LOGOS to populate S3 config, got %q", cfg.S3.BucketLogos)
	}
}

func validConfigForTest() *Config {
	return &Config{
		Environment: "dev",
		Database: DatabaseConfig{
			Host:     "db.example.test",
			User:     "invoice",
			Password: "not-a-real-secret",
			Name:     "invoice",
		},
		Server: ServerConfig{BaseURL: "https://api.example.test"},
		AWS:    AWSConfig{Region: "us-east-1"},
		JWT: JWTConfig{
			AccessTokenExpiry:  1,
			RefreshTokenExpiry: 1,
		},
		S3: S3Config{
			BucketLogos:    "logos",
			BucketInvoices: "invoices",
			BucketProducts: "products",
		},
		SQS: SQSConfig{
			InvoiceQueue: "invoice-queue",
			PaymentQueue: "payment-queue",
		},
		Razorpay: RazorpayConfig{
			KeyID:         "rzp_test_key",
			KeySecret:     "not-a-real-razorpay-secret",
			WebhookSecret: "not-a-real-webhook-secret",
		},
		LLM: LLMConfig{
			APIKey: "not-a-real-llm-key",
			APIURL: "https://llm.example.test/chat/completions",
			Model:  "test-model",
		},
		Credentials: CredentialsConfig{
			EncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		},
		Cognito: CognitoConfig{
			UserPoolID: "pool",
			ClientID:   "client",
			Region:     "us-east-1",
			Domain:     "auth.example.test",
		},
		AllowedOrigins: []string{"https://app.example.test"},
	}
}
