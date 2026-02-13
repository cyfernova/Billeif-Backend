package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Environment    string            `mapstructure:"ENVIRONMENT"`
	Logging        LoggingConfig     `mapstructure:"LOGGING"`
	Server         ServerConfig      `mapstructure:"SERVER"`
	Database       DatabaseConfig    `mapstructure:"DATABASE"`
	Redis          RedisConfig       `mapstructure:"REDIS"`
	AWS            AWSConfig         `mapstructure:"AWS"`
	Cognito        CognitoConfig     `mapstructure:"COGNITO"`
	JWT            JWTConfig         `mapstructure:"JWT"`
	S3             S3Config          `mapstructure:"S3"`
	SQS            SQSConfig         `mapstructure:"SQS"`
	Sentry         SentryConfig      `mapstructure:"SENTRY"`
	AllowedOrigins []string          `mapstructure:"ALLOWED_ORIGINS"`
	LLM            LLMConfig         `mapstructure:"LLM"`
	Credentials    CredentialsConfig `mapstructure:"CREDENTIALS"`
}

type LoggingConfig struct {
	Level              string `mapstructure:"LEVEL"`
	Format             string `mapstructure:"FORMAT"`
	SamplingInitial    int    `mapstructure:"SAMPLING_INITIAL"`
	SamplingThereafter int    `mapstructure:"SAMPLING_THEREAFTER"`
	StacktraceLevel    string `mapstructure:"STACKTRACE_LEVEL"`
}

type LLMConfig struct {
	APIKey  string `mapstructure:"API_KEY"`
	APIURL  string `mapstructure:"API_URL"`
	Model   string `mapstructure:"MODEL"`
	Timeout int    `mapstructure:"TIMEOUT"`
}

