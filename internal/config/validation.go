package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

type Profile string

const (
	ProfileHTTP       Profile = "http"
	ProfileA2A        Profile = "a2a-stream"
	ProfileInvoice    Profile = "sqs-invoice"
	ProfileGST        Profile = "sqs-gst"
	ProfileBargaining Profile = "sqs-bargaining"
	ProfileWebSocket  Profile = "websocket"
	ProfileMigration  Profile = "migration"
	ProfileOutbox     Profile = "outbox"
)

func ValidateForProfile(cfg *Config, profile Profile) error {
	switch profile {
	case "", ProfileHTTP, ProfileA2A:
		if err := validate(cfg); err != nil {
			return err
		}
		if isProductionEnv(cfg.Environment) {
			if err := requireProviderIdentifier(cfg.Secrets.Exa, cfg.LLM.ExaAPIKey, "EXA_SECRET_ARN"); err != nil {
				return err
			}
			if err := requireProviderIdentifier(cfg.Secrets.GSTLookup, cfg.GSTLookup.APIKey, "GST_LOOKUP_SECRET_ARN"); err != nil {
				return err
			}
			if err := requireGSTProvider(cfg); err != nil {
				return err
			}
			if err := requireProviderIdentifier(cfg.Secrets.Deepgram, firstConfigured(cfg.Deepgram.APIKey, cfg.VoiceRealtime.DeepgramAPIKey), "DEEPGRAM_SECRET_ARN"); err != nil {
				return err
			}
			if err := requireProviderIdentifier(cfg.Secrets.DeepSeek, cfg.VoiceRealtime.DeepSeekAPIKey, "DEEPSEEK_SECRET_ARN"); err != nil {
				return err
			}
		}
		return nil
	case ProfileMigration:
		if err := validateProfileBase(cfg); err != nil {
			return err
		}
		return validateProfileDatabase(cfg)
	case ProfileOutbox:
		if err := validateProfileBase(cfg); err != nil {
			return err
		}
		if err := validateProfileDatabase(cfg); err != nil {
			return err
		}
		return validateProfileDependencies(cfg, profile)
	case ProfileInvoice, ProfileGST, ProfileBargaining, ProfileWebSocket:
		if err := validateProfileBase(cfg); err != nil {
			return err
		}
		if err := validateProfileDatabase(cfg); err != nil {
			return err
		}
		if profile == ProfileWebSocket {
			return nil
		}
		if err := validateProfileDependencies(cfg, profile); err != nil {
			return err
		}
		if err := requireProviderIdentifier(cfg.Secrets.CredentialEncryption, cfg.Credentials.EncryptionKey, "CREDENTIAL_ENCRYPTION_SECRET_ARN"); err != nil {
			return err
		}
		if cfg.Credentials.EncryptionKey != "" {
			if err := validateCredentialEncryptionKey(cfg.Credentials.EncryptionKey); err != nil {
				return err
			}
		}
		if profile == ProfileGST {
			return requireGSTProvider(cfg)
		}
		if profile == ProfileBargaining {
			if err := requireProviderIdentifier(cfg.Secrets.LLM, cfg.LLM.APIKey, "LLM_SECRET_ARN"); err != nil {
				return err
			}
			if err := requireProviderIdentifier(cfg.Secrets.Exa, cfg.LLM.ExaAPIKey, "EXA_SECRET_ARN"); err != nil {
				return err
			}
			return validateLLMEndpoint(cfg.LLM)
		}
		return nil
	default:
		return fmt.Errorf("unknown configuration profile %q", profile)
	}
}

func validateProfileDependencies(cfg *Config, profile Profile) error {
	switch profile {
	case ProfileInvoice, ProfileGST:
		if strings.TrimSpace(cfg.S3.BucketInvoices) == "" {
			return fmt.Errorf("S3_BUCKET_INVOICES is required")
		}
	case ProfileBargaining:
		if strings.TrimSpace(cfg.SQS.BargainingQueue) == "" {
			return fmt.Errorf("SQS_BARGAINING_QUEUE is required")
		}
	case ProfileOutbox:
		if strings.TrimSpace(cfg.SQS.InvoiceQueue) == "" {
			return fmt.Errorf("SQS_INVOICE_QUEUE is required")
		}
	}
	return nil
}

func validateProfileBase(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if strings.TrimSpace(cfg.Environment) == "" {
		return fmt.Errorf("ENVIRONMENT is required")
	}
	if err := validateLogging(cfg.Logging); err != nil {
		return err
	}
	return nil
}

