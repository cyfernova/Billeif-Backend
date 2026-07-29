package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestProductionValidationProfilesRequireOnlyEntrypointConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		mutate  func(*Config)
	}{
		{
			name:    "invoice",
			profile: ProfileInvoice,
			mutate: func(cfg *Config) {
				cfg.Secrets.CredentialEncryption = "credential-secret"
			},
		},
		{
			name:    "gst",
			profile: ProfileGST,
			mutate: func(cfg *Config) {
				cfg.Secrets.CredentialEncryption = "credential-secret"
				cfg.Secrets.GSTProvider = "gst-provider-secret"
			},
		},
		{
			name:    "bargaining",
			profile: ProfileBargaining,
			mutate: func(cfg *Config) {
				cfg.Secrets.CredentialEncryption = "credential-secret"
				cfg.Secrets.LLM = "llm-secret"
				cfg.Secrets.Exa = "exa-secret"
				cfg.LLM.APIURL = "https://llm.example.test/chat"
				cfg.LLM.Model = "production-model"
			},
		},
		{
			name:    "payment",
			profile: ProfilePayment,
		},
		{
			name:    "websocket",
			profile: ProfileWebSocket,
			mutate: func(cfg *Config) {
				cfg.Secrets.Database = "database-secret"
				cfg.SSM.DatabaseHostParam = "/app/database/host"
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				Environment: "production",
				Logging:     LoggingConfig{Level: "info", Format: "json"},
				AWS:         AWSConfig{Region: "ap-south-1"},
				Database:    DatabaseConfig{Port: 5432, Name: "invoice", SSLMode: "require"},
			}
			if tc.profile != ProfilePayment {
				cfg.Secrets.Database = "database-secret"
				cfg.SSM.DatabaseHostParam = "/app/database/host"
			}
			if tc.mutate != nil {
				tc.mutate(cfg)
			}
			if err := ValidateForProfile(cfg, tc.profile); err != nil {
				t.Fatalf("profile rejected its exact production configuration: %v", err)
			}
		})
	}
}

func TestProductionValidationProfilesFailClosedForTheirOwnSecretIdentifiers(t *testing.T) {
	tests := []struct {
		profile Profile
		want    string
	}{
		{ProfileInvoice, "CREDENTIAL_ENCRYPTION_SECRET_ARN"},
		{ProfileGST, "GST_PROVIDER_SECRET_ARN"},
		{ProfileBargaining, "LLM_SECRET_ARN"},
		{ProfileWebSocket, "DATABASE_SECRET_ARN"},
	}
	for _, tc := range tests {
		t.Run(string(tc.profile), func(t *testing.T) {
			cfg := &Config{
				Environment: "production",
				Logging:     LoggingConfig{Level: "info", Format: "json"},
				AWS:         AWSConfig{Region: "ap-south-1"},
				Database:    DatabaseConfig{Port: 5432, Name: "invoice", SSLMode: "require"},
			}
			if tc.profile == ProfileGST || tc.profile == ProfileBargaining {
				cfg.Secrets.CredentialEncryption = "credential-secret"
			}
			if tc.profile != ProfileWebSocket {
				cfg.Secrets.Database = "database-secret"
				cfg.SSM.DatabaseHostParam = "/app/database/host"
			} else {
				cfg.SSM.DatabaseHostParam = "/app/database/host"
			}
			if tc.profile == ProfileBargaining {
				cfg.LLM.APIURL = "https://llm.example.test/chat"
				cfg.LLM.Model = "production-model"
			}
			err := ValidateForProfile(cfg, tc.profile)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s error, got %v", tc.want, err)
			}
		})
	}
}

func TestHTTPProfileRequiresVoiceProviderIdentifiersWhileRoutesExposeRealtimeVoice(t *testing.T) {
	cfg := validConfigForTest()
	cfg.Environment = "production"
	cfg.Secrets.Deepgram = ""
	cfg.Secrets.DeepSeek = ""
	cfg.Deepgram.APIKey = ""
	cfg.VoiceRealtime.DeepgramAPIKey = ""
	cfg.VoiceRealtime.DeepSeekAPIKey = ""
	cfg.LLM.ExaAPIKey = "exa-key"
	cfg.GSTLookup.APIKey = "gst-lookup-key"
	cfg.GST.APIToken = "gst-provider-token"

	err := ValidateForProfile(cfg, ProfileHTTP)
	if err == nil || !strings.Contains(err.Error(), "DEEPGRAM_SECRET_ARN") {
		t.Fatalf("expected HTTP voice provider identifier validation, got %v", err)
	}
}

func TestLoadForProfileAcceptsScopedProductionWorkerAndWebSocketEnvironments(t *testing.T) {
	tests := []struct {
		profile Profile
		secrets map[string]string
	}{
		{ProfileInvoice, map[string]string{"CREDENTIAL_ENCRYPTION_SECRET_ARN": "credential-secret"}},
		{ProfileGST, map[string]string{
			"CREDENTIAL_ENCRYPTION_SECRET_ARN": "credential-secret",
			"GST_PROVIDER_SECRET_ARN":          "gst-provider-secret",
		}},
		{ProfileBargaining, map[string]string{
			"CREDENTIAL_ENCRYPTION_SECRET_ARN": "credential-secret",
			"LLM_SECRET_ARN":                   "llm-secret",
			"EXA_SECRET_ARN":                   "exa-secret",
			"LLM_API_URL":                      "https://llm.example.test/chat",
			"LLM_MODEL":                        "production-model",
		}},
		{ProfileWebSocket, nil},
	}

	for _, tc := range tests {
		t.Run(string(tc.profile), func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Setenv("ENVIRONMENT", "production")
			t.Setenv("LOG_LEVEL", "info")
			t.Setenv("LOG_FORMAT", "json")
			t.Setenv("AWS_REGION", "ap-south-1")
			t.Setenv("DATABASE_PORT", "5432")
			t.Setenv("DATABASE_NAME", "invoice")
			t.Setenv("DATABASE_SSL_MODE", "require")
			t.Setenv("DATABASE_HOST_SSM_PARAM", "/app/database/host")
			t.Setenv("DATABASE_SECRET_ARN", "database-secret")
			for _, key := range []string{
				"CREDENTIAL_ENCRYPTION_SECRET_ARN", "RAZORPAY_SECRET_ARN", "LLM_SECRET_ARN",
				"EXA_SECRET_ARN", "GST_LOOKUP_SECRET_ARN", "GST_PROVIDER_SECRET_ARN",
				"DEEPGRAM_SECRET_ARN", "DEEPSEEK_SECRET_ARN", "LLM_API_URL", "LLM_MODEL",
			} {
				t.Setenv(key, "")
			}
			for key, value := range tc.secrets {
				t.Setenv(key, value)
			}

			if _, err := LoadForProfile(tc.profile); err != nil {
				t.Fatalf("scoped production load failed: %v", err)
			}
		})
	}
}