type CredentialsConfig struct {
	EncryptionKey string `mapstructure:"ENCRYPTION_KEY"`
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

	// Sensible defaults for logging
	viper.SetDefault("LOGGING.LEVEL", "info")
	viper.SetDefault("LOGGING.FORMAT", "json")
	viper.SetDefault("LOGGING.STACKTRACE_LEVEL", "error")

	// Explicitly bind environment variables for nested config
	_ = viper.BindEnv("ENVIRONMENT")
	_ = viper.BindEnv("LOGGING.LEVEL", "LOG_LEVEL")
	_ = viper.BindEnv("LOGGING.FORMAT", "LOG_FORMAT")
	_ = viper.BindEnv("LOGGING.SAMPLING_INITIAL", "LOG_SAMPLING_INITIAL")
	_ = viper.BindEnv("LOGGING.SAMPLING_THEREAFTER", "LOG_SAMPLING_THEREAFTER")
	_ = viper.BindEnv("LOGGING.STACKTRACE_LEVEL", "LOG_STACKTRACE_LEVEL")
	_ = viper.BindEnv("SERVER.PORT", "SERVER_PORT")
	_ = viper.BindEnv("SERVER.BASE_URL", "SERVER_BASE_URL")
	_ = viper.BindEnv("SERVER.READ_TIMEOUT", "SERVER_READ_TIMEOUT")
	_ = viper.BindEnv("SERVER.WRITE_TIMEOUT", "SERVER_WRITE_TIMEOUT")
	_ = viper.BindEnv("DATABASE.HOST", "DATABASE_HOST")
	_ = viper.BindEnv("DATABASE.PORT", "DATABASE_PORT")
	_ = viper.BindEnv("DATABASE.USER", "DATABASE_USER")
	_ = viper.BindEnv("DATABASE.PASSWORD", "DATABASE_PASSWORD")
	_ = viper.BindEnv("DATABASE.NAME", "DATABASE_NAME")
	_ = viper.BindEnv("DATABASE.SSL_MODE", "DATABASE_SSL_MODE")
	_ = viper.BindEnv("REDIS.HOST", "REDIS_HOST")
	_ = viper.BindEnv("REDIS.PORT", "REDIS_PORT")
	_ = viper.BindEnv("REDIS.PASSWORD", "REDIS_PASSWORD")
	_ = viper.BindEnv("REDIS.DB", "REDIS_DB")
	_ = viper.BindEnv("AWS.REGION", "AWS_REGION")
	_ = viper.BindEnv("AWS.ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID")
	_ = viper.BindEnv("AWS.SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY")
	_ = viper.BindEnv("AWS.ENDPOINT", "AWS_ENDPOINT")
	_ = viper.BindEnv("COGNITO.USER_POOL_ID", "COGNITO_USER_POOL_ID")
	_ = viper.BindEnv("COGNITO.CLIENT_ID", "COGNITO_CLIENT_ID")
	_ = viper.BindEnv("COGNITO.REGION", "COGNITO_REGION")
	_ = viper.BindEnv("COGNITO.JWKS_REFRESH_RATE", "COGNITO_JWKS_REFRESH_RATE")
	_ = viper.BindEnv("JWT.ACCESS_TOKEN_EXPIRY", "JWT_ACCESS_TOKEN_EXPIRY")
	_ = viper.BindEnv("JWT.REFRESH_TOKEN_EXPIRY", "JWT_REFRESH_TOKEN_EXPIRY")
	_ = viper.BindEnv("S3.BUCKET_LOGOS", "S3_BUCKET_LOGOS")
	_ = viper.BindEnv("S3.BUCKET_INVOICES", "S3_BUCKET_INVOICES")
	_ = viper.BindEnv("S3.BUCKET_PRODUCTS", "S3_BUCKET_PRODUCTS")
	_ = viper.BindEnv("S3.BUCKET_EMAIL_SINK", "S3_BUCKET_EMAIL_SINK")
	_ = viper.BindEnv("SQS.INVOICE_QUEUE", "SQS_INVOICE_QUEUE")
	_ = viper.BindEnv("SQS.PAYMENT_QUEUE", "SQS_PAYMENT_QUEUE")
	_ = viper.BindEnv("SENTRY.DSN", "SENTRY_DSN")
	_ = viper.BindEnv("SENTRY.SAMPLE_RATE", "SENTRY_SAMPLE_RATE")
	_ = viper.BindEnv("SENTRY.TRACES_SAMPLE_RATE", "SENTRY_TRACES_SAMPLE_RATE")
	_ = viper.BindEnv("SENTRY.ENABLE_TRACING", "SENTRY_ENABLE_TRACING")
	_ = viper.BindEnv("SENTRY.DEBUG", "SENTRY_DEBUG")
	_ = viper.BindEnv("ALLOWED_ORIGINS", "ALLOWED_ORIGINS")
	_ = viper.BindEnv("LLM.API_KEY", "LLM_API_KEY")
	_ = viper.BindEnv("LLM.API_URL", "LLM_API_URL")
	_ = viper.BindEnv("LLM.MODEL", "LLM_MODEL")
	_ = viper.BindEnv("LLM.TIMEOUT", "LLM_TIMEOUT")
	_ = viper.BindEnv("CREDENTIALS.ENCRYPTION_KEY", "CREDENTIAL_ENCRYPTION_KEY")

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
	if cfg.Logging.Level == "" {
		if isProductionEnv(cfg.Environment) {
			cfg.Logging.Level = "info"
		} else {
			cfg.Logging.Level = "debug"
		}
	}
	if cfg.Logging.Format == "" {
		if isProductionEnv(cfg.Environment) {
			cfg.Logging.Format = "json"
		} else {
			cfg.Logging.Format = "console"
		}
	}
	if cfg.Logging.SamplingInitial == 0 && cfg.Logging.SamplingThereafter == 0 && isProductionEnv(cfg.Environment) {
		cfg.Logging.SamplingInitial = 100
		cfg.Logging.SamplingThereafter = 100
	}
	if cfg.Logging.StacktraceLevel == "" {
		cfg.Logging.StacktraceLevel = "error"
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

func isProductionEnv(env string) bool {
	normalized := strings.ToLower(strings.TrimSpace(env))
	return normalized == "prod" || normalized == "production"
}
