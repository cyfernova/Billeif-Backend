package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type LambdaVoiceConfig struct {
	Environment                 string
	LogLevel                    string
	LogFormat                   string
	AWS                         AWSConfig
	Cognito                     CognitoConfig
	WebSocket                   WebSocketConfig
	VoiceRealtime               VoiceRealtimeConfig
	VoiceSessionsTable          string
	SessionWorkerFunctionName   string
	EventPollIntervalMS         int
	EventTTLSeconds             int
	MaxOutboundChunkBytes       int
	ProviderReadyTimeoutSeconds int
}

func LoadLambdaVoiceConfig(requireWorkerFunction bool) (*LambdaVoiceConfig, error) {
	cfg := &LambdaVoiceConfig{
		Environment: requireEnv("ENVIRONMENT"),
		LogLevel:    requireEnv("LOG_LEVEL"),
		LogFormat:   requireEnv("LOG_FORMAT"),
		AWS: AWSConfig{
			Region:    requireEnv("AWS_REGION"),
			AccessKey: strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
			SecretKey: strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
			Endpoint:  strings.TrimSpace(os.Getenv("AWS_ENDPOINT")),
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
			DeepgramAPIKey:               requireEnv("DEEPGRAM_API_KEY"),
			DeepgramVoiceAgentURL:        requireEnv("DEEPGRAM_VOICE_AGENT_URL"),
			InputEncoding:                requireEnv("DEEPGRAM_VOICE_INPUT_ENCODING"),
			InputSampleRate:              requirePositiveIntEnv("DEEPGRAM_VOICE_INPUT_SAMPLE_RATE"),
			OutputEncoding:               requireEnv("DEEPGRAM_VOICE_OUTPUT_ENCODING"),
			OutputSampleRate:             requirePositiveIntEnv("DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE"),
			ListenModel:                  requireEnv("DEEPGRAM_VOICE_LISTEN_MODEL"),
			SpeakModel:                   requireEnv("DEEPGRAM_VOICE_SPEAK_MODEL"),
			DeepSeekAPIKey:               requireEnv("DEEPSEEK_API_KEY"),
			DeepSeekBaseURL:              requireEnv("DEEPSEEK_BASE_URL"),
			DeepSeekModel:                requireEnv("DEEPSEEK_MODEL"),
			MaxSessionSeconds:            requirePositiveIntEnv("VOICE_WS_MAX_SESSION_SECONDS"),
			PingIntervalSeconds:          requirePositiveIntEnv("VOICE_WS_PING_INTERVAL_SECONDS"),
			WriteTimeoutSeconds:          requirePositiveIntEnv("VOICE_WS_WRITE_TIMEOUT_SECONDS"),
			MaxFrameBytes:                requirePositiveIntEnv("VOICE_WS_MAX_FRAME_BYTES"),
			MaxConcurrentSessionsPerUser: requirePositiveIntEnv("VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER"),
		},
		VoiceSessionsTable:          requireEnv("VOICE_SESSIONS_TABLE"),
		EventPollIntervalMS:         requirePositiveIntEnv("VOICE_WS_EVENT_POLL_INTERVAL_MS"),
		EventTTLSeconds:             requirePositiveIntEnv("VOICE_WS_EVENT_TTL_SECONDS"),
		MaxOutboundChunkBytes:       requirePositiveIntEnv("VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES"),
		ProviderReadyTimeoutSeconds: requirePositiveIntEnv("VOICE_WS_PROVIDER_READY_TIMEOUT_SECONDS"),
	}

	if requireWorkerFunction {
		cfg.SessionWorkerFunctionName = requireEnv("VOICE_SESSION_WORKER_FUNCTION_NAME")
	} else {
		cfg.SessionWorkerFunctionName = strings.TrimSpace(os.Getenv("VOICE_SESSION_WORKER_FUNCTION_NAME"))
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
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
	if err := c.VoiceRealtime.ValidateForRuntime(); err != nil {
		return err
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
