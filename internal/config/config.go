package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Environment    string              `mapstructure:"ENVIRONMENT"`
	Logging        LoggingConfig       `mapstructure:"LOGGING"`
	Server         ServerConfig        `mapstructure:"SERVER"`
	Database       DatabaseConfig      `mapstructure:"DATABASE"`
	Redis          RedisConfig         `mapstructure:"REDIS"`
	AWS            AWSConfig           `mapstructure:"AWS"`
	SSM            SSMConfig           `mapstructure:"SSM"`
	Secrets        SecretIdentifiers   `mapstructure:"SECRETS"`
	WebSocket      WebSocketConfig     `mapstructure:"WEBSOCKET"`
	Cognito        CognitoConfig       `mapstructure:"COGNITO"`
	JWT            JWTConfig           `mapstructure:"JWT"`
	S3             S3Config            `mapstructure:"S3"`
	Razorpay       RazorpayConfig      `mapstructure:"RAZORPAY"`
	FX             FXConfig            `mapstructure:"FX"`
	WhatsApp       WhatsAppConfig      `mapstructure:"WHATSAPP"`
	SQS            SQSConfig           `mapstructure:"SQS"`
	SES            SESConfig           `mapstructure:"SES"`
	Sentry         SentryConfig        `mapstructure:"SENTRY"`
	Shipping       ShippingConfig      `mapstructure:"SHIPPING"`
	GST            GSTConfig           `mapstructure:"GST"`
	GSTLookup      GSTLookupConfig     `mapstructure:"GST_LOOKUP"`
	Entitlements   EntitlementsConfig  `mapstructure:"ENTITLEMENTS"`
	AllowedOrigins []string            `mapstructure:"ALLOWED_ORIGINS"`
	LLM            LLMConfig           `mapstructure:"LLM"`
	Deepgram       DeepgramConfig      `mapstructure:"DEEPGRAM"`
	VoiceRealtime  VoiceRealtimeConfig `mapstructure:"VOICE_REALTIME"`
	Credentials    CredentialsConfig   `mapstructure:"CREDENTIALS"`
	MCP            MCPConfig           `mapstructure:"MCP"`
}

type LoggingConfig struct {
	Level              string `mapstructure:"LEVEL"`
	Format             string `mapstructure:"FORMAT"`
	SamplingInitial    int    `mapstructure:"SAMPLING_INITIAL"`
	SamplingThereafter int    `mapstructure:"SAMPLING_THEREAFTER"`
	StacktraceLevel    string `mapstructure:"STACKTRACE_LEVEL"`
}

type LLMConfig struct {
	APIKey     string `mapstructure:"API_KEY"`
	APIURL     string `mapstructure:"API_URL"`
	Model      string `mapstructure:"MODEL"`
	Timeout    int    `mapstructure:"TIMEOUT"`
	ExaAPIKey  string `mapstructure:"EXA_API_KEY"`
	ExaBaseURL string `mapstructure:"EXA_BASE_URL"`
	ExaTimeout int    `mapstructure:"EXA_TIMEOUT"`
}

type DeepgramConfig struct {
	APIKey string `mapstructure:"API_KEY"`
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
	Port           int    `mapstructure:"PORT"`
	BaseURL        string `mapstructure:"BASE_URL"`
	SwaggerUsePkce bool   `mapstructure:"SWAGGER_USE_PKCE"`
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
	Region       string    `mapstructure:"REGION"`
	AccessKey    string    `mapstructure:"ACCESS_KEY_ID"`
	SecretKey    string    `mapstructure:"SECRET_ACCESS_KEY"`
	SessionToken string    `mapstructure:"SESSION_TOKEN"`
	Endpoint     string    `mapstructure:"ENDPOINT"`
	WAF          WAFConfig `mapstructure:"WAF"`
}

type WAFConfig struct {
	Enabled          bool   `mapstructure:"ENABLED"`
	WebACLArn        string `mapstructure:"WEB_ACL_ARN"`
	RateLimitHeader  string `mapstructure:"RATE_LIMIT_HEADER"`
	BlockedResponse  string `mapstructure:"BLOCKED_RESPONSE"`
	HeaderMatchCount int    `mapstructure:"HEADER_MATCH_COUNT"`
}

type SSMConfig struct {
	DatabaseHostParam string `mapstructure:"DATABASE_HOST_PARAM"`
}

