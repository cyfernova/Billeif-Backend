package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Environment    string         `mapstructure:"ENVIRONMENT"`
	Server         ServerConfig   `mapstructure:"SERVER"`
	Database       DatabaseConfig `mapstructure:"DATABASE"`
	Redis          RedisConfig    `mapstructure:"REDIS"`
	AWS            AWSConfig      `mapstructure:"AWS"`
	Cognito        CognitoConfig  `mapstructure:"COGNITO"`
	JWT            JWTConfig      `mapstructure:"JWT"`
	S3             S3Config       `mapstructure:"S3"`
	SQS            SQSConfig      `mapstructure:"SQS"`
	Sentry         SentryConfig   `mapstructure:"SENTRY"`
	AllowedOrigins []string  `mapstructure:"ALLOWED_ORIGINS"`
	LLM            LLMConfig `mapstructure:"LLM"`
}

type LLMConfig struct {
	APIKey  string `mapstructure:"API_KEY"`
	APIURL  string `mapstructure:"API_URL"`
	Model   string `mapstructure:"MODEL"`
	Timeout int    `mapstructure:"TIMEOUT"`
}

type SentryConfig struct {
	DSN              string  `mapstructure:"DSN"`
	SampleRate       float64 `mapstructure:"SAMPLE_RATE"`
	TracesSampleRate float64 `mapstructure:"TRACES_SAMPLE_RATE"`
	EnableTracing    bool    `mapstructure:"ENABLE_TRACING"`
	Debug            bool    `mapstructure:"DEBUG"`
}

type ServerConfig struct {
	Port         int    `mapstructure:"PORT"`
	BaseURL      string `mapstructure:"BASE_URL"`
	ReadTimeout  int    `mapstructure:"READ_TIMEOUT"`
	WriteTimeout int    `mapstructure:"WRITE_TIMEOUT"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"HOST"`
	Port     int    `mapstructure:"PORT"`
	User     string `mapstructure:"USER"`
	Password string `mapstructure:"PASSWORD"`
	Name     string `mapstructure:"NAME"`
	SSLMode  string `mapstructure:"SSL_MODE"`
}

type RedisConfig struct {
	Host     string `mapstructure:"HOST"`
	Port     int    `mapstructure:"PORT"`
	Password string `mapstructure:"PASSWORD"`
	DB       int    `mapstructure:"DB"`
}

type AWSConfig struct {
	Region    string `mapstructure:"REGION"`
	AccessKey string `mapstructure:"ACCESS_KEY_ID"`
	SecretKey string `mapstructure:"SECRET_ACCESS_KEY"`
	Endpoint  string `mapstructure:"ENDPOINT"`
}

type CognitoConfig struct {
	UserPoolID      string        `mapstructure:"USER_POOL_ID"`
	ClientID        string        `mapstructure:"CLIENT_ID"`
	Region          string        `mapstructure:"REGION"`
	JWKSRefreshRate time.Duration `mapstructure:"JWKS_REFRESH_RATE"`
}

type JWTConfig struct {
	AccessTokenExpiry  time.Duration `mapstructure:"ACCESS_TOKEN_EXPIRY"`
	RefreshTokenExpiry time.Duration `mapstructure:"REFRESH_TOKEN_EXPIRY"`
}

type S3Config struct {
	BucketLogos     string `mapstructure:"BUCKET_LOGOS"`
	BucketInvoices  string `mapstructure:"BUCKET_INVOICES"`
	BucketProducts  string `mapstructure:"BUCKET_PRODUCTS"`
	BucketEmailSink string `mapstructure:"BUCKET_EMAIL_SINK"`
}

type SQSConfig struct {
	InvoiceQueue string `mapstructure:"INVOICE_QUEUE"`
	PaymentQueue string `mapstructure:"PAYMENT_QUEUE"`
}

