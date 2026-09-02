package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Profile string

const (
	ProfileHTTP                   Profile = "http"
	ProfileA2A                    Profile = "a2a-stream"
	ProfileInvoice                Profile = "sqs-invoice"
	ProfileGST                    Profile = "sqs-gst"
	ProfileBargaining             Profile = "sqs-bargaining"
	ProfileWebSocket              Profile = "websocket"
	ProfileMigration              Profile = "migration"
	ProfileOutbox                 Profile = "outbox"
	ProfileRecurringInvoices      Profile = "recurring-invoices"
	ProfileSubscriptionReconciler Profile = "subscription-reconciler"
	ProfileEmailDelivery          Profile = "sqs-email-delivery"
	ProfileSESFeedback            Profile = "sqs-ses-feedback"
	ProfileBulkImport             Profile = "bulk-import"
)

func ValidateForProfile(cfg *Config, profile Profile) error {
	switch profile {
	case "", ProfileHTTP, ProfileA2A:
		if err := validate(cfg); err != nil {
			return err
		}
		if isProductionEnv(cfg.Environment) {
			if profile != ProfileA2A {
				if err := validateProductionRateLimit(cfg.Redis); err != nil {
					return err
				}
			}
			if profile != ProfileA2A && strings.TrimSpace(cfg.Secrets.InvoiceCursorHMAC) == "" {
				return fmt.Errorf("INVOICE_CURSOR_HMAC_SECRET_ARN is required")
			}
			if err := requireProviderIdentifier(cfg.Secrets.Exa, cfg.LLM.ExaAPIKey, "EXA_SECRET_ARN"); err != nil {
				return err
			}
			if err := requireProviderIdentifier(cfg.Secrets.GSTLookup, cfg.GSTLookup.APIKey, "GST_LOOKUP_SECRET_ARN"); err != nil {
				return err
			}
			if err := requireGSTProvider(cfg); err != nil {
				return err
			}
			if err := requireProviderIdentifier(cfg.Secrets.DeepSeek, cfg.DeepSeek.APIKey, "DEEPSEEK_SECRET_ARN"); err != nil {
				return err
			}
		}
		return nil
	case ProfileMigration, ProfileRecurringInvoices:
		if err := validateProfileBase(cfg); err != nil {
			return err
		}
		return validateProfileDatabase(cfg)
	case ProfileBulkImport:
		if err := validateProfileBase(cfg); err != nil {
			return err
		}
		if err := validateProfileDatabase(cfg); err != nil {
			return err
		}
		return validateProfileDependencies(cfg, profile)
	case ProfileSubscriptionReconciler:
		if err := validateProfileBase(cfg); err != nil {
			return err
		}
		if err := validateProfileDatabase(cfg); err != nil {
			return err
		}
		return requireProviderIdentifier(cfg.Secrets.Razorpay, cfg.Razorpay.KeySecret, "RAZORPAY_SECRET_ARN")
	case ProfileOutbox:
		if err := validateProfileBase(cfg); err != nil {
			return err
		}
		if err := validateProfileDatabase(cfg); err != nil {
			return err
		}
		return validateProfileDependencies(cfg, profile)
	case ProfileEmailDelivery, ProfileSESFeedback:
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
		if profile == ProfileInvoice || profile == ProfileGST {
			if err := requireProviderIdentifier(cfg.Secrets.CredentialEncryption, cfg.Credentials.EncryptionKey, "CREDENTIAL_ENCRYPTION_SECRET_ARN"); err != nil {
				return err
			}
			if cfg.Credentials.EncryptionKey != "" {
				if err := validateCredentialEncryptionKey(cfg.Credentials.EncryptionKey); err != nil {
					return err
				}
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

func validateProductionRateLimit(cfg RedisConfig) error {
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("REDIS_HOST is required for production HTTP rate limiting")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("REDIS_PORT must be a valid TCP port for production HTTP rate limiting")
	}
	if strings.TrimSpace(cfg.UserID) == "" {
		return fmt.Errorf("REDIS_USER_ID is required for production IAM authentication")
	}
	if strings.TrimSpace(cfg.CacheName) == "" {
		return fmt.Errorf("REDIS_CACHE_NAME is required for production IAM authentication")
	}
	if !cfg.TLSEnabled {
		return fmt.Errorf("REDIS_TLS_ENABLED must be true in production")
	}
	if !cfg.IAMAuthEnabled {
		return fmt.Errorf("REDIS_IAM_AUTH_ENABLED must be true in production")
	}
	if !cfg.ClusterMode {
		return fmt.Errorf("REDIS_CLUSTER_MODE must be true for the production serverless cache")
	}
	if strings.TrimSpace(cfg.Password) != "" {
		return fmt.Errorf("REDIS_PASSWORD must be empty when production IAM authentication is enabled")
	}
	if cfg.DB != 0 {
		return fmt.Errorf("REDIS_DB must be 0 in production cluster mode")
	}
	if strings.TrimSpace(cfg.TrustedProxyCIDR) != "" {
		return fmt.Errorf("RATE_LIMIT_TRUSTED_PROXY_CIDR is local-only and must be empty in production")
	}
	if cfg.DecisionTimeout <= 0 {
		return fmt.Errorf("RATE_LIMIT_DECISION_TIMEOUT must be positive in production")
	}
	return nil
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
		if strings.TrimSpace(cfg.SQS.EmailDeliveryQueue) == "" {
			return fmt.Errorf("SQS_EMAIL_DELIVERY_QUEUE is required")
		}
	case ProfileEmailDelivery:
		if strings.TrimSpace(cfg.S3.BucketInvoices) == "" {
			return fmt.Errorf("S3_BUCKET_INVOICES is required")
		}
		if strings.TrimSpace(cfg.SES.SenderEmail) == "" {
			return fmt.Errorf("SES_SENDER_EMAIL is required")
		}
		if strings.TrimSpace(cfg.SES.ConfigurationSet) == "" {
			return fmt.Errorf("SES_CONFIGURATION_SET is required")
		}
	case ProfileSESFeedback:
		if strings.TrimSpace(cfg.SES.SendingAccountID) == "" {
			return fmt.Errorf("SES_SENDING_ACCOUNT_ID is required")
		}
		if strings.TrimSpace(cfg.SES.ConfigurationSet) == "" {
			return fmt.Errorf("SES_CONFIGURATION_SET is required")
		}
	case ProfileBulkImport:
		if strings.TrimSpace(cfg.S3.BucketDrive) == "" {
			return fmt.Errorf("S3_BUCKET_DRIVE is required")
		}
		if strings.TrimSpace(cfg.S3.BucketInvoices) == "" {
			return fmt.Errorf("S3_BUCKET_INVOICES is required")
		}
		if strings.TrimSpace(cfg.SQS.BulkImportQueue) == "" {
			return fmt.Errorf("SQS_BULK_IMPORT_QUEUE is required")
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
	if err := validateVoiceSessionConfig(cfg.VoiceSession); err != nil {
		return err
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
	if cfg.SQS.EmailDeliveryQueue == "" {
		return fmt.Errorf("SQS_EMAIL_DELIVERY_QUEUE is required")
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

func validateVoiceSessionConfig(cfg VoiceSessionConfig) error {
	tableConfigured := strings.TrimSpace(cfg.TableName) != ""
	runtimeConfigured := strings.TrimSpace(cfg.AgentRuntimeARN) != ""
	if tableConfigured != runtimeConfigured {
		return fmt.Errorf("VOICE_SESSIONS_TABLE_NAME and AGENTCORE_RUNTIME_ARN must be configured together")
	}
	if cfg.AdmissionEnabled && !tableConfigured {
		return fmt.Errorf("VOICE_ADMISSION_ENABLED requires voice session infrastructure")
	}
	if cfg.ProtocolVersion != 0 && cfg.ProtocolVersion != 1 {
		return fmt.Errorf("VOICE_PROTOCOL_VERSION must be 1")
	}
	if cfg.KVSChannelCount != 0 && cfg.KVSChannelCount != 12 {
		return fmt.Errorf("VOICE_KVS_CHANNEL_COUNT must be 12")
	}
	maxDuration := cfg.MaxDuration
	if maxDuration == 0 {
		maxDuration = 55 * time.Minute
	}
	rotateAfter := cfg.RotateAfter
	if rotateAfter == 0 {
		rotateAfter = 52 * time.Minute
	}
	leaseDuration := cfg.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = 2 * time.Minute
	}
	if maxDuration < 0 || maxDuration > 55*time.Minute {
		return fmt.Errorf("VOICE_SESSION_MAX_DURATION must be at most 55m")
	}
	if rotateAfter < 0 || rotateAfter >= maxDuration {
		return fmt.Errorf("VOICE_SESSION_ROTATE_AFTER must be shorter than max duration")
	}
	if leaseDuration < 0 || leaseDuration > 5*time.Minute {
		return fmt.Errorf("VOICE_SESSION_LEASE_DURATION must be at most 5m")
	}
	if cfg.GlobalCapacityLimit != 0 && cfg.GlobalCapacityLimit != 100 {
		return fmt.Errorf("VOICE_GLOBAL_CAPACITY_LIMIT must be 100")
	}
	if cfg.PerUserCapacityLimit != 0 && cfg.PerUserCapacityLimit != 1 {
		return fmt.Errorf("VOICE_PER_USER_CAPACITY_LIMIT must be 1")
	}
	stage := strings.TrimSpace(cfg.RolloutStage)
	if stage == "" {
		stage = "disabled"
	}
	if !containsString([]string{"disabled", "internal", "5", "25", "50", "100"}, stage) {
		return fmt.Errorf("VOICE_ROLLOUT_STAGE must be disabled, internal, 5, 25, 50, or 100")
	}
	hashPattern := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for _, hash := range cfg.RolloutInternalSubjectHashes {
		if !hashPattern.MatchString(hash) {
			return fmt.Errorf("VOICE_ROLLOUT_INTERNAL_SUB_HASHES entries must use sha256:<64 lowercase hex>")
		}
	}
	if cfg.AdmissionEnabled && stage == "disabled" {
		return fmt.Errorf("VOICE_ROLLOUT_STAGE must not be disabled when VOICE_ADMISSION_ENABLED is true")
	}
	if cfg.AdmissionEnabled && stage == "internal" && len(cfg.RolloutInternalSubjectHashes) == 0 {
		return fmt.Errorf("VOICE_ROLLOUT_INTERNAL_SUB_HASHES is required for internal admission")
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