type SecretIdentifiers struct {
	Database             string `mapstructure:"DATABASE"`
	CredentialEncryption string `mapstructure:"CREDENTIAL_ENCRYPTION"`
	Razorpay             string `mapstructure:"RAZORPAY"`
	LegacyJWT            string `mapstructure:"LEGACY_JWT"`
	GoogleOAuth          string `mapstructure:"GOOGLE_OAUTH"`
	FCM                  string `mapstructure:"FCM"`
	APNS                 string `mapstructure:"APNS"`
	LLM                  string `mapstructure:"LLM"`
	Exa                  string `mapstructure:"EXA"`
	GSTLookup            string `mapstructure:"GST_LOOKUP"`
	GSTProvider          string `mapstructure:"GST_PROVIDER"`
	Deepgram             string `mapstructure:"DEEPGRAM"`
	DeepSeek             string `mapstructure:"DEEPSEEK"`
}

type WebSocketConfig struct {
	APIEndpoint      string `mapstructure:"API_ENDPOINT"`
	ConnectionsTable string `mapstructure:"CONNECTIONS_TABLE"`
}

type CognitoConfig struct {
	UserPoolID      string             `mapstructure:"USER_POOL_ID"`
	ClientID        string             `mapstructure:"CLIENT_ID"`
	Domain          string             `mapstructure:"DOMAIN"`
	Region          string             `mapstructure:"REGION"`
	JWKSRefreshRate time.Duration      `mapstructure:"JWKS_REFRESH_RATE"`
	Phone           CognitoPhoneConfig `mapstructure:"PHONE"`
}

type CognitoPhoneConfig struct {
	UserPoolID       string `mapstructure:"USER_POOL_ID"`
	ClientID         string `mapstructure:"CLIENT_ID"`
	Region           string `mapstructure:"REGION"`
	OTPCooldownTable string `mapstructure:"OTP_COOLDOWN_TABLE"`
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
	BucketDrive     string `mapstructure:"BUCKET_DRIVE"`
}

type RazorpayConfig struct {
	KeyID         string `mapstructure:"KEY_ID"`
	KeySecret     string `mapstructure:"KEY_SECRET"`
	WebhookSecret string `mapstructure:"WEBHOOK_SECRET"`
	BaseURL       string `mapstructure:"BASE_URL"`
	Timeout       int    `mapstructure:"TIMEOUT"`
}

type FXConfig struct {
	Provider string `mapstructure:"PROVIDER"`
	BaseURL  string `mapstructure:"BASE_URL"`
	APIKey   string `mapstructure:"API_KEY"`
	Timeout  int    `mapstructure:"TIMEOUT"`
}

type WhatsAppConfig struct {
	BaseURL string `mapstructure:"BASE_URL"`
	Timeout int    `mapstructure:"TIMEOUT"`
}

type SQSConfig struct {
	InvoiceQueue       string `mapstructure:"INVOICE_QUEUE"`
	EmailDeliveryQueue string `mapstructure:"EMAIL_DELIVERY_QUEUE"`
	GSTQueue           string `mapstructure:"GST_QUEUE"`
	BargainingQueue    string `mapstructure:"BARGAINING_QUEUE"`
}

type SESConfig struct {
	SenderEmail      string `mapstructure:"SENDER_EMAIL"`
	ConfigurationSet string `mapstructure:"CONFIGURATION_SET"`
	SendingAccountID string `mapstructure:"SENDING_ACCOUNT_ID"`
}

type ShippingConfig struct {
	DefaultProvider           string `mapstructure:"DEFAULT_PROVIDER"`
	Timeout                   int    `mapstructure:"TIMEOUT"`
	ShiprocketBaseURL         string `mapstructure:"SHIPROCKET_BASE_URL"`
	ShiprocketEmail           string `mapstructure:"SHIPROCKET_EMAIL"`
	ShiprocketPassword        string `mapstructure:"SHIPROCKET_PASSWORD"`
	ShiprocketAuthPath        string `mapstructure:"SHIPROCKET_AUTH_PATH"`
	ShiprocketOrderPath       string `mapstructure:"SHIPROCKET_ORDER_PATH"`
	ShiprocketAssignAWBPath   string `mapstructure:"SHIPROCKET_ASSIGN_AWB_PATH"`
	ShiprocketLabelPath       string `mapstructure:"SHIPROCKET_LABEL_PATH"`
	ShiprocketTrackingBaseURL string `mapstructure:"SHIPROCKET_TRACKING_BASE_URL"`
}

type GSTLookupConfig struct {
	BaseURL string `mapstructure:"BASE_URL"`
	APIKey  string `mapstructure:"API_KEY"`
	Timeout int    `mapstructure:"TIMEOUT"`
}

