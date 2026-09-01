package config

import (
	"context"
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

func TestProductionHTTPRateLimitRequiresDistributedTLSIAMBackend(t *testing.T) {
	cfg := validConfigForTest()
	cfg.Environment = "production"
	cfg.Secrets.InvoiceCursorHMAC = "invoice-cursor-secret"
	cfg.LLM.ExaAPIKey = "not-a-real-exa-key"
	cfg.GSTLookup.APIKey = "not-a-real-gst-lookup-key"
	cfg.GST.APIToken = "not-a-real-gst-provider-token"
	cfg.DeepSeek.APIKey = "not-a-real-deepseek-key"

	err := ValidateForProfile(cfg, ProfileHTTP)
	if err == nil || !strings.Contains(err.Error(), "REDIS_HOST") {
		t.Fatalf("production HTTP config error = %v, want missing distributed rate-limit backend", err)
	}
}

func TestValidateProductionRateLimitRejectsUnsafeBackendConfiguration(t *testing.T) {
	valid := validProductionRateLimitConfigForTest()
	tests := []struct {
		name   string
		mutate func(*RedisConfig)
		want   string
	}{
		{name: "port", mutate: func(cfg *RedisConfig) { cfg.Port = 0 }, want: "REDIS_PORT"},
		{name: "IAM user", mutate: func(cfg *RedisConfig) { cfg.UserID = "" }, want: "REDIS_USER_ID"},
		{name: "cache name", mutate: func(cfg *RedisConfig) { cfg.CacheName = "" }, want: "REDIS_CACHE_NAME"},
		{name: "TLS", mutate: func(cfg *RedisConfig) { cfg.TLSEnabled = false }, want: "REDIS_TLS_ENABLED"},
		{name: "IAM auth", mutate: func(cfg *RedisConfig) { cfg.IAMAuthEnabled = false }, want: "REDIS_IAM_AUTH_ENABLED"},
		{name: "cluster mode", mutate: func(cfg *RedisConfig) { cfg.ClusterMode = false }, want: "REDIS_CLUSTER_MODE"},
		{name: "password", mutate: func(cfg *RedisConfig) { cfg.Password = "long-lived-secret" }, want: "REDIS_PASSWORD"},
		{name: "database", mutate: func(cfg *RedisConfig) { cfg.DB = 1 }, want: "REDIS_DB"},
		{name: "production proxy", mutate: func(cfg *RedisConfig) { cfg.TrustedProxyCIDR = "127.0.0.1/32" }, want: "RATE_LIMIT_TRUSTED_PROXY_CIDR"},
		{name: "decision timeout", mutate: func(cfg *RedisConfig) { cfg.DecisionTimeout = 0 }, want: "RATE_LIMIT_DECISION_TIMEOUT"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			err := validateProductionRateLimit(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateProductionRateLimit() error = %v, want %s", err, tc.want)
			}
		})
	}

	if err := validateProductionRateLimit(valid); err != nil {
		t.Fatalf("valid production rate-limit backend rejected: %v", err)
	}
}

func validProductionRateLimitConfigForTest() RedisConfig {
	return RedisConfig{
		Host:            "cache.example.test",
		Port:            6379,
		UserID:          "billeif-http",
		CacheName:       "billeif-production-rate-limit",
		TLSEnabled:      true,
		IAMAuthEnabled:  true,
		ClusterMode:     true,
		DecisionTimeout: 250 * time.Millisecond,
	}
}

func TestVoiceSessionDefaultsAreBoundedAndNonSecret(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)
	if cfg.VoiceSession.AdmissionEnabled || cfg.VoiceSession.RolloutStage != "disabled" || len(cfg.VoiceSession.RolloutInternalSubjectHashes) != 0 {
		t.Fatalf("voice rollout must fail closed by default: %#v", cfg.VoiceSession)
	}
	if cfg.VoiceSession.ProtocolVersion != 1 || cfg.VoiceSession.KVSChannelCount != 12 {
		t.Fatalf("unexpected protocol/channel defaults: %#v", cfg.VoiceSession)
	}
	if cfg.VoiceSession.MaxDuration != 55*time.Minute || cfg.VoiceSession.RotateAfter != 52*time.Minute || cfg.VoiceSession.LeaseDuration != 2*time.Minute {
		t.Fatalf("unexpected voice duration defaults: %#v", cfg.VoiceSession)
	}
	if cfg.VoiceSession.AgentRuntimeQualifier != "PROD" || cfg.VoiceSession.GlobalCapacityLimit != 100 || cfg.VoiceSession.PerUserCapacityLimit != 1 {
		t.Fatalf("unexpected voice control defaults: %#v", cfg.VoiceSession)
	}
}

