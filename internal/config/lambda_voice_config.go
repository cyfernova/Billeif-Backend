package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type LambdaVoiceConfig struct {
	Enabled                     bool
	Environment                 string
	LogLevel                    string
	LogFormat                   string
	AWS                         AWSConfig
	Cognito                     CognitoConfig
	WebSocket                   WebSocketConfig
	VoiceRealtime               VoiceRealtimeConfig
	MCP                         MCPConfig
	VoiceSessionsTable          string
	SessionWorkerFunctionName   string
	EventPollIntervalMS         int
	EventTTLSeconds             int
	MaxOutboundChunkBytes       int
	ProviderReadyTimeoutSeconds int
	DeepgramSecretIdentifier    string
	DeepSeekSecretIdentifier    string
}

func LoadLambdaVoiceConfig(requireWorkerFunction bool) (*LambdaVoiceConfig, error) {
	region := requireEnv("AWS_REGION")
	cfg := &LambdaVoiceConfig{
		Enabled:     boolEnv("VOICE_ENABLED"),
		Environment: requireEnv("ENVIRONMENT"),
		LogLevel:    requireEnv("LOG_LEVEL"),
		LogFormat:   requireEnv("LOG_FORMAT"),
		AWS: AWSConfig{
			Region:       region,
			AccessKey:    strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
			SecretKey:    strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
			SessionToken: strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN")),
			Endpoint:     strings.TrimSpace(os.Getenv("AWS_ENDPOINT")),
		},
		Cognito: CognitoConfig{
			UserPoolID:      requireEnv("COGNITO_USER_POOL_ID"),
			ClientID:        requireEnv("COGNITO_CLIENT_ID"),
			Region:          requireEnv("COGNITO_REGION"),
			JWKSRefreshRate: 10 * time.Minute,
			Phone: CognitoPhoneConfig{
				UserPoolID: strings.TrimSpace(os.Getenv("COGNITO_PHONE_USER_POOL_ID")),
				ClientID:   strings.TrimSpace(os.Getenv("COGNITO_PHONE_CLIENT_ID")),
				Region:     strings.TrimSpace(os.Getenv("COGNITO_PHONE_REGION")),
			},
		},
		WebSocket: WebSocketConfig{
			APIEndpoint:      requireEnv("WEBSOCKET_API_ENDPOINT"),
			ConnectionsTable: requireEnv("WEBSOCKET_CONNECTIONS_TABLE"),
		},
		VoiceRealtime: VoiceRealtimeConfig{
			DeepgramAPIKey:               strings.TrimSpace(os.Getenv("DEEPGRAM_API_KEY")),
			DeepgramVoiceAgentURL:        requireEnv("DEEPGRAM_VOICE_AGENT_URL"),
			InputEncoding:                requireEnv("DEEPGRAM_VOICE_INPUT_ENCODING"),
			InputSampleRate:              requirePositiveIntEnv("DEEPGRAM_VOICE_INPUT_SAMPLE_RATE"),
			OutputEncoding:               requireEnv("DEEPGRAM_VOICE_OUTPUT_ENCODING"),
			OutputSampleRate:             requirePositiveIntEnv("DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE"),
			ListenModel:                  requireEnv("DEEPGRAM_VOICE_LISTEN_MODEL"),
			SpeakModel:                   requireEnv("DEEPGRAM_VOICE_SPEAK_MODEL"),
			DeepSeekAPIKey:               strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
			DeepSeekBaseURL:              requireEnv("DEEPSEEK_BASE_URL"),
			DeepSeekModel:                requireEnv("DEEPSEEK_MODEL"),
			MaxSessionSeconds:            requirePositiveIntEnv("VOICE_WS_MAX_SESSION_SECONDS"),
			PingIntervalSeconds:          requirePositiveIntEnv("VOICE_WS_PING_INTERVAL_SECONDS"),
			WriteTimeoutSeconds:          requirePositiveIntEnv("VOICE_WS_WRITE_TIMEOUT_SECONDS"),
			MaxFrameBytes:                requirePositiveIntEnv("VOICE_WS_MAX_FRAME_BYTES"),
			MaxConcurrentSessionsPerUser: requirePositiveIntEnv("VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER"),
		},
		MCP: MCPConfig{
			ServerURL:          strings.TrimSpace(os.Getenv("MCP_SERVER_URL")),
			Timeout:            durationEnv("MCP_TIMEOUT", DefaultMCPConfig().Timeout),
			InsecureSkipVerify: boolEnv("MCP_INSECURE_SKIP_VERIFY"),
			TLSCertFile:        strings.TrimSpace(os.Getenv("MCP_TLS_CERT_FILE")),
			TLSKeyFile:         strings.TrimSpace(os.Getenv("MCP_TLS_KEY_FILE")),
			TLSCACertFile:      strings.TrimSpace(os.Getenv("MCP_TLS_CA_FILE")),
		},
		VoiceSessionsTable:          requireEnv("VOICE_SESSIONS_TABLE"),
		EventPollIntervalMS:         requirePositiveIntEnv("VOICE_WS_EVENT_POLL_INTERVAL_MS"),
		EventTTLSeconds:             requirePositiveIntEnv("VOICE_WS_EVENT_TTL_SECONDS"),
		MaxOutboundChunkBytes:       requirePositiveIntEnv("VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES"),
		ProviderReadyTimeoutSeconds: requirePositiveIntEnv("VOICE_WS_PROVIDER_READY_TIMEOUT_SECONDS"),
		DeepgramSecretIdentifier:    strings.TrimSpace(os.Getenv("DEEPGRAM_SECRET_ARN")),
		DeepSeekSecretIdentifier:    strings.TrimSpace(os.Getenv("DEEPSEEK_SECRET_ARN")),
	}

	if requireWorkerFunction {
		cfg.SessionWorkerFunctionName = requireEnv("VOICE_SESSION_WORKER_FUNCTION_NAME")
	} else {
		cfg.SessionWorkerFunctionName = strings.TrimSpace(os.Getenv("VOICE_SESSION_WORKER_FUNCTION_NAME"))
	}

	validationCfg := cfg
	if requireWorkerFunction {
		copyForWebSocket := *cfg
		copyForWebSocket.VoiceRealtime.DeepgramAPIKey = "not-required-by-websocket"
		copyForWebSocket.VoiceRealtime.DeepSeekAPIKey = "not-required-by-websocket"
		validationCfg = &copyForWebSocket
	}
	if err := validationCfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func ResolveLambdaVoiceRuntime(ctx context.Context, cfg *LambdaVoiceConfig, client SecretsManagerAPI) error {
	if cfg == nil {
		return &ConfigurationError{Resource: "lambda voice config", Reason: "config is required"}
	}
	needsDeepgram := strings.TrimSpace(cfg.VoiceRealtime.DeepgramAPIKey) == ""
	needsDeepSeek := strings.TrimSpace(cfg.VoiceRealtime.DeepSeekAPIKey) == ""
	if !needsDeepgram && !needsDeepSeek {
		return cfg.Validate()
	}
	identifiers := []string{cfg.DeepgramSecretIdentifier, cfg.DeepSeekSecretIdentifier}
	if client == nil {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWS.Region))
		if err != nil {
			return &ResolutionError{Resource: "AWS SDK configuration"}
		}
		client = secretsmanager.NewFromConfig(awsCfg)
	}
	resolver, err := NewSecretResolver(client, identifiers, 5*time.Minute, time.Now)
	if err != nil {
		return err
	}
	if needsDeepgram {
		cfg.VoiceRealtime.DeepgramAPIKey, err = resolver.JSONField(ctx, cfg.DeepgramSecretIdentifier, "api_key")
		if err != nil {
			return err
		}
	}
	if needsDeepSeek {
		cfg.VoiceRealtime.DeepSeekAPIKey, err = resolver.JSONField(ctx, cfg.DeepSeekSecretIdentifier, "api_key")
		if err != nil {
			return err
		}
	}
	return cfg.Validate()
}

