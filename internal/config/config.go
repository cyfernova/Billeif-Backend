package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	App       AppConfig
	AWS       AWSConfig
	DynamoDB  DynamoDBConfig
	S3        S3Config
	SES       SESConfig
	SNS       SNSConfig
	SQS       SQSConfig
	RateLimit RateLimitConfig
	PDF       PDFConfig
}

type AppConfig struct {
	Env      string
	Port     string
	Name     string
	LogLevel string
}

type AWSConfig struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	EndpointURL     string
}

type DynamoDBConfig struct {
	Endpoint      string
	TableInvoices string
	TableClients  string
}

type S3Config struct {
	Endpoint        string
	BucketInvoices  string
	BucketLogos     string
	BucketDocuments string
}

type SESConfig struct {
	Endpoint      string
	FromEmail     string
	VerifiedEmail string
}

type SNSConfig struct {
	Endpoint           string
	TopicNotifications string
	TopicAlerts        string
}

type SQSConfig struct {
	Endpoint               string
	QueueInvoiceProcessing string
	QueueEmail             string
}

type RateLimitConfig struct {
	Requests int
	Window   string
}

type PDFConfig struct {
	FontPath   string
	LogoHeight int
}

func Load() (*Config, error) {
	viper.SetConfigName(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.AddConfigPath("..")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.App.Port == "" {
		c.App.Port = "8080"
	}
	if c.App.Env == "" {
		c.App.Env = os.Getenv("APP_ENV")
		if c.App.Env == "" {
			c.App.Env = "development"
		}
	}
	if c.AWS.Region == "" {
		return fmt.Errorf("AWS_REGION is required")
	}
	if c.DynamoDB.Endpoint == "" {
		return fmt.Errorf("DYNAMODB_ENDPOINT is required")
	}
	if c.DynamoDB.TableInvoices == "" {
		return fmt.Errorf("DYNAMODB_TABLE_INVOICES is required")
	}
	if c.DynamoDB.TableClients == "" {
		return fmt.Errorf("DYNAMODB_TABLE_CLIENTS is required")
	}
	if c.S3.Endpoint == "" {
		return fmt.Errorf("S3_ENDPOINT is required")
	}
	if c.S3.BucketInvoices == "" {
		return fmt.Errorf("S3_BUCKET_INVOICES is required")
	}
	if c.S3.BucketLogos == "" {
		return fmt.Errorf("S3_BUCKET_LOGOS is required")
	}
	if c.S3.BucketDocuments == "" {
		return fmt.Errorf("S3_BUCKET_DOCUMENTS is required")
	}
	if c.SES.Endpoint == "" {
		return fmt.Errorf("SES_ENDPOINT is required")
	}
	if c.SES.FromEmail == "" {
		return fmt.Errorf("SES_FROM_EMAIL is required")
	}
	if c.SES.VerifiedEmail == "" {
		return fmt.Errorf("SES_VERIFIED_EMAIL is required")
	}
	if c.SNS.Endpoint == "" {
		return fmt.Errorf("SNS_ENDPOINT is required")
	}
	if c.SNS.TopicNotifications == "" {
		return fmt.Errorf("SNS_TOPIC_NOTIFICATIONS is required")
	}
	if c.SNS.TopicAlerts == "" {
		return fmt.Errorf("SNS_TOPIC_ALERTS is required")
	}
	if c.SQS.Endpoint == "" {
		return fmt.Errorf("SQS_ENDPOINT is required")
	}
	if c.SQS.QueueInvoiceProcessing == "" {
		return fmt.Errorf("SQS_QUEUE_INVOICE_PROCESSING is required")
	}
	if c.SQS.QueueEmail == "" {
		return fmt.Errorf("SQS_QUEUE_EMAIL is required")
	}
	if c.RateLimit.Requests == 0 {
		c.RateLimit.Requests = 100
	}
	if c.RateLimit.Window == "" {
		c.RateLimit.Window = "1m"
	}
	if c.PDF.LogoHeight == 0 {
		c.PDF.LogoHeight = 50
	}
	return nil
}

func (c *Config) IsDevelopment() bool {
	return c.App.Env == "development"
}
