package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

func validate(cfg *Config) error {
	if cfg.Environment == "" {
		return fmt.Errorf("ENVIRONMENT is required")
	}
	if err := validateLogging(cfg.Logging); err != nil {
		return err
	}

	if cfg.Database.Host == "" {
		return fmt.Errorf("DATABASE_HOST is required")
	}
	if cfg.Database.User == "" {
		return fmt.Errorf("DATABASE_USER is required")
	}
	if cfg.Database.Password == "" {
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
	if cfg.SQS.PaymentQueue == "" {
		return fmt.Errorf("SQS_PAYMENT_QUEUE is required")
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		return fmt.Errorf("LLM_API_KEY is required")
	}
	if strings.TrimSpace(cfg.LLM.APIURL) == "" {
		return fmt.Errorf("LLM_API_URL is required")
	}
	parsedLLMURL, err := url.Parse(cfg.LLM.APIURL)
	if err != nil || parsedLLMURL.Scheme != "https" || parsedLLMURL.Host == "" {
		return fmt.Errorf("LLM_API_URL must be an absolute https URL")
	}
	if strings.TrimSpace(cfg.LLM.Model) == "" {
		return fmt.Errorf("LLM_MODEL is required")
	}
	if cfg.Credentials.EncryptionKey == "" {
		return fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY is required")
	}
	key, err := base64.StdEncoding.DecodeString(cfg.Credentials.EncryptionKey)
	if err != nil {
		return fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be base64 encoded: %w", err)
	}
	if len(key) != 32 {
		return fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must decode to exactly 32 bytes")
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
		if strings.TrimSpace(cfg.Razorpay.KeyID) == "" {
			return fmt.Errorf("RAZORPAY_KEY_ID is required in production")
		}
		if strings.TrimSpace(cfg.Razorpay.KeySecret) == "" {
			return fmt.Errorf("RAZORPAY_KEY_SECRET is required in production")
		}
		if strings.TrimSpace(cfg.Razorpay.WebhookSecret) == "" {
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