func (c *LambdaVoiceConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("lambda voice config is required")
	}
	required := map[string]string{
		"ENVIRONMENT":                 c.Environment,
		"LOG_LEVEL":                   c.LogLevel,
		"LOG_FORMAT":                  c.LogFormat,
		"AWS_REGION":                  c.AWS.Region,
		"COGNITO_USER_POOL_ID":        c.Cognito.UserPoolID,
		"COGNITO_CLIENT_ID":           c.Cognito.ClientID,
		"COGNITO_REGION":              c.Cognito.Region,
		"WEBSOCKET_API_ENDPOINT":      c.WebSocket.APIEndpoint,
		"WEBSOCKET_CONNECTIONS_TABLE": c.WebSocket.ConnectionsTable,
		"VOICE_SESSIONS_TABLE":        c.VoiceSessionsTable,
	}
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required for lambda realtime voice", key)
		}
	}
	voice := c.VoiceRealtime
	if strings.TrimSpace(voice.DeepgramAPIKey) == "" && strings.TrimSpace(c.DeepgramSecretIdentifier) != "" {
		voice.DeepgramAPIKey = "configured-by-secret-identifier"
	}
	if strings.TrimSpace(voice.DeepSeekAPIKey) == "" && strings.TrimSpace(c.DeepSeekSecretIdentifier) != "" {
		voice.DeepSeekAPIKey = "configured-by-secret-identifier"
	}
	if err := voice.ValidateForRuntime(); err != nil {
		return err
	}
	if strings.TrimSpace(c.MCP.ServerURL) != "" && c.MCP.Timeout <= 0 {
		return fmt.Errorf("MCP_TIMEOUT must be positive")
	}
	if c.EventPollIntervalMS <= 0 {
		return fmt.Errorf("VOICE_WS_EVENT_POLL_INTERVAL_MS must be positive")
	}
	if c.EventTTLSeconds <= 0 {
		return fmt.Errorf("VOICE_WS_EVENT_TTL_SECONDS must be positive")
	}
	if c.MaxOutboundChunkBytes <= 0 {
		return fmt.Errorf("VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES must be positive")
	}
	if c.ProviderReadyTimeoutSeconds <= 0 {
		return fmt.Errorf("VOICE_WS_PROVIDER_READY_TIMEOUT_SECONDS must be positive")
	}
	return nil
}

func requireEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func requirePositiveIntEnv(key string) int {
	value := strings.TrimSpace(os.Getenv(key))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func boolEnv(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes"
}