func TestValidateVoiceSessionRolloutFailsClosed(t *testing.T) {
	base := VoiceSessionConfig{
		TableName:       "voice-sessions",
		AgentRuntimeARN: "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/test",
	}
	tests := []struct {
		name   string
		mutate func(*VoiceSessionConfig)
		want   string
	}{
		{name: "admission requires non-disabled rollout", mutate: func(cfg *VoiceSessionConfig) {
			cfg.AdmissionEnabled = true
			cfg.RolloutStage = "disabled"
		}, want: "VOICE_ROLLOUT_STAGE must not be disabled"},
		{name: "internal rollout requires allowlist", mutate: func(cfg *VoiceSessionConfig) {
			cfg.AdmissionEnabled = true
			cfg.RolloutStage = "internal"
		}, want: "VOICE_ROLLOUT_INTERNAL_SUB_HASHES"},
		{name: "raw subject is rejected", mutate: func(cfg *VoiceSessionConfig) {
			cfg.RolloutStage = "internal"
			cfg.RolloutInternalSubjectHashes = []string{"cognito-sub-is-pii"}
		}, want: "sha256:<64 lowercase hex>"},
		{name: "unknown stage is rejected", mutate: func(cfg *VoiceSessionConfig) {
			cfg.RolloutStage = "10"
		}, want: "disabled, internal, 5, 25, 50, or 100"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if err := validateVoiceSessionConfig(cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q validation error, got %v", tc.want, err)
			}
		})
	}

	valid := base
	valid.AdmissionEnabled = true
	valid.RolloutStage = "internal"
	valid.RolloutInternalSubjectHashes = []string{"sha256:a82dbfadf4306ff5b431dd63bb680db4f6f00cd92583e349717838e2fb70fee0"}
	if err := validateVoiceSessionConfig(valid); err != nil {
		t.Fatalf("valid hashed internal rollout rejected: %v", err)
	}

	missingRuntime := VoiceSessionConfig{AdmissionEnabled: true, RolloutStage: "100"}
	if err := validateVoiceSessionConfig(missingRuntime); err == nil || !strings.Contains(err.Error(), "requires voice session infrastructure") {
		t.Fatalf("admission without runtime infrastructure must fail closed, got %v", err)
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

func TestValidateRejectsPlaceholderLLMConfig(t *testing.T) {
	cfg := validConfigForTest()
	cfg.LLM.APIURL = "https://test.com"

	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "LLM_API_URL cannot use placeholder host") {
		t.Fatalf("expected placeholder LLM_API_URL to fail validation, got %v", err)
	}

	cfg = validConfigForTest()
	cfg.LLM.Model = "test"
	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "LLM_MODEL cannot be a placeholder value") {
		t.Fatalf("expected placeholder LLM_MODEL to fail validation, got %v", err)
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
	viper.Set("REDIS_TLS_ENABLED", "true")
	viper.Set("REDIS_IAM_AUTH_ENABLED", "true")
	viper.Set("REDIS_CLUSTER_MODE", "true")

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
	if !cfg.Redis.TLSEnabled || !cfg.Redis.IAMAuthEnabled || !cfg.Redis.ClusterMode {
		t.Fatalf("expected flat Redis security flags to populate Redis config, got %#v", cfg.Redis)
	}
}