type GSTConfig struct {
	Provider                 string `mapstructure:"PROVIDER"`
	BaseURL                  string `mapstructure:"BASE_URL"`
	AuthPath                 string `mapstructure:"AUTH_PATH"`
	ValidatePath             string `mapstructure:"VALIDATE_PATH"`
	EInvoicePath             string `mapstructure:"EINVOICE_PATH"`
	EInvoiceCancelPath       string `mapstructure:"EINVOICE_CANCEL_PATH"`
	EWayBillPath             string `mapstructure:"EWAYBILL_PATH"`
	EWayBillPartBPath        string `mapstructure:"EWAYBILL_PARTB_PATH"`
	EWayBillMultiVehiclePath string `mapstructure:"EWAYBILL_MULTI_VEHICLE_PATH"`
	EWayBillPDFPath          string `mapstructure:"EWAYBILL_PDF_PATH"`
	DistancePath             string `mapstructure:"DISTANCE_PATH"`
	ClientID                 string `mapstructure:"CLIENT_ID"`
	ClientSecret             string `mapstructure:"CLIENT_SECRET"`
	Username                 string `mapstructure:"USERNAME"`
	Password                 string `mapstructure:"PASSWORD"`
	APIToken                 string `mapstructure:"API_TOKEN"`
	GSPName                  string `mapstructure:"GSP_NAME"`
	Timeout                  int    `mapstructure:"TIMEOUT"`
	Sandbox                  bool   `mapstructure:"SANDBOX"`
}

type EntitlementsConfig struct {
	JSON string `mapstructure:"JSON"`
}

func Load() (*Config, error) {
	return LoadForProfile(ProfileHTTP)
}