func validateProfileDatabase(cfg *Config) error {
	if strings.TrimSpace(cfg.AWS.Region) == "" {
		return fmt.Errorf("AWS_REGION is required")
	}
	if cfg.Database.Host == "" && cfg.SSM.DatabaseHostParam == "" {
		return fmt.Errorf("DATABASE_HOST is required")
	}
	if cfg.Database.User == "" && cfg.Secrets.Database == "" {
		return fmt.Errorf("DATABASE_SECRET_ARN is required")
	}
	if cfg.Database.Password == "" && cfg.Secrets.Database == "" {
		return fmt.Errorf("DATABASE_SECRET_ARN is required")
	}
	if cfg.Database.Name == "" {
		return fmt.Errorf("DATABASE_NAME is required")
	}
	return nil
}

func requireProviderIdentifier(identifier, explicit, envName string) error {
	if strings.TrimSpace(identifier) == "" && strings.TrimSpace(explicit) == "" {
		return fmt.Errorf("%s is required", envName)
	}
	return nil
}

func requireGSTProvider(cfg *Config) error {
	explicit := firstConfigured(cfg.GST.APIToken, cfg.GST.ClientID, cfg.GST.ClientSecret, cfg.GST.Username, cfg.GST.Password)
	return requireProviderIdentifier(cfg.Secrets.GSTProvider, explicit, "GST_PROVIDER_SECRET_ARN")
}

func firstConfigured(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validateCredentialEncryptionKey(value string) error {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be base64 encoded: %w", err)
	}
	if len(key) != 32 {
		return fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must decode to exactly 32 bytes")
	}
	return nil
}

func validateLLMEndpoint(cfg LLMConfig) error {
	if strings.TrimSpace(cfg.APIURL) == "" {
		return fmt.Errorf("LLM_API_URL is required")
	}
	parsedLLMURL, err := url.Parse(cfg.APIURL)
	if err != nil || parsedLLMURL.Scheme != "https" || parsedLLMURL.Host == "" {
		return fmt.Errorf("LLM_API_URL must be an absolute https URL")
	}
	if isPlaceholderLLMHost(parsedLLMURL.Hostname()) {
		return fmt.Errorf("LLM_API_URL cannot use placeholder host %q", parsedLLMURL.Hostname())
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("LLM_MODEL is required")
	}
	if isPlaceholderLLMValue(cfg.Model) {
		return fmt.Errorf("LLM_MODEL cannot be a placeholder value")
	}
	return nil
}

