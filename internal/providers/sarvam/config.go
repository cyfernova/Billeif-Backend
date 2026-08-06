package sarvam

import "strings"

const (
	DefaultBaseURL    = "https://api.sarvam.ai"
	DefaultSTTModel   = "saaras:v3"
	DefaultLLMModel   = "sarvam-105b"
	DefaultTTSModel   = "bulbul:v3"
	DefaultTTSSpeaker = "shubh"

	APIKeyHeader = "api-subscription-key"

	MaxRequestBytes  int64 = 1 << 20
	MaxResponseBytes int64 = 32 << 20
	TTSMaxCharacters       = 3500
)

type Config struct {
	APIKey     string
	BaseURL    string
	STTModel   string
	LLMModel   string
	TTSModel   string
	TTSSpeaker string
}

func (c Config) withDefaults() Config {
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.STTModel = strings.TrimSpace(c.STTModel)
	c.LLMModel = strings.TrimSpace(c.LLMModel)
	c.TTSModel = strings.TrimSpace(c.TTSModel)
	c.TTSSpeaker = strings.TrimSpace(c.TTSSpeaker)
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.STTModel == "" {
		c.STTModel = DefaultSTTModel
	}
	if c.LLMModel == "" {
		c.LLMModel = DefaultLLMModel
	}
	if c.TTSModel == "" {
		c.TTSModel = DefaultTTSModel
	}
	if c.TTSSpeaker == "" {
		c.TTSSpeaker = DefaultTTSSpeaker
	}
	return c
}
