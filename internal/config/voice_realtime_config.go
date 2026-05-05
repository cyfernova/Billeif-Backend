package config

import (
	"fmt"
	"strings"
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
	return c
}

func (c VoiceRealtimeConfig) DeepSeekChatCompletionsURL() string {
	baseURL := strings.TrimRight(strings.TrimSpace(c.DeepSeekBaseURL), "/")
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
	if strings.TrimSpace(c.ListenModel) == "" {
		return fmt.Errorf("DEEPGRAM_VOICE_LISTEN_MODEL is required for realtime voice")
	}
	if strings.TrimSpace(c.SpeakModel) == "" {
		return fmt.Errorf("DEEPGRAM_VOICE_SPEAK_MODEL is required for realtime voice")
	}
	if strings.TrimSpace(c.InputEncoding) == "" {
		return fmt.Errorf("DEEPGRAM_VOICE_INPUT_ENCODING is required for realtime voice")
	}
	if c.InputSampleRate <= 0 {
		return fmt.Errorf("DEEPGRAM_VOICE_INPUT_SAMPLE_RATE must be positive")
	}
	if strings.TrimSpace(c.OutputEncoding) == "" {
		return fmt.Errorf("DEEPGRAM_VOICE_OUTPUT_ENCODING is required for realtime voice")
	}
	if c.OutputSampleRate <= 0 {
		return fmt.Errorf("DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE must be positive")
	}
	if strings.TrimSpace(c.DeepSeekAPIKey) == "" {
		return fmt.Errorf("DEEPSEEK_API_KEY is required for realtime voice")
	}
	if strings.TrimSpace(c.DeepSeekBaseURL) == "" {
		return fmt.Errorf("DEEPSEEK_BASE_URL is required for realtime voice")
	}
	if strings.TrimSpace(c.DeepSeekModel) == "" {
		return fmt.Errorf("DEEPSEEK_MODEL is required for realtime voice")
	}
	if c.MaxFrameBytes <= 0 {
		return fmt.Errorf("VOICE_WS_MAX_FRAME_BYTES must be positive")
	}
	if c.MaxSessionSeconds <= 0 {
		return fmt.Errorf("VOICE_WS_MAX_SESSION_SECONDS must be positive")
	}
	if c.PingIntervalSeconds <= 0 {
		return fmt.Errorf("VOICE_WS_PING_INTERVAL_SECONDS must be positive")
	}
	if c.WriteTimeoutSeconds <= 0 {
		return fmt.Errorf("VOICE_WS_WRITE_TIMEOUT_SECONDS must be positive")
	}
	if c.MaxConcurrentSessionsPerUser <= 0 {
		return fmt.Errorf("VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER must be positive")
	}
	return nil
}
