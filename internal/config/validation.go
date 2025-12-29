package config

import (
	"fmt"
)

func validate(cfg *Config) error {
	if cfg.Environment == "" {
		return fmt.Errorf("ENVIRONMENT is required")
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

	if cfg.AWS.Region == "" {
		return fmt.Errorf("AWS_REGION is required")
	}
	if cfg.AWS.LocalStack && cfg.AWS.Endpoint == "" {
		return fmt.Errorf("AWS_ENDPOINT is required when LOCALSTACK=true")
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

	return nil
}
