package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Database  DatabaseConfig  `mapstructure:"database"`
	AWS       AWSConfig       `mapstructure:"aws"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	App       AppConfig       `mapstructure:"app"`
}

type ServerConfig struct {
	Port            int      `mapstructure:"port"`
	Mode            string   `mapstructure:"mode"`
	ReadTimeout     int      `mapstructure:"read_timeout"`
	WriteTimeout    int      `mapstructure:"write_timeout"`
	ShutdownTimeout int      `mapstructure:"shutdown_timeout"`
	AllowedOrigins  []string `mapstructure:"allowed_origins"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
}

type AWSConfig struct {
	Region            string        `mapstructure:"region"`
	AccessKeyID       string        `mapstructure:"access_key_id"`
	SecretAccessKey   string        `mapstructure:"secret_access_key"`
	LocalStackEndpoint string       `mapstructure:"localstack_endpoint"`
	Cognito           CognitoConfig `mapstructure:"cognito"`
	DynamoDB          DynamoDBConfig `mapstructure:"dynamodb"`
	S3                S3Config      `mapstructure:"s3"`
}

type CognitoConfig struct {
	UserPoolID   string `mapstructure:"user_pool_id"`
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	Region       string `mapstructure:"region"`
}

type DynamoDBConfig struct {
	SessionsTable     string `mapstructure:"sessions_table"`
	PreferencesTable  string `mapstructure:"preferences_table"`
	Region            string `mapstructure:"region"`
}

type S3Config struct {
	ProfilePicturesBucket string `mapstructure:"profile_pictures_bucket"`
	Region               string `mapstructure:"region"`
}

type JWTConfig struct {
	Secret          string `mapstructure:"secret"`
	AccessTokenExp  int    `mapstructure:"access_token_exp"`
	RefreshTokenExp int    `mapstructure:"refresh_token_exp"`
	Issuer          string `mapstructure:"issuer"`
}

type AppConfig struct {
	Environment string         `mapstructure:"environment"`
	LogLevel    string         `mapstructure:"log_level"`
	RateLimit   RateLimitConfig `mapstructure:"rate_limit"`
}

type RateLimitConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Requests int `mapstructure:"requests"`
	Window  int  `mapstructure:"window"`
}

func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Configure viper
	v.SetEnvPrefix("")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read from .env file
	v.SetConfigFile(".env")
	if err := v.ReadInConfig(); err != nil {
		// .env file is optional
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("server.read_timeout", 60)
	v.SetDefault("server.write_timeout", 60)
	v.SetDefault("server.shutdown_timeout", 10)
	v.SetDefault("server.allowed_origins", []string{"http://localhost:3000", "http://localhost:8080"})

	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "invoice_user")
	v.SetDefault("database.password", "invoice_password")
	v.SetDefault("database.dbname", "invoice_db")
	v.SetDefault("database.sslmode", "disable")

	// AWS defaults
	v.SetDefault("aws.region", "us-east-1")
	v.SetDefault("aws.access_key_id", "test")
	v.SetDefault("aws.secret_access_key", "test")
	v.SetDefault("aws.localstack_endpoint", "http://localhost:4566")
	v.SetDefault("aws.cognito.region", "us-east-1")
	v.SetDefault("aws.dynamodb.region", "us-east-1")
	v.SetDefault("aws.s3.region", "us-east-1")

	// JWT defaults
	v.SetDefault("jwt.access_token_exp", 15)
	v.SetDefault("jwt.refresh_token_exp", 10080)
	v.SetDefault("jwt.issuer", "invoice-backend")

	// App defaults
	v.SetDefault("app.environment", "development")
	v.SetDefault("app.log_level", "info")
	v.SetDefault("app.rate_limit.enabled", true)
	v.SetDefault("app.rate_limit.requests", 100)
	v.SetDefault("app.rate_limit.window", 60)
}

// GetDSN returns the PostgreSQL connection string
func (c *DatabaseConfig) GetDSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
}

// IsDevelopment returns true if the environment is development
func (c *AppConfig) IsDevelopment() bool {
	return c.Environment == "development"
}

// IsProduction returns true if the environment is production
func (c *AppConfig) IsProduction() bool {
	return c.Environment == "production"
}
