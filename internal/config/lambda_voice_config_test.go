package config

import (
	"testing"
)

func TestLoadLambdaVoiceConfigRequiresVoiceEnv(t *testing.T) {
	clearLambdaVoiceEnv(t)
	t.Setenv("ENVIRONMENT", "dev")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("COGNITO_USER_POOL_ID", "pool")
	t.Setenv("COGNITO_CLIENT_ID", "client")
	t.Setenv("COGNITO_REGION", "us-east-1")
	t.Setenv("WEBSOCKET_API_ENDPOINT", "https://example.execute-api.us-east-1.amazonaws.com/dev")
	t.Setenv("WEBSOCKET_CONNECTIONS_TABLE", "connections")
	t.Setenv("VOICE_SESSIONS_TABLE", "voice-sessions")

	_, err := LoadLambdaVoiceConfig(true)
	if err == nil {
		t.Fatal("expected missing voice provider config to fail")
	}
}

func TestLoadLambdaVoiceConfigAcceptsExplicitVoiceEnv(t *testing.T) {
	setValidLambdaVoiceEnv(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "session-token")
	t.Setenv("DEEPGRAM_API_KEY", "dg")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek")
	t.Setenv("MCP_SERVER_URL", "https://mcp.example.test")
	t.Setenv("MCP_TIMEOUT", "2s")

	cfg, err := LoadLambdaVoiceConfig(true)
	if err != nil {
		t.Fatalf("expected explicit voice config to load: %v", err)
	}
	if cfg.VoiceRealtime.InputSampleRate != 24000 {
		t.Fatalf("unexpected input sample rate: %d", cfg.VoiceRealtime.InputSampleRate)
	}
	if cfg.SessionWorkerFunctionName != "voice-worker" {
		t.Fatalf("unexpected worker function name: %s", cfg.SessionWorkerFunctionName)
	}
	if cfg.AWS.SessionToken != "session-token" {
		t.Fatalf("unexpected AWS session token: %s", cfg.AWS.SessionToken)
	}
	if cfg.MCP.ServerURL != "https://mcp.example.test" {
		t.Fatalf("unexpected MCP server URL: %s", cfg.MCP.ServerURL)
	}
	if cfg.MCP.Timeout.String() != "2s" {
		t.Fatalf("unexpected MCP timeout: %s", cfg.MCP.Timeout)
	}
}

func TestLoadLambdaVoiceConfigWithIdentifiersDoesNotFetchAWS(t *testing.T) {
	clearLambdaVoiceEnv(t)
	setValidLambdaVoiceEnv(t)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("DEEPGRAM_SECRET_ARN", "configured-deepgram-secret")
	t.Setenv("DEEPSEEK_SECRET_ARN", "configured-deepseek-secret")

	cfg, err := LoadLambdaVoiceConfig(true)
	if err != nil {
		t.Fatalf("identifier-only parsing unexpectedly fetched AWS: %v", err)
	}
	if cfg.VoiceRealtime.DeepgramAPIKey != "" || cfg.VoiceRealtime.DeepSeekAPIKey != "" {
		t.Fatal("config parsing must not resolve secret values")
	}
}

func setValidLambdaVoiceEnv(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"ENVIRONMENT":                               "dev",
		"LOG_LEVEL":                                 "info",
		"LOG_FORMAT":                                "json",
		"AWS_REGION":                                "us-east-1",
		"COGNITO_USER_POOL_ID":                      "pool",
		"COGNITO_CLIENT_ID":                         "client",
		"COGNITO_REGION":                            "us-east-1",
		"WEBSOCKET_API_ENDPOINT":                    "https://example.execute-api.us-east-1.amazonaws.com/dev",
		"WEBSOCKET_CONNECTIONS_TABLE":               "connections",
		"VOICE_SESSIONS_TABLE":                      "voice-sessions",
		"VOICE_SESSION_WORKER_FUNCTION_NAME":        "voice-worker",
		"DEEPGRAM_VOICE_AGENT_URL":                  "wss://voice.example.test",
		"DEEPGRAM_VOICE_INPUT_ENCODING":             "linear16",
		"DEEPGRAM_VOICE_INPUT_SAMPLE_RATE":          "24000",
		"DEEPGRAM_VOICE_OUTPUT_ENCODING":            "linear16",
		"DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE":         "24000",
		"DEEPGRAM_VOICE_LISTEN_MODEL":               "listen",
		"DEEPGRAM_VOICE_SPEAK_MODEL":                "speak",
		"DEEPSEEK_BASE_URL":                         "https://deepseek.example.test",
		"DEEPSEEK_MODEL":                            "model",
		"VOICE_WS_MAX_SESSION_SECONDS":              "840",
		"VOICE_WS_PING_INTERVAL_SECONDS":            "20",
		"VOICE_WS_WRITE_TIMEOUT_SECONDS":            "5",
		"VOICE_WS_MAX_FRAME_BYTES":                  "16000",
		"VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER": "1",
		"VOICE_WS_EVENT_POLL_INTERVAL_MS":           "25",
		"VOICE_WS_EVENT_TTL_SECONDS":                "900",
		"VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES":         "12000",
		"VOICE_WS_PROVIDER_READY_TIMEOUT_SECONDS":   "10",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func clearLambdaVoiceEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"VOICE_SESSION_WORKER_FUNCTION_NAME",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"DEEPGRAM_API_KEY",
		"DEEPGRAM_SECRET_ARN",
		"DEEPGRAM_VOICE_AGENT_URL",
		"DEEPGRAM_VOICE_INPUT_ENCODING",
		"DEEPGRAM_VOICE_INPUT_SAMPLE_RATE",
		"DEEPGRAM_VOICE_OUTPUT_ENCODING",
		"DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE",
		"DEEPGRAM_VOICE_LISTEN_MODEL",
		"DEEPGRAM_VOICE_SPEAK_MODEL",
		"DEEPSEEK_API_KEY",
		"DEEPSEEK_SECRET_ARN",
		"DEEPSEEK_BASE_URL",
		"DEEPSEEK_MODEL",
		"VOICE_WS_MAX_SESSION_SECONDS",
		"VOICE_WS_PING_INTERVAL_SECONDS",
		"VOICE_WS_WRITE_TIMEOUT_SECONDS",
		"VOICE_WS_MAX_FRAME_BYTES",
		"VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER",
		"VOICE_WS_EVENT_POLL_INTERVAL_MS",
		"VOICE_WS_EVENT_TTL_SECONDS",
		"VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES",
		"VOICE_WS_PROVIDER_READY_TIMEOUT_SECONDS",
		"MCP_SERVER_URL",
		"MCP_TIMEOUT",
		"MCP_INSECURE_SKIP_VERIFY",
		"MCP_TLS_CERT_FILE",
		"MCP_TLS_KEY_FILE",
		"MCP_TLS_CA_FILE",
	} {
		t.Setenv(key, "")
	}
}