func LoadForProfile(profile Profile) (*Config, error) {
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.AddConfigPath("..")
	viper.AutomaticEnv()

	// Set up key replacer for underscores to dots for nested config
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// First try to read .env file if it exists
	viper.SetConfigName(".env")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read .env: %w", err)
		}
	}

	// Then try to merge .env.local if it exists (overrides .env values)
	viper.SetConfigName(".env.local")
	if err := viper.MergeInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("merge .env.local: %w", err)
		}
	}

	// Reset config name for backward compatibility
	viper.SetConfigName(".env")

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
	_ = viper.BindEnv("SERVER.BASE_URL", "SERVER_BASE_URL", "A2A_SERVER_DOMAIN")
	_ = viper.BindEnv("SERVER.SWAGGER_USE_PKCE", "SERVER_SWAGGER_USE_PKCE")
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
	_ = viper.BindEnv("AWS.SESSION_TOKEN", "AWS_SESSION_TOKEN")
	_ = viper.BindEnv("AWS.ENDPOINT", "AWS_ENDPOINT")
	_ = viper.BindEnv("AWS.WAF.ENABLED", "WAF_ENABLED")
	_ = viper.BindEnv("AWS.WAF.WEB_ACL_ARN", "WAF_WEB_ACL_ARN")
	_ = viper.BindEnv("AWS.WAF.RATE_LIMIT_HEADER", "WAF_RATE_LIMIT_HEADER")
	_ = viper.BindEnv("AWS.WAF.BLOCKED_RESPONSE", "WAF_BLOCKED_RESPONSE")
	_ = viper.BindEnv("AWS.WAF.HEADER_MATCH_COUNT", "WAF_HEADER_MATCH_COUNT")
	_ = viper.BindEnv("SSM.DATABASE_HOST_PARAM", "DATABASE_HOST_SSM_PARAM")
	_ = viper.BindEnv("SECRETS.DATABASE", "DATABASE_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.CREDENTIAL_ENCRYPTION", "CREDENTIAL_ENCRYPTION_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.RAZORPAY", "RAZORPAY_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.LEGACY_JWT", "LEGACY_JWT_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.GOOGLE_OAUTH", "GOOGLE_OAUTH_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.FCM", "FCM_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.APNS", "APNS_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.LLM", "LLM_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.EXA", "EXA_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.GST_LOOKUP", "GST_LOOKUP_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.GST_PROVIDER", "GST_PROVIDER_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.DEEPGRAM", "DEEPGRAM_SECRET_ARN")
	_ = viper.BindEnv("SECRETS.DEEPSEEK", "DEEPSEEK_SECRET_ARN")
	_ = viper.BindEnv("WEBSOCKET.API_ENDPOINT", "WEBSOCKET_API_ENDPOINT")
	_ = viper.BindEnv("WEBSOCKET.CONNECTIONS_TABLE", "WEBSOCKET_CONNECTIONS_TABLE")
	_ = viper.BindEnv("COGNITO.USER_POOL_ID", "COGNITO_USER_POOL_ID")
	_ = viper.BindEnv("COGNITO.CLIENT_ID", "COGNITO_CLIENT_ID")
	_ = viper.BindEnv("COGNITO.DOMAIN", "COGNITO_DOMAIN")
	_ = viper.BindEnv("COGNITO.REGION", "COGNITO_REGION")
	_ = viper.BindEnv("COGNITO.JWKS_REFRESH_RATE", "COGNITO_JWKS_REFRESH_RATE")
	_ = viper.BindEnv("COGNITO.PHONE.USER_POOL_ID", "COGNITO_PHONE_USER_POOL_ID")
	_ = viper.BindEnv("COGNITO.PHONE.CLIENT_ID", "COGNITO_PHONE_CLIENT_ID")
	_ = viper.BindEnv("COGNITO.PHONE.REGION", "COGNITO_PHONE_REGION")
	_ = viper.BindEnv("COGNITO.PHONE.OTP_COOLDOWN_TABLE", "COGNITO_PHONE_OTP_COOLDOWN_TABLE")
	_ = viper.BindEnv("JWT.ACCESS_TOKEN_EXPIRY", "JWT_ACCESS_TOKEN_EXPIRY")
	_ = viper.BindEnv("JWT.REFRESH_TOKEN_EXPIRY", "JWT_REFRESH_TOKEN_EXPIRY")
	_ = viper.BindEnv("S3.BUCKET_LOGOS", "S3_BUCKET_LOGOS")
	_ = viper.BindEnv("S3.BUCKET_INVOICES", "S3_BUCKET_INVOICES")
	_ = viper.BindEnv("S3.BUCKET_PRODUCTS", "S3_BUCKET_PRODUCTS")
	_ = viper.BindEnv("S3.BUCKET_EMAIL_SINK", "S3_BUCKET_EMAIL_SINK")
	_ = viper.BindEnv("S3.BUCKET_DRIVE", "S3_BUCKET_DRIVE")
	_ = viper.BindEnv("RAZORPAY.KEY_ID", "RAZORPAY_KEY_ID", "RAZORPAY_KEY")
	_ = viper.BindEnv("RAZORPAY.KEY_SECRET", "RAZORPAY_KEY_SECRET", "RAZORPAY_SECRET")
	_ = viper.BindEnv("RAZORPAY.WEBHOOK_SECRET", "RAZORPAY_WEBHOOK_SECRET")
	_ = viper.BindEnv("RAZORPAY.BASE_URL", "RAZORPAY_BASE_URL")
	_ = viper.BindEnv("RAZORPAY.TIMEOUT", "RAZORPAY_TIMEOUT")
	_ = viper.BindEnv("FX.PROVIDER", "FX_PROVIDER")
	_ = viper.BindEnv("FX.BASE_URL", "FX_BASE_URL")
	_ = viper.BindEnv("FX.API_KEY", "FX_API_KEY")
	_ = viper.BindEnv("FX.TIMEOUT", "FX_TIMEOUT")
	_ = viper.BindEnv("WHATSAPP.BASE_URL", "WHATSAPP_BASE_URL")
	_ = viper.BindEnv("WHATSAPP.TIMEOUT", "WHATSAPP_TIMEOUT")
	_ = viper.BindEnv("SQS.INVOICE_QUEUE", "SQS_INVOICE_QUEUE")
	_ = viper.BindEnv("SQS.EMAIL_DELIVERY_QUEUE", "SQS_EMAIL_DELIVERY_QUEUE")
	_ = viper.BindEnv("SQS.GST_QUEUE", "SQS_GST_QUEUE")
	_ = viper.BindEnv("SQS.BARGAINING_QUEUE", "SQS_BARGAINING_QUEUE")
	_ = viper.BindEnv("SES.SENDER_EMAIL", "SES_SENDER_EMAIL")
	_ = viper.BindEnv("SES.CONFIGURATION_SET", "SES_CONFIGURATION_SET")
	_ = viper.BindEnv("SES.SENDING_ACCOUNT_ID", "SES_SENDING_ACCOUNT_ID")
	_ = viper.BindEnv("SENTRY.DSN", "SENTRY_DSN")
	_ = viper.BindEnv("SENTRY.SAMPLE_RATE", "SENTRY_SAMPLE_RATE")
	_ = viper.BindEnv("SENTRY.TRACES_SAMPLE_RATE", "SENTRY_TRACES_SAMPLE_RATE")
	_ = viper.BindEnv("SENTRY.ENABLE_TRACING", "SENTRY_ENABLE_TRACING")
	_ = viper.BindEnv("SENTRY.DEBUG", "SENTRY_DEBUG")
	_ = viper.BindEnv("SHIPPING.DEFAULT_PROVIDER", "SHIPPING_DEFAULT_PROVIDER")
	_ = viper.BindEnv("SHIPPING.TIMEOUT", "SHIPPING_TIMEOUT")
	_ = viper.BindEnv("SHIPPING.SHIPROCKET_BASE_URL", "SHIPPING_SHIPROCKET_BASE_URL", "SHIPROCKET_BASE_URL")
	_ = viper.BindEnv("SHIPPING.SHIPROCKET_EMAIL", "SHIPPING_SHIPROCKET_EMAIL", "SHIPROCKET_EMAIL")
	_ = viper.BindEnv("SHIPPING.SHIPROCKET_PASSWORD", "SHIPPING_SHIPROCKET_PASSWORD", "SHIPROCKET_PASSWORD")
	_ = viper.BindEnv("SHIPPING.SHIPROCKET_AUTH_PATH", "SHIPPING_SHIPROCKET_AUTH_PATH")
	_ = viper.BindEnv("SHIPPING.SHIPROCKET_ORDER_PATH", "SHIPPING_SHIPROCKET_ORDER_PATH")
	_ = viper.BindEnv("SHIPPING.SHIPROCKET_ASSIGN_AWB_PATH", "SHIPPING_SHIPROCKET_ASSIGN_AWB_PATH")
	_ = viper.BindEnv("SHIPPING.SHIPROCKET_LABEL_PATH", "SHIPPING_SHIPROCKET_LABEL_PATH")
	_ = viper.BindEnv("SHIPPING.SHIPROCKET_TRACKING_BASE_URL", "SHIPPING_SHIPROCKET_TRACKING_BASE_URL")
	_ = viper.BindEnv("GST.PROVIDER", "GST_PROVIDER")
	_ = viper.BindEnv("GST.BASE_URL", "GST_BASE_URL")
	_ = viper.BindEnv("GST.AUTH_PATH", "GST_AUTH_PATH")
	_ = viper.BindEnv("GST.VALIDATE_PATH", "GST_VALIDATE_PATH")
	_ = viper.BindEnv("GST.EINVOICE_PATH", "GST_EINVOICE_PATH")
	_ = viper.BindEnv("GST.EINVOICE_CANCEL_PATH", "GST_EINVOICE_CANCEL_PATH")
	_ = viper.BindEnv("GST.EWAYBILL_PATH", "GST_EWAYBILL_PATH")
	_ = viper.BindEnv("GST.EWAYBILL_PARTB_PATH", "GST_EWAYBILL_PARTB_PATH")
	_ = viper.BindEnv("GST.EWAYBILL_MULTI_VEHICLE_PATH", "GST_EWAYBILL_MULTI_VEHICLE_PATH")
	_ = viper.BindEnv("GST.EWAYBILL_PDF_PATH", "GST_EWAYBILL_PDF_PATH")
	_ = viper.BindEnv("GST.DISTANCE_PATH", "GST_DISTANCE_PATH")
	_ = viper.BindEnv("GST.CLIENT_ID", "GST_CLIENT_ID")
	_ = viper.BindEnv("GST.CLIENT_SECRET", "GST_CLIENT_SECRET")
	_ = viper.BindEnv("GST.USERNAME", "GST_USERNAME")
	_ = viper.BindEnv("GST.PASSWORD", "GST_PASSWORD")
	_ = viper.BindEnv("GST.API_TOKEN", "GST_API_TOKEN")
	_ = viper.BindEnv("GST.GSP_NAME", "GST_GSP_NAME")
	_ = viper.BindEnv("GST.TIMEOUT", "GST_TIMEOUT")
	_ = viper.BindEnv("GST.SANDBOX", "GST_SANDBOX")
	_ = viper.BindEnv("GST_LOOKUP.BASE_URL", "GST_LOOKUP_BASE_URL")
	_ = viper.BindEnv("GST_LOOKUP.API_KEY", "GST_LOOKUP_API_KEY")
	_ = viper.BindEnv("GST_LOOKUP.TIMEOUT", "GST_LOOKUP_TIMEOUT")
	_ = viper.BindEnv("ENTITLEMENTS.JSON", "ENTITLEMENTS_JSON")
	_ = viper.BindEnv("ALLOWED_ORIGINS", "ALLOWED_ORIGINS")
	_ = viper.BindEnv("LLM.API_KEY", "LLM_API_KEY")
	_ = viper.BindEnv("LLM.API_URL", "LLM_API_URL")
	_ = viper.BindEnv("LLM.MODEL", "LLM_MODEL")
	_ = viper.BindEnv("LLM.TIMEOUT", "LLM_TIMEOUT")
	_ = viper.BindEnv("LLM.EXA_API_KEY", "EXA_API_KEY")
	_ = viper.BindEnv("LLM.EXA_BASE_URL", "EXA_BASE_URL")
	_ = viper.BindEnv("LLM.EXA_TIMEOUT", "EXA_TIMEOUT")
	_ = viper.BindEnv("DEEPGRAM.API_KEY", "DEEPGRAM_API_KEY")
	_ = viper.BindEnv("VOICE_REALTIME.DEEPGRAM_API_KEY", "DEEPGRAM_API_KEY")
	_ = viper.BindEnv("VOICE_REALTIME.DEEPGRAM_VOICE_AGENT_URL", "DEEPGRAM_VOICE_AGENT_URL")
	_ = viper.BindEnv("VOICE_REALTIME.INPUT_ENCODING", "DEEPGRAM_VOICE_INPUT_ENCODING")
	_ = viper.BindEnv("VOICE_REALTIME.INPUT_SAMPLE_RATE", "DEEPGRAM_VOICE_INPUT_SAMPLE_RATE")
	_ = viper.BindEnv("VOICE_REALTIME.OUTPUT_ENCODING", "DEEPGRAM_VOICE_OUTPUT_ENCODING")
	_ = viper.BindEnv("VOICE_REALTIME.OUTPUT_SAMPLE_RATE", "DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE")
	_ = viper.BindEnv("VOICE_REALTIME.LISTEN_MODEL", "DEEPGRAM_VOICE_LISTEN_MODEL")
	_ = viper.BindEnv("VOICE_REALTIME.SPEAK_MODEL", "DEEPGRAM_VOICE_SPEAK_MODEL")
	_ = viper.BindEnv("VOICE_REALTIME.DEEPSEEK_API_KEY", "DEEPSEEK_API_KEY")
	_ = viper.BindEnv("VOICE_REALTIME.DEEPSEEK_BASE_URL", "DEEPSEEK_BASE_URL")
	_ = viper.BindEnv("VOICE_REALTIME.DEEPSEEK_MODEL", "DEEPSEEK_MODEL")
	_ = viper.BindEnv("VOICE_REALTIME.MAX_SESSION_SECONDS", "VOICE_WS_MAX_SESSION_SECONDS")
	_ = viper.BindEnv("VOICE_REALTIME.PING_INTERVAL_SECONDS", "VOICE_WS_PING_INTERVAL_SECONDS")
	_ = viper.BindEnv("VOICE_REALTIME.WRITE_TIMEOUT_SECONDS", "VOICE_WS_WRITE_TIMEOUT_SECONDS")
	_ = viper.BindEnv("VOICE_REALTIME.MAX_FRAME_BYTES", "VOICE_WS_MAX_FRAME_BYTES")
	_ = viper.BindEnv("VOICE_REALTIME.MAX_CONCURRENT_SESSIONS_PER_USER", "VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER")
	_ = viper.BindEnv("CREDENTIALS.ENCRYPTION_KEY", "CREDENTIAL_ENCRYPTION_KEY")
	_ = viper.BindEnv("MCP.SERVER_URL", "MCP_SERVER_URL")
	_ = viper.BindEnv("MCP.TIMEOUT", "MCP_TIMEOUT")
	_ = viper.BindEnv("MCP.INSECURE_SKIP_VERIFY", "MCP_INSECURE_SKIP_VERIFY")
	_ = viper.BindEnv("MCP.TLS_CERT_FILE", "MCP_TLS_CERT_FILE")
	_ = viper.BindEnv("MCP.TLS_KEY_FILE", "MCP_TLS_KEY_FILE")
	_ = viper.BindEnv("MCP.TLS_CA_FILE", "MCP_TLS_CA_FILE")

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	applyFlatEnvFileFallbacks(&cfg)

	if rawAllowedOrigins := viper.GetString("ALLOWED_ORIGINS"); rawAllowedOrigins != "" {
		cfg.AllowedOrigins = parseAllowedOrigins(rawAllowedOrigins)
	}

	if err := ValidateForProfile(&cfg, profile); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	setDefaults(&cfg)

	return &cfg, nil
}