func validate(cfg *Config) error {
	if cfg.Environment == "" {
		return fmt.Errorf("ENVIRONMENT is required")
	}
	if err := validateLogging(cfg.Logging); err != nil {
		return err
	}

	if cfg.Database.Host == "" && cfg.SSM.DatabaseHostParam == "" {
		return fmt.Errorf("DATABASE_HOST is required")
	}
	if cfg.Database.User == "" && cfg.Secrets.Database == "" {
		return fmt.Errorf("DATABASE_USER is required")
	}
	if cfg.Database.Password == "" && cfg.Secrets.Database == "" {
		return fmt.Errorf("DATABASE_PASSWORD is required")
	}
	if cfg.Database.Name == "" {
		return fmt.Errorf("DATABASE_NAME is required")
	}

	// Reject wildcard origins when credentials are allowed (CORS safety)
	for _, origin := range cfg.AllowedOrigins {
		if strings.Contains(origin, "*") {
			return fmt.Errorf("wildcard origins (\"*\") are not allowed when AllowCredentials is enabled; got %q", origin)
		}
	}
	if len(cfg.AllowedOrigins) == 0 {
		return fmt.Errorf("ALLOWED_ORIGINS is required")
	}

	if cfg.AWS.Region == "" {
		return fmt.Errorf("AWS_REGION is required")
	}

	if cfg.JWT.AccessTokenExpiry <= 0 {
		return fmt.Errorf("JWT_ACCESS_TOKEN_EXPIRY must be positive")
	}
	if cfg.JWT.RefreshTokenExpiry <= 0 {
		return fmt.Errorf("JWT_REFRESH_TOKEN_EXPIRY must be positive")
	}

	if cfg.S3.BucketLogos == "" {
		return fmt.Errorf("S3_BUCKET_LOGOS is required")
	}
	if cfg.S3.BucketInvoices == "" {
		return fmt.Errorf("S3_BUCKET_INVOICES is required")
	}
	if cfg.S3.BucketProducts == "" {
		return fmt.Errorf("S3_BUCKET_PRODUCTS is required")
	}

	if cfg.SQS.InvoiceQueue == "" {
		return fmt.Errorf("SQS_INVOICE_QUEUE is required")
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" && strings.TrimSpace(cfg.Secrets.LLM) == "" {
		return fmt.Errorf("LLM_API_KEY is required")
	}
	if err := validateLLMEndpoint(cfg.LLM); err != nil {
		return err
	}
	if cfg.Credentials.EncryptionKey == "" && cfg.Secrets.CredentialEncryption == "" {
		return fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY is required")
	}
	if cfg.Credentials.EncryptionKey != "" {
		if err := validateCredentialEncryptionKey(cfg.Credentials.EncryptionKey); err != nil {
			return err
		}
	}

	if cfg.Cognito.Phone.UserPoolID != "" || cfg.Cognito.Phone.ClientID != "" || cfg.Cognito.Phone.Region != "" {
		if cfg.Cognito.Phone.UserPoolID == "" {
			return fmt.Errorf("COGNITO_PHONE_USER_POOL_ID is required when phone auth is configured")
		}
		if cfg.Cognito.Phone.ClientID == "" {
			return fmt.Errorf("COGNITO_PHONE_CLIENT_ID is required when phone auth is configured")
		}
		if cfg.Cognito.Phone.Region == "" {
			return fmt.Errorf("COGNITO_PHONE_REGION is required when phone auth is configured")
		}
		if cfg.Cognito.Phone.OTPCooldownTable == "" {
			return fmt.Errorf("COGNITO_PHONE_OTP_COOLDOWN_TABLE is required when phone auth is configured")
		}
	}

	if (cfg.Shipping.ShiprocketEmail != "" && cfg.Shipping.ShiprocketPassword == "") || (cfg.Shipping.ShiprocketEmail == "" && cfg.Shipping.ShiprocketPassword != "") {
		return fmt.Errorf("both shipping shiprocket email and password must be configured together")
	}

	if isProductionEnv(cfg.Environment) {
		if strings.TrimSpace(cfg.Server.BaseURL) == "" {
			return fmt.Errorf("SERVER_BASE_URL is required in production")
		}
		if strings.TrimSpace(cfg.Cognito.UserPoolID) == "" {
			return fmt.Errorf("COGNITO_USER_POOL_ID is required in production")
		}
		if strings.TrimSpace(cfg.Cognito.ClientID) == "" {
			return fmt.Errorf("COGNITO_CLIENT_ID is required in production")
		}
		if strings.TrimSpace(cfg.Cognito.Region) == "" {
			return fmt.Errorf("COGNITO_REGION is required in production")
		}
		if strings.TrimSpace(cfg.Cognito.Domain) == "" {
			return fmt.Errorf("COGNITO_DOMAIN is required in production")
		}
		if strings.TrimSpace(cfg.Razorpay.KeyID) == "" && strings.TrimSpace(cfg.Secrets.Razorpay) == "" {
			return fmt.Errorf("RAZORPAY_KEY_ID is required in production")
		}
		if strings.TrimSpace(cfg.Razorpay.KeySecret) == "" && strings.TrimSpace(cfg.Secrets.Razorpay) == "" {
			return fmt.Errorf("RAZORPAY_KEY_SECRET is required in production")
		}
		if strings.TrimSpace(cfg.Razorpay.WebhookSecret) == "" && strings.TrimSpace(cfg.Secrets.Razorpay) == "" {
			return fmt.Errorf("RAZORPAY_WEBHOOK_SECRET is required in production")
		}
		if cfg.MCP.ServerURL != "" {
			parsedMCPURL, err := url.Parse(cfg.MCP.ServerURL)
			if err != nil {
				return fmt.Errorf("MCP_SERVER_URL is invalid: %w", err)
			}
			if parsedMCPURL.Scheme != "https" {
				return fmt.Errorf("MCP_SERVER_URL must use https in production")
			}
		}
		if cfg.MCP.InsecureSkipVerify {
			return fmt.Errorf("MCP_INSECURE_SKIP_VERIFY cannot be enabled in production")
		}
	}

	return nil
}

func isPlaceholderLLMHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "test.com", "www.test.com", "example.com", "www.example.com", "placeholder.com", "www.placeholder.com":
		return true
	default:
		return false
	}
}

func isPlaceholderLLMValue(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	switch normalized {
	case "test", "placeholder", "dummy", "changeme", "change-me":
		return true
	default:
		return strings.HasPrefix(normalized, "your-")
	}
}

func validateLogging(logging LoggingConfig) error {
	level := strings.ToLower(strings.TrimSpace(logging.Level))
	switch level {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error")
	}

	format := strings.ToLower(strings.TrimSpace(logging.Format))
	switch format {
	case "", "json", "console":
	default:
		return fmt.Errorf("LOG_FORMAT must be one of: json, console")
	}

	if logging.SamplingInitial < 0 {
		return fmt.Errorf("LOG_SAMPLING_INITIAL must be >= 0")
	}
	if logging.SamplingThereafter < 0 {
		return fmt.Errorf("LOG_SAMPLING_THEREAFTER must be >= 0")
	}

	stackLevel := strings.ToLower(strings.TrimSpace(logging.StacktraceLevel))
	switch stackLevel {
	case "", "debug", "info", "warn", "error", "dpanic", "panic", "fatal":
	default:
		return fmt.Errorf("LOG_STACKTRACE_LEVEL must be one of: debug, info, warn, error, dpanic, panic, fatal")
	}

	return nil
}
