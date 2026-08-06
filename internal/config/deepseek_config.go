package config

// DeepSeekConfig preserves the OpenAI-compatible provider configuration used
// by non-voice application runtimes.
type DeepSeekConfig struct {
	APIKey  string `mapstructure:"API_KEY"`
	BaseURL string `mapstructure:"BASE_URL"`
	Model   string `mapstructure:"MODEL"`
}