func TestSSMResolutionCanBeSkippedWhenExplicitDBValuesExist(t *testing.T) {
	cfg := validConfigForTest()
	cfg.SSM.DatabaseHostParam = "/invoice-backend/dev/db/host"
	cfg.Database.Host = "127.0.0.1"
	cfg.Database.User = "invoice_user"
	cfg.Database.Password = "local-placeholder"

	if err := ResolveRuntime(context.Background(), cfg, RuntimeResolvers{}); err != nil {
		t.Fatalf("expected explicit DB values to skip SSM lookups, got %v", err)
	}

	if cfg.Database.Host != "127.0.0.1" {
		t.Fatalf("expected explicit DATABASE_HOST to be preserved, got %q", cfg.Database.Host)
	}
	if cfg.Database.User != "invoice_user" {
		t.Fatalf("expected explicit DATABASE_USER to be preserved, got %q", cfg.Database.User)
	}
	if cfg.Database.Password != "local-placeholder" {
		t.Fatal("expected explicit DATABASE_PASSWORD to be preserved")
	}
}

func TestLoadWithExplicitLocalDatabaseCredentialsDoesNotRequireAWS(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("ENVIRONMENT", "dev")
	t.Setenv("DATABASE_HOST", "127.0.0.1")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "invoice_local")
	t.Setenv("DATABASE_PASSWORD", "local-only-password")
	t.Setenv("DATABASE_NAME", "invoice_local")
	t.Setenv("DATABASE_SSL_MODE", "disable")
	t.Setenv("DATABASE_SECRET_ARN", "")
	t.Setenv("AWS_REGION", "ap-south-1")
	t.Setenv("JWT_ACCESS_TOKEN_EXPIRY", "1h")
	t.Setenv("JWT_REFRESH_TOKEN_EXPIRY", "720h")
	t.Setenv("S3_BUCKET_LOGOS", "local-logos")
	t.Setenv("S3_BUCKET_INVOICES", "local-invoices")
	t.Setenv("S3_BUCKET_PRODUCTS", "local-products")
	t.Setenv("SQS_INVOICE_QUEUE", "local-invoice-queue")
	t.Setenv("SQS_EMAIL_DELIVERY_QUEUE", "local-email-delivery-queue")
	t.Setenv("PLATFORM_OPERATOR_GROUP", "platform_operator")
	t.Setenv("LLM_API_KEY", "local-llm-key")
	t.Setenv("LLM_API_URL", "https://llm.example.test/chat/completions")
	t.Setenv("LLM_MODEL", "local-model")
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("ALLOWED_ORIGINS", "http://127.0.0.1:3000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("explicit local config unexpectedly required AWS: %v", err)
	}
	if cfg.Database.Host != "127.0.0.1" || cfg.Database.User != "invoice_local" || cfg.Database.Password != "local-only-password" {
		t.Fatal("explicit local database credentials were not preserved")
	}
	if cfg.Cognito.OperatorGroup != "platform_operator" {
		t.Fatalf("operator group = %q", cfg.Cognito.OperatorGroup)
	}
}

func TestResolveRuntimeExtractsRDSManagedCredentialFields(t *testing.T) {
	cfg := validConfigForTest()
	cfg.Database.User = ""
	cfg.Database.Password = ""
	cfg.Secrets.Database = "rds-managed-secret"
	client := &fakeSecretsManager{values: []string{`{"username":"managed_user","password":"` + sentinelSecret + `"}`}}

	if err := ResolveRuntime(context.Background(), cfg, RuntimeResolvers{Secrets: client}); err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	if cfg.Database.User != "managed_user" {
		t.Fatalf("expected managed username, got %q", cfg.Database.User)
	}
	if cfg.Database.Password != sentinelSecret {
		t.Fatal("expected managed password field to be installed")
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
			InvoiceQueue:       "invoice-queue",
			EmailDeliveryQueue: "email-delivery-queue",
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

func TestHTTPProfileRequiresDedicatedEmailDeliveryQueue(t *testing.T) {
	cfg := validConfigForTest()
	cfg.SQS.EmailDeliveryQueue = ""

	err := ValidateForProfile(cfg, ProfileHTTP)

	if err == nil || !strings.Contains(err.Error(), "SQS_EMAIL_DELIVERY_QUEUE") {
		t.Fatalf("HTTP profile error = %v, want dedicated email delivery queue", err)
	}
}
