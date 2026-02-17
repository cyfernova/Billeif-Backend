package config

import (
	"encoding/base64"
	"fmt"
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

	if cfg.Redis.Host == "" {
		return fmt.Errorf("REDIS_HOST is required")
	}

	isProd := isProductionEnv(cfg.Environment)

	// Require Redis password in production
	if isProd && cfg.Redis.Password == "" {
		return fmt.Errorf("REDIS_PASSWORD is required in production")
	}

	// Validate SSL config: if enabled, cert and key paths must be set
	if cfg.Server.SSLEnabled {
		if cfg.Server.SSLCertPath == "" {
			return fmt.Errorf("SSL_CERT_PATH is required when SSL_ENABLED is true")
		}
		if cfg.Server.SSLKeyPath == "" {
			return fmt.Errorf("SSL_KEY_PATH is required when SSL_ENABLED is true")
		}
	}

	// Reject wildcard origins when credentials are allowed (CORS safety)
	for _, origin := range cfg.AllowedOrigins {
		if strings.Contains(origin, "*") {
			return fmt.Errorf("wildcard origins (\"*\") are not allowed when AllowCredentials is enabled; got %q", origin)
		}
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
