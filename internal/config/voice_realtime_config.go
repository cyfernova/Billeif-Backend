package config

import (
	"fmt"
	"strings"
)

const (
	defaultDeepgramVoiceAgentURL = "wss://agent.deepgram.com/v1/agent/converse"
	defaultDeepSeekBaseURL       = "https://api.deepseek.com"
	defaultDeepSeekModel         = "deepseek-v4-flash"
)

type VoiceRealtimeConfig struct {
	DeepgramAPIKey               string `mapstructure:"DEEPGRAM_API_KEY"`
	DeepgramVoiceAgentURL        string `mapstructure:"DEEPGRAM_VOICE_AGENT_URL"`
	InputEncoding                string `mapstructure:"INPUT_ENCODING"`
	InputSampleRate              int    `mapstructure:"INPUT_SAMPLE_RATE"`
	OutputEncoding               string `mapstructure:"OUTPUT_ENCODING"`
	OutputSampleRate             int    `mapstructure:"OUTPUT_SAMPLE_RATE"`
	ListenModel                  string `mapstructure:"LISTEN_MODEL"`
	SpeakModel                   string `mapstructure:"SPEAK_MODEL"`
	DeepSeekAPIKey               string `mapstructure:"DEEPSEEK_API_KEY"`
	DeepSeekBaseURL              string `mapstructure:"DEEPSEEK_BASE_URL"`
	DeepSeekModel                string `mapstructure:"DEEPSEEK_MODEL"`
	MaxSessionSeconds            int    `mapstructure:"MAX_SESSION_SECONDS"`
	PingIntervalSeconds          int    `mapstructure:"PING_INTERVAL_SECONDS"`
	WriteTimeoutSeconds          int    `mapstructure:"WRITE_TIMEOUT_SECONDS"`
	MaxFrameBytes                int    `mapstructure:"MAX_FRAME_BYTES"`
	MaxConcurrentSessionsPerUser int    `mapstructure:"MAX_CONCURRENT_SESSIONS_PER_USER"`
}

func (c VoiceRealtimeConfig) WithDefaults(deepgram DeepgramConfig) VoiceRealtimeConfig {
	if strings.TrimSpace(c.DeepgramAPIKey) == "" {
		c.DeepgramAPIKey = strings.TrimSpace(deepgram.APIKey)
	}
	if strings.TrimSpace(c.DeepgramVoiceAgentURL) == "" {
		c.DeepgramVoiceAgentURL = defaultDeepgramVoiceAgentURL
	}
	if strings.TrimSpace(c.InputEncoding) == "" {
		c.InputEncoding = "linear16"
	}
	if c.InputSampleRate == 0 {
		c.InputSampleRate = 24000
	}
	if strings.TrimSpace(c.OutputEncoding) == "" {
		c.OutputEncoding = "linear16"
	}
	if c.OutputSampleRate == 0 {
		c.OutputSampleRate = 24000
	}
	if strings.TrimSpace(c.ListenModel) == "" {
		c.ListenModel = "nova-3"
	}
	if strings.TrimSpace(c.SpeakModel) == "" {
		c.SpeakModel = "aura-2-thalia-en"
	}
	if strings.TrimSpace(c.DeepSeekBaseURL) == "" {
		c.DeepSeekBaseURL = defaultDeepSeekBaseURL
	}
	if strings.TrimSpace(c.DeepSeekModel) == "" {
		c.DeepSeekModel = defaultDeepSeekModel
	}
	if c.MaxSessionSeconds == 0 {
		c.MaxSessionSeconds = 900
	}
	if c.PingIntervalSeconds == 0 {
		c.PingIntervalSeconds = 20
	}
	if c.WriteTimeoutSeconds == 0 {
		c.WriteTimeoutSeconds = 5
	}
	if c.MaxFrameBytes == 0 {
		c.MaxFrameBytes = 32768
	}
	if c.MaxConcurrentSessionsPerUser == 0 {
		c.MaxConcurrentSessionsPerUser = 1
	}
	return c
}

func (c VoiceRealtimeConfig) DeepSeekChatCompletionsURL() string {
	baseURL := strings.TrimRight(strings.TrimSpace(c.DeepSeekBaseURL), "/")
	if baseURL == "" {
		baseURL = defaultDeepSeekBaseURL
	}
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	return baseURL + "/chat/completions"
}

func (c VoiceRealtimeConfig) ValidateForRuntime() error {
	if strings.TrimSpace(c.DeepgramAPIKey) == "" {
		return fmt.Errorf("DEEPGRAM_API_KEY is required for realtime voice")
	}
	if strings.TrimSpace(c.DeepgramVoiceAgentURL) == "" {
		return fmt.Errorf("DEEPGRAM_VOICE_AGENT_URL is required for realtime voice")
	}
	if strings.TrimSpace(c.DeepSeekAPIKey) == "" {
		return fmt.Errorf("DEEPSEEK_API_KEY is required for realtime voice")
	}
	if c.MaxFrameBytes <= 0 {
		return fmt.Errorf("VOICE_WS_MAX_FRAME_BYTES must be positive")
	}
	if c.MaxSessionSeconds <= 0 {
		return fmt.Errorf("VOICE_WS_MAX_SESSION_SECONDS must be positive")
	}
	return nil
}