func applyFlatEnvFileFallbacks(cfg *Config) {
	if cfg == nil {
		return
	}

	setIfEmpty(&cfg.Environment, "ENVIRONMENT")
	setIfZeroInt(&cfg.Server.Port, "SERVER_PORT")
	setIfEmpty(&cfg.Server.BaseURL, "SERVER_BASE_URL")

	setIfEmpty(&cfg.Database.Host, "DATABASE_HOST")
	setIfZeroInt(&cfg.Database.Port, "DATABASE_PORT")
	setIfEmpty(&cfg.Database.User, "DATABASE_USER")
	setIfEmpty(&cfg.Database.Password, "DATABASE_PASSWORD")
	setIfEmpty(&cfg.Database.Name, "DATABASE_NAME")
	setIfEmpty(&cfg.Database.SSLMode, "DATABASE_SSL_MODE")

	setIfEmpty(&cfg.AWS.Region, "AWS_REGION")
	setIfEmpty(&cfg.AWS.AccessKey, "AWS_ACCESS_KEY_ID")
	setIfEmpty(&cfg.AWS.SecretKey, "AWS_SECRET_ACCESS_KEY")
	setIfEmpty(&cfg.AWS.SessionToken, "AWS_SESSION_TOKEN")
	setIfEmpty(&cfg.AWS.Endpoint, "AWS_ENDPOINT")

	setIfEmpty(&cfg.SSM.DatabaseHostParam, "DATABASE_HOST_SSM_PARAM")
	setIfEmpty(&cfg.Secrets.Database, "DATABASE_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.CredentialEncryption, "CREDENTIAL_ENCRYPTION_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.Razorpay, "RAZORPAY_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.LegacyJWT, "LEGACY_JWT_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.GoogleOAuth, "GOOGLE_OAUTH_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.FCM, "FCM_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.APNS, "APNS_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.LLM, "LLM_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.Exa, "EXA_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.GSTLookup, "GST_LOOKUP_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.GSTProvider, "GST_PROVIDER_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.Deepgram, "DEEPGRAM_SECRET_ARN")
	setIfEmpty(&cfg.Secrets.DeepSeek, "DEEPSEEK_SECRET_ARN")

	setIfEmpty(&cfg.Cognito.UserPoolID, "COGNITO_USER_POOL_ID")
	setIfEmpty(&cfg.Cognito.ClientID, "COGNITO_CLIENT_ID")
	setIfEmpty(&cfg.Cognito.Domain, "COGNITO_DOMAIN")
	setIfEmpty(&cfg.Cognito.Region, "COGNITO_REGION")
	setIfZeroDuration(&cfg.Cognito.JWKSRefreshRate, "COGNITO_JWKS_REFRESH_RATE")
	setIfEmpty(&cfg.Cognito.Phone.UserPoolID, "COGNITO_PHONE_USER_POOL_ID")
	setIfEmpty(&cfg.Cognito.Phone.ClientID, "COGNITO_PHONE_CLIENT_ID")
	setIfEmpty(&cfg.Cognito.Phone.Region, "COGNITO_PHONE_REGION")
	setIfEmpty(&cfg.Cognito.Phone.OTPCooldownTable, "COGNITO_PHONE_OTP_COOLDOWN_TABLE")

	setIfZeroDuration(&cfg.JWT.AccessTokenExpiry, "JWT_ACCESS_TOKEN_EXPIRY")
	setIfZeroDuration(&cfg.JWT.RefreshTokenExpiry, "JWT_REFRESH_TOKEN_EXPIRY")

	setIfEmpty(&cfg.S3.BucketLogos, "S3_BUCKET_LOGOS")
	setIfEmpty(&cfg.S3.BucketInvoices, "S3_BUCKET_INVOICES")
	setIfEmpty(&cfg.S3.BucketProducts, "S3_BUCKET_PRODUCTS")
	setIfEmpty(&cfg.S3.BucketEmailSink, "S3_BUCKET_EMAIL_SINK")
	setIfEmpty(&cfg.S3.BucketDrive, "S3_BUCKET_DRIVE")

	setIfEmpty(&cfg.Razorpay.KeyID, "RAZORPAY_KEY_ID")
	setIfEmpty(&cfg.Razorpay.KeySecret, "RAZORPAY_KEY_SECRET")
	setIfEmpty(&cfg.Razorpay.WebhookSecret, "RAZORPAY_WEBHOOK_SECRET")
	setIfEmpty(&cfg.Razorpay.BaseURL, "RAZORPAY_BASE_URL")
	setIfZeroInt(&cfg.Razorpay.Timeout, "RAZORPAY_TIMEOUT")

	setIfEmpty(&cfg.SQS.InvoiceQueue, "SQS_INVOICE_QUEUE")
	setIfEmpty(&cfg.SQS.EmailDeliveryQueue, "SQS_EMAIL_DELIVERY_QUEUE")
	setIfEmpty(&cfg.SQS.GSTQueue, "SQS_GST_QUEUE")
	setIfEmpty(&cfg.SQS.BargainingQueue, "SQS_BARGAINING_QUEUE")
	setIfEmpty(&cfg.SES.SenderEmail, "SES_SENDER_EMAIL")
	setIfEmpty(&cfg.SES.ConfigurationSet, "SES_CONFIGURATION_SET")
	setIfEmpty(&cfg.SES.SendingAccountID, "SES_SENDING_ACCOUNT_ID")

	setIfEmpty(&cfg.LLM.APIKey, "LLM_API_KEY")
	setIfEmpty(&cfg.LLM.APIURL, "LLM_API_URL")
	setIfEmpty(&cfg.LLM.Model, "LLM_MODEL")
	setIfZeroInt(&cfg.LLM.Timeout, "LLM_TIMEOUT")
	setIfEmpty(&cfg.LLM.ExaAPIKey, "EXA_API_KEY")
	setIfEmpty(&cfg.LLM.ExaBaseURL, "EXA_BASE_URL")
	setIfZeroInt(&cfg.LLM.ExaTimeout, "EXA_TIMEOUT")

	setIfEmpty(&cfg.Credentials.EncryptionKey, "CREDENTIAL_ENCRYPTION_KEY")
}

func setIfEmpty(target *string, key string) {
	if target == nil || strings.TrimSpace(*target) != "" {
		return
	}
	if value := strings.TrimSpace(viper.GetString(key)); value != "" {
		*target = value
	}
}

func setIfZeroInt(target *int, key string) {
	if target == nil || *target != 0 || !viper.IsSet(key) {
		return
	}
	*target = viper.GetInt(key)
}

func setIfZeroDuration(target *time.Duration, key string) {
	if target == nil || *target != 0 || !viper.IsSet(key) {
		return
	}
	*target = viper.GetDuration(key)
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
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if cfg.Database.SSLMode == "" {
		if isProductionEnv(cfg.Environment) {
			cfg.Database.SSLMode = "require"
		} else {
			cfg.Database.SSLMode = "disable"
		}
	}
	if cfg.Redis.Port == 0 {
		cfg.Redis.Port = 6379
	}
	if cfg.AWS.Region == "" {
		cfg.AWS.Region = "us-east-1"
	}
	if cfg.WebSocket.ConnectionsTable == "" {
		cfg.WebSocket.ConnectionsTable = "invoice-backend-ws-connections"
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
	if cfg.LLM.Timeout == 0 {
		cfg.LLM.Timeout = 60
	}
	if cfg.LLM.ExaBaseURL == "" {
		cfg.LLM.ExaBaseURL = "https://api.exa.ai/search"
	}
	if cfg.LLM.ExaTimeout == 0 {
		cfg.LLM.ExaTimeout = 12
	}
	cfg.VoiceRealtime = cfg.VoiceRealtime.WithDefaults(cfg.Deepgram)
	if cfg.S3.BucketDrive == "" {
		cfg.S3.BucketDrive = cfg.S3.BucketInvoices
	}
	if cfg.Razorpay.Timeout == 0 {
		cfg.Razorpay.Timeout = 30
	}
	if cfg.FX.Provider == "" {
		cfg.FX.Provider = "open-er-api"
	}
	if cfg.FX.BaseURL == "" {
		cfg.FX.BaseURL = "https://open.er-api.com/v6/latest"
	}
	if cfg.FX.Timeout == 0 {
		cfg.FX.Timeout = 15
	}
	if cfg.WhatsApp.BaseURL == "" {
		cfg.WhatsApp.BaseURL = "https://graph.facebook.com/v20.0"
	}
	if cfg.WhatsApp.Timeout == 0 {
		cfg.WhatsApp.Timeout = 15
	}
	if cfg.Shipping.DefaultProvider == "" {
		cfg.Shipping.DefaultProvider = "manual"
	}
	if cfg.Shipping.Timeout == 0 {
		cfg.Shipping.Timeout = 30
	}
	if cfg.Shipping.ShiprocketBaseURL == "" {
		cfg.Shipping.ShiprocketBaseURL = "https://apiv2.shiprocket.in/v1/external"
	}
	if cfg.Shipping.ShiprocketAuthPath == "" {
		cfg.Shipping.ShiprocketAuthPath = "/auth/login"
	}
	if cfg.Shipping.ShiprocketOrderPath == "" {
		cfg.Shipping.ShiprocketOrderPath = "/orders/create/adhoc"
	}
	if cfg.Shipping.ShiprocketAssignAWBPath == "" {
		cfg.Shipping.ShiprocketAssignAWBPath = "/courier/assign/awb"
	}
	if cfg.Shipping.ShiprocketLabelPath == "" {
		cfg.Shipping.ShiprocketLabelPath = "/courier/generate/label"
	}
	if cfg.GSTLookup.Timeout == 0 {
		cfg.GSTLookup.Timeout = 15
	}
	if cfg.AWS.WAF.HeaderMatchCount == 0 {
		cfg.AWS.WAF.HeaderMatchCount = 100
	}
	if cfg.AWS.WAF.BlockedResponse == "" {
		cfg.AWS.WAF.BlockedResponse = "rate limit exceeded"
	}
}

func parseAllowedOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func isProductionEnv(env string) bool {
	normalized := strings.ToLower(strings.TrimSpace(env))
	return normalized == "prod" || normalized == "production"
}

const a2aBasePath = "/api/v1/a2a"

func (s ServerConfig) ResolveBaseURL() string {
	baseURL := strings.TrimSpace(s.BaseURL)
	if baseURL == "" {
		return ""
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return ""
	}
	return baseURL
}

func (s ServerConfig) A2AMessageEndpoint() string {
	baseURL := s.ResolveBaseURL()
	if baseURL == "" {
		return a2aBasePath
	}
	return baseURL + a2aBasePath
}

func (c CognitoConfig) ResolveHostedUIDomain() string {
	domain := strings.TrimSpace(c.Domain)
	if domain == "" {
		return ""
	}

	domain = strings.TrimRight(domain, "/")
	if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
		return domain
	}

	return "https://" + domain
}