func Load() (*Config, error) {
	viper.SetConfigName(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.AddConfigPath("..")
	viper.AutomaticEnv()


	// Explicitly bind environment variables for nested config
	viper.BindEnv("ENVIRONMENT")
	viper.BindEnv("SERVER.PORT", "SERVER_PORT")
	viper.BindEnv("SERVER.BASE_URL", "SERVER_BASE_URL")
	viper.BindEnv("SERVER.READ_TIMEOUT", "SERVER_READ_TIMEOUT")
	viper.BindEnv("SERVER.WRITE_TIMEOUT", "SERVER_WRITE_TIMEOUT")
	viper.BindEnv("DATABASE.HOST", "DATABASE_HOST")
	viper.BindEnv("DATABASE.PORT", "DATABASE_PORT")
	viper.BindEnv("DATABASE.USER", "DATABASE_USER")
	viper.BindEnv("DATABASE.PASSWORD", "DATABASE_PASSWORD")
	viper.BindEnv("DATABASE.NAME", "DATABASE_NAME")
	viper.BindEnv("DATABASE.SSL_MODE", "DATABASE_SSL_MODE")
	viper.BindEnv("REDIS.HOST", "REDIS_HOST")
	viper.BindEnv("REDIS.PORT", "REDIS_PORT")
	viper.BindEnv("REDIS.PASSWORD", "REDIS_PASSWORD")
	viper.BindEnv("REDIS.DB", "REDIS_DB")
	viper.BindEnv("AWS.REGION", "AWS_REGION")
	viper.BindEnv("AWS.ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID")
	viper.BindEnv("AWS.SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY")
	viper.BindEnv("AWS.ENDPOINT", "AWS_ENDPOINT")
	viper.BindEnv("COGNITO.USER_POOL_ID", "COGNITO_USER_POOL_ID")
	viper.BindEnv("COGNITO.CLIENT_ID", "COGNITO_CLIENT_ID")
	viper.BindEnv("COGNITO.REGION", "COGNITO_REGION")
	viper.BindEnv("COGNITO.JWKS_REFRESH_RATE", "COGNITO_JWKS_REFRESH_RATE")
	viper.BindEnv("JWT.ACCESS_TOKEN_EXPIRY", "JWT_ACCESS_TOKEN_EXPIRY")
	viper.BindEnv("JWT.REFRESH_TOKEN_EXPIRY", "JWT_REFRESH_TOKEN_EXPIRY")
	viper.BindEnv("S3.BUCKET_LOGOS", "S3_BUCKET_LOGOS")
	viper.BindEnv("S3.BUCKET_INVOICES", "S3_BUCKET_INVOICES")
	viper.BindEnv("S3.BUCKET_PRODUCTS", "S3_BUCKET_PRODUCTS")
	viper.BindEnv("S3.BUCKET_EMAIL_SINK", "S3_BUCKET_EMAIL_SINK")
	viper.BindEnv("SQS.INVOICE_QUEUE", "SQS_INVOICE_QUEUE")
	viper.BindEnv("SQS.PAYMENT_QUEUE", "SQS_PAYMENT_QUEUE")
	viper.BindEnv("SENTRY.DSN", "SENTRY_DSN")
	viper.BindEnv("SENTRY.SAMPLE_RATE", "SENTRY_SAMPLE_RATE")
	viper.BindEnv("SENTRY.TRACES_SAMPLE_RATE", "SENTRY_TRACES_SAMPLE_RATE")
	viper.BindEnv("SENTRY.ENABLE_TRACING", "SENTRY_ENABLE_TRACING")
	viper.BindEnv("SENTRY.DEBUG", "SENTRY_DEBUG")
	viper.BindEnv("ALLOWED_ORIGINS", "ALLOWED_ORIGINS")
	viper.BindEnv("LLM.API_KEY", "LLM_API_KEY")
	viper.BindEnv("LLM.API_URL", "LLM_API_URL")
	viper.BindEnv("LLM.MODEL", "LLM_MODEL")
	viper.BindEnv("LLM.TIMEOUT", "LLM_TIMEOUT")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	setDefaults(&cfg)

	return &cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Environment == "" {
		cfg.Environment = "dev"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 30
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 30
	}
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if cfg.Database.SSLMode == "" {
		cfg.Database.SSLMode = "disable"
	}
	if cfg.Redis.Port == 0 {
		cfg.Redis.Port = 6379
	}
	if cfg.AWS.Region == "" {
		cfg.AWS.Region = "us-east-1"
	}
	if cfg.JWT.AccessTokenExpiry == 0 {
		cfg.JWT.AccessTokenExpiry = time.Hour
	}
	if cfg.JWT.RefreshTokenExpiry == 0 {
		cfg.JWT.RefreshTokenExpiry = 720 * time.Hour
	}
	if cfg.Cognito.JWKSRefreshRate == 0 {
		cfg.Cognito.JWKSRefreshRate = 10 * time.Minute
	}
	if cfg.Cognito.Region == "" {
		cfg.Cognito.Region = "us-east-1"
	}
	if len(cfg.AllowedOrigins) == 0 {
		cfg.AllowedOrigins = []string{"http://localhost:3000"}
	}
	if cfg.LLM.Timeout == 0 {
		cfg.LLM.Timeout = 60
	}
}
